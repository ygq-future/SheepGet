package main

import (
	"context"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sheep-get/internal/browser"
	"sheep-get/internal/clipboard"
	"sheep-get/internal/config"
	"sheep-get/internal/credentials"
	"sheep-get/internal/duplicate"
	"sheep-get/internal/engine"
	"sheep-get/internal/logging"
	"sheep-get/internal/protocol"
	"sheep-get/internal/server"
	"sheep-get/internal/storage"
	"sheep-get/internal/sys"
	"sheep-get/internal/task"
	"sheep-get/internal/window"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// FileConflictResult represents whether target file exists and suggests an alternative filename.
type FileConflictResult struct {
	Exists               bool   `json:"exists"`
	SuggestedFilename    string `json:"suggestedFilename"`
	ExistingPath         string `json:"existingPath,omitempty"`
	ExistingTaskID       string `json:"existingTaskID,omitempty"`
	CanReuseExistingFile bool   `json:"canReuseExistingFile,omitempty"`
}

// DestinationInfo describes the resolved save location of a download: its directory and
// the category that matched the filename.
type DestinationInfo struct {
	Directory  string `json:"directory"`
	CategoryID string `json:"categoryId"`
}

// App struct
type App struct {
	app                 *application.App
	ctx                 context.Context
	manager             *engine.Manager
	store               task.TaskStore
	storage             *storage.Storage
	settings            *config.SettingsService
	windowQueue         *window.QueueController
	progressPositioned  bool
	progressAlwaysOnTop bool
	clipboardWatcher    *clipboard.Watcher
	loopbackServer      *server.Server
	logger              atomic.Pointer[logging.Logger]
	logError            error
	windowTimerLock     sync.Mutex
	fileInfoTimer       *time.Timer
	progressTimer       *time.Timer
	destroyingWindows   map[string]bool
}

type wailsWindowView struct {
	app  *App
	name string
}

// log 返回日志出口；尚未接上日志（测试直接构造 App）时丢弃日志。
func (a *App) log() *logging.Logger {
	if logger := a.logger.Load(); logger != nil {
		return logger
	}
	return logging.Discard()
}

// applyLogging 按设置开关日志文件。默认关闭：关闭时不打开、不创建文件；开启时按需打开，
// 并把出口换到引擎上。打开失败（目录只读、磁盘满）只影响日志，不影响下载。
//
// 出口用原子指针存放：开关随用户操作在设置线程上切换，而读它的地方分散在启动、退出与
// 环回服务的回调里，不该靠「谁先谁后」来保证不出竞争。
func (a *App) applyLogging(enabled bool) {
	current := a.logger.Load()
	if !enabled {
		if current != nil && current.Path() != "" {
			_ = current.Close()
		}
		a.logger.Store(logging.Discard())
		a.logError = nil
		if a.manager != nil {
			a.manager.SetLogger(logging.Discard().Logger)
		}
		return
	}

	if current != nil && current.Path() != "" {
		return // 已经在记录
	}
	if a.storage == nil {
		return
	}

	logger, err := logging.New(a.storage.LogsDir())
	if err != nil {
		a.logError = err
		fmt.Fprintf(os.Stderr, "failed to open log file: %v\n", err)
		return
	}
	a.logger.Store(logger)
	a.logError = nil
	if a.manager != nil {
		a.manager.SetLogger(logger.Logger)
	}
}

func (w *wailsWindowView) Show() {
	if w.app == nil {
		return
	}
	w.app.cancelWindowIdleDestroy(w.name)
	wailsApp := w.app.getApp()
	if wailsApp == nil {
		return
	}
	if win, ok := wailsApp.Window.GetByName(w.name); ok {
		window.ShowAndRaise(win)
		return
	}
	if w.name == winNameFileInfo {
		fileInfoWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:           winNameFileInfo,
			Title:          "新建下载 - SheepGet",
			Width:          fileInfoWindowWidth,
			Height:         fileInfoWindowHeight,
			Frameless:      true,
			BackgroundType: application.BackgroundTypeTransparent,
			DisableResize:  true,
			URL:            "/?window=fileinfo",
		})
		fileInfoWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			if w.app.isWindowDestroying(winNameFileInfo) {
				return
			}
			event.Cancel()
			_ = w.app.CancelCurrentFileInfo()
		})
		window.ShowAndRaise(fileInfoWindow)
	}
}

func (w *wailsWindowView) Hide() {
	if w.app == nil {
		return
	}
	wailsApp := w.app.getApp()
	if wailsApp == nil {
		return
	}
	if win, ok := wailsApp.Window.GetByName(w.name); ok {
		win.Hide()
		if w.name == winNameFileInfo {
			w.app.scheduleWindowIdleDestroy(w.name)
		}
	}
}

func (w *wailsWindowView) Focus() {
	if w.app == nil {
		return
	}
	wailsApp := w.app.getApp()
	if wailsApp != nil {
		if win, ok := wailsApp.Window.GetByName(w.name); ok {
			window.Raise(win)
		}
	}
}

func (w *wailsWindowView) Emit(event string, data any) {
	if w.app == nil {
		return
	}
	wailsApp := w.app.getApp()
	if wailsApp != nil {
		wailsApp.Event.Emit(event, data)
	}
}

