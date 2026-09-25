// 窗口生命周期在桌面端的落点：三个独立窗口的声明、宿主适配器，以及面向界面与托盘的那几个操作。
//
// 策略本身住在 internal/windowing（可注入假窗口与假时钟做单测）；这个文件是唯一知道 Wails
// 窗口类型的地方——声明用桌面端自己的词汇写，翻译成 WebviewWindowOptions 只发生在这里。
package main

import (
	"net/url"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"sheep-get/internal/config"
	"sheep-get/internal/protocol"
	"sheep-get/internal/window"
	"sheep-get/internal/windowing"
)

// windowsHost 把注册表的声明翻译成 Wails 窗口。
type windowsHost struct {
	app *App
}

func (h *windowsHost) Open(options windowing.Options, onClose func() bool) (windowing.Window, bool) {
	wailsApp := h.app.getApp()
	if wailsApp == nil {
		return nil, false
	}
	win := wailsApp.Window.NewWithOptions(webviewOptions(options))
	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if onClose() {
			event.Cancel()
		}
	})
	return &wailsWindow{win: win}, true
}

func (h *windowsHost) Find(name string) (windowing.Window, bool) {
	wailsApp := h.app.getApp()
	if wailsApp == nil {
		return nil, false
	}
	win, ok := wailsApp.Window.GetByName(name)
	if !ok {
		return nil, false
	}
	return &wailsWindow{win: win}, true
}

func (h *windowsHost) WorkArea() (windowing.Rect, bool) {
	wailsApp := h.app.getApp()
	if wailsApp == nil {
		return windowing.Rect{}, false
	}
	primary := wailsApp.Screen.GetPrimary()
	if primary == nil || primary.WorkArea.Width <= 0 || primary.WorkArea.Height <= 0 {
		return windowing.Rect{}, false
	}
	return windowing.Rect{
		X:      primary.WorkArea.X,
		Y:      primary.WorkArea.Y,
		Width:  primary.WorkArea.Width,
		Height: primary.WorkArea.Height,
	}, true
}

type wailsWindow struct {
	win application.Window
}

func (w *wailsWindow) Show()                     { window.ShowAndRaise(w.win) }
func (w *wailsWindow) Hide()                     { w.win.Hide() }
func (w *wailsWindow) Raise()                    { window.Raise(w.win) }
func (w *wailsWindow) Minimise()                 { w.win.Minimise() }
func (w *wailsWindow) Close()                    { w.win.Close() }
func (w *wailsWindow) IsVisible() bool           { return w.win.IsVisible() }
func (w *wailsWindow) SetPosition(x, y int)      { w.win.SetPosition(x, y) }
func (w *wailsWindow) SetAlwaysOnTop(on bool)    { w.win.SetAlwaysOnTop(on) }
func (w *wailsWindow) SetSize(width, height int) { w.win.SetSize(width, height) }

func (w *wailsWindow) SetBackground(colour windowing.Colour) {
	w.win.SetBackgroundColour(application.RGBA{
		Red:   colour.R,
		Green: colour.G,
		Blue:  colour.B,
		Alpha: colour.A,
	})
}

func webviewOptions(o windowing.Options) application.WebviewWindowOptions {
	options := application.WebviewWindowOptions{
		Name:            o.Name,
		Title:           o.Title,
		URL:             o.URL,
		Width:           o.Width,
		Height:          o.Height,
		MinWidth:        o.MinWidth,
		MinHeight:       o.MinHeight,
		MaxWidth:        o.MaxWidth,
		MaxHeight:       o.MaxHeight,
		Frameless:       o.Frameless,
		DisableResize:   o.DisableResize,
		Hidden:          o.Hidden,
		InitialPosition: application.WindowCentered,
	}
	if o.DisableMaximise {
		options.MaximiseButtonState = application.ButtonDisabled
	}
	if o.Positioned {
		options.InitialPosition = application.WindowXY
		options.X, options.Y = o.X, o.Y
	}
	if o.Transparent {
		options.BackgroundType = application.BackgroundTypeTransparent
	}
	if o.Background.A != 0 {
		options.BackgroundColour = application.RGBA{
			Red:   o.Background.R,
			Green: o.Background.G,
			Blue:  o.Background.B,
			Alpha: o.Background.A,
		}
	}
	return options
}

