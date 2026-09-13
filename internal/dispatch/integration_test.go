package dispatch

// Integration tests: the dispatcher driven against a REAL SFTP server.
//
// Every other test in this package passes a nil connection factory, which
// makes dialing a hard failure and keeps those tests fast and hermetic. The
// cost is that the half of cancel that reaches across the network — removing
// an upload's placeholders from the server — had never run anywhere: not in
// a test, not on a developer machine that cannot reach a seedbox. These
// close that gap by running pkg/sftp's own server on a loopback socket and
// serving it the OS filesystem, so a "remote" path is just a temp directory
// and a connection is a real one that can be closed.

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"warpseed/internal/engine/sftpfast"
	"warpseed/internal/queue"
)

// newLocalSFTPClient runs a real pkg/sftp server on a loopback socket and
// returns a client speaking to it. The server serves the OS filesystem, so
// callers address it with ordinary temp-dir paths.
//
// A SOCKET, not the os.Pipe pair the engine's own harness uses, because the
// dispatcher closes every connection when a transfer ends and sftp's Close
// waits for the transport to report EOF. Over bare pipes closing the write
// end tells the read end nothing, so that Close never returns and the first
// completed transfer hangs forever. The engine's tests never hit it because
// they never close a client. Loopback also makes this a more honest
// rehearsal of production, which is a socket too.
func newLocalSFTPClient(t *testing.T) *sftpfast.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return // listener closed by cleanup
		}
		srv, serr := sftp.NewServer(conn)
		if serr != nil {
			conn.Close()
			return
		}
		_ = srv.Serve() // returns when the client hangs up
		srv.Close()
		conn.Close()
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	sc, err := sftp.NewClientPipe(conn, conn,
		sftp.UseConcurrentReads(true),
		sftp.MaxConcurrentRequestsPerFile(16),
	)
	if err != nil {
		t.Fatalf("sftp client: %v", err)
	}
	t.Cleanup(func() {
		conn.Close()
		ln.Close()
	})
	return sftpfast.NewFromSFTP(sc)
}

// liveDispatcher wires a dispatcher to real SFTP connections and reports how
// many times it dialed, which is how the batch-cancel tests prove one
// connection is shared across a folder rather than opened per row.
func liveDispatcher(t *testing.T) (*Dispatcher, *queue.Store, int64, *int32) {
	t.Helper()
	store, site := openStoreWithSite(t)
	var dials int32
	d := New(store, nopSink{}, func(_ context.Context, _ int64, n int) ([]*sftpfast.Client, error) {
		atomic.AddInt32(&dials, 1)
		cs := make([]*sftpfast.Client, 0, n)
		for i := 0; i < n; i++ {
			cs = append(cs, newLocalSFTPClient(t))
		}
		return cs, nil
	})
	// Small files, so nothing crosses the chunking threshold and each
	// transfer takes a single connection unless a test says otherwise.
	set(t, store, "transfers.chunk_min_mb", "4096")
	set(t, store, "transfers.upload_chunk_min_mb", "4096")
	return d, store, site, &dials
}

// waitFor polls until cond holds, failing the test rather than hanging.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func stateOf(t *testing.T, s *queue.Store, id int64) string {
	t.Helper()
	got, err := s.TransferByID(id)
	if err != nil {
		t.Fatalf("read transfer %d: %v", id, err)
	}
	return got.State
}

