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

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
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

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.manager.AddListener(a)
}

// OnTaskUpdated emits wails event to the frontend whenever a task changes
func (a *App) OnTaskUpdated(t *task.Task) {
	if a.ctx != nil && a.ctx.Value("frontend") != nil {
		wailsRuntime.EventsEmit(a.ctx, "task:updated", t)
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
