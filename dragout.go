package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"
)

/* Dragging a file out of a pane onto Explorer.

   The web view can hand Explorer a download link (the DownloadURL drag type),
   and Explorer then fetches it when the file is dropped. So the app serves
   files on a loopback-only HTTP port. The URL carries a secret chosen at
   startup, so nothing else on the machine (another process, a web page) can
   ask this port for a file. Remote files are fetched over a connection of
   their own, never the browse session, which may be mid-listing. */

func (a *App) startDragServer() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("drag-out: no loopback listener: %v", err)
		return
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		ln.Close()
		return
	}
	secret := hex.EncodeToString(raw[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/"+secret+"/file", a.serveDragFile)
	a.dragBase = fmt.Sprintf("http://%s/%s/file", ln.Addr().String(), secret)
	a.dragSrv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = a.dragSrv.Serve(ln) }()
}

// DragBase is the URL the frontend appends ?site=&path= to when it starts a
// drag-out, or "" if the listener could not start.
func (a *App) DragBase() string { return a.dragBase }

func (a *App) serveDragFile(w http.ResponseWriter, r *http.Request) {
	siteID, _ := strconv.ParseInt(r.URL.Query().Get("site"), 10, 64)
	p := r.URL.Query().Get("path")
	if p == "" {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(p)
	if siteID != 0 {
		name = path.Base(p)
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Type", "application/octet-stream")

	if siteID == 0 {
		f, err := os.Open(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, name, st.ModTime(), f)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	clients, err := a.dialTransfers(ctx, siteID, 1)
	cancel()
	if err != nil || len(clients) == 0 {
		http.Error(w, "could not connect", http.StatusBadGateway)
		return
	}
	c := clients[0]
	defer c.Close()
	size, mtime, isDir, err := c.StatRemoteEntry(p)
	if err != nil || isDir {
		http.NotFound(w, r)
		return
	}
	rf, err := c.OpenReader(p)
	if err != nil {
		http.Error(w, "could not open the file", http.StatusBadGateway)
		return
	}
	defer rf.Close()
	http.ServeContent(w, r, name, time.Unix(mtime, 0), io.NewSectionReader(rf, 0, size))
}

// EnqueueUploadsFromPaths queues files and folders dropped onto a server pane
// from Explorer. It looks each path up itself, since the drop only carries
// the paths.
func (a *App) EnqueueUploadsFromPaths(siteID int64, paths []string, remoteDir string) ([]int64, error) {
	items := make([]UploadItem, 0, len(paths))
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		size := st.Size()
		if st.IsDir() {
			size = 0
		}
		items = append(items, UploadItem{Src: p, Size: size, IsDir: st.IsDir()})
	}
	return a.EnqueueUploads(siteID, items, remoteDir)
}
