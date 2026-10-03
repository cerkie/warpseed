package sftpfast

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A destination well past Windows' 260-character path limit: six 60-character
// folders and a 200-character file name.
func longDest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < 6; i++ {
		dir = filepath.Join(dir, strings.Repeat(string(rune('a'+i)), 60))
	}
	dst := filepath.Join(dir, strings.Repeat("n", 196)+".mkv")
	if len(dst) < 600 {
		t.Fatalf("test path only %d characters", len(dst))
	}
	return dst
}

func TestDownloadToPathOverTheWindowsLimit(t *testing.T) {
	c := newTestClient(t)
	src := filepath.Join(t.TempDir(), "src.bin")
	data := writeRandomFile(t, src, 300<<10)
	dst := longDest(t)

	if err := c.Download(context.Background(), src, dst, nil, nil); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !bytes.Equal(mustRead(t, dst), data) {
		t.Fatal("content mismatch")
	}
	if _, err := os.Stat(dst + PartSuffix); !os.IsNotExist(err) {
		t.Fatal("part file left behind")
	}
}

func TestChunkedDownloadToPathOverTheWindowsLimit(t *testing.T) {
	cs := testClients(t, 3)
	src := filepath.Join(t.TempDir(), "src.bin")
	data := writeRandomFile(t, src, 1<<20+5)
	dst := longDest(t)

	err := DownloadChunks(context.Background(), cs, src, dst, int64(len(data)), plan(int64(len(data)), 3), func(int, int64) {}, nil)
	if err != nil {
		t.Fatalf("DownloadChunks: %v", err)
	}
	if !bytes.Equal(mustRead(t, dst), data) {
		t.Fatal("content mismatch")
	}
}

func TestDownloadOfA255CharacterName(t *testing.T) {
	// The name fits Windows exactly, but name + ".wspart" does not, so the
	// in-progress file needs a stand-in; the finished file keeps its name.
	c := newTestClient(t)
	src := filepath.Join(t.TempDir(), "src.bin")
	data := writeRandomFile(t, src, 100<<10)
	dst := filepath.Join(t.TempDir(), strings.Repeat("n", 251)+".mkv")

	if err := c.Download(context.Background(), src, dst, nil, nil); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !bytes.Equal(mustRead(t, dst), data) {
		t.Fatal("content mismatch")
	}
	if p := PartPath(dst, PartSuffix); p == dst+PartSuffix || PartPath(dst, PartSuffix) != p {
		t.Fatal("part path must be a stable stand-in")
	}
}

func TestPartPathLeavesShortNamesAlone(t *testing.T) {
	if got := PartPath(`C:\x\a.mkv`, PartSuffix); got != `C:\x\a.mkv`+PartSuffix {
		t.Fatal(got)
	}
}
