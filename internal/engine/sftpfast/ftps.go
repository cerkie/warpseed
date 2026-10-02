package sftpfast

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jlaffaye/ftp"

	"warpseed/internal/engine/core"
)

// tailVerifyBytes is how much of the already-transferred data a resume
// re-reads from the server and compares with the other side before trusting it.
const tailVerifyBytes = 64 << 10

// ftpConn is one FTP-over-TLS control connection. The protocol is strictly
// request/response on a single channel, so every call holds mu; a transfer
// holds it for its whole duration, which is why transfers get dedicated
// clients and browsing never shares one with them.
type ftpConn struct {
	mu sync.Mutex
	c  *ftp.ServerConn

	// Every socket this client opened, control and data. Closing them is how
	// a stuck transfer is aborted: the holder of mu cannot be asked to stop.
	rawMu sync.Mutex
	raw   []net.Conn
}

// FTPSConfig describes an FTPS endpoint. Verify receives the server's leaf
// certificate (DER) and decides whether to trust it; as with SSH host keys
// there is no insecure-ignore path.
type FTPSConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	// Implicit selects TLS from the first byte (usually port 990); otherwise
	// the connection starts plain and upgrades with AUTH TLS.
	Implicit bool
	Timeout  time.Duration
	Verify   func(certDER []byte) error
}

// DialFTPS connects, secures the control and data channels, and logs in.
func DialFTPS(ctx context.Context, cfg FTPSConfig) (*Client, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))

	tlsCfg := &tls.Config{
		ServerName: cfg.Host,
		MinVersion: tls.VersionTLS12,
		// Chain validation is replaced by fingerprint pinning: seedbox FTPS
		// endpoints overwhelmingly present self-signed certificates.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("ftps: server presented no certificate")
			}
			return cfg.Verify(raw[0])
		},
		// Servers commonly insist the data channel resume the control
		// channel's TLS session; this is what makes that possible.
		ClientSessionCache: tls.NewLRUClientSessionCache(16),
	}

	fc := &ftpConn{}
	var dials int32
	dialer := net.Dialer{Timeout: cfg.Timeout}
	opts := []ftp.DialOption{
		ftp.DialWithContext(ctx),
		ftp.DialWithShutTimeout(cfg.Timeout),
		// A custom dial func replaces the library's own TLS wrapping for
		// every connection, so it must secure them itself: the first dial is
		// the control channel (TLS from byte one only when implicit; explicit
		// upgrades itself after AUTH TLS), every later one is a data channel.
		ftp.DialWithDialFunc(func(network, address string) (net.Conn, error) {
			c, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			fc.rawMu.Lock()
			fc.raw = append(fc.raw, c)
			fc.rawMu.Unlock()
			if atomic.AddInt32(&dials, 1) == 1 && !cfg.Implicit {
				return c, nil
			}
			return tls.Client(c, tlsCfg), nil
		}),
	}
	if cfg.Implicit {
		opts = append(opts, ftp.DialWithTLS(tlsCfg))
	} else {
		opts = append(opts, ftp.DialWithExplicitTLS(tlsCfg))
	}

	conn, err := ftp.Dial(addr, opts...)
	if err != nil {
		// core.Classify keys on phrases, so give 421 (server full) its own.
		var te *textproto.Error
		if errors.As(err, &te) && te.Code == 421 {
			err = fmt.Errorf("too many connections: %w", err)
		}
		return nil, fmt.Errorf("ftps connect %s: %w", addr, err)
	}
	if err := conn.Login(cfg.User, cfg.Password); err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("unable to authenticate (ftps %s): %w", addr, err)
	}
	fc.c = conn
	return &Client{ftp: fc}, nil
}

// statLocked finds one path's facts. MLST answers directly where supported;
// everywhere else the parent listing is the only portable source. A missing
// path returns os.ErrNotExist.
func (f *ftpConn) statLocked(p string) (*ftp.Entry, error) {
	p = path.Clean(p)
	if p == "/" {
		return &ftp.Entry{Type: ftp.EntryTypeFolder}, nil
	}
	if e, err := f.c.GetEntry(p); err == nil {
		return e, nil
	}
	// SIZE (+ MDTM) identifies a file in one or two round trips; the listing
	// below can mean reading a whole directory just to find one entry.
	if n, err := f.c.FileSize(p); err == nil {
		e := &ftp.Entry{Name: path.Base(p), Type: ftp.EntryTypeFile, Size: uint64(n)}
		if f.c.IsGetTimeSupported() {
			e.Time, _ = f.c.GetTime(p)
		}
		return e, nil
	}
	entries, err := f.c.List(path.Dir(p))
	var te *textproto.Error
	if errors.As(err, &te) && te.Code == 550 {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name == path.Base(p) {
			return e, nil
		}
	}
	return nil, os.ErrNotExist
}

