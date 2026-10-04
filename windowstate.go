package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

/* The window's size and whether it was maximised, remembered across launches.
 * A small file beside the log rather than a setting: it has to be read before
 * the window exists, and the database is only opened once the window is up. */

const (
	defaultWinW, defaultWinH = 1280, 800
	minWinW, minWinH         = 900, 560
)

type windowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

func windowStatePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "window.json"), nil
}

// loadWindowState returns the saved size, or the defaults when there is none or
// it is not usable. A size below the window's minimum is the pill's, not ours.
func loadWindowState() windowState {
	st := windowState{Width: defaultWinW, Height: defaultWinH}
	path, err := windowStatePath()
	if err != nil {
		return st
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	var saved windowState
	if json.Unmarshal(b, &saved) != nil || saved.Width < minWinW || saved.Height < minWinH || saved.Width > 16000 || saved.Height > 16000 {
		return st
	}
	return saved
}

// RememberWindow is called by the page a moment after the window stops
// changing size. It records the size the window would be reopened at.
func (a *App) RememberWindow() {
	if a.ctx == nil {
		return
	}
	a.mu.Lock()
	mini := a.mini
	a.mu.Unlock()
	if mini || wruntime.WindowIsMinimised(a.ctx) || wruntime.WindowIsFullscreen(a.ctx) {
		return
	}
	path, err := windowStatePath()
	if err != nil {
		return
	}
	st := loadWindowState()
	if wruntime.WindowIsMaximised(a.ctx) {
		// Keep the size from before it was maximised, so un-maximising next
		// launch lands somewhere sensible.
		st.Maximised = true
	} else {
		w, h := wruntime.WindowGetSize(a.ctx)
		if w < minWinW || h < minWinH {
			return
		}
		st = windowState{Width: w, Height: h}
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, b, 0o600)
}
