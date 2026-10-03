//go:build windows

package main

import (
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

/* Native drag out. The web view's own drag cannot hand files to other
   programs, so when a pane drag leaves the window the app cancels the web
   view's drag and starts a Windows (OLE) one with the same mouse button.

   The files are offered as a list of real paths (CF_HDROP), which every file
   manager understands. This PC's files are offered where they are. A
   server's files are offered as empty placeholder files; the file manager
   "copies" them, and warpseed spots where the copies appeared and queues the
   real downloads there. That way nothing waits on a transfer inside the drop.

   The COM objects are built by hand with syscall callbacks (no cgo). Their
   methods run on the window thread, inside DoDragDrop's message loop. */

var (
	ole32            = windows.NewLazySystemDLL("ole32.dll")
	user32           = windows.NewLazySystemDLL("user32.dll")
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procOleInit      = ole32.NewProc("OleInitialize")
	procDoDragDrop   = ole32.NewProc("DoDragDrop")
	procSendInput    = user32.NewProc("SendInput")
	procKeyState     = user32.NewProc("GetAsyncKeyState")
	procGetFG        = user32.NewProc("GetForegroundWindow")
	procGetWinPID    = user32.NewProc("GetWindowThreadProcessId")
	procSetWinLong   = user32.NewProc("SetWindowLongPtrW")
	procCallWinProc  = user32.NewProc("CallWindowProcW")
	procPostMessage  = user32.NewProc("PostMessageW")
	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
)

const (
	cfHDrop        = 15
	tymedHGlobal   = 1
	dropEffectCopy = 1

	sOK            = 0
	sFalse         = 1
	eNotImpl       = 0x80004001
	eNoInterface   = 0x80004002
	ePointer       = 0x80004003
	eFail          = 0x80004005
	eInvalidArg    = 0x80070057
	dvEFormatEtc   = 0x80040064
	oleEAdviseNoSp = 0x80040003
	dataSSameFmt   = 0x40130
	dragDropDrop   = 0x40100
	dragDropCancel = 0x40101
	dragDropCursor = 0x40102
)

var (
	iidUnknown    = windows.GUID{Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidDataObject = windows.GUID{Data1: 0x0000010E, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidDropSource = windows.GUID{Data1: 0x00000121, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidEnumFmt    = windows.GUID{Data1: 0x00000103, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

type formatEtc struct {
	CF     uint16
	Ptd    uintptr
	Aspect uint32
	Lindex int32
	Tymed  uint32
}

type stgMedium struct {
	Tymed uint32
	Data  uintptr
	Unk   uintptr
}

// ptr turns an address handed to a callback into a pointer without tripping
// the unsafe.Pointer(uintptr) vet check; the memory belongs to the caller.
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

func hr(code uint32) uintptr { return uintptr(code) }

// ---- COM object plumbing ----

// com is the head of every object: the vtable pointer must come first.
type com struct {
	vtbl uintptr
	n    int32
}

func (c *com) base() *com { return c }

type comObject interface{ base() *com }

var (
	comMu   sync.Mutex
	comObjs = map[uintptr]comObject{} // keeps objects alive while Windows holds them
	vtKeep  [][]uintptr
)

func register(o comObject, vtbl uintptr) uintptr {
	c := o.base()
	c.vtbl, c.n = vtbl, 1
	addr := uintptr(unsafe.Pointer(c))
	comMu.Lock()
	comObjs[addr] = o
	comMu.Unlock()
	return addr
}

func lookup(this uintptr) comObject {
	comMu.Lock()
	defer comMu.Unlock()
	return comObjs[this]
}

func vtable(fns ...uintptr) uintptr {
	v := append([]uintptr(nil), fns...)
	vtKeep = append(vtKeep, v)
	return uintptr(unsafe.Pointer(&v[0]))
}

func comAddRef(this uintptr) uintptr {
	o := lookup(this)
	if o == nil {
		return 0
	}
	return uintptr(atomic.AddInt32(&o.base().n, 1))
}

func comRelease(this uintptr) uintptr {
	o := lookup(this)
	if o == nil {
		return 0
	}
	n := atomic.AddInt32(&o.base().n, -1)
	if n == 0 {
		comMu.Lock()
		delete(comObjs, this)
		comMu.Unlock()
	}
	return uintptr(n)
}

func comQuery(this, riid, ppv uintptr, iids ...windows.GUID) uintptr {
	if ppv == 0 {
		return hr(ePointer)
	}
	*(*uintptr)(ptr(ppv)) = 0
	id := *(*windows.GUID)(ptr(riid))
	for _, g := range iids {
		if id == g {
			*(*uintptr)(ptr(ppv)) = this
			comAddRef(this)
			return sOK
		}
	}
	return hr(eNoInterface)
}

func notImpl() uintptr { return hr(eNotImpl) }

// ---- what is being dragged ----

type dragSession struct {
	app    *App
	siteID int64
	items  []DragOutItem
	paths  []string // what is offered: the files themselves, or placeholders
	dir    string   // where the placeholders live, for a server drag
}

func (s *dragSession) remote() bool { return s.siteID != 0 }

// prepare fills in paths. For a server drag it writes one empty placeholder
// per file, named like the file.
func (s *dragSession) prepare() error {
	if !s.remote() {
		for _, it := range s.items {
			s.paths = append(s.paths, it.Path)
		}
		return nil
	}
	if err := os.MkdirAll(dragTempRoot(), 0o700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(dragTempRoot(), "d")
	if err != nil {
		return err
	}
	s.dir = dir
	for _, it := range s.items {
		p := filepath.Join(dir, safeName(it.Name))
		if it.IsDir {
			err = os.Mkdir(p, 0o700)
		} else if f, ferr := os.Create(p); ferr == nil {
			f.Close()
		} else {
			err = ferr
		}
		if err != nil {
			return err
		}
		s.paths = append(s.paths, p)
	}
	return nil
}

// safeName makes a server file name legal on Windows.
func safeName(n string) string {
	n = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, n)
	if n == "" {
		return "_"
	}
	return n
}

func hglobal(b []byte) (uintptr, error) {
	h, _, err := procGlobalAlloc.Call(0x2, uintptr(len(b))) // GMEM_MOVEABLE
	if h == 0 {
		return 0, err
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return 0, err
	}
	copy(unsafe.Slice((*byte)(ptr(p)), len(b)), b)
	procGlobalUnlock.Call(h)
	return h, nil
}

func dropFiles(paths []string) []byte {
	b := make([]byte, 20)
	binary.LittleEndian.PutUint32(b[0:], 20) // offset of the file list
	binary.LittleEndian.PutUint32(b[16:], 1) // wide characters
	for _, p := range paths {
		for _, u := range utf16.Encode([]rune(p)) {
			b = binary.LittleEndian.AppendUint16(b, u)
		}
		b = binary.LittleEndian.AppendUint16(b, 0)
	}
	return binary.LittleEndian.AppendUint16(b, 0)
}

// ---- the objects ----

type dropSource struct{ com }

type dataObject struct {
	com
	sess *dragSession
}

type formatEnum struct {
	com
	pos int
}

var (
	vtOnce                   sync.Once
	vtSource, vtData, vtEnum uintptr
	hdropFormat              = formatEtc{CF: cfHDrop, Aspect: 1, Lindex: -1, Tymed: tymedHGlobal}
)

func initVtables() {
	cb := syscall.NewCallback
	addRef, release := cb(comAddRef), cb(comRelease)

	vtSource = vtable(
		cb(func(this, riid, ppv uintptr) uintptr { return comQuery(this, riid, ppv, iidUnknown, iidDropSource) }),
		addRef, release,
		cb(func(this, esc, keys uintptr) uintptr {
			if uint32(esc) != 0 {
				return dragDropCancel
			}
			if uint32(keys)&1 == 0 { // MK_LBUTTON released
				return dragDropDrop
			}
			return sOK
		}),
		cb(func(this, effect uintptr) uintptr { return dragDropCursor }),
	)

	vtData = vtable(
		cb(func(this, riid, ppv uintptr) uintptr { return comQuery(this, riid, ppv, iidUnknown, iidDataObject) }),
		addRef, release,
		cb(dataGetData),
		cb(func(this, a, b uintptr) uintptr { return notImpl() }), // GetDataHere
		cb(dataQueryGetData),
		cb(func(this, in, out uintptr) uintptr { // GetCanonicalFormatEtc
			if out != 0 {
				(*formatEtc)(ptr(out)).Ptd = 0
			}
			return dataSSameFmt
		}),
		cb(func(this, a, b, c uintptr) uintptr { return notImpl() }), // SetData
		cb(func(this, dir, out uintptr) uintptr { // EnumFormatEtc
			if out == 0 {
				return hr(eInvalidArg)
			}
			if uint32(dir) != 1 { // DATADIR_GET only
				return notImpl()
			}
			*(*uintptr)(ptr(out)) = register(&formatEnum{}, vtEnum)
			return sOK
		}),
		cb(func(this, a, b, c, d uintptr) uintptr { return hr(oleEAdviseNoSp) }), // DAdvise
		cb(func(this, a uintptr) uintptr { return hr(oleEAdviseNoSp) }),          // DUnadvise
		cb(func(this, a uintptr) uintptr { return hr(oleEAdviseNoSp) }),          // EnumDAdvise
	)

	vtEnum = vtable(
		cb(func(this, riid, ppv uintptr) uintptr { return comQuery(this, riid, ppv, iidUnknown, iidEnumFmt) }),
		addRef, release,
		cb(func(this, celt, rgelt, fetched uintptr) uintptr { // Next
			e, _ := lookup(this).(*formatEnum)
			if e == nil || rgelt == 0 {
				return hr(eInvalidArg)
			}
			n := uint32(0)
			if e.pos == 0 && uint32(celt) > 0 {
				*(*formatEtc)(ptr(rgelt)) = hdropFormat
				e.pos, n = 1, 1
			}
			if fetched != 0 {
				*(*uint32)(ptr(fetched)) = n
			}
			if n == uint32(celt) {
				return sOK
			}
			return sFalse
		}),
		cb(func(this, n uintptr) uintptr { // Skip
			if e, _ := lookup(this).(*formatEnum); e != nil {
				e.pos = 1
			}
			return sOK
		}),
		cb(func(this uintptr) uintptr { // Reset
			if e, _ := lookup(this).(*formatEnum); e != nil {
				e.pos = 0
			}
			return sOK
		}),
		cb(func(this, out uintptr) uintptr { // Clone
			e, _ := lookup(this).(*formatEnum)
			if e == nil || out == 0 {
				return hr(eFail)
			}
			*(*uintptr)(ptr(out)) = register(&formatEnum{pos: e.pos}, vtEnum)
			return sOK
		}),
	)
}

func dataQueryGetData(this, pfe uintptr) uintptr {
	if pfe == 0 {
		return hr(eInvalidArg)
	}
	fe := (*formatEtc)(ptr(pfe))
	if fe.CF == cfHDrop && fe.Tymed&tymedHGlobal != 0 {
		return sOK
	}
	return hr(dvEFormatEtc)
}

func dataGetData(this, pfe, pmed uintptr) uintptr {
	d, _ := lookup(this).(*dataObject)
	if d == nil || pfe == 0 || pmed == 0 {
		return hr(eInvalidArg)
	}
	if dataQueryGetData(this, pfe) != sOK {
		return hr(dvEFormatEtc)
	}
	h, err := hglobal(dropFiles(d.sess.paths))
	if err != nil {
		return hr(eFail)
	}
	*(*stgMedium)(ptr(pmed)) = stgMedium{Tymed: tymedHGlobal, Data: h}
	return sOK
}

// ---- starting the drag ----

type keyInput struct {
	Type  uint32
	_     uint32
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	_     uint32
	Extra uintptr
	_     [8]byte
}

func leftButtonDown() bool {
	r, _, _ := procKeyState.Call(0x01) // VK_LBUTTON
	return r&0x8000 != 0
}

// cancelWebViewDrag presses and releases Escape, which ends the web view's own
// drag (still running while the pointer is outside the window) so a native
// one can take over with the mouse button still held.
func cancelWebViewDrag() {
	down := keyInput{Type: 1, Vk: 0x1B, Scan: 1}
	up := keyInput{Type: 1, Vk: 0x1B, Scan: 1, Flags: 2}
	// Held for a moment: the drag loop samples the key, so a tap can be missed.
	procSendInput.Call(1, uintptr(unsafe.Pointer(&down)), unsafe.Sizeof(down))
	time.Sleep(150 * time.Millisecond)
	procSendInput.Call(1, uintptr(unsafe.Pointer(&up)), unsafe.Sizeof(up))
}

/* The drag has to run on the window's own thread: the web view shares that
   thread's input queue, so only there does the drag loop see the mouse. The
   window procedure is wrapped once; a posted message makes the window thread
   run the drag. */

const wmRunDrag = 0x8000 + 0x57 // WM_APP + 0x57

var (
	hookMu     sync.Mutex
	hookedHwnd uintptr
	origProc   uintptr
	pendingRun *dragSession
)

func ownForegroundWindow() uintptr {
	h, _, _ := procGetFG.Call()
	if h == 0 {
		return 0
	}
	var pid uint32
	procGetWinPID.Call(h, uintptr(unsafe.Pointer(&pid)))
	if int(pid) != os.Getpid() {
		return 0
	}
	return h
}

func hookWindow(hwnd uintptr) {
	hookMu.Lock()
	defer hookMu.Unlock()
	if hookedHwnd == hwnd {
		return
	}
	proc := syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		if msg == wmRunDrag {
			hookMu.Lock()
			s := pendingRun
			pendingRun = nil
			hookMu.Unlock()
			if s != nil {
				runDrag(s)
			}
			return 0
		}
		r, _, _ := procCallWinProc.Call(origProc, h, msg, wp, lp)
		return r
	})
	old, _, _ := procSetWinLong.Call(hwnd, ^uintptr(3), proc) // GWLP_WNDPROC = -4
	if old == 0 {
		return
	}
	origProc, hookedHwnd = old, hwnd
}

// runDrag runs on the window thread, inside the window procedure.
func runDrag(sess *dragSession) {
	vtOnce.Do(initVtables)
	procOleInit.Call(0)
	if err := sess.prepare(); err != nil {
		log.Printf("drag-out: could not prepare files: %v", err)
		return
	}
	var watch *dropWatch
	if sess.remote() {
		watch = startDropWatch()
	}
	data := register(&dataObject{sess: sess}, vtData)
	src := register(&dropSource{}, vtSource)
	var effect uint32
	res, _, _ := procDoDragDrop.Call(data, src, dropEffectCopy, uintptr(unsafe.Pointer(&effect)))
	comRelease(data)
	comRelease(src)
	log.Printf("drag-out: finished, result=%#x effect=%d", uint32(res), effect)
	if !sess.remote() {
		return
	}
	if effect&dropEffectCopy == 0 {
		watch.stop()
		_ = os.RemoveAll(sess.dir)
		return
	}
	go sess.app.queueDroppedDownloads(sess, watch)
}

func (a *App) startNativeDrag(siteID int64, items []DragOutItem) error {
	sess := &dragSession{app: a, siteID: siteID, items: items}
	if len(sess.items) == 0 {
		return nil
	}
	hwnd := ownForegroundWindow()
	if hwnd == 0 || !leftButtonDown() {
		return nil
	}
	hookWindow(hwnd)
	go func() {
		cancelWebViewDrag()
		time.Sleep(30 * time.Millisecond)
		if !leftButtonDown() {
			return
		}
		hookMu.Lock()
		pendingRun = sess
		hookMu.Unlock()
		procPostMessage.Call(hwnd, wmRunDrag, 0, 0)
	}()
	return nil
}