func (f *ftpConn) stat(p string) (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, err := f.statLocked(p)
	if err != nil {
		return nil, err
	}
	return ftpInfo{e}, nil
}

func (f *ftpConn) alive(timeout time.Duration) bool {
	done := make(chan error, 1)
	go func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		done <- f.c.NoOp()
	}()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(timeout):
		return false
	}
}

func (f *ftpConn) close() error {
	// Never wait for the lock: closing is how a stuck transfer is aborted,
	// and the holder of the lock is exactly that stuck transfer. A polite
	// QUIT is only for an idle connection; otherwise just cut the sockets.
	if f.mu.TryLock() {
		_ = f.c.Quit()
		f.mu.Unlock()
	}
	f.rawMu.Lock()
	defer f.rawMu.Unlock()
	for _, c := range f.raw {
		_ = c.Close()
	}
	f.raw = nil
	return nil
}

func (f *ftpConn) list(remotePath string) (core.Listing, error) {
	clean := path.Clean(remotePath)
	f.mu.Lock()
	defer f.mu.Unlock()
	infos, err := f.c.List(clean)
	if err != nil {
		return core.Listing{}, fmt.Errorf("read remote dir %q: %w", clean, err)
	}
	entries := make([]core.Entry, 0, len(infos))
	for _, fi := range infos {
		if fi.Name == "." || fi.Name == ".." {
			continue
		}
		e := core.Entry{
			Name:    fi.Name,
			IsDir:   fi.Type == ftp.EntryTypeFolder,
			Size:    int64(fi.Size),
			ModTime: fi.Time.UTC().Format("2006-01-02T15:04:05Z"),
		}
		// A link is only worth presenting as a folder if it opens as one.
		if fi.Type == ftp.EntryTypeLink && f.c.ChangeDir(path.Join(clean, fi.Name)) == nil {
			e.IsDir = true
		}
		if e.IsDir {
			e.Size = -1
		}
		entries = append(entries, e)
	}
	return newListing(clean, entries), nil
}

func (f *ftpConn) home() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	home, err := f.c.CurrentDir()
	if err != nil {
		return "", fmt.Errorf("resolve remote home: %w", err)
	}
	return home, nil
}

// mkdirAllLocked creates every missing directory down to dir.
func (f *ftpConn) mkdirAllLocked(dir string) error {
	var missing []string
	for p := path.Clean(dir); p != "/" && p != "."; p = path.Dir(p) {
		// CWD is one round trip; stat can mean listing a whole directory.
		if f.c.ChangeDir(p) == nil {
			break
		}
		missing = append(missing, p)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := f.c.MakeDir(missing[i]); err != nil {
			return err
		}
	}
	return nil
}

func (f *ftpConn) remove(ctx context.Context, remotePath string) error {
	clean := path.Clean(remotePath)
	if clean == "/" || clean == "." {
		return fmt.Errorf("refusing to delete %q", remotePath)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := f.statLocked(clean)
	if err != nil {
		return fmt.Errorf("stat %q: %w", clean, err)
	}
	// A link is removed as a link, never followed: recursing through one
	// would delete the target's contents somewhere else on the server.
	if st.Type != ftp.EntryTypeFolder {
		if err := f.c.Delete(clean); err != nil {
			return fmt.Errorf("delete %q: %w", path.Base(clean), err)
		}
		return nil
	}
	return f.removeTreeLocked(ctx, clean, 0)
}

func (f *ftpConn) removeTreeLocked(ctx context.Context, dir string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 32 {
		return fmt.Errorf("directory tree deeper than 32 levels at %q", dir)
	}
	entries, err := f.c.List(dir)
	if err != nil {
		return fmt.Errorf("read %q: %w", dir, err)
	}
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		child := path.Join(dir, e.Name)
		if e.Type == ftp.EntryTypeFolder {
			err = f.removeTreeLocked(ctx, child, depth+1)
		} else {
			err = f.c.Delete(child)
		}
		if err != nil {
			return fmt.Errorf("delete %q: %w", child, err)
		}
	}
	if err := f.c.RemoveDir(dir); err != nil {
		return fmt.Errorf("delete folder %q: %w", path.Base(dir), err)
	}
	return nil
}

