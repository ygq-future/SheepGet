package main

import (
	"embed"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

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
		OnShutdown: func() {
			app.Shutdown()
		},
	})

	app.SetApplication(wailsApp)

	// Create main window (Name: "main")
	mainWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
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

	// Closing main window hides it to system tray instead of exiting application
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		mainWindow.Hide()
	})

	// Pre-create independent FileInfo window (Name: "fileinfo")
	fileInfoWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:           "fileinfo",
		Title:          "新建下载 - SheepGet",
		Width:          460,
		Height:         300,
		Frameless:      true,
		BackgroundType: application.BackgroundTypeTransparent,
		DisableResize:  true,
		Hidden:         true,
		URL:            "/?window=fileinfo",
	})
	// Closing fileinfo window cancels current active item and advances queue
	fileInfoWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		_ = app.CancelCurrentFileInfo()
	})

	// Configure cross-platform system tray
	systemTray := wailsApp.SystemTray.New()
	systemTray.SetTooltip("SheepGet 下载管理器")
	if len(appIcon) > 0 {
		systemTray.SetIcon(appIcon)
	}
	systemTray.OnClick(func() {
		mainWindow.Show()
		mainWindow.Focus()
	})

	trayMenu := wailsApp.NewMenu()
	trayMenu.Add("显示主窗口").OnClick(func(_ *application.Context) {
		mainWindow.Show()
		mainWindow.Focus()
	})
	trayMenu.Add("新建下载").OnClick(func(_ *application.Context) {
		_, _ = app.OpenNewDownload()
	})
	trayMenu.AddSeparator()
	trayMenu.Add("退出").OnClick(func(_ *application.Context) {
		wailsApp.Quit()
	})
	systemTray.SetMenu(trayMenu)
	err := wailsApp.Run()
	if err != nil {
		println("Error:", err.Error())
	}
}
