package main

import (
	"embed"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	appevents "sheep-get/internal/events"
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

	app.setApplication(wailsApp)

	// Create main window (Name: "main")
	mainWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      winNameMain,
		Title:     "sheep-get",
		Width:     800,
		Height:    520,
		MinWidth:  800,
		MinHeight: 520,
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
		Name:           winNameFileInfo,
		Title:          "新建下载 - SheepGet",
		Width:          fileInfoWindowWidth,
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
	// Pre-create independent Progress window (Name: "progress")
	var (
		progX       = 0
		progY       = 0
		progInitPos = application.WindowCentered
	)
	if primary := wailsApp.Screen.GetPrimary(); primary != nil && primary.WorkArea.Width > 0 && primary.WorkArea.Height > 0 {
		progX = primary.WorkArea.X + primary.WorkArea.Width - progressWindowWidth - progressWindowEdgeGap
		progY = primary.WorkArea.Y + primary.WorkArea.Height - progressWindowBottomOffset - progressWindowEdgeGap
		progInitPos = application.WindowXY
	}
	progressWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            winNameProgress,
		Title:           "下载进度 - SheepGet",
		Width:           progressWindowWidth,
		Height:          progressWindowHeight,
		MinWidth:        progressWindowMinWidth,
		MaxWidth:        progressWindowMaxWidth,
		MinHeight:       progressWindowMinH,
		MaxHeight:       progressWindowMaxH,
		InitialPosition: progInitPos,
		X:               progX,
		Y:               progY,
		Frameless:       true,
		BackgroundType:  application.BackgroundTypeTransparent,
		DisableResize:   false,
		Hidden:          true,
		URL:             "/?window=progress",
	})
	// Closing progress window hides it instead of terminating
	progressWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		progressWindow.Hide()
		wailsApp.Event.Emit(appevents.ProgressClearViewed)
	})

	// Configure cross-platform system tray
	systemTray := wailsApp.SystemTray.New()
	systemTray.SetTooltip("SheepGet 下载管理器")
	if len(appIcon) > 0 {
		systemTray.SetIcon(appIcon)
	}
	systemTray.OnClick(func() {
		showAndRaise(mainWindow)
	})

	trayMenu := wailsApp.NewMenu()
	trayMenu.Add("显示主窗口").OnClick(func(_ *application.Context) {
		showAndRaise(mainWindow)
	})
	trayMenu.Add("偏好设置").OnClick(func(_ *application.Context) {
		showAndRaise(mainWindow)
		wailsApp.Event.Emit(appevents.AppOpenSettings)
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