// NewApp creates a new App application struct
func NewApp() *App {
	storeDir, err := storage.ResolveDataDir("")
	if err != nil {
		panic(fmt.Sprintf("failed to resolve storage: %v", err))
	}

	store, err := task.NewFileTaskStore(storeDir.TasksDB())
	if err != nil {
		panic(fmt.Sprintf("failed to init task store: %v", err))
	}

	defaultDownloadDir := sys.DefaultDownloadDir()
	defaultTempDir := storeDir.TempDir()

	app := &App{
		store:   store,
		storage: storeDir,
	}

	settingsSvc := config.NewSettingsService(
		storeDir.ConfigFile(),
		defaultDownloadDir,
		defaultTempDir,
		func(updated *config.Settings) error {
			return app.OnSettingsUpdated(updated)
		},
	)
	app.settings = settingsSvc

	activeSettings := settingsSvc.Get()
	downloader := engine.NewHTTPDownloader(nil)
	downloader.SetTempDirectory(activeSettings.Download.TempDirectory)
	downloader.SetUseServerFileTime(activeSettings.Download.UseServerFileTime)
	// 配置层已保证自定义代理地址可解析；这里失败说明默认值或校验被破坏，
	// 与上面的初始化失败同等处理，不能让不生效的代理留在运行中的下载器上。
	if err := downloader.SetProxy(string(activeSettings.Proxy.Mode), activeSettings.Proxy.CustomAddr); err != nil {
		panic(fmt.Sprintf("failed to apply stored proxy settings: %v", err))
	}

	mgr := engine.NewManager(store, downloader, engine.Config{
		MaxActiveTasks:            activeSettings.Download.MaxConcurrentDownloads,
		TempDirectory:             activeSettings.Download.TempDirectory,
		UseServerFileTime:         activeSettings.Download.UseServerFileTime,
		DefaultConnectionsPerTask: activeSettings.Download.DefaultConnectionsPerTask,
	})
	app.manager = mgr

	// 日志默认关闭：正常使用不落盘，只有打开开关才写文件（写不出来时降级为丢弃，不拦下载）。
	app.applyLogging(activeSettings.General.EnableLogging)
	startupFields := []any{
		"mode", string(storeDir.Mode),
		"dataDir", storeDir.DataDir,
		"log", app.log().Path(),
	}
	logger := app.log()
	if logErr := app.logError; logErr != nil {
		startupFields = append(startupFields, "logError", logErr.Error())
	}
	logger.Info("应用启动", startupFields...)

	winView := &wailsWindowView{
		app:  app,
		name: winNameFileInfo,
	}
	app.windowQueue = window.NewQueueController(mgr, settingsSvc, winView)
	adapter := &loopbackServerAdapter{app: app}
	loopbackSrv := server.NewServer(storeDir.SessionFile(), adapter, adapter)
	st := settingsSvc.Get()
	if st.General.ServerPort > 0 {
		loopbackSrv.SetTargetPort(st.General.ServerPort)
	} else {
		loopbackSrv.SetTargetPort(protocol.PortDefaultServer)
	}
	loopbackSrv.SetOnStatusChange(func(status server.Status) {
		if wailsApp := app.getApp(); wailsApp != nil {
			wailsApp.Event.Emit(protocol.EventServerStatusChanged, status)
		}
	})
	app.loopbackServer = loopbackSrv
	return app
}

// SetApplication sets the Wails application reference
func (a *App) setApplication(app *application.App) {
	a.app = app
	if a.clipboardWatcher == nil && app != nil && app.Clipboard != nil {
		a.clipboardWatcher = clipboard.NewWatcher(
			app.Clipboard,
			a.GetSettings,
			func(urlStr string) {
				_, _ = a.TriggerDownload(window.DownloadRequest{URL: urlStr})
			},
		)
		st := a.GetSettings()
		if st.Clipboard.Enabled {
			a.clipboardWatcher.Start()
		}
	}
}
func (a *App) getApp() *application.App {
	if a.app != nil {
		return a.app
	}
	return application.Get()
}

// ServiceStartup is called when Wails v3 service initializes
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.startup(ctx)
	return nil
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.manager.AddListener(a)
	if a.loopbackServer != nil {
		if err := a.loopbackServer.Start(); err != nil {
			a.log().Warn("环回服务启动失败", "error", err.Error())
			fmt.Fprintf(os.Stderr, "failed to start loopback server: %v\n", err)
		} else {
			actualPort := a.loopbackServer.Port()
			a.log().Info("环回服务已启动", "port", actualPort)
			if actualPort > 0 && a.settings != nil {
				st := a.settings.Get()
				if st.General.ServerPort != actualPort {
					st.General.ServerPort = actualPort
					_, _ = a.settings.Update(st)
				}
			}
		}
	}
}

// Shutdown is called when the app is terminating to cleanly stop manager and persist state.
func (a *App) Shutdown() {
	a.log().Info("应用退出")
	if a.clipboardWatcher != nil {
		a.clipboardWatcher.Stop()
	}
	if a.manager != nil {
		a.manager.Close()
	}
	if a.loopbackServer != nil {
		_ = a.loopbackServer.Stop()
	}
	if exitFile := os.Getenv("SHEEP_GET_DEV_EXIT_FILE"); exitFile != "" {
		_ = os.WriteFile(exitFile, []byte("exit"), 0600)
	}
	a.cancelWindowIdleDestroy(winNameFileInfo)
	a.cancelWindowIdleDestroy(winNameProgress)
	_ = a.log().Close()
}

// OnTaskUpdated emits wails event to the frontend whenever a task changes
func (a *App) OnTaskUpdated(t *task.Task) {
	if app := a.getApp(); app != nil {
		app.Event.Emit(protocol.EventTaskUpdated, t)
	}
}

// OnTaskDeleted emits wails event to the frontend whenever a task is deleted
func (a *App) OnTaskDeleted(taskID string) {
	if app := a.getApp(); app != nil {
		app.Event.Emit(protocol.EventTaskDeleted, taskID)
	}
}

// OnSettingsUpdated applies changed settings to the running engine and broadcasts them.
// 应用代理可能失败，错误交回发起更新的调用方，避免界面继续显示一个实际未生效的代理。
func (a *App) OnSettingsUpdated(s *config.Settings) error {
	if a.manager != nil && s != nil {
		a.manager.SetMaxActiveTasks(s.Download.MaxConcurrentDownloads)
		a.manager.SetTempDirectory(s.Download.TempDirectory)
		a.manager.SetUseServerFileTime(s.Download.UseServerFileTime)
		if err := a.manager.SetProxy(string(s.Proxy.Mode), s.Proxy.CustomAddr); err != nil {
			return err
		}
	}
	if s != nil {
		a.applyLogging(s.General.EnableLogging)
	}
	if a.loopbackServer != nil && s != nil {
		a.loopbackServer.BroadcastTakeoverConfig(takeoverSyncFromSettings(s))
	}
	if a.clipboardWatcher != nil {
		a.clipboardWatcher.OnSettingsUpdated(s)
	}
	if app := a.getApp(); app != nil {
		app.Event.Emit(protocol.EventSettingsUpdated, s)
	}
	if app := a.getApp(); app != nil && s != nil {
		if mainWin, ok := app.Window.GetByName(winNameMain); ok {
			bg := mainWindowDarkBackgroundColour
			if s.Appearance.Theme == config.ThemeLight {
				bg = mainWindowLightBackgroundColour
			}
			mainWin.SetBackgroundColour(bg)
		}
	}
	if app := a.getApp(); app != nil && s != nil && s.General.LightweightMode {
		if mainWin, ok := app.Window.GetByName(winNameMain); ok && !mainWin.IsVisible() {
			mainWin.Close()
		}
		if progWin, ok := app.Window.GetByName(winNameProgress); ok && !progWin.IsVisible() {
			a.progressPositioned = false
			progWin.Close()
		}
	}
	return nil
}

