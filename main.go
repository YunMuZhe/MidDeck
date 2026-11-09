package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		os.Exit(runMCP())
	}
	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "MidDeck",
		Width:     1360,
		Height:    860,
		MinWidth:  1024,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 240, G: 241, B: 244, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "MidDeck",
				Message: "One deck for all your middleware.\nhttps://github.com/YunMuZhe/MidDeck",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