func (f *ftpConn) renameEntry(remotePath, newName string) error {
	clean := path.Clean(remotePath)
	target := path.Join(path.Dir(clean), newName)
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.statLocked(target); err == nil {
		return fmt.Errorf("%q already exists", newName)
	}
	if err := f.c.Rename(clean, target); err != nil {
		return fmt.Errorf("rename to %q: %w", newName, err)
	}
	return nil
}

func (f *ftpConn) mkdirEntry(parent, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.c.MakeDir(path.Join(path.Clean(parent), name)); err != nil {
		return fmt.Errorf("create folder %q: %w", name, err)
	}
	return nil
}

func (f *ftpConn) removeRemote(remotePath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.c.Delete(remotePath); err != nil && !isNotExistRemote(err) {
		return fmt.Errorf("remove remote %q: %w", remotePath, err)
	}
	return nil
}

// tailMatches is the shared resume check with the remote side read over a
// RETR stream. The caller already holds the connection lock.
func (f *ftpConn) tailMatches(remotePath string, lf io.ReaderAt, offset int64) (bool, error) {
	r := &ftpReader{f: f, path: remotePath}
	defer r.closeLocked()
	return tailMatches(readerAtFunc(r.readAt), lf, offset)
}

type readerAtFunc func([]byte, int64) (int, error)

func (fn readerAtFunc) ReadAt(p []byte, off int64) (int, error) { return fn(p, off) }

// download is the single-stream resumable fetch: REST to the part's size once
// its tail is proven, then a size check before the rename so a torn data
// connection can never be published as a finished file.
func (f *ftpConn) download(ctx context.Context, remotePath, localPath string, onStart func(int64), progress func(int64)) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// SIZE and MDTM are one round trip each; a full stat can fall back to
	// listing the parent directory, which on a big folder is the whole delay.
	remoteSize, err := f.sizeLocked(remotePath)
	if err != nil {
		return fmt.Errorf("open remote %q: %w", remotePath, err)
	}
	var mtime time.Time
	if f.c.IsGetTimeSupported() {
		mtime, _ = f.c.GetTime(remotePath)
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("create local dir: %w", err)
	}
	part := localPath + PartSuffix
	lf, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open part file: %w", err)
	}
	defer lf.Close()

	offset := int64(0)
	if pst, err := lf.Stat(); err == nil && pst.Size() <= remoteSize {
		offset = pst.Size()
	}
	if offset > 0 {
		if ok, err := f.tailMatches(remotePath, lf, offset); err != nil {
			return fmt.Errorf("verify resume of %q: %w", remotePath, err)
		} else if !ok {
			offset = 0
		}
	}
	if err := lf.Truncate(offset); err != nil {
		return fmt.Errorf("truncate stale part: %w", err)
	}
	onStart(offset)

	if offset < remoteSize {
		if _, err := lf.Seek(offset, io.SeekStart); err != nil {
			return fmt.Errorf("seek part to %d: %w", offset, err)
		}
		resp, err := f.c.RetrFrom(remotePath, uint64(offset))
		if err != nil {
			return fmt.Errorf("open remote %q: %w", remotePath, err)
		}
		stop := context.AfterFunc(ctx, func() { _ = resp.SetDeadline(time.Now()) })
		_, copyErr := io.Copy(&progressWriter{ctx: ctx, w: lf, onWrite: progress}, resp)
		stop()
		_ = resp.Close()
		if copyErr != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("download cancelled: %w", ctx.Err())
			}
			return fmt.Errorf("transfer %q: %w", remotePath, copyErr)
		}
	}

	if err := lf.Sync(); err != nil {
		return fmt.Errorf("sync part: %w", err)
	}
	// Never publish a short file: a dropped data channel looks like a clean
	// EOF to the reader. The part stays for the next resume.
	if pst, err := lf.Stat(); err != nil || pst.Size() != remoteSize {
		return fmt.Errorf("transfer %q: unexpected EOF (short of %d bytes)", remotePath, remoteSize)
	}
	if err := lf.Close(); err != nil {
		return fmt.Errorf("close part: %w", err)
	}
	if err := os.Rename(part, localPath); err != nil {
		return fmt.Errorf("finalize %q: %w", localPath, err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(localPath, mtime, mtime); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("set mtime: %w", err)
		}
	}
	return nil
}