// GetSettings returns current active settings
func (a *App) GetSettings() config.Settings {
	if a.settings == nil {
		return config.DefaultSettings(a.GetDefaultDownloadDir(), "")
	}
	return a.settings.Get()
}

// UpdateSettings persists updated settings and broadcasts to all windows
func (a *App) UpdateSettings(s config.Settings) (config.Settings, error) {
	if a.settings == nil {
		return s, fmt.Errorf("settings service not initialized")
	}
	prev := a.settings.Get()
	updated, err := a.settings.Update(s)
	if err == nil && prev.General.LaunchAtStartup != updated.General.LaunchAtStartup {
		a.syncLaunchAtStartup(updated.General.LaunchAtStartup)
	}
	if err == nil && prev.General.ServerPort != updated.General.ServerPort && updated.General.ServerPort > 0 {
		if _, restartErr := a.RestartServer(updated.General.ServerPort); restartErr != nil {
			updated.General.ServerPort = prev.General.ServerPort
			_, _ = a.settings.Update(updated)
			return updated, fmt.Errorf("设置已保存但本地端口重启失败: %w", restartErr)
		}
	}
	return updated, err
}

func (a *App) syncLaunchAtStartup(enabled bool) {
	wailsApp := a.getApp()
	if wailsApp != nil && wailsApp.Autostart != nil {
		if enabled {
			_ = wailsApp.Autostart.Enable()
		} else {
			_ = wailsApp.Autostart.Disable()
		}
	}
}

// SetLaunchAtStartup configures whether SheepGet starts at system login.
func (a *App) SetLaunchAtStartup(enabled bool) error {
	if a.settings == nil {
		return fmt.Errorf("settings service not initialized")
	}
	current := a.settings.Get()
	current.General.LaunchAtStartup = enabled
	_, err := a.UpdateSettings(current)
	return err
}

// IsLaunchAtStartup reports whether autostart is enabled in system/settings.
func (a *App) IsLaunchAtStartup() bool {
	wailsApp := a.getApp()
	if wailsApp != nil && wailsApp.Autostart != nil {
		if enabled, err := wailsApp.Autostart.IsEnabled(); err == nil {
			return enabled
		}
	}
	if a.settings != nil {
		return a.settings.Get().General.LaunchAtStartup
	}
	return false
}

// RestartServer restarts the local HTTP loopback server on the specified port.
// If port <= 0, it uses the port configured in settings.
func (a *App) RestartServer(port int) (int, error) {
	if a.loopbackServer == nil {
		return 0, fmt.Errorf("loopback server not initialized")
	}
	if port <= 0 {
		if a.settings != nil {
			port = a.settings.Get().General.ServerPort
		}
		if port <= 0 {
			port = protocol.PortDefaultServer
		}
	}

	if err := a.loopbackServer.Restart(port); err != nil {
		return 0, err
	}

	actualPort := a.loopbackServer.Port()
	if a.settings != nil {
		st := a.settings.Get()
		if st.General.ServerPort != actualPort {
			st.General.ServerPort = actualPort
			_, _ = a.settings.Update(st)
		}
	}
	return actualPort, nil
}

// GetServerStatus returns the active loopback server runtime status.
func (a *App) GetServerStatus() server.Status {
	if a.loopbackServer != nil {
		return a.loopbackServer.Status()
	}
	port := protocol.PortDefaultServer
	if a.settings != nil {
		port = a.settings.Get().General.ServerPort
	}
	return server.Status{
		Running:        false,
		Port:           port,
		ConnectedCount: 0,
	}
}

// GetStorageInfo returns current storage mode and directories
func (a *App) GetStorageInfo() map[string]string {
	mode := "installed"
	dataDir := ""
	if a.storage != nil {
		mode = string(a.storage.Mode)
		dataDir = a.storage.DataDir
	}
	return map[string]string{
		"mode":    mode,
		"dataDir": dataDir,
	}
}

// GetDefaultDownloadDir returns the default downloads folder from settings or system fallback
func (a *App) GetDefaultDownloadDir() string {
	if a.settings != nil {
		cfg := a.settings.Get()
		if cfg.Download.DefaultDirectory != "" {
			return cfg.Download.DefaultDirectory
		}
	}
	return sys.DefaultDownloadDir()
}

// ResolveDestination resolves the save directory and matched category for a filename.
// 分类规则只在后端实现一次：界面用它展示命中分类并填充目录，不再自建同一规则。
func (a *App) ResolveDestination(filename string) DestinationInfo {
	if a.settings == nil {
		return DestinationInfo{Directory: sys.DefaultDownloadDir()}
	}
	cat, dir := a.settings.Get().Download.ResolveDestination(filename)
	return DestinationInfo{Directory: dir, CategoryID: cat.ID}
}

// categoryDirectory returns only the save directory for internal callers that do not need
// the matched category; the frontend uses ResolveDestination.
func (a *App) categoryDirectory(filename string) string {
	if a.settings == nil {
		return a.GetDefaultDownloadDir()
	}
	return a.settings.Get().Download.ResolveCategoryDirectory(filename)
}

// AssignExtensionToCategory assigns an extension to a target category and updates settings.
func (a *App) AssignExtensionToCategory(ext, targetCategoryID string) error {
	if a.settings == nil {
		return fmt.Errorf("settings service not initialized")
	}
	current := a.settings.Get()
	updatedDownload, changed := current.Download.AssignExtensionToCategory(ext, targetCategoryID)
	if !changed {
		return nil
	}
	current.Download = updatedDownload
	_, err := a.settings.Update(current)
	return err
}

// SetCategoryDirectory sets the save directory for a category by ID and updates settings.
func (a *App) SetCategoryDirectory(targetCategoryID, directory string) error {
	if a.settings == nil {
		return fmt.Errorf("settings service not initialized")
	}
	current := a.settings.Get()
	updatedDownload, changed := current.Download.SetCategoryDirectory(targetCategoryID, directory)
	if !changed {
		return nil
	}
	current.Download = updatedDownload
	_, err := a.settings.Update(current)
	return err
}

// AddTask adds a new download task
func (a *App) AddTask(urlStr, dir, filename string, maxConn int) (*task.Task, error) {
	if maxConn <= 0 && a.settings != nil {
		maxConn = a.settings.Get().Download.DefaultConnectionsPerTask
	}
	if maxConn <= 0 {
		maxConn = config.DefaultConnectionsPerTask
	}
	if dir == "" {
		if filename != "" {
			dir = a.categoryDirectory(filename)
		} else {
			dir = a.GetDefaultDownloadDir()
		}
	}
	return a.manager.AddTask(a.ctx, urlStr, dir, filename, maxConn)
}

