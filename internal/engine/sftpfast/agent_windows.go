package sftpfast

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// dialAgent connects to the Windows OpenSSH agent.
func dialAgent() (net.Conn, error) {
	return winio.DialPipe(`\.\pipe\openssh-ssh-agent`, nil)
}
