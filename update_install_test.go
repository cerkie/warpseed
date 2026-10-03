package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func digestOf(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

func TestTrustedAssetURL(t *testing.T) {
	const repo = "cerkie/warpseed"
	for url, want := range map[string]bool{
		"https://github.com/cerkie/warpseed/releases/download/v1.3.1/warpseed.exe": true,
		"http://github.com/cerkie/warpseed/releases/download/v1.3.1/warpseed.exe":  false, // not https
		"https://evil.example/cerkie/warpseed/releases/download/v1/warpseed.exe":   false, // not github
		"https://github.com/other/warpseed/releases/download/v1/warpseed.exe":      false, // not the chosen repo
		"https://github.com/cerkie/warpseed/archive/v1.zip":                        false, // not a release file
		"https://github.com.evil.example/cerkie/warpseed/releases/download/v1/x":   false,
	} {
		if got := trustedAssetURL(url, repo); got != want {
			t.Errorf("trustedAssetURL(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestPickAssetAndKind(t *testing.T) {
	assets := []releaseAsset{{Name: "warpseed.exe"}, {Name: "warpseed-amd64-installer.exe"}, {Name: "notes.txt"}}
	if a := pickAsset(assets, "portable"); a == nil || a.Name != "warpseed.exe" {
		t.Fatalf("portable: %+v", a)
	}
	if a := pickAsset(assets, "installer"); a == nil || a.Name != "warpseed-amd64-installer.exe" {
		t.Fatalf("installer: %+v", a)
	}
	if pickAsset(assets[2:], "portable") != nil {
		t.Fatal("picked an asset that is not there")
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, "warpseed.exe")
	if detectInstallKind(exe) != "portable" {
		t.Fatal("a folder with no uninstaller is a portable copy")
	}
	if err := os.WriteFile(filepath.Join(dir, "uninstall.exe"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if detectInstallKind(exe) != "installer" {
		t.Fatal("a folder with the uninstaller is an installed copy")
	}
}

func TestDownloadVerified(t *testing.T) {
	body := []byte(strings.Repeat("warpseed release ", 5000))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	ctx := context.Background()

	t.Run("a matching file is kept", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "w.exe")
		var last int64
		err := downloadVerified(ctx, srv.Client(), releaseAsset{URL: srv.URL, Size: int64(len(body)), Digest: digestOf(body)}, dest,
			func(done, total int64) { last = done })
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(dest)
		if string(got) != string(body) || last != int64(len(body)) {
			t.Fatalf("content or progress wrong (progress %d)", last)
		}
		if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
			t.Fatal("the part file was left behind")
		}
	})

	for name, asset := range map[string]releaseAsset{
		"a wrong checksum":     {URL: srv.URL, Size: int64(len(body)), Digest: digestOf([]byte("something else"))},
		"a missing checksum":   {URL: srv.URL, Size: int64(len(body))},
		"a smaller size":       {URL: srv.URL, Size: int64(len(body)) - 10, Digest: digestOf(body)},
		"a larger size":        {URL: srv.URL, Size: int64(len(body)) + 10, Digest: digestOf(body)},
		"a missing file (404)": {URL: srv.URL + "/missing", Size: 10, Digest: digestOf(body)},
	} {
		t.Run(name+" is refused and leaves nothing", func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "w.exe")
			if err := downloadVerified(ctx, srv.Client(), asset, dest, nil); err == nil {
				t.Fatal("expected an error")
			}
			for _, p := range []string{dest, dest + ".part"} {
				if _, err := os.Stat(p); !os.IsNotExist(err) {
					t.Fatalf("%s was left behind", p)
				}
			}
		})
	}
}

func TestSwapExecutable(t *testing.T) {
	dir := t.TempDir()
	target, next := filepath.Join(dir, "warpseed.exe"), filepath.Join(dir, "warpseed.exe.new")
	_ = os.WriteFile(target, []byte("old"), 0o700)
	_ = os.WriteFile(next, []byte("new"), 0o700)

	if err := swapExecutable(target, next); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Fatalf("target = %q", b)
	}
	if b, _ := os.ReadFile(target + ".old"); string(b) != "old" {
		t.Fatalf("old = %q", b)
	}

	// If the new file is not there, the old one must be put back.
	if err := swapExecutable(target, filepath.Join(dir, "nope")); err == nil {
		t.Fatal("expected an error")
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Fatalf("the running exe was lost: %q", b)
	}
}

// TestDownloadRealRelease fetches the published installer through the real
// client, redirects and all. It needs the network, so it only runs on request:
// WS_LIVE_TEST=1 go test -run RealRelease .
func TestDownloadRealRelease(t *testing.T) {
	if os.Getenv("WS_LIVE_TEST") != "1" {
		t.Skip("set WS_LIVE_TEST=1 to download from GitHub")
	}
	a := &App{}
	rel, err := a.fetchLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	asset := pickAsset(rel.Assets, "installer")
	if asset == nil || !digestRe.MatchString(asset.Digest) || !trustedAssetURL(asset.URL, forkRepo) {
		t.Fatalf("release %s has no installable installer: %+v", rel.Version, asset)
	}
	dest := filepath.Join(t.TempDir(), installerAsset)
	if err := downloadVerified(context.Background(), updateClient(), *asset, dest, nil); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(dest); err != nil || st.Size() != asset.Size {
		t.Fatalf("downloaded file wrong: %v %v", st, err)
	}
}
