// Command harness exercises warpseed's transfer engine and dispatcher
// against a REAL SFTP server, on the machine the app actually ships to.
//
// Why it exists: the unit tests drive the dispatcher against an in-process
// server on loopback, which proves the logic but not the platform. Windows is
// where the interesting failures live — NTFS preallocation, file locking,
// path semantics — and none of it is reachable from the Linux box this code
// is written on. This is the thing to run there.
//
// It downloads nothing from the internet and needs no public test server: it
// generates a file, uploads it, and pulls it back, so every byte it checks is
// a byte it made. Point it at a seedbox for a real-world run, or at an SFTP
// server on the Windows box itself for a fast one with no network in the way.
// Windows 10 and 11 ship OpenSSH Server as an optional feature:
//
//	Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
//	Start-Service sshd
//	go build -o harness.exe ./cmd/harness
//	.\harness.exe -host localhost -user $env:USERNAME -remote-dir C:\wstest -trust-new-host
//
// Everything it writes goes inside -remote-dir and -local-dir, and it cleans
// up after itself unless -keep is set. It exits non-zero if anything failed
// and writes the log it prints to -log, so the file can be sent on as-is.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"warpseed/internal/dispatch"
	"warpseed/internal/engine/sftpfast"
	"warpseed/internal/queue"
)

var (
	flagHost      = flag.String("host", "", "SFTP host (required)")
	flagPort      = flag.Int("port", 22, "SFTP port")
	flagUser      = flag.String("user", "", "SFTP username (required)")
	flagPass      = flag.String("pass", "", "SFTP password; prefer the WARPSEED_HARNESS_PASSWORD environment variable")
	flagRemoteDir = flag.String("remote-dir", "", "an existing, writable directory on the server to work in (required)")
	flagLocalDir  = flag.String("local-dir", "", "local scratch directory (default: a new temp directory)")
	flagSizeMB    = flag.Int("size-mb", 64, "size of the test file in MiB; raise it to give the timing scenarios more room")
	flagHostKey   = flag.String("hostkey", "", "expected host key fingerprint (SHA256:...); omit once to be shown it")
	flagTrustNew  = flag.Bool("trust-new-host", false, "accept the key the server offers and print it, instead of requiring -hostkey")
	flagKeep      = flag.Bool("keep", false, "leave the test files behind for inspection")
	flagLog       = flag.String("log", "warpseed-harness.log", "write the log here as well as to the screen")
	flagOnly      = flag.String("only", "", "run only scenarios whose name contains this")
	flagSelfTest  = flag.Bool("self-test", false, "run against an SSH/SFTP server inside this process, on loopback; needs no server, no credentials and no network")
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nharness: %v\n", err)
		os.Exit(1)
	}
}

// ------------------------------------------------------------------ logging

type logger struct {
	f     *os.File
	start time.Time
}

func (l *logger) line(format string, args ...any) {
	msg := fmt.Sprintf("[%7.2fs] ", time.Since(l.start).Seconds()) + fmt.Sprintf(format, args...)
	fmt.Println(msg)
	if l.f != nil {
		fmt.Fprintln(l.f, msg)
	}
}

// section heads a block so a pasted log reads without the source beside it.
func (l *logger) section(name string) {
	l.line("")
	l.line("=== %s", name)
}

// ------------------------------------------------------------------ harness

type scenario struct {
	name string
	what string
	fn   func(context.Context) error
}

type harness struct {
	log      *logger
	store    *queue.Store
	disp     *dispatch.Dispatcher
	site     int64
	cfg      sftpfast.Config
	hostKey  ssh.HostKeyCallback
	localDir string
	remote   string // remote working directory, POSIX-spelled
	size     int64
	failures int
	// dials counts every connection opened. The dispatcher's factory runs on
	// transfer and sweep goroutines while the main one is also connecting to
	// check results, so this is touched concurrently.
	dials atomic.Int64
}

