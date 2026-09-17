package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sheep-get/internal/config"
	"sheep-get/internal/engine"
	"sheep-get/internal/storage"
	"sheep-get/internal/task"
	"sheep-get/internal/window"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// FileConflictResult represents whether target file exists and suggests an alternative filename.
type FileConflictResult struct {
	Exists               bool   `json:"exists"`
	SuggestedFilename    string `json:"suggestedFilename"`
	ExistingPath         string `json:"existingPath,omitempty"`
	ExistingTaskID       string `json:"existingTaskID,omitempty"`
	CanReuseExistingFile bool   `json:"canReuseExistingFile,omitempty"`
}

// App struct
type App struct {
	app                *application.App
	ctx                context.Context
	manager            *engine.Manager
	store              task.TaskStore
	storage            *storage.Storage
	settings           *config.SettingsService
	windowQueue        *window.QueueController
	progressPositioned bool
}

type wailsWindowView struct {
	getApp      func() *application.App
	name        string
	getSettings func() config.Settings
}

func getThemeRGBA(s config.Settings) application.RGBA {
	isDark := false
	switch s.Appearance.Theme {
	case config.ThemeDark:
		isDark = true
	case config.ThemeLight:
		isDark = false
	default:
		isDark = isSystemDarkMode()
	}

	if isDark {
		return application.RGBA{Red: 12, Green: 14, Blue: 18, Alpha: 255}
	}
	return application.RGBA{Red: 241, Green: 245, Blue: 249, Alpha: 255}
}

func (w *wailsWindowView) Show() {
	if app := w.getApp(); app != nil {
		if win, ok := app.Window.GetByName(w.name); ok {
			if w.name != "fileinfo" && w.getSettings != nil {
				win.SetBackgroundColour(getThemeRGBA(w.getSettings()))
			}
			win.Show()
		}
	}
}

func (w *wailsWindowView) Hide() {
	if app := w.getApp(); app != nil {
		if win, ok := app.Window.GetByName(w.name); ok {
			win.Hide()
		}
	}
}

func (w *wailsWindowView) Focus() {
	if app := w.getApp(); app != nil {
		if win, ok := app.Window.GetByName(w.name); ok {
			win.Focus()
		}
	}
}

