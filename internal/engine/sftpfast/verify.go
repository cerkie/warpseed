package sftpfast

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ErrNoRemoteHash says the server cannot hash a file for us: an FTP(S) site,
// or an SSH server that gives no shell or has no sha256sum. Callers treat it as
// "could not check", never as a failed check.
var ErrNoRemoteHash = errors.New("the server cannot hash files")

// remoteHashTimeout bounds one remote hash. sha256sum reads the whole file, so
// this has to allow for a large one on a slow disk.
const remoteHashTimeout = 30 * time.Minute

// shellQuote wraps s in single quotes for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RemoteSHA256 asks the server to hash remotePath with sha256sum, so a file can
// be compared with its copy without sending it again. Only SFTP sites over a
// real SSH connection can do this.
func (c *Client) RemoteSHA256(ctx context.Context, remotePath string) (string, error) {
	if c.ssh == nil {
		return "", ErrNoRemoteHash
	}
	sess, err := c.ssh.NewSession()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoRemoteHash, err)
	}
	defer sess.Close()

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := sess.Output("sha256sum -- " + shellQuote(remotePath))
		done <- result{out, err}
	}()
	ctx, cancel := context.WithTimeout(ctx, remoteHashTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		_ = sess.Close()
		return "", ctx.Err()
	case r := <-done:
		if r.err != nil {
			return "", fmt.Errorf("%w: %v", ErrNoRemoteHash, r.err)
		}
		return parseSHA256Sum(string(r.out))
	}
}

// parseSHA256Sum pulls the digest out of sha256sum's "<hex>  <name>" line.
func parseSHA256Sum(out string) (string, error) {
	f := strings.Fields(out)
	if len(f) == 0 || len(f[0]) != 64 {
		return "", fmt.Errorf("%w: unexpected sha256sum output", ErrNoRemoteHash)
	}
	if _, err := hex.DecodeString(f[0]); err != nil {
		return "", fmt.Errorf("%w: unexpected sha256sum output", ErrNoRemoteHash)
	}
	return strings.ToLower(f[0]), nil
}

// LocalSHA256 hashes a local file, stopping early if ctx is cancelled.
func LocalSHA256(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, rerr := f.Read(buf)
		h.Write(buf[:n])
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
