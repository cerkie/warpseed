package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"warpseed/internal/engine/sftpfast"
	"warpseed/internal/queue"
)

// scenarios are ordered so an early failure explains the ones after it:
// nothing transfers if connect fails, and nothing is worth verifying if a
// round trip does not come back byte for byte.
func (h *harness) scenarios() []scenario {
	return []scenario{
		{"connect", "dial the server, pin the host key, list the working directory", h.scConnect},
		{"round-trip-verify", "upload a generated file, pull it back, compare it byte for byte", h.scRoundTrip},
		{"pause-resume-download", "pause the queue mid-download, check the part file survives, resume, verify the result", h.scPauseResume},
		{"cancel-download", "cancel a download mid-flight and check nothing is left on this machine", h.scCancelDownload},
		{"cancel-upload", "cancel an upload mid-flight and check nothing is left on the server", h.scCancelUpload},
		{"bulk-cancel-uploads", "queue several uploads, cancel them all at once, check the server is clean", h.scBulkCancel},
		{"hyperlane-download", "download over several connections at once and verify the assembled file", h.scHyperlane},
	}
}

// ------------------------------------------------------------------ helpers

func (h *harness) remotePath(name string) string { return path.Join(h.remote, name) }
func (h *harness) localPath(name string) string  { return filepath.Join(h.localDir, name) }