func (w *wailsWindowView) Emit(event string, data any) {
	if app := w.getApp(); app != nil {
		app.Event.Emit(event, data)
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

	defaultDownloadDir := getDefaultDownloadDir()
	defaultTempDir := storeDir.TempDir()

	app := &App{
		store:   store,
		storage: storeDir,
	}

	settingsSvc := config.NewSettingsService(
		storeDir.ConfigFile(),
		defaultDownloadDir,
		defaultTempDir,
		func(updated *config.Settings) {
			app.OnSettingsUpdated(updated)
		},
	)
	app.settings = settingsSvc

	activeSettings := settingsSvc.Get()
	downloader := engine.NewHTTPDownloader(nil)
	downloader.SetTempDirectory(activeSettings.Download.TempDirectory)
	downloader.SetUseServerFileTime(activeSettings.Download.UseServerFileTime)

	mgr := engine.NewManager(store, downloader, engine.Config{
		MaxActiveTasks:    activeSettings.Download.MaxConcurrentDownloads,
		TempDirectory:     activeSettings.Download.TempDirectory,
		UseServerFileTime: activeSettings.Download.UseServerFileTime,
	})
	app.manager = mgr

	winView := &wailsWindowView{
		getApp:      app.getApp,
		name:        "fileinfo",
		getSettings: settingsSvc.Get,
	}
	app.windowQueue = window.NewQueueController(mgr, settingsSvc, winView)
	app.windowQueue.SetOnShowCompleted(app.ShowProgressWindow)
	return app
}

// SetApplication sets the Wails application reference
func (a *App) SetApplication(app *application.App) {
	a.app = app
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
}

// Shutdown is called when the app is terminating to cleanly stop manager and persist state.
func (a *App) Shutdown() {
	if a.manager != nil {
		a.manager.Close()
	}
}

// OnTaskUpdated emits wails event to the frontend whenever a task changes
func (a *App) OnTaskUpdated(t *task.Task) {
	if app := a.getApp(); app != nil {
		app.Event.Emit("task:updated", t)
	}
}

// OnTaskDeleted emits wails event to the frontend whenever a task is deleted
func (a *App) OnTaskDeleted(taskID string) {
	if app := a.getApp(); app != nil {
		app.Event.Emit("task:deleted", taskID)
	}
}

func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// OnSettingsUpdated emits wails event to the frontend whenever settings change
func (a *App) OnSettingsUpdated(s *config.Settings) {
	if a.manager != nil && s != nil {
		a.manager.SetMaxActiveTasks(s.Download.MaxConcurrentDownloads)
		a.manager.SetTempDirectory(s.Download.TempDirectory)
		a.manager.SetUseServerFileTime(s.Download.UseServerFileTime)
	}
	if app := a.getApp(); app != nil {
		app.Event.Emit("settings:updated", s)
	}
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
	return a.settings.Update(s)
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
	return getDefaultDownloadDir()
}

// ResolveCategoryDirectory resolves the target directory for a given filename based on current settings.
func (a *App) ResolveCategoryDirectory(filename string) string {
	if a.settings == nil {
		return a.GetDefaultDownloadDir()
	}
	cfg := a.settings.Get()
	return cfg.Download.ResolveCategoryDirectory(filename)
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
	if dir == "" {
		if filename != "" {
			dir = a.ResolveCategoryDirectory(filename)
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
		if err == nil && t != nil {
			destPath := filepath.Join(t.Directory, t.Filename)
			_ = os.Remove(destPath)
			_ = os.Remove(destPath + ".sheepget")
			if a.manager != nil {
				partPath := a.manager.GetPartPath(t)
				_ = os.Remove(partPath)
			}
		}
	}
	return a.manager.Delete(a.ctx, id)
}

// ListTasks lists all tasks
func (a *App) ListTasks() ([]*task.Task, error) {
	return a.manager.List(a.ctx)
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
func (a *App) ProbeURL(urlStr string) (*engine.ProbeResult, error) {
	return a.manager.ProbeURL(a.ctx, urlStr)
}

func (a *App) isFilenameTaken(dir, candidate string) bool {
	if engine.FileExists(filepath.Join(dir, candidate)) {
		return true
	}
	if a.manager != nil {
		if tasks, err := a.manager.List(a.ctx); err == nil {
			for _, t := range tasks {
				if engine.SamePath(t.Directory, dir) && engine.SameFilename(t.Filename, candidate) {
					if t.Status == task.StatusDownloading || t.Status == task.StatusQueued || t.Status == task.StatusProcessing {
						return true
					}
				}
			}
		}
	}
	if a.windowQueue != nil {
		for _, it := range a.windowQueue.GetQueueItems() {
			if it != nil && engine.SamePath(it.Directory, dir) && engine.SameFilename(it.Filename, candidate) {
				return true
			}
		}
	}
	return false
}

// CheckFileConflict checks if filename exists in dir and returns conflict status and suggested name.
func (a *App) CheckFileConflict(dir, filename string) FileConflictResult {
	if dir == "" {
		dir = getDefaultDownloadDir()
	}
	if !a.isFilenameTaken(dir, filename) {
		return FileConflictResult{
			Exists:            false,
			SuggestedFilename: filename,
		}
	}
	return FileConflictResult{
		Exists: true,
		SuggestedFilename: engine.NextNumberedCopy(filename, func(cand string) bool {
			return a.isFilenameTaken(dir, cand)
		}),
	}
}

// CheckURLFilesExist checks if any file previously downloaded with urlStr (or filename variants) exists in dir.
func (a *App) CheckURLFilesExist(urlStr, dir, filename string) FileConflictResult {
	if dir == "" {
		dir = getDefaultDownloadDir()
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
	var suggested string
	if exists {
		suggested = engine.NextNumberedCopy(filename, func(cand string) bool {
			return a.isFilenameTaken(dir, cand)
		})
	} else {
		suggested = filename
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

// ResolveDuplicate resolves a duplicate task using strategies "continue", "redownload", "copy", or "show_completed".
func (a *App) ResolveDuplicate(taskID, strategy, dir, filename string, maxConn int) (*task.Task, error) {
	if dir == "" {
		dir = getDefaultDownloadDir()
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
			dir = a.ResolveCategoryDirectory(filename)
		} else {
			dir = a.GetDefaultDownloadDir()
		}
	}
	return a.manager.StartPreDownload(a.ctx, urlStr, dir, filename, maxConn)
}

// ConfirmPreDownload confirms the pre-download task with final user-chosen directory and filename.
func (a *App) ConfirmPreDownload(taskID, finalDir, finalFilename string, maxConn int) (*task.Task, error) {
	if finalDir == "" {
		finalDir = getDefaultDownloadDir()
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

func (a *App) SelectDirectory() (string, error) {
	app := a.getApp()
	if app == nil {
		return "", fmt.Errorf("application not initialized")
	}
	return app.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:                "选择保存目录",
		Directory:            getDefaultDownloadDir(),
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
			if app := a.getApp(); app != nil {
				app.Event.Emit("progress:clear_viewed")
			}
			a.ShowProgressWindow(t.ID)
			if a.GetFileInfoQueueLength() > 0 {
				if app := a.getApp(); app != nil {
					if fileWin, ok := app.Window.GetByName("fileinfo"); ok {
						fileWin.Focus()
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

// ShowMainWindow makes the main window visible and brings it to focus.
func (a *App) ShowMainWindow() {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName("main"); ok {
			win.Show()
			win.Focus()
		}
	}
}

// MinimiseFileInfoWindow minimises the file info window.
func (a *App) MinimiseFileInfoWindow() {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName("fileinfo"); ok {
			win.Minimise()
		}
	}
}

// SetFileInfoWindowHeight dynamically adjusts the fileinfo window's height to wrap its content.
func (a *App) SetFileInfoWindowHeight(height int) {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName("fileinfo"); ok {
			if height < 240 {
				height = 240
			}
			if height > 700 {
				height = 700
			}
			win.SetSize(460, height)
		}
	}
}

// SetProgressWindowHeight dynamically adjusts the progress window's height between minHeight and maxHeight.
func (a *App) SetProgressWindowHeight(height int) {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName("progress"); ok {
			if height < 160 {
				height = 160
			}
			if height > 640 {
				height = 640
			}
			win.SetSize(560, height)
		}
	}
}

// ShowProgressWindow brings up or focuses the shared download progress window and highlights the task.
func (a *App) ShowProgressWindow(taskID string) {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName("progress"); ok {
			if !a.progressPositioned {
				if primary := app.Screen.GetPrimary(); primary != nil && primary.WorkArea.Width > 0 && primary.WorkArea.Height > 0 {
					x := primary.WorkArea.X + primary.WorkArea.Width - 560 - 32
					y := primary.WorkArea.Y + primary.WorkArea.Height - 320 - 32
					win.SetPosition(x, y)
				}
				a.progressPositioned = true
			}
			win.Show()
			win.Focus()
			if taskID != "" {
				app.Event.Emit("progress:focus_completed", taskID)
				app.Event.Emit("progress:focus_task", taskID)
			}
			return
		}
		progWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:           "progress",
			Title:          "下载进度 - SheepGet",
			Width:          560,
			Height:         160,
			MinWidth:       560,
			MaxWidth:       560,
			MinHeight:      160,
			MaxHeight:      640,
			Frameless:      true,
			BackgroundType: application.BackgroundTypeTransparent,
			URL:            fmt.Sprintf("/?window=progress&focus=%s", url.QueryEscape(taskID)),
		})
		progWin.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			event.Cancel()
			progWin.Hide()
		})
		progWin.Focus()
		if taskID != "" {
			app.Event.Emit("progress:focus_completed", taskID)
			app.Event.Emit("progress:focus_task", taskID)
		}
	}
}

// MinimiseProgressWindow minimises the progress window.
func (a *App) MinimiseProgressWindow() {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName("progress"); ok {
			win.Minimise()
		}
	}
}

// HideProgressWindow hides the progress window.
func (a *App) HideProgressWindow() {
	if app := a.getApp(); app != nil {
		if win, ok := app.Window.GetByName("progress"); ok {
			win.Hide()
			app.Event.Emit("progress:clear_viewed")
		}
	}
}