// PauseTask pauses an active or queued task
func (a *App) PauseTask(id string) error {
	return a.manager.Pause(a.ctx, id)
}

// ResumeTask resumes a paused or errored task
func (a *App) ResumeTask(id string) error {
	return a.manager.Resume(a.ctx, id)
}

// RetryTask retries a failed task
func (a *App) RetryTask(id string) error {
	return a.manager.Retry(a.ctx, id)
}

// RetryProcessingTask retries media processing for a task.
func (a *App) RetryProcessingTask(id string) error {
	return a.manager.RetryProcessing(a.ctx, id)
}

func (a *App) DeleteTask(id string, deleteDiskFile bool) error {
	if deleteDiskFile {
		t, err := a.store.Get(a.ctx, id)
		if err == nil && t != nil && a.manager != nil {
			a.manager.RemoveTaskFiles(t)
		}
	}
	return a.manager.Delete(a.ctx, id)
}

// ListTasks lists all tasks
func (a *App) ListTasks() ([]*task.Task, error) {
	return a.manager.List(a.ctx)
}

// ScanCleanup scans cleanable tasks and files according to options.
func (a *App) ScanCleanup(opts engine.CleanupScanOptions) (*engine.CleanupScanResult, error) {
	if a.manager == nil {
		return nil, fmt.Errorf("manager not initialized")
	}
	a.fillLogCleanupTarget(&opts.LogDir, &opts.ActiveLogPath)
	return a.manager.ScanCleanup(a.ctx, opts)
}

// fillLogCleanupTarget 把「日志在哪、哪一份正在写」交给引擎：目录与存储模式由应用决定，
// 「多旧算旧、哪些文件算日志」由引擎判定。
func (a *App) fillLogCleanupTarget(dir *string, active *string) {
	if a.storage != nil {
		*dir = a.storage.LogsDir()
	}
	*active = a.log().Path()
}

// ExecuteCleanup executes deletion of selected cleanable items.
func (a *App) ExecuteCleanup(opts engine.CleanupExecuteOptions) (*engine.CleanupExecuteResult, error) {
	if a.manager == nil {
		return nil, fmt.Errorf("manager not initialized")
	}
	a.fillLogCleanupTarget(&opts.LogDir, &opts.ActiveLogPath)
	return a.manager.ExecuteCleanup(a.ctx, opts)
}

// OpenFile opens the downloaded file with system default application after verifying existence
func (a *App) OpenFile(filePath string) error {
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("文件不存在或已被删除: %s", filePath)
		}
		return fmt.Errorf("无法访问文件: %w", err)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", filePath)
	case "darwin":
		cmd = exec.Command("open", filePath)
	default:
		cmd = exec.Command("xdg-open", filePath)
	}
	return cmd.Start()
}

// OpenFolder reveals the downloaded file in the system file manager
func (a *App) OpenFolder(folderPath string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", folderPath)
	case "darwin":
		cmd = exec.Command("open", folderPath)
	default:
		cmd = exec.Command("xdg-open", folderPath)
	}
	return cmd.Start()
}

// ProbeURL inspects the URL metadata and reports whether an existing task already uses the URL.
// 手动改链接时由文件信息窗口调用，因此带上这一项的请求上下文，与登记时的探测口径一致。
func (a *App) ProbeURL(urlStr string) (*engine.ProbeResult, error) {
	return a.manager.ProbeURL(a.ctx, urlStr, a.activeItemHeaders(urlStr))
}

// activeItemHeaders 返回与这个链接对应的请求上下文（Referer/Cookie 等）。
// 它只认当前这一项：用户改过链接之后，手上这份上下文已经不属于那个链接了。
func (a *App) activeItemHeaders(urlStr string) map[string]string {
	if a.windowQueue == nil {
		return nil
	}
	item, err := a.windowQueue.GetActive()
	if err != nil || item == nil || item.URL != urlStr {
		return nil
	}
	return item.Headers
}

// mediaDurationProbeTimeout 是界面等一次时长探测的上限。它在等的时候转的是加载动画，
// 不能因为一个不响应的服务器一直转下去——超时就按「识别不出来」显示。
const mediaDurationProbeTimeout = 10 * time.Second

// ProbeMediaDuration 读远端音视频文件的时长（秒），供文件信息窗口在下载前展示。
// 识别不出来返回 0：时长是附加信息，不是下载流程的一环，读不到就不显示，绝不编一个数值。
func (a *App) ProbeMediaDuration(urlStr, filename string, totalBytes int64) float64 {
	if a.manager == nil || urlStr == "" {
		return 0
	}
	// 界面只等一小会儿：它转的是加载动画，不能因为一个不响应的服务器一直转下去。
	ctx, cancel := context.WithTimeout(a.ctx, mediaDurationProbeTimeout)
	defer cancel()

	seconds, ok := a.manager.ProbeMediaDuration(ctx, urlStr, filename, totalBytes, a.activeItemHeaders(urlStr))
	if !ok {
		return 0
	}
	return seconds
}

// occupancy 组装一次目标落点的占用判定：磁盘、任务库，以及窗口队列已经发给其它排队项的名字。
// 正在编辑的那一项不算占着自己的名字——窗口里的冲突提示回答的是「除了它自己，还有谁占着」。
func (a *App) occupancy() engine.Occupancy {
	var reserved engine.Reserved
	if a.windowQueue != nil {
		activeID := ""
		if active, err := a.windowQueue.GetActive(); err == nil && active != nil {
			activeID = active.ID
		}
		reserved = a.windowQueue.ReservedNames(activeID)
	}
	if a.manager == nil {
		return engine.Occupancy{Reserved: reserved}
	}
	return a.manager.Occupancy(a.ctx, reserved)
}

// CheckFileConflict checks if filename exists in dir and returns conflict status and suggested name.
func (a *App) CheckFileConflict(dir, filename string) FileConflictResult {
	if dir == "" {
		dir = sys.DefaultDownloadDir()
	}
	occupancy := a.occupancy()
	return FileConflictResult{
		Exists:            occupancy.Taken(dir, filename),
		SuggestedFilename: occupancy.Suggest(dir, filename),
	}
}