// makeFile writes size bytes of random data and returns its digest. Random
// rather than zeros: a bug that writes holes or repeats a block still yields
// a file of the right length, and only content catches it.
func (h *harness) makeFile(p string, size int64) (string, error) {
	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(f, sum), rand.Reader, size); err != nil {
		return "", fmt.Errorf("generate %s: %w", p, err)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func digest(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func (h *harness) enqueue(direction, src, dst string, size int64) (int64, error) {
	return h.store.EnqueueTransfer(queue.Transfer{
		SiteID: h.site, Engine: "sftpfast",
		Direction: direction, Src: src, Dst: dst, Size: size,
	})
}

// settings applies transfer settings for one scenario. Each scenario sets
// what it needs rather than inheriting whatever ran before it.
func (h *harness) settings(kv map[string]string) error {
	for k, v := range kv {
		if err := h.store.SetSetting(k, v); err != nil {
			return fmt.Errorf("setting %s: %w", k, err)
		}
	}
	return nil
}

// midFlight throttles a transfer so it lasts about ten seconds, which is
// what lets the pause and cancel scenarios catch one actually moving. The
// limit is derived from the file size rather than fixed, so on a link
// already slower than that it is above the real rate and changes nothing:
// it slows a loopback run down to observable, and leaves a seedbox alone.
func (h *harness) midFlight() error {
	return h.settings(map[string]string{
		"bw.mode":        "fixed",
		"bw.limit_bytes": fmt.Sprintf("%d", h.size/10),
	})
}

// singleStream keeps a transfer on one connection, so scenarios about queue
// behaviour are not also about chunking.
func (h *harness) singleStream() error {
	return h.settings(map[string]string{
		"transfers.chunk_min_mb":        "1048576",
		"transfers.upload_chunk_min_mb": "1048576",
		"transfers.global_max":          "8",
		"transfers.site_max":            "8",
		"bw.mode":                       "off",
	})
}

func (h *harness) waitState(ctx context.Context, id int64, want string, timeout time.Duration) (queue.Transfer, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		t, err := h.store.TransferByID(id)
		if err != nil {
			return t, err
		}
		if t.State == want {
			return t, nil
		}
		if t.State == "failed" && want != "failed" {
			msg := ""
			if t.Error != nil {
				msg = *t.Error
			}
			return t, fmt.Errorf("transfer %d failed while waiting for %q: %s", id, want, msg)
		}
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	t, _ := h.store.TransferByID(id)
	return t, fmt.Errorf("timed out after %s waiting for transfer %d to reach %q; it is %q with %d bytes",
		timeout, id, want, t.State, t.BytesDone)
}

// waitProgress waits until a transfer is genuinely moving bytes, which is
// what makes "mid-flight" mean something in the pause and cancel scenarios.
func (h *harness) waitProgress(ctx context.Context, id, atLeast int64, timeout time.Duration) (queue.Transfer, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		t, err := h.store.TransferByID(id)
		if err != nil {
			return t, err
		}
		if t.BytesDone >= atLeast && t.State == "active" {
			return t, nil
		}
		if t.State == "failed" {
			msg := ""
			if t.Error != nil {
				msg = *t.Error
			}
			return t, fmt.Errorf("transfer %d failed before it moved %d bytes: %s", id, atLeast, msg)
		}
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	t, _ := h.store.TransferByID(id)
	return t, fmt.Errorf("timed out after %s: transfer %d reached only %d of the %d bytes needed to call it mid-flight (state %q)",
		timeout, id, t.BytesDone, atLeast, t.State)
}

// waitActive waits for a transfer to start, without waiting for byte
// progress to appear. Progress reaches the database on a checkpoint
// interval, so a small file can be finished before any lands — waiting on
// bytes would then wait forever for a row that is already done.
func (h *harness) waitActive(ctx context.Context, id int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		t, err := h.store.TransferByID(id)
		if err != nil {
			return err
		}
		switch t.State {
		case "active":
			return nil
		case "failed":
			msg := ""
			if t.Error != nil {
				msg = *t.Error
			}
			return fmt.Errorf("transfer %d failed before it started moving: %s", id, msg)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out after %s waiting for transfer %d to start", timeout, id)
}

// waitGone polls until the check passes, or the deadline passes.
//
// The discard that removes placeholders runs in the BACKGROUND after a row
// reaches its terminal state, and for an upload it may spend up to
// cleanupDialTimeout just connecting. A single check, or a fixed sleep short
// of that, would report data left behind that is merely slow — the worst
// possible lie from the one tool meant to be authoritative about it.
func waitGone(ctx context.Context, timeout time.Duration, check func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := check()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w (still true after %s)", err, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// cleanupGrace is comfortably past the engine's own 30s cleanup dial timeout.
const cleanupGrace = 90 * time.Second

func (h *harness) waitRemoteGone(ctx context.Context, paths []string, what string) error {
	c, err := h.oneClient(ctx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close()
	return waitGone(ctx, cleanupGrace, func() error {
		for _, p := range paths {
			if err := h.remoteGone(c, p, what); err != nil {
				return err
			}
		}
		return nil
	})
}

func waitLocalGone(ctx context.Context, paths []string, what string) error {
	return waitGone(ctx, cleanupGrace, func() error {
		for _, p := range paths {
			if err := localGone(p, what); err != nil {
				return err
			}
		}
		return nil
	})
}

// waitTerminal waits for a row to stop moving, whichever way it stopped.
func (h *harness) waitTerminal(ctx context.Context, id int64, timeout time.Duration) (queue.Transfer, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		t, err := h.store.TransferByID(id)
		if err != nil {
			return t, err
		}
		switch t.State {
		case "cancelled", "completed", "failed":
			return t, nil
		}
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	t, _ := h.store.TransferByID(id)
	return t, fmt.Errorf("timed out after %s: transfer %d is still %q", timeout, id, t.State)
}

// remoteGone fails unless the path is absent on the server.
func (h *harness) remoteGone(c *sftpfast.Client, p, what string) error {
	size, exists, err := c.StatRemoteSize(p)
	if err != nil {
		return fmt.Errorf("stat %s: %w", p, err)
	}
	if exists {
		return fmt.Errorf("%s still on the server: %s (%d bytes)", what, p, size)
	}
	return nil
}

func localGone(p, what string) error {
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("%s still on this machine: %s", what, p)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", p, err)
	}
	return nil
}

// tidy removes what a scenario made, unless -keep says otherwise.
func (h *harness) tidy(ctx context.Context, remotes, locals []string) {
	if *flagKeep {
		h.log.line("      -keep: leaving %d remote and %d local file(s) behind", len(remotes), len(locals))
		return
	}
	for _, p := range locals {
		_ = os.Remove(p)
	}
	if len(remotes) == 0 {
		return
	}
	c, err := h.oneClient(ctx)
	if err != nil {
		h.log.line("      could not connect to tidy up: %v", err)
		return
	}
	defer c.Close()
	for _, p := range remotes {
		_ = c.RemoveRemote(p)
	}
}

// suffixed lists both placeholder spellings for a destination.
func suffixed(p string) []string {
	return []string{p + sftpfast.PartSuffix, p + sftpfast.ChunkPartSuffix}
}

// ---------------------------------------------------------------- scenarios

func (h *harness) scConnect(ctx context.Context) error {
	c, err := h.oneClient(ctx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close()
	if home, err := c.Home(); err != nil {
		h.log.line("      (no home reported: %v)", err)
	} else {
		h.log.line("      home %s", home)
	}
	listing, err := c.List(h.remote)
	if err != nil {
		return fmt.Errorf("list %s — does it exist and is it writable? %w", h.remote, err)
	}
	h.log.line("      %s holds %d entries", h.remote, len(listing.Entries))
	return nil
}

func (h *harness) scRoundTrip(ctx context.Context) error {
	if err := h.singleStream(); err != nil {
		return err
	}
	src := h.localPath("roundtrip.bin")
	remote := h.remotePath("roundtrip.bin")
	back := h.localPath("roundtrip.back.bin")
	defer h.tidy(ctx, append([]string{remote}, suffixed(remote)...),
		append([]string{src, back}, suffixed(back)...))

	want, err := h.makeFile(src, h.size)
	if err != nil {
		return err
	}
	h.log.line("      generated %d MiB, sha256 %s…", h.size>>20, want[:16])

	up, err := h.enqueue("upload", src, remote, h.size)
	if err != nil {
		return err
	}
	h.disp.Wake()
	started := time.Now()
	if _, err := h.waitState(ctx, up, "completed", 30*time.Minute); err != nil {
		return err
	}
	h.log.line("      uploaded in %s (%s)", time.Since(started).Round(time.Millisecond), rate(h.size, time.Since(started)))

	if err := h.withClient(ctx, func(c *sftpfast.Client) error {
		for _, p := range suffixed(remote) {
			if err := h.remoteGone(c, p, "a placeholder from a COMPLETED upload"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	down, err := h.enqueue("download", remote, back, h.size)
	if err != nil {
		return err
	}
	h.disp.Wake()
	started = time.Now()
	if _, err := h.waitState(ctx, down, "completed", 30*time.Minute); err != nil {
		return err
	}
	h.log.line("      downloaded in %s (%s)", time.Since(started).Round(time.Millisecond), rate(h.size, time.Since(started)))

	got, err := digest(back)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("ROUND TRIP CORRUPTED the file: sent %s, got back %s", want, got)
	}
	h.log.line("      sha256 matches after a full round trip")
	return localGone(back+sftpfast.PartSuffix, "a placeholder from a completed download")
}

func (h *harness) scPauseResume(ctx context.Context) error {
	if err := h.singleStream(); err != nil {
		return err
	}
	src := h.localPath("pause.bin")
	remote := h.remotePath("pause.bin")
	back := h.localPath("pause.back.bin")
	defer h.tidy(ctx, append([]string{remote}, suffixed(remote)...),
		append([]string{src, back}, suffixed(back)...))

	want, err := h.seedRemote(ctx, src, remote)
	if err != nil {
		return err
	}

	if err := h.midFlight(); err != nil {
		return err
	}
	id, err := h.enqueue("download", remote, back, h.size)
	if err != nil {
		return err
	}
	h.disp.Wake()
	if _, err := h.waitProgress(ctx, id, h.size/16, 5*time.Minute); err != nil {
		return err
	}

	if err := h.disp.SetPaused(true); err != nil {
		return err
	}
	// Resumed whatever happens below. Without this, the very failure this
	// scenario exists to catch would leave the queue paused, and every later
	// scenario would sit on its own timeout reporting nonsense.
	defer func() { _ = h.disp.SetPaused(false) }()
	paused, err := h.waitState(ctx, id, "pending", 2*time.Minute)
	if err != nil {
		return fmt.Errorf("after Pause queue: %w", err)
	}
	if paused.Error != nil {
		return fmt.Errorf("pausing recorded an error on the transfer: %s", *paused.Error)
	}
	if paused.Attempt != 0 {
		return fmt.Errorf("pausing spent a retry attempt (attempt=%d); a pause is not a failure", paused.Attempt)
	}
	st, err := os.Stat(back + sftpfast.PartSuffix)
	if err != nil {
		return fmt.Errorf("pause did not keep the part file a resume needs: %w", err)
	}
	h.log.line("      paused at %d MiB, part file holds %d MiB, attempt still 0",
		paused.BytesDone>>20, st.Size()>>20)

	if err := h.disp.SetPaused(false); err != nil {
		return err
	}
	h.disp.Wake()
	if _, err := h.waitState(ctx, id, "completed", 30*time.Minute); err != nil {
		return fmt.Errorf("after Resume queue: %w", err)
	}
	got, err := digest(back)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("RESUMED FILE IS CORRUPT: expected %s, got %s", want, got)
	}
	h.log.line("      resumed and finished; sha256 matches the original")
	return nil
}

func (h *harness) scCancelDownload(ctx context.Context) error {
	if err := h.singleStream(); err != nil {
		return err
	}
	src := h.localPath("canceldown.bin")
	remote := h.remotePath("canceldown.bin")
	dst := h.localPath("canceldown.out.bin")
	defer h.tidy(ctx, append([]string{remote}, suffixed(remote)...), append([]string{dst}, suffixed(dst)...))

	if _, err := h.seedRemote(ctx, src, remote); err != nil {
		return err
	}
	if err := h.midFlight(); err != nil {
		return err
	}
	id, err := h.enqueue("download", remote, dst, h.size)
	if err != nil {
		return err
	}
	h.disp.Wake()
	mid, err := h.waitProgress(ctx, id, h.size/16, 5*time.Minute)
	if err != nil {
		return err
	}
	h.log.line("      cancelling with %d MiB already written", mid.BytesDone>>20)

	if err := h.disp.Cancel(id); err != nil {
		return err
	}
	if _, err := h.waitState(ctx, id, "cancelled", 2*time.Minute); err != nil {
		return err
	}
	if err := waitLocalGone(ctx, append(suffixed(dst), dst),
		"a file from a CANCELLED download"); err != nil {
		return err
	}
	h.log.line("      nothing left on this machine")
	return nil
}

func (h *harness) scCancelUpload(ctx context.Context) error {
	if err := h.singleStream(); err != nil {
		return err
	}
	src := h.localPath("cancelup.bin")
	remote := h.remotePath("cancelup.bin")
	defer h.tidy(ctx, append([]string{remote}, suffixed(remote)...), []string{src})

	if _, err := h.makeFile(src, h.size); err != nil {
		return err
	}
	if err := h.midFlight(); err != nil {
		return err
	}
	id, err := h.enqueue("upload", src, remote, h.size)
	if err != nil {
		return err
	}
	h.disp.Wake()
	mid, err := h.waitProgress(ctx, id, h.size/16, 5*time.Minute)
	if err != nil {
		return err
	}
	h.log.line("      cancelling with %d MiB already sent", mid.BytesDone>>20)

	if err := h.disp.Cancel(id); err != nil {
		return err
	}
	if _, err := h.waitState(ctx, id, "cancelled", 2*time.Minute); err != nil {
		return err
	}
	// Cancelling an upload has to dial to reach the server's copy, so this
	// one genuinely takes a while before the server is clean.
	if err := h.waitRemoteGone(ctx, append(suffixed(remote), remote),
		"a file from a CANCELLED upload"); err != nil {
		return err
	}
	h.log.line("      nothing left on the server")
	return nil
}

func (h *harness) scBulkCancel(ctx context.Context) error {
	// Several smaller files, one at a time: this is about the sweep reaching
	// every row and sharing a connection, not about throughput.
	if err := h.singleStream(); err != nil {
		return err
	}
	if err := h.settings(map[string]string{"transfers.global_max": "1", "transfers.site_max": "1"}); err != nil {
		return err
	}
	// Paused for the whole setup. Run pumps on its own two-second ticker, not
	// only when woken, so without this a row can be claimed and finished
	// UNTHROTTLED while the remaining files are still being generated — and
	// then there is nothing in flight left to cancel.
	if err := h.disp.SetPaused(true); err != nil {
		return err
	}
	defer func() { _ = h.disp.SetPaused(false) }()

	const n = 6
	var dsts, remotes, locals []string
	var ids []int64
	each := h.size / 4
	for i := 0; i < n; i++ {
		src := h.localPath(fmt.Sprintf("bulk%d.bin", i))
		remote := h.remotePath(fmt.Sprintf("bulk%d.bin", i))
		if _, err := h.makeFile(src, each); err != nil {
			return err
		}
		id, err := h.enqueue("upload", src, remote, each)
		if err != nil {
			return err
		}
		ids = append(ids, id)
		locals = append(locals, src)
		dsts = append(dsts, remote)
		remotes = append(remotes, remote)
		remotes = append(remotes, suffixed(remote)...)
	}
	defer h.tidy(ctx, remotes, locals)

	if err := h.midFlight(); err != nil {
		return err
	}
	if err := h.disp.SetPaused(false); err != nil {
		return err
	}
	h.disp.Wake()
	// Let the first one get going, so the batch mixes a running row with
	// queued ones — the case the confirmation wording promises. Waiting on
	// its STATE rather than its bytes: a byte count reaches the database on a
	// checkpoint interval, and a small file can finish before one lands.
	if err := h.waitActive(ctx, ids[0], 2*time.Minute); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond) // let it put bytes on the wire

	before := h.dials.Load()
	cancelled, err := h.disp.CancelQueued()
	if err != nil {
		return err
	}
	h.log.line("      Cancel all queued took %d of the %d rows; the running one keeps going", cancelled, n)
	if _, err := h.disp.CancelMany([]int64{ids[0]}); err != nil {
		return err
	}

	// A row that finished before the cancel reached it is not a failure, but
	// its destination is then MEANT to exist — so say so, rather than
	// asserting it is gone and reporting a phantom.
	var mustBeGone []string
	stopped := 0
	for i, id := range ids {
		t, err := h.waitTerminal(ctx, id, 2*time.Minute)
		if err != nil {
			return err
		}
		switch t.State {
		case "cancelled":
			stopped++
			mustBeGone = append(mustBeGone, dsts[i])
			mustBeGone = append(mustBeGone, suffixed(dsts[i])...)
		case "completed":
			h.log.line("      row %d finished before the cancel reached it; only its placeholders are checked", id)
			mustBeGone = append(mustBeGone, suffixed(dsts[i])...)
		default:
			return fmt.Errorf("row %d ended as %q, expected cancelled", id, t.State)
		}
	}
	if stopped == 0 {
		return fmt.Errorf("every row finished before the cancel reached it; nothing was actually cancelled")
	}
	if err := h.waitRemoteGone(ctx, mustBeGone, "a file from a bulk-cancelled upload"); err != nil {
		return err
	}
	// One connection for the sweep, plus the one the check above opened.
	h.log.line("      server clean; cancelling %d rows cost %d connection(s)", stopped, h.dials.Load()-before-1)
	return nil
}

func (h *harness) scHyperlane(ctx context.Context) error {
	if err := h.settings(map[string]string{
		"transfers.chunk_min_mb":  "1",
		"transfers.chunk_streams": "4",
		"transfers.global_max":    "8",
		"transfers.site_max":      "8",
		"bw.mode":                 "off",
	}); err != nil {
		return err
	}
	src := h.localPath("hyper.bin")
	remote := h.remotePath("hyper.bin")
	back := h.localPath("hyper.back.bin")
	defer h.tidy(ctx, append([]string{remote}, suffixed(remote)...),
		append([]string{src, back}, suffixed(back)...))

	want, err := h.seedRemote(ctx, src, remote)
	if err != nil {
		return err
	}
	id, err := h.enqueue("download", remote, back, h.size)
	if err != nil {
		return err
	}
	h.disp.Wake()
	started := time.Now()
	if _, err := h.waitState(ctx, id, "completed", 30*time.Minute); err != nil {
		return err
	}
	took := time.Since(started)
	got, err := digest(back)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("HYPERLANE ASSEMBLED A CORRUPT FILE: expected %s, got %s", want, got)
	}
	h.log.line("      %s over several connections, sha256 matches", rate(h.size, took))
	return localGone(back+sftpfast.ChunkPartSuffix, "a chunk placeholder from a completed download")
}

// ------------------------------------------------------------------ support

// seedRemote puts a known file on the server for the download scenarios to
// pull, and returns its digest. It goes through the dispatcher so a failure
// here is reported as this scenario's own rather than as a mystery later.
func (h *harness) seedRemote(ctx context.Context, src, remote string) (string, error) {
	want, err := h.makeFile(src, h.size)
	if err != nil {
		return "", err
	}
	id, err := h.enqueue("upload", src, remote, h.size)
	if err != nil {
		return "", err
	}
	h.disp.Wake()
	if _, err := h.waitState(ctx, id, "completed", 30*time.Minute); err != nil {
		return "", fmt.Errorf("seeding the server: %w", err)
	}
	return want, nil
}

func (h *harness) withClient(ctx context.Context, fn func(*sftpfast.Client) error) error {
	c, err := h.oneClient(ctx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close()
	return fn(c)
}

func rate(n int64, d time.Duration) string {
	if d <= 0 {
		return "instant"
	}
	return fmt.Sprintf("%.1f MiB/s", float64(n)/(1<<20)/d.Seconds())
}
