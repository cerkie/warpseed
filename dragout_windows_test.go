//go:build windows

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"
)

func TestDropFilesLayout(t *testing.T) {
	b := dropFiles([]string{`C:\a.txt`, `C:\b c.txt`})
	if binary.LittleEndian.Uint32(b[0:]) != 20 || binary.LittleEndian.Uint32(b[16:]) != 1 {
		t.Fatal("bad DROPFILES header")
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 20; i+1 < len(b); i += 2 {
		u = append(u, binary.LittleEndian.Uint16(b[i:]))
	}
	got := string(utf16.Decode(u))
	if got != "C:\\a.txt\x00C:\\b c.txt\x00\x00" {
		t.Fatalf("file list = %q", got)
	}
}

func TestFormatLayouts(t *testing.T) {
	if unsafe.Sizeof(formatEtc{}) != 32 || unsafe.Sizeof(stgMedium{}) != 24 || unsafe.Sizeof(keyInput{}) != 40 {
		t.Fatal("struct sizes differ from the Windows ABI")
	}
}

func TestDataObjectOffersHDrop(t *testing.T) {
	vtOnce.Do(initVtables)
	d := &dataObject{sess: &dragSession{paths: []string{`C:\x.txt`}}}
	addr := register(d, vtData)
	defer comRelease(addr)
	fe := hdropFormat
	if r := dataQueryGetData(addr, uintptr(unsafe.Pointer(&fe))); r != sOK {
		t.Fatalf("HDROP not offered: %#x", r)
	}
	fe.CF = 49999
	if r := dataQueryGetData(addr, uintptr(unsafe.Pointer(&fe))); r == sOK {
		t.Fatal("offered a format it does not have")
	}
	var med stgMedium
	fe = hdropFormat
	if r := dataGetData(addr, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&med))); r != sOK || med.Data == 0 {
		t.Fatalf("GetData: %#x", r)
	}
}

func TestServerDragMakesEmptyPlaceholders(t *testing.T) {
	s := &dragSession{siteID: 7, items: []DragOutItem{{Name: `we:ird.bin`, Size: 99}}}
	if err := s.prepare(); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(s.dir)
	st, err := os.Stat(filepath.Join(s.dir, "we_ird.bin"))
	if err != nil || st.Size() != 0 || len(s.paths) != 1 {
		t.Fatalf("placeholder: %v %v", st, err)
	}
}

func TestDropWatchSeesNewFiles(t *testing.T) {
	w := startDropWatch()
	defer w.stop()
	dir, err := os.MkdirTemp("", "wswatch")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "marker.bin"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(5 * time.Second)
	for {
		select {
		case p := <-w.hits:
			if filepath.Base(p) == "marker.bin" && filepath.Dir(p) == dir {
				return
			}
		case <-timeout:
			t.Fatal("watcher never reported the new file")
		}
	}
}
