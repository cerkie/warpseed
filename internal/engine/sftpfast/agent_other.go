//go:build !windows

package sftpfast

import (
	"errors"
	"net"
	"os"
)

// dialAgent connects to the agent named by SSH_AUTH_SOCK.
func dialAgent() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK is not set")
	}
	return net.Dial("unix", sock)
}
