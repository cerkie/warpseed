package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDragServerServesOnlyUnderItsSecret(t *testing.T) {
	a := &App{}
	a.startDragServer()
	if a.dragBase == "" {
		t.Fatal("drag server did not start")
	}
	defer a.dragSrv.Close()

	file := filepath.Join(t.TempDir(), "hello world.txt")
	if err := os.WriteFile(file, []byte("contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := "?site=0&path=" + url.QueryEscape(file)

	resp, err := http.Get(a.dragBase + q)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "contents" {
		t.Fatalf("got %d %q", resp.StatusCode, body)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "hello world.txt") {
		t.Fatalf("Content-Disposition = %q", cd)
	}

	// Without the secret in the path there is nothing to find.
	bare := a.dragBase[:strings.LastIndex(a.dragBase[:strings.LastIndex(a.dragBase, "/")], "/")]
	resp, err = http.Get(bare + "/file" + q)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("request without the secret got %d", resp.StatusCode)
	}

	// A folder is never served.
	rec := httptest.NewRecorder()
	a.serveDragFile(rec, httptest.NewRequest("GET", "/x?site=0&path="+url.QueryEscape(filepath.Dir(file)), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("folder got %d", rec.Code)
	}
}
