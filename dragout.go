package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

/* Dragging files out of a pane onto Explorer or any other file manager, and
   dropping files from one onto a server pane. The drag out is native
   (dragout_windows.go): the web view cannot hand other programs a file. */

// DragOutItem is one file the user is dragging out of a pane.
type DragOutItem struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"` // RFC3339
	IsDir   bool   `json:"isDir"`
}

// StartDragOut hands the drag the user has in progress to Windows, so it can
// be dropped on Explorer or any other file manager. siteID 0 means This PC.
func (a *App) StartDragOut(siteID int64, items []DragOutItem) error {
	log.Printf("drag-out: request, site=%d, %d items", siteID, len(items))
	if len(items) == 0 {
		return nil
	}
	return a.startNativeDrag(siteID, items)
}

// dragTempRoot holds server files copied to disk for file managers that only
// accept real paths. It is emptied at startup and shutdown.
func dragTempRoot() string { return filepath.Join(os.TempDir(), "warpseed-drag") }

// EnqueueUploadsFromPaths queues files and folders dropped onto a server pane
// from Explorer. It looks each path up itself, since the drop only carries
// the paths.
func (a *App) EnqueueUploadsFromPaths(siteID int64, paths []string, remoteDir string) ([]int64, error) {
	items := make([]UploadItem, 0, len(paths))
	for _, p := range paths {
		if strings.HasPrefix(strings.ToLower(p), strings.ToLower(dragTempRoot())+string(filepath.Separator)) {
			continue // a placeholder from our own server drag, dropped back on the window
		}
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
