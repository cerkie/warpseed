package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

/* Installing an update from inside the app.

   The release is fetched from the repository the user chose, over https, and
   only if GitHub's own SHA-256 for the file matches what arrived. Nothing is
   installed from a release that lacks that checksum; the banner then just opens
   the release page.

   warpseed cannot overwrite the exe it is running from, so the last step is
   handed to a copy of itself (see update_helper_windows.go): it waits for this
   process to exit, installs, and starts the new version. An installed copy runs
   the release's installer silently into the folder it lives in. A portable exe
   is swapped in place. */

const (
	installerAsset = "warpseed-amd64-installer.exe"
	portableAsset  = "warpseed.exe"
	// The release files are about 20 MB; this only stops a runaway download.
	maxUpdateBytes = 200 << 20
)

var digestRe = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)

// detectInstallKind says whether exe is a copy the installer put there (it has
// the installer's uninstall.exe beside it) or a loose portable exe.
func detectInstallKind(exe string) string {
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "uninstall.exe")); err == nil {
		return "installer"
	}
	return "portable"
}

func pickAsset(assets []releaseAsset, kind string) *releaseAsset {
	want := portableAsset
	if kind == "installer" {
		want = installerAsset
	}
	for i := range assets {
		if assets[i].Name == want {
			return &assets[i]
		}
	}
	return nil
}

// trustedAssetURL accepts only an https download from the chosen repository's
// own releases.
func trustedAssetURL(raw, repo string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" {
		return false
	}
	return strings.HasPrefix(u.Path, "/"+repo+"/releases/download/")
}

// installPlan says how this copy of warpseed would update itself to rel, and
// whether it can: it needs the matching file in the release, with a checksum.
func (a *App) installPlan(rel latestRelease) (kind string, can bool) {
	if runtime.GOOS != "windows" {
		return "", false
	}
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	kind = detectInstallKind(exe)
	asset := pickAsset(rel.Assets, kind)
	can = asset != nil && digestRe.MatchString(asset.Digest) && trustedAssetURL(asset.URL, a.updateRepo()) &&
		(asset.Size == 0 || asset.Size <= maxUpdateBytes)
	return kind, can
}

// updateClient follows redirects (GitHub serves release files from its own
// storage hosts) but only to https on GitHub's domains.
func updateClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return errors.New("too many redirects")
			}
			h := req.URL.Hostname()
			if req.URL.Scheme != "https" || !(h == "github.com" || strings.HasSuffix(h, ".github.com") || strings.HasSuffix(h, ".githubusercontent.com")) {
				return fmt.Errorf("refusing to follow a redirect to %s", req.URL.Host)
			}
			return nil
		},
	}
}

// downloadVerified fetches the asset into dest, checking its size and SHA-256.
// dest only exists afterwards if everything matched.
func downloadVerified(ctx context.Context, client *http.Client, asset releaseAsset, dest string, progress func(done, total int64)) error {
	if !digestRe.MatchString(asset.Digest) {
		return errors.New("the release has no checksum to verify the download against")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	total := asset.Size
	if total == 0 {
		total = resp.ContentLength
	}
	part := dest + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	defer os.Remove(part)
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var done int64
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > maxUpdateBytes || (asset.Size > 0 && done > asset.Size) {
				f.Close()
				return errors.New("the download is larger than the release says")
			}
			h.Write(buf[:n])
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			if progress != nil && time.Since(last) > 100*time.Millisecond {
				progress(done, total)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if progress != nil {
		progress(done, total)
	}
	if asset.Size > 0 && done != asset.Size {
		return fmt.Errorf("the download stopped early (%d of %d bytes)", done, asset.Size)
	}
	want := strings.ToLower(strings.TrimPrefix(asset.Digest, "sha256:"))
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return errors.New("the download does not match its checksum, so it was discarded")
	}
	return os.Rename(part, dest)
}

// swapExecutable replaces target with next, keeping the old one beside it as
// target+".old" (a running exe can be renamed on Windows, just not deleted or
// overwritten). It puts everything back if the second step fails.
func swapExecutable(target, next string) error {
	old := target + ".old"
	_ = os.Remove(old)
	if err := os.Rename(target, old); err != nil {
		return err
	}
	if err := os.Rename(next, target); err != nil {
		_ = os.Rename(old, target)
		return err
	}
	return nil
}

// updateState tells the page how an install is going.
type updateState struct {
	Phase string `json:"phase"` // downloading | installing | failed
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
	Error string `json:"error,omitempty"`
}

var updating atomic.Bool

// InstallUpdate downloads the latest release, verifies it, and restarts into
// it. It returns at once; progress arrives as update:state events. The page
// asks the user first when transfers are running.
func (a *App) InstallUpdate() error {
	if runtime.GOOS != "windows" {
		return errors.New("installing updates is only available on Windows")
	}
	if !updating.CompareAndSwap(false, true) {
		return errors.New("an update is already being installed")
	}
	go func() {
		defer updating.Store(false)
		if err := a.runInstall(); err != nil {
			a.sink.Emit("update:state", updateState{Phase: "failed", Error: err.Error()})
		}
	}()
	return nil
}

func (a *App) runInstall() error {
	rel, err := a.fetchLatestRelease(a.ctx)
	if err != nil {
		return err
	}
	if !newerVersion(rel.Version, appVersion) {
		return errors.New("you already have the latest version")
	}
	kind, can := a.installPlan(rel)
	if !can {
		return errors.New("this release cannot be installed from here; use the release page")
	}
	asset := pickAsset(rel.Assets, kind)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}

	// An installer is downloaded to a temp folder. A portable exe goes beside
	// the one it replaces, so the swap is a rename inside one folder.
	var dest string
	if kind == "installer" {
		dir, err := os.MkdirTemp("", "warpseed-update")
		if err != nil {
			return err
		}
		dest = filepath.Join(dir, installerAsset)
	} else {
		dest = exe + ".new"
		probe, err := os.Create(dest)
		if err != nil {
			return fmt.Errorf("warpseed cannot write to %s, so it cannot replace itself there; use the release page", filepath.Dir(exe))
		}
		probe.Close()
		_ = os.Remove(dest)
	}

	a.sink.Emit("update:state", updateState{Phase: "downloading", Total: asset.Size})
	err = downloadVerified(a.ctx, updateClient(), *asset, dest, func(done, total int64) {
		a.sink.Emit("update:state", updateState{Phase: "downloading", Done: done, Total: total})
	})
	if err != nil {
		return err
	}

	a.sink.Emit("update:state", updateState{Phase: "installing"})
	if err := startUpdateHelper(kind, dest, exe); err != nil {
		return err
	}
	// The helper waits for this process to end, then installs and restarts.
	a.quitting.Store(true)
	wruntime.Quit(a.ctx)
	return nil
}

// updateResultPath is where the helper leaves a note on how the install went.
func updateResultPath() string { return filepath.Join(os.TempDir(), "warpseed-update-result.txt") }

// afterUpdate runs at startup: it tidies up after an update and reports one
// that failed. The helper always restarts warpseed, so a failed install leaves
// the old version running, with this explanation.
func (a *App) afterUpdate() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
		_ = os.Remove(exe + ".new")
	}
	b, err := os.ReadFile(updateResultPath())
	if err != nil {
		return
	}
	_ = os.Remove(updateResultPath())
	if msg := strings.TrimSpace(string(b)); msg != "ok" && msg != "" {
		a.sink.Emit("app:error", "The update did not install: "+msg)
	}
}