func run() error {
	flag.Parse()
	if !*flagSelfTest && (*flagHost == "" || *flagUser == "" || *flagRemoteDir == "") {
		flag.Usage()
		return errors.New("-host, -user and -remote-dir are required (or pass -self-test)")
	}
	pass := *flagPass
	if pass == "" {
		pass = os.Getenv("WARPSEED_HARNESS_PASSWORD")
	}
	if pass == "" && !*flagSelfTest {
		return errors.New("no password: pass -pass, or set WARPSEED_HARNESS_PASSWORD")
	}
	if *flagSizeMB < 8 {
		return errors.New("-size-mb below 8 leaves the pause and cancel scenarios no time to catch a transfer mid-flight")
	}

	lf, err := os.Create(*flagLog)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer lf.Close()
	lg := &logger{f: lf, start: time.Now()}

	localDir := *flagLocalDir
	if localDir == "" {
		localDir, err = os.MkdirTemp("", "warpseed-harness-")
	} else {
		err = os.MkdirAll(localDir, 0o755)
	}
	if err != nil {
		return fmt.Errorf("scratch directory: %w", err)
	}

	// Self-test mode supplies its own server, credentials and directories,
	// so everything below can proceed as if they had been given.
	var selfTest *selfTestServer
	if *flagSelfTest {
		// Random per run. Loopback keeps other MACHINES out; it does not keep
		// out other processes on this one, and this server can write wherever
		// the user running it can.
		pass, err = randomPassword()
		if err != nil {
			return err
		}
		selfTest, err = startSelfTestServer(lg, "harness", pass)
		if err != nil {
			return err
		}
		defer selfTest.Close()
		host, port := selfTest.addr()
		*flagHost, *flagPort, *flagUser = host, port, "harness"
		*flagTrustNew = true
		if *flagRemoteDir == "" {
			remoteDir := filepath.Join(localDir, "server-side")
			if err := os.MkdirAll(remoteDir, 0o755); err != nil {
				return fmt.Errorf("self-test server directory: %w", err)
			}
			*flagRemoteDir = remoteDir
		}
		lg.line("self-test: an SSH+SFTP server is running inside this process on %s:%d", host, port)
		lg.line("           it serves this machine's filesystem, so NTFS and the Windows")
		lg.line("           file APIs are exercised for real; only the network is absent.")
		lg.line("           loopback only, with a password generated for this run.")
	}

	h := &harness{
		log:      lg,
		localDir: localDir,
		remote:   toPosix(*flagRemoteDir),
		size:     int64(*flagSizeMB) << 20,
		cfg: sftpfast.Config{
			Host: *flagHost, Port: *flagPort, User: *flagUser, Password: pass,
		},
	}

	lg.line("warpseed transfer harness")
	lg.line("host        %s:%d as %s", *flagHost, *flagPort, *flagUser)
	lg.line("remote dir  %s", h.remote)
	lg.line("local dir   %s", localDir)
	lg.line("test file   %d MiB", *flagSizeMB)
	lg.line("platform    %s/%s, go %s", runtime.GOOS, runtime.GOARCH, runtime.Version())

	if err := h.setHostKey(); err != nil {
		return err
	}
	if err := h.openQueue(); err != nil {
		return err
	}
	defer h.store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.disp.Run(ctx)

	ran := 0
	for _, sc := range h.scenarios() {
		if *flagOnly != "" && !strings.Contains(sc.name, *flagOnly) {
			continue
		}
		ran++
		h.log.section(sc.name)
		h.log.line("%s", sc.what)
		started := time.Now()
		err := sc.fn(ctx)
		took := time.Since(started).Round(time.Millisecond)
		if err != nil {
			h.failures++
			h.log.line("FAIL  %s  (%s)", sc.name, took)
			h.log.line("      %v", err)
			continue
		}
		h.log.line("PASS  %s  (%s)", sc.name, took)
	}

	// Stop the dispatcher the way the app does, so a scenario that left work
	// in flight is drained rather than abandoned.
	cancel()
	h.disp.Stop(5 * time.Second)

	h.log.section("summary")
	if ran == 0 {
		// A typo in -only would otherwise look exactly like a clean run.
		return fmt.Errorf("no scenario matched -only %q, so nothing ran", *flagOnly)
	}
	h.log.line("%d connection(s) opened over the whole run", h.dials.Load())
	if h.failures == 0 {
		h.log.line("all scenarios passed")
		h.log.line("log written to %s", *flagLog)
		return nil
	}
	h.log.line("%d scenario(s) FAILED — send %s to warpseed@zyralabs.tech", h.failures, *flagLog)
	return fmt.Errorf("%d scenario(s) failed", h.failures)
}

