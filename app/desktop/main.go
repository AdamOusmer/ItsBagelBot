// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"ItsBagelBot/app/desktop/internal/overlay"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed all:frontend/obs-dist
var obsAssets embed.FS

func applicationMenu() *application.Menu {
	menu := application.NewMenu()
	if runtime.GOOS == "darwin" {
		menu.AddRole(application.AppMenu)
	}
	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)
	return menu
}

func main() {
	config, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	renderer, err := fs.Sub(obsAssets, "frontend/obs-dist")
	if err != nil {
		log.Fatal(err)
	}
	source, err := overlay.OpenSource(filepath.Join(config, "ItsBagelBot", "obs"))
	if err != nil {
		log.Fatal(err)
	}

	server, err := overlay.NewServer(source, renderer)
	if err != nil {
		log.Fatal(err)
	}

	bridge := &BridgeService{}
	auth := &AuthService{}
	app := application.New(application.Options{
		Name:        "ItsBagelBot",
		Services:    []application.Service{application.NewService(&OverlayService{source: source, server: server}), application.NewService(bridge), application.NewService(auth)},
		Description: "The free Twitch chat bot.",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	auth.config = nativeAuthConfig(app.Browser.OpenURL, bridge)

	app.Menu.Set(applicationMenu())

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:                      "ItsBagelBot",
		Width:                      1280,
		Height:                     820,
		MinWidth:                   480,
		MinHeight:                  640,
		DefaultContextMenuDisabled: true,
		Mac: application.MacWindow{
			Appearance: application.NSAppearanceNameDarkAqua,
			TitleBar:   application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(17, 17, 19),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
