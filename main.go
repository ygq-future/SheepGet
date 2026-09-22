package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
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
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
			AdditionalBrowserArgs:         windowsAdditionalBrowserArgs,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		OnShutdown: func() {
			app.Shutdown()
		},
	})

	app.setApplication(wailsApp)

	st := app.GetSettings()
	isSilent := st.General.SilentStartup
	for _, arg := range os.Args[1:] {
		if arg == "--silent" || arg == "-s" {
			isSilent = true
			break
		}
	}

	// 主窗口生命周期策略：
	// 若处于静默启动且开启了轻量模式，则启动时不创建主窗口 WebView 渲染进程；
	// 若非静默启动，则立即创建并展示主窗口；
	// 若静默启动但未开启轻量模式，则创建主窗口但初始保持隐藏。
	if !isSilent {
		app.ShowMainWindow()
	} else if !st.General.LightweightMode {
		app.ensureMainWindow(true)
	}
	// Configure cross-platform system tray
	systemTray := wailsApp.SystemTray.New()
	systemTray.SetTooltip("SheepGet 下载管理器")
	if len(appIcon) > 0 {
		systemTray.SetIcon(appIcon)
	}
	systemTray.OnClick(func() {
		app.ShowMainWindow()
	})

	trayMenu := wailsApp.NewMenu()
	trayMenu.Add("显示主窗口").OnClick(func(_ *application.Context) {
		app.ShowMainWindow()
	})
	trayMenu.Add("偏好设置").OnClick(func(_ *application.Context) {
		app.OpenSettingsWindow()
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