// upload mirrors Upload for FTP: bytes land in a remote .wspart, a resume is
// APPE from the part's size once its tail is proven, and only a size-verified
// part is renamed into place.
func (f *ftpConn) upload(ctx context.Context, localPath, remotePath string, onStart func(int64), progress func(int64)) error {
	lf, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local %q: %w", localPath, err)
	}
	defer lf.Close()
	lstat, err := lf.Stat()
	if err != nil {
		return fmt.Errorf("stat local %q: %w", localPath, err)
	}
	localSize := lstat.Size()

	f.mu.Lock()
	defer f.mu.Unlock()

	if dir := path.Dir(remotePath); dir != "." && dir != "/" {
		if err := f.mkdirAllLocked(dir); err != nil {
			return fmt.Errorf("create remote dir %q: %w", dir, err)
		}
	}

	part := remotePath + PartSuffix
	offset := int64(0)
	if size, err := f.sizeLocked(part); err == nil && size <= localSize {
		offset = size
		if offset > 0 {
			if ok, err := f.tailMatches(part, lf, offset); err != nil {
				return fmt.Errorf("verify resume of %q: %w", part, err)
			} else if !ok {
				offset = 0
			}
		}
	}
	onStart(offset)

	if offset < localSize || localSize == 0 {
		if _, err := lf.Seek(offset, io.SeekStart); err != nil {
			return fmt.Errorf("seek local to %d: %w", offset, err)
		}
		src := &progressReader{ctx: ctx, r: lf, onRead: progress}
		if offset == 0 {
			err = f.c.Stor(part, src) // truncates any stale part
		} else {
			err = f.c.Append(part, src)
		}
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("upload cancelled: %w", ctx.Err())
			}
			return fmt.Errorf("transfer %q: %w", localPath, err)
		}
	}

	if size, err := f.sizeLocked(part); err != nil || size != localSize {
		return fmt.Errorf("transfer %q: unexpected EOF (server holds less than %d bytes)", localPath, localSize)
	}
	if st, err := f.statLocked(remotePath); err == nil {
		if st.Type == ftp.EntryTypeFolder {
			return fmt.Errorf("remote destination %q is a directory", remotePath)
		}
		if err := f.c.Delete(remotePath); err != nil {
			return fmt.Errorf("replace remote %q: %w", remotePath, err)
		}
	}
	if err := f.c.Rename(part, remotePath); err != nil {
		return fmt.Errorf("finalize remote %q: %w", remotePath, err)
	}
	// Best-effort, as with SFTP: not every server lets clients set mtimes.
	_ = f.c.SetTime(remotePath, lstat.ModTime())
	return nil
}

// errFTPSChunked guards the chunked paths, which need random-access writes.
// The dispatcher plans one stream for FTPS, so this should never be reached.
var errFTPSChunked = errors.New("chunked transfers are SFTP-only")

// ftpInfo presents an FTP entry as an os.FileInfo so both protocols share one
// stat path.
type ftpInfo struct{ *ftp.Entry }

func (i ftpInfo) Name() string       { return i.Entry.Name }
func (i ftpInfo) Size() int64        { return int64(i.Entry.Size) }
func (i ftpInfo) Mode() os.FileMode  { return 0 }
func (i ftpInfo) ModTime() time.Time { return i.Entry.Time }
func (i ftpInfo) IsDir() bool        { return i.Type == ftp.EntryTypeFolder }
func (i ftpInfo) Sys() any           { return nil }

// sizeLocked is a cheap existence-and-size probe (one SIZE round trip).
func (f *ftpConn) sizeLocked(p string) (int64, error) {
	n, err := f.c.FileSize(p)
	var te *textproto.Error
	if errors.As(err, &te) && te.Code == 550 {
		return 0, os.ErrNotExist
	}
	return n, err
}

// ftpReader serves the sequential ReadAt calls of a ranged download from one
// RETR stream, reopening it with REST only when the offset jumps. The data
// beyond a range's end is never read; closing the stream aborts it.
type ftpReader struct {
	f    *ftpConn
	path string
	resp *ftp.Response
	pos  int64
}

func (r *ftpReader) ReadAt(p []byte, off int64) (int, error) {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	return r.readAt(p, off)
}

func (r *ftpReader) readAt(p []byte, off int64) (int, error) {
	if r.resp == nil || off != r.pos {
		r.closeLocked()
		resp, err := r.f.c.RetrFrom(r.path, uint64(off))
		if err != nil {
			return 0, err
		}
		r.resp, r.pos = resp, off
	}
	n, err := io.ReadFull(r.resp, p)
	r.pos += int64(n)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	return n, err
}

func (r *ftpReader) closeLocked() {
	if r.resp != nil {
		_ = r.resp.Close() // aborting mid-file ends in an abort reply; ignorable
		r.resp = nil
	}
}

func (r *ftpReader) Close() error {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	r.closeLocked()
	return nil
}

func (f *ftpConn) removeEmptyDir(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c.RemoveDir(p)
}

func (f *ftpConn) rename(src, dst string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c.Rename(src, dst)
}
