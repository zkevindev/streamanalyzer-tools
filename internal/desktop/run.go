package desktop

import (
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func Run() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "Stream Analyzer",
		Width:            1280,
		Height:           840,
		MinWidth:         1024,
		MinHeight:        720,
		BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 255},
		AssetServer: &assetserver.Options{
			Handler: Shell{App: app},
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
	})
	if err != nil {
		log.Fatal(err)
	}
}
