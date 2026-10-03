//go:build windows

package main

import (
	"sync/atomic"
	"time"
	"unsafe"
)

/* Where the pointer is while a drag leaves and re-enters the window.

   A drag only becomes a native one once the pointer has really gone away from
   the window, so brushing the edge by accident leaves the in-window drag (and
   all its indicators) alone. While a native drag runs, the pointer's position
   inside the window is streamed to the page, so the panes can keep showing
   where a drop would land. */

var (
	procGetCursorPos    = user32.NewProc("GetCursorPos")
	procGetClientRect   = user32.NewProc("GetClientRect")
	procClientToScreen  = user32.NewProc("ClientToScreen")
	procGetDpiForWindow = user32.NewProc("GetDpiForWindow")
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }

// dragOutBusy is set from the moment a drag-out starts watching the pointer
// until the native drag has ended, so repeated edge crossings do not stack.
var dragOutBusy atomic.Bool

// handoffDistance is how far outside the window, in pixels, the pointer must
// go before the drag is handed to Windows.
const handoffDistance = 24

func cursorPos() point {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

// clientScreenRect is the page area of the window in screen coordinates.
func clientScreenRect(hwnd uintptr) rect {
	var r rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	tl := point{r.Left, r.Top}
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&tl)))
	return rect{tl.X, tl.Y, tl.X + r.Right - r.Left, tl.Y + r.Bottom - r.Top}
}

// outsideBy is 0 for a point inside the rectangle, else how far outside it is.
func outsideBy(p point, r rect) int32 {
	d := int32(0)
	for _, v := range []int32{r.Left - p.X, p.X - r.Right, r.Top - p.Y, p.Y - r.Bottom} {
		d = max(d, v)
	}
	return d
}

// dpiScale is the window's pixels per CSS pixel.
func dpiScale(hwnd uintptr) float64 {
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// commitToNativeDrag waits until the pointer has clearly left the window and
// reports true, or false if it came back, or the button was let go, first.
func commitToNativeDrag(hwnd uintptr) bool {
	deadline := time.Now().Add(3 * time.Second)
	inside := 0
	for time.Now().Before(deadline) {
		if !leftButtonDown() {
			return false
		}
		d := outsideBy(cursorPos(), clientScreenRect(hwnd))
		switch {
		case d >= handoffDistance:
			return true
		case d == 0:
			// At the edge for a moment is normal as the pointer leaves; staying
			// inside means it never really left.
			if inside++; inside >= 4 {
				return false
			}
		default:
			inside = 0
		}
		time.Sleep(15 * time.Millisecond)
	}
	return leftButtonDown()
}

// streamCursor tells the page where the pointer is in the window until stop is
// closed: {x, y} in CSS pixels, or null while it is outside.
func (a *App) streamCursor(hwnd uintptr, stop <-chan struct{}) {
	tick := time.NewTicker(33 * time.Millisecond)
	defer tick.Stop()
	scale := dpiScale(hwnd)
	var last point
	outside := true
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		p := cursorPos()
		r := clientScreenRect(hwnd)
		if outsideBy(p, r) > 0 {
			if !outside {
				outside = true
				a.sink.Emit("dragout:pos", nil)
			}
			continue
		}
		if !outside && p == last {
			continue
		}
		outside, last = false, p
		a.sink.Emit("dragout:pos", map[string]float64{
			"x": float64(p.X-r.Left) / scale,
			"y": float64(p.Y-r.Top) / scale,
		})
	}
}
