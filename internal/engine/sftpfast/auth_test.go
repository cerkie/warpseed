package sftpfast

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func writeKey(t *testing.T, passphrase string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "id")
	if err := os.WriteFile(p, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAuthMethods(t *testing.T) {
	plain, locked := writeKey(t, ""), writeKey(t, "hunter2")

	if m, _, err := (Config{Password: "pw"}).authMethods(); err != nil || len(m) != 1 {
		t.Fatalf("password only: %v, %d methods", err, len(m))
	}
	if m, _, err := (Config{KeyPath: plain}).authMethods(); err != nil || len(m) != 1 {
		t.Fatalf("plain key: %v, %d methods", err, len(m))
	}
	if _, _, err := (Config{KeyPath: locked, Password: "hunter2"}).authMethods(); err != nil {
		t.Fatalf("passphrase key with its passphrase: %v", err)
	}
	// A wrong or missing passphrase must read as an auth failure, which the
	// retry ladder surfaces instead of retrying.
	for _, pw := range []string{"", "wrong"} {
		_, _, err := (Config{KeyPath: locked, Password: pw}).authMethods()
		if err == nil || !strings.Contains(err.Error(), "unable to authenticate") {
			t.Fatalf("passphrase %q: err = %v", pw, err)
		}
	}
	if _, _, err := (Config{KeyPath: filepath.Join(t.TempDir(), "nope")}).authMethods(); err == nil {
		t.Fatal("missing key file accepted")
	}
}
