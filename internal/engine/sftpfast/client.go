// Package sftpfast is warpseed's native SFTP engine: pipelined requests,
// large in-flight windows, byte-level resume — the things rclone's SFTP
// backend cannot do (approved plan, Part 2).
package sftpfast

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"warpseed/internal/engine/core"
)

// Config describes one SFTP endpoint plus tuning knobs.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string // the login password, or the key file's passphrase
	// KeyPath is a private key file to log in with; UseAgent uses the SSH agent.
	KeyPath  string
	UseAgent bool
	// Concurrency is outstanding requests per file (research: 64 default,
	// up to 256 saturates 1 Gbps at high RTT).
	Concurrency int
	Timeout     time.Duration
}

func (c Config) withDefaults() Config {
	if c.Port == 0 {
		c.Port = 22
	}
	if c.Concurrency == 0 {
		c.Concurrency = 64
	}
	if c.Timeout == 0 {
		c.Timeout = 15 * time.Second
	}
	return c
}

// preferredCiphers orders AEAD ciphers first: AES-GCM rides AES-NI at
// multi-Gbps per core; chacha20 wins on machines without AES-NI.
var preferredCiphers = []string{
	"aes128-gcm@openssh.com",
	"aes256-gcm@openssh.com",
	"chacha20-poly1305@openssh.com",
	"aes128-ctr",
	"aes256-ctr",
}

// Client is one SSH connection carrying one SFTP session. The connection
// manager (next increment) pools these; a browse Client never carries
// file data.
type Client struct {
	ssh  *ssh.Client // nil for in-process test clients
	sftp *sftp.Client
	ftp  *ftpConn // set instead of ssh/sftp for FTPS sites
}

// Dial connects, authenticates, and opens an SFTP session. hostKey is
// mandatory — there is no insecure-ignore path in this codebase.
func Dial(ctx context.Context, cfg Config, hostKey ssh.HostKeyCallback) (*Client, error) {
	cfg = cfg.withDefaults()
	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))

	auth, closeAgent, err := cfg.authMethods()
	if err != nil {
		return nil, err
	}
	defer closeAgent() // only needed while logging in

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         cfg.Timeout,
		Config:          ssh.Config{Ciphers: preferredCiphers},
	}

	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh handshake %s: %w", addr, err)
	}
	sshClient := ssh.NewClient(sc, chans, reqs)

	sftpClient, err := sftp.NewClient(sshClient,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(cfg.Concurrency),
	)
	if err != nil {
		sshClient.Close()
		return nil, fmt.Errorf("open sftp session: %w", err)
	}
	return &Client{ssh: sshClient, sftp: sftpClient}, nil
}

// Alive reports whether the SSH transport still responds, waiting at most
// timeout. A connection dropped by an idle NAT/firewall often stays half-open:
// requests on it block until the OS gives up (minutes), which is exactly the
// window in which "reconnect" must already work — hence the hard deadline.
func (c *Client) Alive(timeout time.Duration) bool {
	if c.ftp != nil {
		return c.ftp.alive(timeout)
	}
	done := make(chan error, 1)
	go func() {
		if c.ssh == nil { // in-process test client: probe the SFTP session itself
			_, err := c.sftp.RealPath(".")
			done <- err
			return
		}
		// A refused global request still proves the transport is up; only a
		// transport error means the connection is gone.
		_, _, err := c.ssh.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(timeout):
		return false
	}
}

