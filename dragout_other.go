//go:build !windows

package main

import "errors"

func (a *App) startNativeDrag(int64, []DragOutItem) error {
	return errors.New("dragging out is only available on Windows")
}