// declareWindows 登记三个窗口：它们的声明与生命周期策略都写在这里，每窗口一份。
func (a *App) declareWindows(host windowing.Host) {
	a.windows = windowing.New(host, realClock{}, windowIdleDestroyGracePeriod)
	a.windows.Declare(
		windowing.Spec{
			Options: windowing.Options{
				Name:            winNameMain,
				Title:           "SheepGet",
				URL:             "/",
				Width:           mainWindowWidth,
				Height:          mainWindowHeight,
				MinWidth:        mainWindowWidth,
				MinHeight:       mainWindowHeight,
				DisableMaximise: true,
				Background:      a.mainWindowBackground(),
			},
			Policy: windowing.Policy{
				// 轻量模式不拦关闭，让 Wails 销毁窗口与 WebView 渲染进程；其余情况关到托盘。
				OnClose: func(r *windowing.Registry) windowing.Direction {
					if a.lightweightMode() {
						return windowing.AllowClose
					}
					if win, ok := r.Find(winNameMain); ok {
						win.Hide()
					}
					return windowing.CancelClose
				},
			},
		},
		windowing.Spec{
			Options: windowing.Options{
				Name:          winNameFileInfo,
				Title:         "新建下载 - SheepGet",
				URL:           "/?window=fileinfo",
				Width:         fileInfoWindowWidth,
				Height:        fileInfoWindowHeight,
				Frameless:     true,
				Transparent:   true,
				DisableResize: true,
			},
			Policy: windowing.Policy{
				// 关掉这个窗口就是「不要这次下载」：把当前项取消掉，窗口留着由队列决定何时隐藏。
				OnClose: func(*windowing.Registry) windowing.Direction {
					_ = a.CancelCurrentFileInfo()
					return windowing.CancelClose
				},
				// 队列里还有等待确认的项时不能被闲置销毁丢掉。
				Busy:        func() bool { return a.fileInfoQueueLength() > 0 },
				IdleDestroy: a.lightweightMode,
			},
		},
		windowing.Spec{
			Options: windowing.Options{
				Name:        winNameProgress,
				Title:       "下载进度 - SheepGet",
				URL:         "/?window=progress",
				Width:       progressWindowWidth,
				Height:      progressWindowHeight,
				MinWidth:    progressWindowMinWidth,
				MaxWidth:    progressWindowMaxWidth,
				MinHeight:   progressWindowMinH,
				MaxHeight:   progressWindowMaxH,
				Frameless:   true,
				Transparent: true,
			},
			Policy: windowing.Policy{
				// 停靠在主屏右下角：从默认高度向上展开时有足够空间，不至于顶出屏幕。
				Position: func(work windowing.Rect) (int, int, bool) {
					x := work.X + work.Width - progressWindowWidth - progressWindowEdgeGap
					y := work.Y + work.Height - progressWindowBottomOffset - progressWindowEdgeGap
					return x, y, true
				},
				OnClose: func(r *windowing.Registry) windowing.Direction {
					if win, ok := r.Find(winNameProgress); ok {
						win.Hide()
					}
					a.emit(protocol.EventProgressClearViewed, nil)
					r.ScheduleIdleDestroy(winNameProgress)
					return windowing.CancelClose
				},
				IdleDestroy: a.lightweightMode,
				// 销毁之后下次显示要重新贴回屏幕右下角。
				OnIdleDestroy: func(r *windowing.Registry) { r.ResetPosition(winNameProgress) },
			},
		},
	)
}

// realClock 是注册表在运行期的时钟。
type realClock struct{}

func (realClock) AfterFunc(d time.Duration, f func()) windowing.Timer {
	return time.AfterFunc(d, f)
}

func (a *App) lightweightMode() bool {
	if a.settings == nil {
		return false
	}
	return a.settings.Get().General.LightweightMode
}

func (a *App) mainWindowBackground() windowing.Colour {
	if a.settings != nil && a.settings.Get().Appearance.Theme == config.ThemeLight {
		return mainWindowLightBackgroundColour
	}
	return mainWindowDarkBackgroundColour
}

// ensureMainWindow 保证主窗口存在。hidden 只影响创建那一刻：静默启动时先建出来但不显示。
func (a *App) ensureMainWindow(hidden bool, urlPath ...string) windowing.Window {
	target := "/"
	if len(urlPath) > 0 && urlPath[0] != "" {
		target = urlPath[0]
	}
	win, _ := a.windows.Ensure(winNameMain, target, hidden)
	return win
}

// ShowMainWindow makes the main window visible and brings it to focus, creating it if needed.
func (a *App) ShowMainWindow() {
	if _, ok := a.windows.Show(winNameMain, "/"); !ok {
		return
	}
}

