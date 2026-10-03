//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

/* Finding where a file manager put the placeholders. A drop does not say which
   folder it landed in, so while a server drag is in flight warpseed listens for
   new files on every drive. A new file named like a placeholder, outside the
   temp folder, is where the file manager "copied" it. */

type dropWatch struct {
	handles []windows.Handle
	hits    chan string
	once    sync.Once
}

func startDropWatch() *dropWatch {
	w := &dropWatch{hits: make(chan string, 256)}
	mask, _ := windows.GetLogicalDrives()
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := fmt.Sprintf("%c:\\", 'A'+i)
		p, _ := windows.UTF16PtrFromString(root)
		switch windows.GetDriveType(p) {
		case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE:
		default:
			continue
		}
		h, err := windows.CreateFile(p, windows.FILE_LIST_DIRECTORY,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			continue
		}
		w.handles = append(w.handles, h)
		go w.run(h, root)
	}
	return w
}

func (w *dropWatch) run(h windows.Handle, root string) {
	buf := make([]byte, 256<<10)
	for {
		var n uint32
		// Added and renamed files only; closing the handle ends the wait.
		err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), true, windows.FILE_NOTIFY_CHANGE_FILE_NAME|windows.FILE_NOTIFY_CHANGE_DIR_NAME, &n, nil, 0)
		if err != nil {
			return
		}
		for off := uint32(0); n > 0 && off+12 <= n; {
			next := *(*uint32)(unsafe.Pointer(&buf[off]))
			action := *(*uint32)(unsafe.Pointer(&buf[off+4]))
			nameLen := *(*uint32)(unsafe.Pointer(&buf[off+8]))
			if (action == windows.FILE_ACTION_ADDED || action == windows.FILE_ACTION_RENAMED_NEW_NAME) && off+12+nameLen <= n {
				name := windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[off+12])), nameLen/2))
				select {
				case w.hits <- root + name:
				default:
				}
			}
			if next == 0 {
				break
			}
			off += next
		}
	}
}

func (w *dropWatch) stop() {
	w.once.Do(func() {
		for _, h := range w.handles {
			windows.CancelIoEx(h, nil)
			windows.CloseHandle(h)
		}
	})
}

// queueDroppedDownloads waits for the placeholders to show up in the folder
// they were dropped on, removes them, and queues the real downloads there.
func (a *App) queueDroppedDownloads(sess *dragSession, w *dropWatch) {
	defer w.stop()
	defer os.RemoveAll(sess.dir)
	want := map[string]DragOutItem{}
	for _, it := range sess.items {
		want[strings.ToLower(safeName(it.Name))] = it
	}
	tmp := strings.ToLower(dragTempRoot()) + `\`
	deadline := time.After(20 * time.Second)
	for len(want) > 0 {
		select {
		case p := <-w.hits:
			key := strings.ToLower(filepath.Base(p))
			if _, ok := want[key]; ok {
				log.Printf("drag-out: saw %s", p)
			}
			it, ok := want[key]
			if !ok || strings.HasPrefix(strings.ToLower(p), tmp) {
				continue
			}
			delete(want, key)
			dir := filepath.Dir(p)
			// Only the empty placeholder the file manager made is removed (Remove fails on a non-empty folder).
			for i := 0; i < 20; i++ {
				if st, err := os.Stat(p); err != nil || !(st.IsDir() || st.Size() == 0) || os.Remove(p) == nil {
					break
				}
				time.Sleep(250 * time.Millisecond)
			}
			if _, err := a.EnqueueDownloads(sess.siteID, []DownloadItem{{Src: it.Path, Size: it.Size, IsDir: it.IsDir, ModTime: it.ModTime}}, dir); err != nil {
				a.sink.Emit("app:error", fmt.Sprintf("Could not queue %s: %v", it.Name, err))
			}
		case <-deadline:
			log.Printf("drag-out: no placeholder seen, waiting for %d", len(want))
			a.sink.Emit("app:error", "Could not tell which folder you dropped on. Select the files and press F5 to download them.")
			return
		}
	}
}
