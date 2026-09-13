package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// selfTestServer is a real SSH server, with a real SFTP subsystem, running
// inside this process and serving this machine's filesystem.
//
// It exists so the harness can be run with nothing installed: no OpenSSH
// feature to enable, no seedbox, no credentials. That matters most on
// Windows, where the point is to exercise NTFS and the Windows file APIs
// rather than a network, and where asking someone to set up a server first
// is the difference between these tests being run and not.
//
// It is a genuine SSH handshake and a genuine SFTP session over a genuine
// socket — the same code paths as a seedbox — with only the distance removed.
// It binds loopback, generates a throwaway host key per run, and accepts one
// password generated for that run.
//
// Loopback keeps other MACHINES out. It does not keep out other processes on
// this one, and an authenticated session can read and write anything the user
// running the harness can. That is why the password is random per run rather
// than a constant: it should not be guessable by something else on the box
// while a run is in progress.
type selfTestServer struct {
	ln       net.Listener
	user     string
	password string
	log      *logger
}

// randomPassword is unguessable for the lifetime of one run.
func randomPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate self-test password: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func startSelfTestServer(lg *logger, user, password string) (*selfTestServer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate host key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("host key signer: %w", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(pass) == password {
				return nil, nil
			}
			return nil, errors.New("bad credentials")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen on loopback: %w", err)
	}
	s := &selfTestServer{ln: ln, user: user, password: password, log: lg}
	go s.accept(cfg)
	return s, nil
}

func (s *selfTestServer) addr() (string, int) {
	a := s.ln.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func (s *selfTestServer) Close() error { return s.ln.Close() }

func (s *selfTestServer) accept(cfg *ssh.ServerConfig) {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			return // listener closed at the end of the run
		}
		go s.serveConn(nc, cfg)
	}
}

func (s *selfTestServer) serveConn(nc net.Conn, cfg *ssh.ServerConfig) {
	defer nc.Close()
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return // a failed handshake is the client's to report
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			return
		}
		go s.serveSession(ch, chReqs)
	}
}

func (s *selfTestServer) serveSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		// The only thing this server does is SFTP. Anything else is refused
		// rather than half-supported.
		if req.Type != "subsystem" || len(req.Payload) < 4 || string(req.Payload[4:]) != "sftp" {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)
		srv, err := sftp.NewServer(chanRWC{ch})
		if err != nil {
			return
		}
		if err := srv.Serve(); err != nil && !errors.Is(err, io.EOF) {
			s.log.line("      self-test server: %v", err)
		}
		srv.Close()
		return
	}
}

// chanRWC adapts an SSH channel to the ReadWriteCloser the sftp server wants.
// Close shuts the whole channel rather than just the write half, or the
// client's own Close waits on a transport that never reports EOF.
type chanRWC struct{ ssh.Channel }

func (c chanRWC) Close() error { return c.Channel.Close() }