// OpenSettingsWindow 打开主窗口并让界面切到设置页。
func (a *App) OpenSettingsWindow() {
	if _, ok := a.windows.Find(winNameMain); ok {
		a.windows.Show(winNameMain, "")
		a.emit(protocol.EventAppOpenSettings, nil)
		return
	}
	if win := a.ensureMainWindow(false, "/?open=settings"); win != nil {
		a.windows.Show(winNameMain, "")
		a.emit(protocol.EventAppOpenSettings, nil)
	}
}

// ShowProgressWindow brings up or focuses the shared download progress window and highlights the task.
func (a *App) ShowProgressWindow(taskID string) {
	openURL := "/?window=progress"
	if taskID != "" {
		openURL += "&focus=" + url.QueryEscape(taskID)
	}
	win, ok := a.windows.Show(winNameProgress, openURL)
	if !ok {
		return
	}
	// 置顶开关是运行期状态，创建时也要跟着当前值走。
	win.SetAlwaysOnTop(a.progressAlwaysOnTop)
	if taskID != "" {
		a.emit(protocol.EventProgressFocusCompleted, taskID)
		a.emit(protocol.EventProgressFocusTask, taskID)
	}
}

// MinimiseFileInfoWindow minimises the file info window.
func (a *App) MinimiseFileInfoWindow() {
	if win, ok := a.windows.Find(winNameFileInfo); ok {
		win.Minimise()
	}
}

// SetFileInfoWindowHeight dynamically adjusts the fileinfo window's height to wrap its content.
func (a *App) SetFileInfoWindowHeight(height int) {
	if win, ok := a.windows.Find(winNameFileInfo); ok {
		win.SetSize(fileInfoWindowWidth, clamp(height, fileInfoWindowMinH, fileInfoWindowMaxH))
	}
}

// SetProgressWindowHeight adjusts the progress window's height to wrap its content.
// 高度由内容决定：只有一个任务卡片时窗口就收成一张卡片的高度，不套用内容意义上的下限，
// 只有超过上限时才封顶（超出部分由窗口内部滚动）。
func (a *App) SetProgressWindowHeight(height int) {
	if win, ok := a.windows.Find(winNameProgress); ok {
		win.SetSize(progressWindowWidth, clamp(height, progressWindowMinH, progressWindowMaxH))
	}
}

// MinimiseProgressWindow minimises the progress window.
func (a *App) MinimiseProgressWindow() {
	if win, ok := a.windows.Find(winNameProgress); ok {
		win.Minimise()
	}
}

// HideProgressWindow hides the progress window.
func (a *App) HideProgressWindow() {
	win, ok := a.windows.Find(winNameProgress)
	if !ok {
		return
	}
	win.Hide()
	a.emit(protocol.EventProgressClearViewed, nil)
	a.windows.ScheduleIdleDestroy(winNameProgress)
}

// ToggleProgressWindowAlwaysOnTop toggles whether the progress window is always on top.
func (a *App) ToggleProgressWindowAlwaysOnTop() bool {
	a.progressAlwaysOnTop = !a.progressAlwaysOnTop
	if win, ok := a.windows.Find(winNameProgress); ok {
		win.SetAlwaysOnTop(a.progressAlwaysOnTop)
	}
	return a.progressAlwaysOnTop
}

// IsProgressWindowAlwaysOnTop reports whether the progress window is set to always on top.
func (a *App) IsProgressWindowAlwaysOnTop() bool {
	return a.progressAlwaysOnTop
}

// emit 把事件推给前端；宿主还没就绪时什么也不做（与各处原本的判空一致）。
func (a *App) emit(event string, data any) {
	if wailsApp := a.getApp(); wailsApp != nil {
		wailsApp.Event.Emit(event, data)
	}
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// wailsWindowView 是文件信息窗口队列看到的窗口：队列只管显示、隐藏、置前与发事件，
// 具体是哪个窗口、什么时候建，都由窗口注册表的声明与策略决定。
type wailsWindowView struct {
	app  *App
	name string
}

func (w *wailsWindowView) Show() {
	if w.app == nil {
		return
	}
	w.app.windows.Show(w.name, "")
}

func (w *wailsWindowView) Hide() {
	if w.app == nil {
		return
	}
	w.app.windows.Hide(w.name)
}

func (w *wailsWindowView) Focus() {
	if w.app == nil {
		return
	}
	if win, ok := w.app.windows.Find(w.name); ok {
		win.Raise()
	}
}

func (w *wailsWindowView) Emit(event string, data any) {
	if w.app == nil {
		return
	}
	w.app.emit(event, data)
}