// CheckURLFilesExist checks if any file previously downloaded with urlStr (or filename variants) exists in dir.
func (a *App) CheckURLFilesExist(urlStr, dir, filename string) FileConflictResult {
	if dir == "" {
		dir = sys.DefaultDownloadDir()
	}

	// 1. Only report exists=true if the physical file actually exists on disk
	exists := engine.FileExists(filepath.Join(dir, filename))
	if !exists && a.manager != nil && urlStr != "" {
		if tasks, err := a.manager.List(a.ctx); err == nil {
			for _, t := range tasks {
				if t.URL == urlStr && t.Filename != "" {
					if engine.FileExists(filepath.Join(dir, t.Filename)) {
						exists = true
						break
					}
				}
			}
		}
	}

	// 2. Purely read-only suggestion calculation; no deletions here!
	// 建议名与冲突提示出自同一个占用判定（见 occupancy）。
	suggested := filename
	if exists {
		suggested = a.occupancy().Suggest(dir, filename)
	}
	res := FileConflictResult{
		Exists:            exists,
		SuggestedFilename: suggested,
	}

	// 3. If file does not exist in target dir, check if identical completed file exists in another directory
	if !exists && a.manager != nil && urlStr != "" {
		if tasks, err := a.manager.List(a.ctx); err == nil {
			for _, t := range tasks {
				if t.URL == urlStr && t.Directory != "" && !engine.SamePath(t.Directory, dir) {
					targetPath := filepath.Join(t.Directory, t.Filename)
					if fi, err := os.Stat(targetPath); err == nil && !fi.IsDir() {
						if t.Status == task.StatusCompleted && t.TotalBytes > 0 && fi.Size() == t.TotalBytes {
							res.CanReuseExistingFile = true
							res.ExistingPath = targetPath
							res.ExistingTaskID = t.ID
							break
						}
					}
				}
			}
		}
	}
	return res
}

// ResolveDuplicateDecision 按给定链接与最终保存位置裁决这次重复该给哪些动作、默认执行哪个。
// 界面在链接、文件名或目录变化后调用它刷新选项；裁决规则只在 duplicate 包实现一次，
// 界面不据策略自行推导（ADR-0002：后端为唯一事实来源）。
func (a *App) ResolveDuplicateDecision(urlStr, dir, filename string) duplicate.Decision {
	var dupTask *task.Task
	if a.manager != nil && urlStr != "" {
		dupTask, _ = a.manager.FindDuplicateTask(a.ctx, urlStr)
	}
	if dir == "" {
		dir = a.categoryDirectory(filename)
	}
	policy := config.DuplicatePolicyPrompt
	if a.settings != nil {
		policy = a.settings.Get().Download.DuplicateURLPolicy
	}
	return duplicate.Decide(duplicate.Facts{
		Policy:              policy,
		HasHistory:          dupTask != nil,
		HistoryCompleted:    dupTask != nil && dupTask.Status == task.StatusCompleted,
		DestinationOccupied: engine.DestinationOccupied(dir, filename, dupTask),
	})
}

// ResolveDuplicate resolves a duplicate task using strategies "continue", "redownload", "copy", or "show_completed".
func (a *App) ResolveDuplicate(taskID, strategy, dir, filename string, maxConn int) (*task.Task, error) {
	if dir == "" {
		dir = sys.DefaultDownloadDir()
	}
	return a.manager.ResolveDuplicate(a.ctx, taskID, strategy, dir, filename, maxConn)
}

// ReuseExistingFile moves an existing identical file from another directory to targetDir/targetFilename,
// cleans stale duplicate tasks, and registers the file as a completed task.
func (a *App) ReuseExistingFile(existingTaskID, targetDir, targetFilename string) (*task.Task, error) {
	if a.manager == nil {
		return nil, fmt.Errorf("manager not initialized")
	}
	return a.manager.ReuseExistingFile(a.ctx, existingTaskID, targetDir, targetFilename)
}

// StartPreDownload starts downloading in the background while file info dialog is displayed.
func (a *App) StartPreDownload(urlStr, dir, filename string, maxConn int) (*task.Task, error) {
	if dir == "" {
		if filename != "" {
			dir = a.categoryDirectory(filename)
		} else {
			dir = a.GetDefaultDownloadDir()
		}
	}
	return a.manager.StartPreDownload(a.ctx, urlStr, dir, filename, maxConn)
}

// ConfirmPreDownload confirms the pre-download task with final user-chosen directory and filename.
func (a *App) ConfirmPreDownload(taskID, finalDir, finalFilename string, maxConn int) (*task.Task, error) {
	if finalDir == "" {
		finalDir = sys.DefaultDownloadDir()
	}
	return a.manager.ConfirmPreDownload(a.ctx, taskID, finalDir, finalFilename, maxConn)
}

// CancelPreDownload handles cancellation of pre-download dialog.
func (a *App) CancelPreDownload(taskID string) error {
	return a.manager.CancelPreDownload(a.ctx, taskID)
}

// CheckURLConsistency decides whether a refreshed URL still serves the same file, so existing
// progress can be reused. headers replaces the task request context when non-nil.
func (a *App) CheckURLConsistency(taskID, newURL string, headers map[string]string) (*engine.ConsistencyResult, error) {
	return a.manager.CheckURLConsistency(a.ctx, taskID, newURL, headers)
}

// UpdateTaskURL adopts the refreshed URL and request context, then continues the download.
func (a *App) UpdateTaskURL(taskID, newURL string, headers map[string]string) (*task.Task, error) {
	return a.manager.UpdateTaskURL(a.ctx, taskID, newURL, headers)
}

// ResetAndDownloadWithNewURL discards existing progress and restarts the download from the new URL.
func (a *App) ResetAndDownloadWithNewURL(taskID, newURL string, headers map[string]string) (*task.Task, error) {
	return a.manager.ResetAndDownloadWithNewURL(a.ctx, taskID, newURL, headers)
}

func (a *App) SelectDirectory(defaultDir string) (string, error) {
	app := a.getApp()
	if app == nil {
		return "", fmt.Errorf("application not initialized")
	}

	targetDir := strings.TrimSpace(defaultDir)
	if targetDir != "" {
		cleaned := filepath.Clean(targetDir)
		if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
			targetDir = cleaned
		} else {
			targetDir = ""
		}
	}
	if targetDir == "" {
		targetDir = sys.DefaultDownloadDir()
	}

	return app.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:                "选择保存目录",
		Directory:            targetDir,
		CanChooseDirectories: true,
		CanChooseFiles:       false,
	}).PromptForSingleSelection()
}

// ValidateDirectory checks whether a directory path exists and is accessible.
func (a *App) ValidateDirectory(dirPath string) (bool, string) {
	dirPath = filepath.Clean(dirPath)
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "目录不存在"
		}
		return false, fmt.Sprintf("无法访问目录: %v", err)
	}
	if !info.IsDir() {
		return false, "指定路径不是一个文件夹"
	}
	return true, ""
}

