package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

func runGUI(assets embed.FS) {
	if !preflight() {
		return
	}
	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "Cadastre",
		Width:     1360,
		Height:    860,
		MinWidth:  960,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 19, B: 24, A: 1},
		OnStartup:        app.startup,
		DragAndDrop:      &options.DragAndDrop{EnableFileDrop: true},
		Windows:          &windows.Options{Theme: windows.SystemDefault},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		fatal("Cadastre", err.Error())
	}
}
