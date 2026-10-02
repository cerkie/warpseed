package sftpfast

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

type ftpsDriver struct {
	root     string
	listener net.Listener
	tls      *tls.Config
	implicit bool
}

func (d *ftpsDriver) GetSettings() (*ftpserver.Settings, error) {
	req := ftpserver.MandatoryEncryption
	if d.implicit {
		req = ftpserver.ImplicitEncryption
	}
	return &ftpserver.Settings{Listener: d.listener, PublicHost: "127.0.0.1", TLSRequired: req}, nil
}
func (d *ftpsDriver) ClientConnected(ftpserver.ClientContext) (string, error) { return "test", nil }
func (d *ftpsDriver) ClientDisconnected(ftpserver.ClientContext)              {}
func (d *ftpsDriver) GetTLSConfig() (*tls.Config, error)                      { return d.tls, nil }
func (d *ftpsDriver) AuthUser(_ ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	if user != "u" || pass != "p" {
		return nil, errors.New("bad credentials")
	}
	return afero.NewBasePathFs(afero.NewOsFs(), d.root), nil
}

// startFTPS runs an in-process FTPS server and returns its root and config.
func startFTPS(t *testing.T, implicit bool) (string, FTPSConfig, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	drv := &ftpsDriver{
		root: root, listener: ln, implicit: implicit,
		tls: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}},
	}
	if implicit {
		drv.listener = tls.NewListener(ln, drv.tls)
	}
	srv := ftpserver.NewFtpServer(drv)
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() { _ = srv.Stop() })

	cfg := FTPSConfig{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port,
		User: "u", Password: "p", Implicit: implicit, Timeout: 5 * time.Second,
		Verify: func([]byte) error { return nil },
	}
	return root, cfg, der
}

func TestFTPSRoundTrip(t *testing.T) {
	for _, implicit := range []bool{false, true} {
		name := "explicit"
		if implicit {
			name = "implicit"
		}
		t.Run(name, func(t *testing.T) {
			root, cfg, der := startFTPS(t, implicit)
			ctx := context.Background()

			var seen []byte
			cfg.Verify = func(d []byte) error { seen = d; return nil }
			c, err := DialFTPS(ctx, cfg)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer c.Close()
			if !bytes.Equal(seen, der) {
				t.Fatalf("verifier saw a different certificate")
			}
			if !c.Alive(2 * time.Second) {
				t.Fatal("not alive")
			}

			// Upload ~3 MB, verify bytes server-side.
			data := make([]byte, 3<<20+123)
			_, _ = rand.Read(data)
			src := filepath.Join(t.TempDir(), "src.bin")
			if err := os.WriteFile(src, data, 0o644); err != nil {
				t.Fatal(err)
			}
			var total int64
			if err := c.Upload(ctx, src, "/up/deep/file.bin", nil, func(d int64) { total += d }); err != nil {
				t.Fatalf("upload: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(root, "up", "deep", "file.bin"))
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("uploaded bytes differ (err=%v len=%d)", err, len(got))
			}
			if total != int64(len(data)) {
				t.Fatalf("progress %d != %d", total, len(data))
			}
			if _, err := os.Stat(filepath.Join(root, "up", "deep", "file.bin"+PartSuffix)); err == nil {
				t.Fatal("part file left behind")
			}

			// Resume an upload from a half-written part.
			half := len(data) / 2
			if err := os.WriteFile(filepath.Join(root, "up", "resume.bin"+PartSuffix), data[:half], 0o644); err != nil {
				t.Fatal(err)
			}
			var start int64 = -1
			if err := c.Upload(ctx, src, "/up/resume.bin", func(o int64) { start = o }, nil); err != nil {
				t.Fatalf("resume upload: %v", err)
			}
			if start != int64(half) {
				t.Fatalf("resumed at %d, want %d", start, half)
			}
			got, _ = os.ReadFile(filepath.Join(root, "up", "resume.bin"))
			if !bytes.Equal(got, data) {
				t.Fatal("resumed upload bytes differ")
			}

			// Download, fresh and resumed.
			dst := filepath.Join(t.TempDir(), "d", "out.bin")
			if err := c.Download(ctx, "/up/deep/file.bin", dst, nil, nil); err != nil {
				t.Fatalf("download: %v", err)
			}
			got, _ = os.ReadFile(dst)
			if !bytes.Equal(got, data) {
				t.Fatal("downloaded bytes differ")
			}
			dst2 := filepath.Join(t.TempDir(), "out2.bin")
			if err := os.WriteFile(dst2+PartSuffix, data[:half], 0o644); err != nil {
				t.Fatal(err)
			}
			start = -1
			if err := c.Download(ctx, "/up/deep/file.bin", dst2, func(o int64) { start = o }, nil); err != nil {
				t.Fatalf("resume download: %v", err)
			}
			got, _ = os.ReadFile(dst2)
			if start != int64(half) || !bytes.Equal(got, data) {
				t.Fatalf("resumed download wrong (start=%d)", start)
			}

			// Browse and file operations.
			l, err := c.List("/up")
			if err != nil {
				t.Fatal(err)
			}
			if len(l.Entries) < 2 || !l.Entries[0].IsDir || l.Entries[0].Name != "deep" {
				t.Fatalf("listing wrong: %+v", l.Entries)
			}
			size, _, isDir, err := c.StatRemoteEntry("/up/deep/file.bin")
			if err != nil || isDir || size != int64(len(data)) {
				t.Fatalf("stat: size=%d dir=%v err=%v", size, isDir, err)
			}
			if _, exists, err := c.StatRemoteSize("/up/nope"); err != nil || exists {
				t.Fatalf("missing file: exists=%v err=%v", exists, err)
			}
			if err := c.MkdirEntry("/up", "new"); err != nil {
				t.Fatal(err)
			}
			if err := c.RenameEntry("/up/new", "renamed"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, "up", "renamed")); err != nil {
				t.Fatal("rename did not happen")
			}
			var walked int
			if err := c.WalkFiles(ctx, "/up", func(string, int64, int64) error { walked++; return nil }); err != nil || walked != 2 {
				t.Fatalf("walk: n=%d err=%v", walked, err)
			}
			if err := c.Remove(ctx, "/up"); err != nil {
				t.Fatalf("remove tree: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "up")); err == nil {
				t.Fatal("tree not removed")
			}
		})
	}
}

func TestFTPSRejectsUntrustedCertAndBadLogin(t *testing.T) {
	_, cfg, _ := startFTPS(t, false)
	bad := cfg
	bad.Verify = func([]byte) error { return errors.New("pin mismatch") }
	if _, err := DialFTPS(context.Background(), bad); err == nil {
		t.Fatal("dial succeeded despite verifier rejecting the certificate")
	}
	badLogin := cfg
	badLogin.Password = "wrong"
	_, err := DialFTPS(context.Background(), badLogin)
	if err == nil {
		t.Fatal("dial succeeded with a wrong password")
	}
}

// A part whose bytes are not the file's own must restart, not be completed.
func TestFTPSResumeRejectsMismatchedPart(t *testing.T) {
	root, cfg, _ := startFTPS(t, false)
	ctx := context.Background()
	c, err := DialFTPS(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	data := make([]byte, 200<<10)
	_, _ = rand.Read(data)
	bogus := make([]byte, len(data)/2) // right size, wrong content
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "f.bin"+PartSuffix), bogus, 0o644); err != nil {
		t.Fatal(err)
	}
	start := int64(-1)
	if err := c.Upload(ctx, src, "/f.bin", func(o int64) { start = o }, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "f.bin")); start != 0 || !bytes.Equal(got, data) {
		t.Fatalf("upload resumed a mismatched part (start=%d)", start)
	}

	dst := filepath.Join(t.TempDir(), "out.bin")
	if err := os.WriteFile(dst+PartSuffix, bogus, 0o644); err != nil {
		t.Fatal(err)
	}
	start = -1
	if err := c.Download(ctx, "/f.bin", dst, func(o int64) { start = o }, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); start != 0 || !bytes.Equal(got, data) {
		t.Fatalf("download resumed a mismatched part (start=%d)", start)
	}
}