// TriggerDownload requests a new download, dispatching to FileInfo window queue or duplicate skip policy.
func (a *App) TriggerDownload(req window.DownloadRequest) (*window.DownloadResponse, error) {
	if a.windowQueue == nil {
		return nil, fmt.Errorf("window queue not initialized")
	}
	return a.windowQueue.Enqueue(a.ctx, req)
}

type loopbackServerAdapter struct {
	app *App
}

func (a *loopbackServerAdapter) HandleHandover(ctx context.Context, req *server.HandoverRequest) (*server.HandoverResponse, error) {
	return a.app.handleHandover(ctx, req)
}

func (a *loopbackServerAdapter) HandleHLSVariants(ctx context.Context, req *server.HLSVariantsRequest) (*server.HLSVariantsResponse, error) {
	return a.app.handleHLSVariants(ctx, req)
}

func (a *loopbackServerAdapter) HandleMediaProbe(ctx context.Context, req *server.MediaProbeRequest) (*server.MediaProbeResponse, error) {
	return a.app.handleMediaProbe(ctx, req)
}

func (a *loopbackServerAdapter) GetTakeoverSync() server.TakeoverConfigSync {
	return a.app.getTakeoverSync()
}

func takeoverSyncFromSettings(st *config.Settings) server.TakeoverConfigSync {
	if st == nil {
		return server.TakeoverConfigSync{}
	}
	return server.TakeoverConfigSync{
		Extensions:    st.Download.AllExtensions(),
		ExcludedSites: st.Takeover.ExcludedSites,
		PauseShortcut: st.Takeover.PauseShortcut,
		ForceShortcut: st.Takeover.ForceShortcut,
	}
}

func (a *App) getTakeoverSync() server.TakeoverConfigSync {
	if a.settings == nil {
		return server.TakeoverConfigSync{}
	}
	st := a.settings.Get()
	return takeoverSyncFromSettings(&st)
}

func (a *App) handleHandover(_ context.Context, req *server.HandoverRequest) (*server.HandoverResponse, error) {
	if a.windowQueue == nil {
		return &server.HandoverResponse{Accepted: false, Reason: "window queue not initialized"}, nil
	}

	st := a.GetSettings()
	if req.SourceType == "browser_takeover" && req.PageContext.PageURL != "" {
		if config.SiteMatchesExcluded(req.PageContext.PageURL, st.Takeover.ExcludedSites) {
			return &server.HandoverResponse{Accepted: false, Reason: "site_excluded"}, nil
		}
	}

	headers := make(map[string]string)
	if req.Credentials != nil {
		for k, v := range req.Credentials.Headers {
			headers[k] = v
		}
		if req.Credentials.Cookies != "" && headers["Cookie"] == "" {
			headers["Cookie"] = req.Credentials.Cookies
		}
	}
	if req.PageContext.Referrer != "" && headers["Referer"] == "" {
		headers["Referer"] = req.PageContext.Referrer
	}

	pageURL := req.PageContext.PageURL
	if pageURL == "" {
		pageURL = req.PageContext.Referrer
	}

	dlReq := window.DownloadRequest{
		URL:        req.URL,
		Filename:   req.FilenameSuggestion,
		Headers:    headers,
		VariantURI: req.VariantURI,
		PageURL:    pageURL,
	}
	resp, err := a.TriggerDownload(dlReq)
	if err != nil {
		return &server.HandoverResponse{Accepted: false, Reason: err.Error()}, nil
	}
	return &server.HandoverResponse{
		Accepted:    resp.Handled,
		QueueItemID: resp.RequestID,
	}, nil
}

// handleHLSVariants 读取一份清单的可选清晰度，供扩展悬浮条在交接前弹菜单。
// 请求上下文（Referer/Cookie）与交接走同一条整理路径：扩展交过来什么就带什么。
// 它不导出为 Wails 绑定——只被 loopback 服务器调用，绑定只会把 server 包的类型
// 泄漏进桌面前端的模型图里。
func (a *App) handleHLSVariants(ctx context.Context, req *server.HLSVariantsRequest) (*server.HLSVariantsResponse, error) {
	headers := make(map[string]string)
	if req.Credentials != nil {
		for k, v := range req.Credentials.Headers {
			headers[k] = v
		}
		if req.Credentials.Cookies != "" && headers["Cookie"] == "" {
			headers["Cookie"] = req.Credentials.Cookies
		}
	}

	opts, err := a.manager.HLSVariantOptions(ctx, req.URL, credentials.New(headers))
	if err != nil {
		return nil, err
	}

	out := &server.HLSVariantsResponse{Variants: make([]server.HLSVariantOption, 0, len(opts))}
	for _, o := range opts {
		out.Variants = append(out.Variants, server.HLSVariantOption{
			URI:       o.URI,
			Label:     o.Label,
			Bandwidth: o.Bandwidth,
		})
	}
	return out, nil
}

// handleMediaProbe 为扩展面板探测一条链接的展示信息（时长与大小）。
// 请求上下文（Referer/Cookie）与交接走同一条整理路径：扩展交过来什么就带什么。
// 它不导出为 Wails 绑定——只被 loopback 服务器调用，导出只会把 server 包的类型
// 泄漏进桌面前端的模型图里。
func (a *App) handleMediaProbe(ctx context.Context, req *server.MediaProbeRequest) (*server.MediaProbeResponse, error) {
	headers := make(map[string]string)
	if req.Credentials != nil {
		for k, v := range req.Credentials.Headers {
			headers[k] = v
		}
		if req.Credentials.Cookies != "" && headers["Cookie"] == "" {
			headers["Cookie"] = req.Credentials.Cookies
		}
	}

	ov, err := a.manager.ProbeMediaOverview(ctx, req.URL, req.Filename, req.MimeType, req.IsHls, req.TotalBytes, headers)
	if err != nil {
		return nil, err
	}
	return &server.MediaProbeResponse{
		DurationSeconds: ov.DurationSeconds,
		TotalBytes:      ov.TotalBytes,
		Variants:        ov.Variants,
	}, nil
}

// OpenNewDownload opens the FileInfo window with an empty/manual request.
func (a *App) OpenNewDownload() (*window.DownloadResponse, error) {
	return a.TriggerDownload(window.DownloadRequest{})
}

// GetActiveFileInfo returns the active item currently shown in the FileInfo window.
func (a *App) GetActiveFileInfo() (*window.FileInfoItem, error) {
	if a.windowQueue == nil {
		return nil, fmt.Errorf("window queue not initialized")
	}
	return a.windowQueue.GetActive()
}

