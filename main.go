package main

import (
	"embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	wailsApp := application.New(application.Options{
		Name:        "sheep-get",
		Description: "Modern Desktop Download Manager",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})

	app.SetApplication(wailsApp)

	// Create main window
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "sheep-get",
		Width:  1024,
		Height: 768,
		BackgroundColour: application.RGBA{
			Red:   27,
			Green: 38,
			Blue:  54,
			Alpha: 255,
		},
		URL: "/",
	})

	err := wailsApp.Run()
	if err != nil {
		println("Error:", err.Error())
	}
}
