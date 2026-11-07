// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"embed"
	"log"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

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
	app := application.New(application.Options{
		Name:        "ItsBagelBot",
		Description: "The free Twitch chat bot.",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

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