// SubmitFileInfo confirms the active FileInfo request with user selections.
func (a *App) SubmitFileInfo(sub window.FileInfoSubmission) (*task.Task, error) {
	if a.windowQueue == nil {
		return nil, fmt.Errorf("window queue not initialized")
	}
	t, err := a.windowQueue.Submit(a.ctx, sub)
	if err != nil {
		return nil, err
	}
	if t != nil && a.settings != nil {
		st := a.settings.Get()
		if st.Download.ShowProgressWindow {
			a.ShowProgressWindow(t.ID)
			if a.GetFileInfoQueueLength() > 0 {
				if app := a.getApp(); app != nil {
					if fileWin, ok := app.Window.GetByName(winNameFileInfo); ok {
						window.Raise(fileWin)
					}
				}
			}
		}
	}
	return t, nil
}

// CancelCurrentFileInfo cancels the active FileInfo request and advances the queue.
func (a *App) CancelCurrentFileInfo() error {
	if a.windowQueue == nil {
		return nil
	}
	return a.windowQueue.CancelCurrent(a.ctx)
}

// SelectHLSVariant 记下用户在文件信息窗口里选定的清晰度，并把这一版的事实（大小、时长）
// 取回来。多清晰度的清单必须先选定才能提交；只有一项时不构成选择，无需调用。
// urlStr 由界面把当前链接显式传进来：手输链接时队列项里没有登记过 URL。
func (a *App) SelectHLSVariant(requestID, urlStr, variantURI string) error {
	if a.windowQueue == nil {
		return fmt.Errorf("window queue not initialized")
	}
	return a.windowQueue.SelectHLSVariant(a.ctx, requestID, urlStr, variantURI)
}

// GetFileInfoQueueLength returns the number of requests currently waiting in the queue.
func (a *App) GetFileInfoQueueLength() int {
	if a.windowQueue == nil {
		return 0
	}
	return a.windowQueue.QueueLength()
}

// GetFileInfoQueueItems returns all currently enqueued items in the file info window.
func (a *App) GetFileInfoQueueItems() []*window.FileInfoItem {
	if a.windowQueue == nil {
		return nil
	}
	return a.windowQueue.GetQueueItems()
}

// SwitchFileInfoActive switches the active file info dialog item to index.
func (a *App) SwitchFileInfoActive(index int) (*window.FileInfoItem, error) {
	if a.windowQueue == nil {
		return nil, fmt.Errorf("window queue not initialized")
	}
	return a.windowQueue.SwitchActive(index)
}

// ensureMainWindow returns the main window, creating it if it doesn't exist yet.
func (a *App) ensureMainWindow(hidden bool, urlPath ...string) application.Window {
	app := a.getApp()
	if app == nil {
		return nil
	}
	if win, ok := app.Window.GetByName(winNameMain); ok {
		return win
	}
	targetURL := "/"
	if len(urlPath) > 0 && urlPath[0] != "" {
		targetURL = urlPath[0]
	}
	bg := mainWindowDarkBackgroundColour
	if a.settings != nil && a.settings.Get().Appearance.Theme == config.ThemeLight {
		bg = mainWindowLightBackgroundColour
	}
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             winNameMain,
		Title:            "SheepGet",
		Width:            mainWindowWidth,
		Height:           mainWindowHeight,
		MinWidth:         mainWindowWidth,
		MinHeight:        mainWindowHeight,
		Hidden:           hidden,
		BackgroundColour: bg,
		URL:              targetURL,
	})
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		st := a.GetSettings()
		if st.General.LightweightMode {
			// 在轻量模式下不拦截关闭事件，允许 Wails 销毁窗口与 WebView 渲染进程
			return
		}
		// 默认模式下拦截并隐藏到托盘
		event.Cancel()
		mainWindow.Hide()
	})
	return mainWindow
}

// ShowMainWindow makes the main window visible and brings it to focus, creating it if needed.
func (a *App) ShowMainWindow() {
	if win := a.ensureMainWindow(false); win != nil {
		window.ShowAndRaise(win)
	}
}

// OpenSettingsWindow ensures the main window is open and switched to the preferences tab.
func (a *App) OpenSettingsWindow() {
	app := a.getApp()
	if app == nil {
		return
	}
	if win, ok := app.Window.GetByName(winNameMain); ok {
		window.ShowAndRaise(win)
		app.Event.Emit(protocol.EventAppOpenSettings)
		return
	}
	win := a.ensureMainWindow(false, "/?open=settings")
	if win != nil {
		window.ShowAndRaise(win)
		app.Event.Emit(protocol.EventAppOpenSettings)
	}
}

// MinimiseFileInfoWindow minimises the file info window.
func (a *App) MinimiseFileInfoWindow() {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName(winNameFileInfo); ok {
			win.Minimise()
		}
	}
}

// SetFileInfoWindowHeight dynamically adjusts the fileinfo window's height to wrap its content.
func (a *App) SetFileInfoWindowHeight(height int) {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName(winNameFileInfo); ok {
			if height < fileInfoWindowMinH {
				height = fileInfoWindowMinH
			}
			if height > fileInfoWindowMaxH {
				height = fileInfoWindowMaxH
			}
			win.SetSize(fileInfoWindowWidth, height)
		}
	}
}

// SetProgressWindowHeight adjusts the progress window's height to wrap its content.
// 高度由内容决定：只有一个任务卡片时窗口就收成一张卡片的高度，不套用内容意义上的下限，
// 只有超过上限时才封顶（超出部分由窗口内部滚动）。
func (a *App) SetProgressWindowHeight(height int) {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName(winNameProgress); ok {
			if height < progressWindowMinH {
				height = progressWindowMinH
			}
			if height > progressWindowMaxH {
				height = progressWindowMaxH
			}
			win.SetSize(progressWindowWidth, height)
		}
	}
}

