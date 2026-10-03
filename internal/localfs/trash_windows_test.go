//go:build windows

package localfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrashRefusesPathsTheBinCannotHold(t *testing.T) {
	root := t.TempDir()
	deep := root
	for i := 0; i < 5; i++ {
		deep = filepath.Join(deep, strings.Repeat("d", 60))
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(deep, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The file's own path is too long for the bin. (A short folder holding it
	// is fine: the bin only records the folder's own path.)
	for _, target := range []string{file} {
		if _, err := Trash([]string{target}); !errors.Is(err, ErrBinTooLong) {
			t.Fatalf("Trash(%d chars) = %v, want ErrBinTooLong", len(target), err)
		}
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("file was touched: %v", err)
	}
	// And the permanent delete does reach it.
	if n, err := Delete([]string{filepath.Join(root, strings.Repeat("d", 60))}); err != nil || n != 1 {
		t.Fatalf("Delete: %d, %v", n, err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("file survived the permanent delete")
	}
}