func (c *Client) Close() error {
	if c.ftp != nil {
		return c.ftp.close()
	}
	var first error
	if c.sftp != nil {
		first = c.sftp.Close()
	}
	if c.ssh != nil {
		if err := c.ssh.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// List reads a remote directory, sorted dirs-first then case-insensitive —
// identical presentation contract to localfs.List.
func (c *Client) List(remotePath string) (core.Listing, error) {
	if c.ftp != nil {
		return c.ftp.list(remotePath)
	}
	clean := path.Clean(remotePath)
	if clean == "" || clean == "." {
		clean = "/"
	}
	infos, err := c.sftp.ReadDir(clean)
	if err != nil {
		return core.Listing{}, fmt.Errorf("read remote dir %q: %w", clean, err)
	}

	entries := make([]core.Entry, 0, len(infos))
	for _, fi := range infos {
		e := core.Entry{
			Name:    fi.Name(),
			IsDir:   fi.IsDir(),
			Size:    fi.Size(),
			ModTime: fi.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
			Mode:    fi.Mode().String(),
		}
		if e.IsDir {
			e.Size = -1
		}
		entries = append(entries, e)
	}
	return newListing(clean, entries), nil
}

// Home resolves the session's initial directory (the SFTP server's idea of
// "."), so remote panes open where the user lands, not at "/".
func (c *Client) Home() (string, error) {
	if c.ftp != nil {
		return c.ftp.home()
	}
	home, err := c.sftp.RealPath(".")
	if err != nil {
		return "", fmt.Errorf("resolve remote home: %w", err)
	}
	return home, nil
}

// newFromSFTP lets tests drive the engine over an in-process pipe pair
// without SSH.
func newFromSFTP(sc *sftp.Client) *Client {
	return &Client{sftp: sc}
}

// NewFromSFTP wraps an SFTP session the caller already owns. The ssh half
// stays nil, so Close closes only the SFTP client and the transport remains
// the caller's to shut down.
//
// It exists so tests OUTSIDE this package can hand the dispatcher genuine
// *Client values driven by a real pkg/sftp server over pipes. The dispatcher
// is where cancel decides to delete a user's bytes, and until this seam
// existed every one of its tests ran with dialing disabled, so the remote
// half of that decision had never executed. Production code calls Dial.
func NewFromSFTP(sc *sftp.Client) *Client {
	return newFromSFTP(sc)
}

// stat is Stat for whichever protocol the client speaks.
func (c *Client) stat(p string) (os.FileInfo, error) {
	if c.ftp != nil {
		return c.ftp.stat(p)
	}
	return c.sftp.Stat(p)
}

// newListing sorts entries dirs-first then case-insensitive and fills in the
// parent, so every protocol presents a directory the same way.
func newListing(clean string, entries []core.Entry) core.Listing {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	parent := path.Dir(clean)
	if parent == clean {
		parent = ""
	}
	return core.Listing{Path: clean, Parent: parent, Entries: entries}
}

// openReaderAt opens a remote file for ranged reads over either protocol.
func (c *Client) openReaderAt(remotePath string) (interface {
	io.ReaderAt
	io.Closer
}, error) {
	if c.ftp != nil {
		return &ftpReader{f: c.ftp, path: remotePath}, nil
	}
	return c.sftp.Open(remotePath)
}

// authMethods builds the login methods a config asks for: a key file (the
// password field then holds its passphrase), the SSH agent, or a password.
func (c Config) authMethods() (methods []ssh.AuthMethod, closeAgent func(), err error) {
	closeAgent = func() {}
	if c.KeyPath != "" {
		pem, rerr := os.ReadFile(c.KeyPath)
		if rerr != nil {
			return nil, closeAgent, fmt.Errorf("read key file: %w", rerr)
		}
		signer, perr := ssh.ParsePrivateKey(pem)
		var missing *ssh.PassphraseMissingError
		if errors.As(perr, &missing) {
			signer, perr = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(c.Password))
		}
		if perr != nil {
			return nil, closeAgent, fmt.Errorf("unable to authenticate: key %s: %w", filepath.Base(c.KeyPath), perr)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if c.UseAgent {
		conn, derr := dialAgent()
		if derr != nil {
			return nil, closeAgent, fmt.Errorf("unable to authenticate: SSH agent: %w", derr)
		}
		closeAgent = func() { conn.Close() }
		methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
	}
	if len(methods) == 0 {
		methods = append(methods, ssh.Password(c.Password))
	}
	return methods, closeAgent, nil
}

// OpenReader opens a remote file for reading at arbitrary offsets, over
// whichever protocol the client speaks.
func (c *Client) OpenReader(remotePath string) (interface {
	io.ReaderAt
	io.Closer
}, error) {
	return c.openReaderAt(remotePath)
}