// ShowProgressWindow brings up or focuses the shared download progress window and highlights the task.
func (a *App) ShowProgressWindow(taskID string) {
	a.cancelWindowIdleDestroy(winNameProgress)
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName(winNameProgress); ok {
			a.windowTimerLock.Lock()
			positioned := a.progressPositioned
			if !positioned {
				if primary := app.Screen.GetPrimary(); primary != nil && primary.WorkArea.Width > 0 && primary.WorkArea.Height > 0 {
					x := primary.WorkArea.X + primary.WorkArea.Width - progressWindowWidth - progressWindowEdgeGap
					y := primary.WorkArea.Y + primary.WorkArea.Height - progressWindowBottomOffset - progressWindowEdgeGap
					win.SetPosition(x, y)
				}
				a.progressPositioned = true
			}
			a.windowTimerLock.Unlock()
			window.ShowAndRaise(win)
			if taskID != "" {
				app.Event.Emit(protocol.EventProgressFocusCompleted, taskID)
				app.Event.Emit(protocol.EventProgressFocusTask, taskID)
			}
			return
		}

		var (
			progX       = 0
			progY       = 0
			progInitPos = application.WindowCentered
		)
		if primary := app.Screen.GetPrimary(); primary != nil && primary.WorkArea.Width > 0 && primary.WorkArea.Height > 0 {
			progX = primary.WorkArea.X + primary.WorkArea.Width - progressWindowWidth - progressWindowEdgeGap
			progY = primary.WorkArea.Y + primary.WorkArea.Height - progressWindowBottomOffset - progressWindowEdgeGap
			progInitPos = application.WindowXY
		}

		progWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
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
			AlwaysOnTop:     a.progressAlwaysOnTop,
			BackgroundType:  application.BackgroundTypeTransparent,
			URL:             fmt.Sprintf("/?window=progress&focus=%s", url.QueryEscape(taskID)),
		})
		a.windowTimerLock.Lock()
		a.progressPositioned = true
		a.windowTimerLock.Unlock()
		progWin.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			if a.isWindowDestroying(winNameProgress) {
				return
			}
			event.Cancel()
			progWin.Hide()
			app.Event.Emit(protocol.EventProgressClearViewed)
			a.scheduleWindowIdleDestroy(winNameProgress)
		})
		window.ShowAndRaise(progWin)
		if taskID != "" {
			app.Event.Emit(protocol.EventProgressFocusCompleted, taskID)
			app.Event.Emit(protocol.EventProgressFocusTask, taskID)
		}
	}
}

// MinimiseProgressWindow minimises the progress window.
func (a *App) MinimiseProgressWindow() {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName(winNameProgress); ok {
			win.Minimise()
		}
	}
}

// HideProgressWindow hides the progress window.
func (a *App) HideProgressWindow() {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName(winNameProgress); ok {
			win.Hide()
			app.Event.Emit(protocol.EventProgressClearViewed)
			a.scheduleWindowIdleDestroy(winNameProgress)
		}
	}
}

func (a *App) isWindowDestroying(name string) bool {
	a.windowTimerLock.Lock()
	defer a.windowTimerLock.Unlock()
	return a.destroyingWindows[name]
}

func (a *App) setWindowDestroying(name string, destroying bool) {
	a.windowTimerLock.Lock()
	defer a.windowTimerLock.Unlock()
	if a.destroyingWindows == nil {
		a.destroyingWindows = make(map[string]bool)
	}
	if destroying {
		a.destroyingWindows[name] = true
	} else {
		delete(a.destroyingWindows, name)
	}
}

func (a *App) cancelWindowIdleDestroy(name string) {
	a.windowTimerLock.Lock()
	defer a.windowTimerLock.Unlock()
	switch name {
	case winNameFileInfo:
		if a.fileInfoTimer != nil {
			a.fileInfoTimer.Stop()
			a.fileInfoTimer = nil
		}
	case winNameProgress:
		if a.progressTimer != nil {
			a.progressTimer.Stop()
			a.progressTimer = nil
		}
	}
}

func (a *App) scheduleWindowIdleDestroy(name string) {
	st := a.GetSettings()
	if !st.General.LightweightMode {
		return
	}

	a.windowTimerLock.Lock()
	defer a.windowTimerLock.Unlock()

	switch name {
	case winNameFileInfo:
		if a.fileInfoTimer != nil {
			a.fileInfoTimer.Stop()
		}
		a.fileInfoTimer = time.AfterFunc(windowIdleDestroyGracePeriod, func() {
			a.windowTimerLock.Lock()
			a.fileInfoTimer = nil
			a.windowTimerLock.Unlock()

			if a.GetFileInfoQueueLength() == 0 {
				if app := a.getApp(); app != nil {
					if win, ok := app.Window.GetByName(winNameFileInfo); ok && !win.IsVisible() {
						a.setWindowDestroying(winNameFileInfo, true)
						win.Close()
						a.setWindowDestroying(winNameFileInfo, false)
					}
				}
			}
		})

	case winNameProgress:
		if a.progressTimer != nil {
			a.progressTimer.Stop()
		}
		a.progressTimer = time.AfterFunc(windowIdleDestroyGracePeriod, func() {
			a.windowTimerLock.Lock()
			a.progressTimer = nil
			a.windowTimerLock.Unlock()

			if app := a.getApp(); app != nil {
				if win, ok := app.Window.GetByName(winNameProgress); ok && !win.IsVisible() {
					a.windowTimerLock.Lock()
					a.progressPositioned = false
					a.windowTimerLock.Unlock()

					a.setWindowDestroying(winNameProgress, true)
					win.Close()
					a.setWindowDestroying(winNameProgress, false)
				}
			}
		})
	}
}

// ToggleProgressWindowAlwaysOnTop toggles whether the progress window is always on top.
func (a *App) ToggleProgressWindowAlwaysOnTop() bool {
	a.progressAlwaysOnTop = !a.progressAlwaysOnTop
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName(winNameProgress); ok {
			win.SetAlwaysOnTop(a.progressAlwaysOnTop)
		}
	}
	return a.progressAlwaysOnTop
}

// IsProgressWindowAlwaysOnTop reports whether the progress window is set to always on top.
func (a *App) IsProgressWindowAlwaysOnTop() bool {
	return a.progressAlwaysOnTop
}

// OpenExtensionFolder reveals the bundled extension directory in the system file manager.
func (a *App) OpenExtensionFolder() error {
	dir, err := browser.ExtensionDirectory()
	if err != nil {
		return err
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,", dir)
	case "darwin":
		cmd = exec.Command("open", "-R", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}

	return cmd.Start()
}

// PrepareExtensionPage 唤起浏览器并把扩展管理页地址写入剪贴板，返回该地址。
// 用户在浏览器地址栏粘贴即可进入扩展管理页，随后手动加载扩展目录。
func (a *App) PrepareExtensionPage(browserName string) (string, error) {
	var exePath string
	if runtime.GOOS == "windows" {
		exePath = browser.FindBrowserExe(browserName)
	}
	plan, err := browser.PlanExtensionInstall(runtime.GOOS, browserName, exePath)
	if err != nil {
		return "", err
	}

	app := a.getApp()
	if app == nil || app.Clipboard == nil || !app.Clipboard.SetText(plan.Address) {
		return "", fmt.Errorf("写入剪贴板失败")
	}

	if err := exec.Command(plan.Exe, plan.Args...).Start(); err != nil {
		return "", fmt.Errorf("唤起浏览器失败: %w", err)
	}

	return plan.Address, nil
}