func TestFTPSChunkedDownload(t *testing.T) {
	root, cfg, _ := startFTPS(t, false)
	ctx := context.Background()
	data := make([]byte, 5<<20+777)
	_, _ = rand.Read(data)
	if err := os.WriteFile(filepath.Join(root, "big.bin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	var clients []*Client
	for i := 0; i < 3; i++ {
		c, err := DialFTPS(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		clients = append(clients, c)
	}

	size := int64(len(data))
	per := size / 4
	var ranges []ChunkRange
	for i := 0; i < 4; i++ {
		r := ChunkRange{Idx: i, Offset: int64(i) * per, Length: per}
		if i == 3 {
			r.Length = size - r.Offset
		}
		ranges = append(ranges, r)
	}
	dst := filepath.Join(t.TempDir(), "big.bin")

	// Fresh run, then a resumed one: range 0 half-written by "an earlier attempt".
	for _, resume := range []bool{false, true} {
		if resume {
			part := make([]byte, size)
			copy(part, data[:per/2])
			if err := os.WriteFile(dst+ChunkPartSuffix, part, 0o644); err != nil {
				t.Fatal(err)
			}
			ranges[0].Done = per / 2
		}
		var got int64
		var mu sync.Mutex
		err := DownloadChunks(ctx, clients, "/big.bin", dst, size, ranges,
			func(_ int, d int64) { mu.Lock(); got += d; mu.Unlock() }, nil)
		if err != nil {
			t.Fatalf("resume=%v: %v", resume, err)
		}
		b, _ := os.ReadFile(dst)
		if !bytes.Equal(b, data) {
			t.Fatalf("resume=%v: assembled bytes differ", resume)
		}
		want := size
		if resume {
			want -= per / 2
		}
		if got != want {
			t.Fatalf("resume=%v: progress %d, want %d", resume, got, want)
		}
		_ = os.Remove(dst)
	}
}
