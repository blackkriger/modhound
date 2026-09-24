package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/blackkriger/modhound/internal/config"
	"github.com/blackkriger/modhound/internal/httpx"
	"github.com/blackkriger/modhound/internal/icons"
)

var Version = "dev"

const (
	windowWidth   = 920
	windowHeight  = 600
	consoleHeight = 220
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	httpx.UserAgent = "modhound/" + Version + " (github.com/blackkriger/modhound)"
	store, err := config.Open()
	if err != nil {
		log.Fatal(err)
	}
	app := NewApp(store)
	err = wails.Run(&options.App{
		Title:            "modhound",
		Width:            windowWidth,
		Height:           windowHeight,
		DisableResize:    true,
		Frameless:        true,
		AssetServer:      &assetserver.Options{Assets: assets, Handler: icons.Handler()},
		BackgroundColour: &options.RGBA{R: 22, G: 22, B: 22, A: 255},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		Bind:             []interface{}{app},
		Windows:          &windows.Options{Theme: windows.SystemDefault},
	})
	if err != nil {
		log.Fatal(err)
	}
}