func TestLiveUploadThroughTheDispatcher(t *testing.T) {
	// Arrange — a real file, pumped through the dispatcher onto a real
	// server. The whole claim/run/complete path, which no other test in this
	// package exercises end to end.
	d, store, site, _ := liveDispatcher(t)
	local := filepath.Join(t.TempDir(), "movie.mkv")
	remote := filepath.Join(t.TempDir(), "movie.mkv")
	want := make([]byte, 512<<10)
	for i := range want {
		want[i] = byte(i * 7)
	}
	if err := os.WriteFile(local, want, 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := store.EnqueueTransfer(queue.Transfer{
		SiteID: site, Engine: "sftpfast", Direction: "upload",
		Src: local, Dst: remote, Size: int64(len(want)),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Act
	d.pump(context.Background())
	d.running.Wait()
	d.sweeps.Wait()

	// Assert — the bytes landed, byte for byte, and the row says so.
	if got := stateOf(t, store, id); got != "completed" {
		t.Fatalf("row is %q, want completed", got)
	}
	landed, err := os.ReadFile(remote)
	if err != nil {
		t.Fatalf("uploaded file: %v", err)
	}
	if len(landed) != len(want) {
		t.Fatalf("uploaded %d bytes, want %d", len(landed), len(want))
	}
	for i := range want {
		if landed[i] != want[i] {
			t.Fatalf("uploaded bytes differ at offset %d", i)
		}
	}
	// The placeholder must not survive a success.
	if _, err := os.Stat(remote + sftpfast.PartSuffix); !os.IsNotExist(err) {
		t.Errorf("a completed upload left its placeholder behind: %v", err)
	}
}

func TestLiveDownloadThroughTheDispatcher(t *testing.T) {
	// Arrange
	d, store, site, _ := liveDispatcher(t)
	remote := filepath.Join(t.TempDir(), "iso.img")
	local := filepath.Join(t.TempDir(), "iso.img")
	want := make([]byte, 384<<10)
	for i := range want {
		want[i] = byte(i % 251)
	}
	if err := os.WriteFile(remote, want, 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := store.EnqueueTransfer(queue.Transfer{
		SiteID: site, Engine: "sftpfast", Direction: "download",
		Src: remote, Dst: local, Size: int64(len(want)),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Act
	d.pump(context.Background())
	d.running.Wait()
	d.sweeps.Wait()

	// Assert
	if got := stateOf(t, store, id); got != "completed" {
		t.Fatalf("row is %q, want completed", got)
	}
	landed, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("downloaded file: %v", err)
	}
	if len(landed) != len(want) {
		t.Fatalf("downloaded %d bytes, want %d", len(landed), len(want))
	}
	if _, err := os.Stat(local + sftpfast.PartSuffix); !os.IsNotExist(err) {
		t.Errorf("a completed download left its placeholder behind: %v", err)
	}
}

func TestLiveBatchCancelRemovesRemotePlaceholders(t *testing.T) {
	// Arrange — the path this file exists for. Several part-uploaded rows
	// with real .wspart and .wschunk files sitting on the server, cancelled
	// in one batch. Until now the remote half of the discard had never run:
	// every other test either disables dialing or makes it fail.
	d, store, site, dials := liveDispatcher(t)
	dir := t.TempDir()
	ids := make([]int64, 0, 5)
	parts := make([]string, 0, 10)
	for i := 0; i < 5; i++ {
		dst := filepath.Join(dir, "ep"+string(rune('0'+i))+".mkv")
		for _, suffix := range []string{sftpfast.PartSuffix, sftpfast.ChunkPartSuffix} {
			p := dst + suffix
			if err := os.WriteFile(p, []byte("half a file"), 0o644); err != nil {
				t.Fatal(err)
			}
			parts = append(parts, p)
		}
		id, err := store.EnqueueTransfer(queue.Transfer{
			SiteID: site, Engine: "sftpfast", Direction: "upload",
			Src: filepath.Join(dir, "src"), Dst: dst, Size: 1 << 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateTransferProgress(id, 11); err != nil {
			t.Fatal(err)
		}
		if err := store.SetTransferState(id, "paused", nil); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// Act
	n, err := d.CancelMany(ids)
	if err != nil {
		t.Fatal(err)
	}
	d.sweeps.Wait()

	// Assert — every placeholder actually gone from the server.
	if n != len(ids) {
		t.Fatalf("cancelled %d rows, want %d", n, len(ids))
	}
	for _, p := range parts {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("placeholder %s survived the cancel: %v", filepath.Base(p), err)
		}
	}
	for _, id := range ids {
		got, _ := store.TransferByID(id)
		if got.State != "cancelled" || got.BytesDone != 0 {
			t.Errorf("row %d = %q with %d bytes, want cancelled/0", id, got.State, got.BytesDone)
		}
	}
	// One connection for the whole folder, not one per row.
	if got := atomic.LoadInt32(dials); got != 1 {
		t.Errorf("dialed %d times to clean up %d rows, want 1", got, len(ids))
	}
}

func TestLiveCancelOfARunningUploadRemovesItsPlaceholder(t *testing.T) {
	// Arrange — a transfer actually in flight, cancelled mid-copy. The
	// engine has to let go, and the placeholder it was writing has to be
	// removed from the server afterwards.
	d, store, site, _ := liveDispatcher(t)
	local := filepath.Join(t.TempDir(), "big.bin")
	remote := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(local, make([]byte, 8<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	// Throttled so the copy is still running when the cancel lands.
	set(t, store, "bw.mode", "fixed")
	set(t, store, "bw.limit_bytes", "524288")
	id, err := store.EnqueueTransfer(queue.Transfer{
		SiteID: site, Engine: "sftpfast", Direction: "upload",
		Src: local, Dst: remote, Size: 8 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	d.refreshLimiter()
	d.pump(context.Background())
	waitFor(t, "the upload to start moving bytes", func() bool {
		got, _ := store.TransferByID(id)
		return got.State == "active" && got.BytesDone > 0
	})

	// Act
	if err := d.Cancel(id); err != nil {
		t.Fatal(err)
	}
	d.running.Wait()
	d.sweeps.Wait()

	// Assert
	if got := stateOf(t, store, id); got != "cancelled" {
		t.Fatalf("row is %q, want cancelled", got)
	}
	for _, suffix := range []string{sftpfast.PartSuffix, sftpfast.ChunkPartSuffix} {
		if _, err := os.Stat(remote + suffix); !os.IsNotExist(err) {
			t.Errorf("cancelling a running upload left %s on the server: %v", suffix, err)
		}
	}
	if _, err := os.Stat(remote); !os.IsNotExist(err) {
		t.Errorf("a cancelled upload published its destination file")
	}
}

func TestLivePauseRequeuesARunningTransferCleanly(t *testing.T) {
	// Arrange — the claim the 1.1.10 release notes make, against a real
	// engine: pausing is not a failure, it keeps the bytes, and the row
	// comes back as a clean pending rather than on the retry ladder.
	d, store, site, _ := liveDispatcher(t)
	remote := filepath.Join(t.TempDir(), "season.tar")
	local := filepath.Join(t.TempDir(), "season.tar")
	if err := os.WriteFile(remote, make([]byte, 8<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	set(t, store, "bw.mode", "fixed")
	set(t, store, "bw.limit_bytes", "524288")
	id, err := store.EnqueueTransfer(queue.Transfer{
		SiteID: site, Engine: "sftpfast", Direction: "download",
		Src: remote, Dst: local, Size: 8 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	d.refreshLimiter()
	d.pump(context.Background())
	waitFor(t, "the download to start moving bytes", func() bool {
		got, _ := store.TransferByID(id)
		return got.State == "active" && got.BytesDone > 0
	})

	// Act
	if err := d.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	d.running.Wait()
	d.sweeps.Wait()

	// Assert — pending and clean, with the part file and its bytes kept.
	got, _ := store.TransferByID(id)
	if got.State != "pending" || got.Error != nil || got.Attempt != 0 {
		t.Fatalf("paused row = %q err %v attempt %d; want a clean pending", got.State, got.Error, got.Attempt)
	}
	if got.BytesDone <= 0 {
		t.Fatalf("pause discarded the progress: %d bytes", got.BytesDone)
	}
	st, err := os.Stat(local + sftpfast.PartSuffix)
	if err != nil {
		t.Fatalf("pause removed the part file a resume needs: %v", err)
	}
	if st.Size() <= 0 {
		t.Fatalf("part file is empty after a pause with %d bytes recorded", got.BytesDone)
	}

	// Act — and a paused queue starts nothing, even with the row pending.
	d.pump(context.Background())
	if got := stateOf(t, store, id); got != "pending" {
		t.Fatalf("row went %q while the queue was paused", got)
	}
}