// setHostKey pins the server's key, or arranges to show it so the next run
// can pin it.
//
// There is deliberately no accept-anything mode. The engine refuses to dial
// without a host key callback, and a tool that taught the habit of skipping
// the check would be worse than no tool. -trust-new-host is the explicit,
// printed, one-run equivalent of the app's trust-on-first-use prompt.
func (h *harness) setHostKey() error {
	want := strings.TrimSpace(*flagHostKey)
	if want == "" && !*flagTrustNew {
		h.log.line("")
		h.log.line("No -hostkey given. Re-run once with -trust-new-host to accept the key")
		h.log.line("the server offers and print its fingerprint, then pass that fingerprint")
		h.log.line("as -hostkey on every run after it.")
		return errors.New("no host key pinned")
	}
	var once sync.Once
	h.hostKey = func(_ string, _ net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if want == "" {
			// Runs on every connection, from several goroutines; say it once.
			once.Do(func() {
				h.log.line("      host key %s (%s)", got, key.Type())
				h.log.line("      pin it on the next run with -hostkey %s", got)
			})
			return nil
		}
		if got != want {
			return fmt.Errorf("host key mismatch: server offered %s, expected %s", got, want)
		}
		return nil
	}
	return nil
}

func (h *harness) openQueue() error {
	dbPath := filepath.Join(h.localDir, "harness-queue.db")
	// A fresh queue every run, sidecars included: the store runs in WAL mode,
	// and a -wal left by an interrupted run would be recovered into the new
	// database and start moving bytes for the PREVIOUS run's rows. This is
	// never the app's own database.
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		_ = os.Remove(p)
	}
	store, err := queue.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open queue: %w", err)
	}
	site, err := store.SaveSite(queue.Site{
		Name: "harness", Protocol: "sftp",
		Host: h.cfg.Host, Port: h.cfg.Port, Username: h.cfg.User,
	})
	if err != nil {
		store.Close() // nothing has taken ownership of it yet
		return fmt.Errorf("save site: %w", err)
	}
	h.store = store
	h.site = site
	h.disp = dispatch.New(store, sinkFunc(h.onEvent), h.dial)
	return nil
}

// dial is the dispatcher's connection factory, pointed at the real server.
// A short grant is not an error: the dispatcher is built to run on whatever
// the server actually hands over, and saying so here exercises that.
func (h *harness) dial(ctx context.Context, _ int64, n int) ([]*sftpfast.Client, error) {
	out := make([]*sftpfast.Client, 0, n)
	for i := 0; i < n; i++ {
		c, err := sftpfast.Dial(ctx, h.cfg, h.hostKey)
		if err != nil {
			if len(out) > 0 {
				h.log.line("      server granted %d of %d connections (%v)", len(out), n, err)
				return out, nil
			}
			return nil, err
		}
		h.dials.Add(1)
		out = append(out, c)
	}
	return out, nil
}

// oneClient opens a single connection for the harness's own checking, which
// must not go through the dispatcher.
func (h *harness) oneClient(ctx context.Context) (*sftpfast.Client, error) {
	c, err := sftpfast.Dial(ctx, h.cfg, h.hostKey)
	if err != nil {
		return nil, err
	}
	h.dials.Add(1)
	return c, nil
}

type sinkFunc func(string, any)

func (f sinkFunc) Emit(event string, payload any) { f(event, payload) }

// onEvent logs state changes only. Progress fires several times a second per
// transfer and would bury everything else.
func (h *harness) onEvent(event string, payload any) {
	if event != "transfer:state" {
		return
	}
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	if e, has := m["error"]; has {
		h.log.line("      transfer %v -> %v (%v)", m["id"], m["state"], e)
		return
	}
	h.log.line("      transfer %v -> %v", m["id"], m["state"])
}

// toPosix spells a path the way an SFTP server wants it, so -remote-dir can
// be given in whichever form is natural on the machine running this.
func toPosix(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	return strings.TrimSuffix(p, "/")
}
