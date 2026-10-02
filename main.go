package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "warpseed",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 18, G: 19, B: 20, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		// NOT HideWindowOnClose: that branch skips OnBeforeClose entirely, and
		// with no tray in Wails v2 a hidden window is one only Task Manager
		// can find.
		OnBeforeClose: app.beforeClose,
		// A second launch brings the running one forward instead of opening a
		// second window onto the same database.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "tech.zyralabs.warpseed",
			OnSecondInstanceLaunch: app.secondInstance,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
