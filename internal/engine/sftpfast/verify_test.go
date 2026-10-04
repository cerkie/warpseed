package sftpfast

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/a/b":       `'/a/b'`,
		"it's a.txt": `'it'\''s a.txt'`,
		"$(rm -rf)":  `'$(rm -rf)'`,
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestParseSHA256Sum(t *testing.T) {
	const h = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got, err := parseSHA256Sum(h + "  file name.bin\n"); err != nil || got != h {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "sha256sum: nope: No such file", "zz" + h[2:] + " x"} {
		if _, err := parseSHA256Sum(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestLocalSHA256(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LocalSHA256(context.Background(), p)
	if err != nil || got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("got %q, %v", got, err)
	}
}
