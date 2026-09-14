package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// FileConflictResult represents whether target file exists and suggests an alternative filename.
type FileConflictResult struct {
	Exists            bool   `json:"exists"`
	SuggestedFilename string `json:"suggestedFilename"`
}

// App struct
type App struct {
	app     *application.App
	ctx     context.Context
	manager *engine.Manager
	store   task.TaskStore
}

// NewApp creates a new App application struct
func NewApp() *App {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = "."
	}
	appDataDir := filepath.Join(userConfigDir, "SheepGet")
	storeFile := filepath.Join(appDataDir, "tasks.json")

	store, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		panic(fmt.Sprintf("failed to init task store: %v", err))
	}

	downloader := engine.NewHTTPDownloader(nil)
	mgr := engine.NewManager(store, downloader, engine.Config{MaxActiveTasks: 3})

	app := &App{
		manager: mgr,
		store:   store,
	}

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

// OnTaskUpdated emits wails event to the frontend whenever a task changes
func (a *App) OnTaskUpdated(t *task.Task) {
	if app := a.getApp(); app != nil {
		app.Event.Emit("task:updated", t)
	}
}

// Greet remains for compatibility with existing tests
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// AddTask adds a new download task
func (a *App) AddTask(urlStr, dir, filename string, maxConn int) (*task.Task, error) {
	if dir == "" {
		dir = getDefaultDownloadDir()
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

func (a *App) DeleteTask(id string, deleteDiskFile bool) error {
	if deleteDiskFile {
		t, err := a.store.Get(a.ctx, id)
		if err == nil && t != nil {
			destPath := filepath.Join(t.Directory, t.Filename)
			_ = os.Remove(destPath)
			_ = os.Remove(destPath + ".sheepget")
		}
	}
	return a.manager.Delete(a.ctx, id)
}

// ListTasks lists all tasks
func (a *App) ListTasks() ([]*task.Task, error) {
	return a.manager.List(a.ctx)
}

// GetDefaultDownloadDir returns the default downloads folder
func (a *App) GetDefaultDownloadDir() string {
	return getDefaultDownloadDir()
}

// OpenFile opens the downloaded file with system default application
func (a *App) OpenFile(filePath string) error {
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

// CheckFileConflict checks if filename exists in dir and returns conflict status and suggested name.
func (a *App) CheckFileConflict(dir, filename string) FileConflictResult {
	if dir == "" {
		dir = getDefaultDownloadDir()
	}
	exists, suggested := engine.CheckFileConflict(dir, filename)
	return FileConflictResult{
		Exists:            exists,
		SuggestedFilename: suggested,
	}
}

// ResolveDuplicate resolves a duplicate task using strategies "continue", "redownload", "copy", or "show_completed".
func (a *App) ResolveDuplicate(taskID, strategy, dir, filename string, maxConn int) (*task.Task, error) {
	if dir == "" {
		dir = getDefaultDownloadDir()
	}
	return a.manager.ResolveDuplicate(a.ctx, taskID, strategy, dir, filename, maxConn)
}

// StartPreDownload starts downloading in the background while file info dialog is displayed.
func (a *App) StartPreDownload(urlStr, dir, filename string, maxConn int) (*task.Task, error) {
	if dir == "" {
		dir = getDefaultDownloadDir()
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

// SelectDirectory opens native directory picker dialog.
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
