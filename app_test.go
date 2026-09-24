package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sheep-get/internal/config"
	"sheep-get/internal/duplicate"
	"sheep-get/internal/engine"
	"sheep-get/internal/server"
	"sheep-get/internal/storage"
	"sheep-get/internal/task"
	"sheep-get/internal/window"
	"sheep-get/internal/windowing"
)

// serveRangedPayload stands in for a remote resource: it honours Range requests and, when referer
// is set, answers 403 to any request that does not carry it (an expired link needing request info).
func serveRangedPayload(payload []byte, referer string, delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if referer != "" && r.Header.Get("Referer") != referer {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Header().Set("Content-Disposition", `attachment; filename="flow.bin"`)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.WriteHeader(http.StatusOK)
			writeSlowly(w, payload, delay)
			return
		}
		rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.Split(rangeSpec, "-")
		start, _ := strconv.ParseInt(parts[0], 10, 64)
		end := int64(len(payload)) - 1
		if len(parts) > 1 && parts[1] != "" {
			if parsed, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				end = parsed
			}
		}
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		writeSlowly(w, payload[start:end+1], delay)
	}))
}

func writeSlowly(w http.ResponseWriter, data []byte, delay time.Duration) {
	offset := 0
	for offset < len(data) {
		chunkEnd := min(offset+4096, len(data))
		if _, err := w.Write(data[offset:chunkEnd]); err != nil {
			return
		}
		offset = chunkEnd
		if delay > 0 {
			time.Sleep(delay)
		}
	}
}

func newTestApp(t *testing.T) (*App, task.TaskStore, string) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 2})
	t.Cleanup(mgr.Close)

	settingsSvc := config.NewSettingsService(
		filepath.Join(tmpDir, "config.json"),
		tmpDir,
		filepath.Join(tmpDir, "temp"),
		nil,
	)
	app := &App{
		manager:  mgr,
		store:    store,
		settings: settingsSvc,
		storage: &storage.Storage{
			Mode:    storage.ModeInstalled,
			DataDir: tmpDir,
		},
	}
	adapter := &loopbackServerAdapter{app: app}
	loopbackSrv := server.NewServer(filepath.Join(tmpDir, "session.json"), adapter, adapter)
	app.loopbackServer = loopbackSrv
	// 无头测试里宿主窗口不存在，注册表的宿主适配器会如实说「没有窗口」；
	// 窗口策略本身由 internal/windowing 的用例覆盖。
	app.declareWindows(&windowsHost{app: app})
	winView := &wailsWindowView{
		app:  app,
		name: winNameFileInfo,
	}
	app.windowQueue = window.NewQueueController(mgr, settingsSvc, winView)
	app.windowQueue.SetOnShowCompleted(app.ShowProgressWindow)
	app.startup(context.Background())
	return app, store, tmpDir
}

// waitActiveItem 等文件信息窗口显示出想要的那一项。登记时的名字只是猜测，探测回来会被
// 服务器给的真名替换掉，因此这里按名字等。
func waitActiveItem(t *testing.T, app *App, wantFilename string) *window.FileInfoItem {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		item, err := app.GetActiveFileInfo()
		if err == nil && item != nil && item.Filename == wantFilename {
			return item
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("file info window never showed %q", wantFilename)
	return nil
}

// waitPreDownloadTask 等这一项的提前下载起步，返回它建出来的任务 ID。
func waitPreDownloadTask(t *testing.T, app *App, itemID string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		item, err := app.GetActiveFileInfo()
		if err == nil && item != nil && item.ID == itemID && item.PreDownloadTaskID != "" {
			return item.PreDownloadTaskID
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pre-download for item %s never started", itemID)
	return ""
}

// 无头环境里没有真窗口，但窗口的声明与策略就在 App 上：用一个记录型宿主把声明驱动起来，
// 就能验证「关到托盘」「关掉文件信息窗口等于取消这次下载」这类应用侧策略。
type headlessWindow struct {
	onClose   func() bool
	visible   bool
	closed    bool
	alwaysTop bool
}

func (w *headlessWindow) Show()                          { w.visible = true }
func (w *headlessWindow) Hide()                          { w.visible = false }
func (w *headlessWindow) Raise()                         { w.visible = true }
func (w *headlessWindow) Minimise()                      { w.visible = false }
func (w *headlessWindow) Close()                         { w.closed = true }
func (w *headlessWindow) IsVisible() bool                { return w.visible }
func (w *headlessWindow) SetSize(int, int)               {}
func (w *headlessWindow) SetPosition(int, int)           {}
func (w *headlessWindow) SetAlwaysOnTop(on bool)         { w.alwaysTop = on }
func (w *headlessWindow) SetBackground(windowing.Colour) {}

type headlessHost struct {
	windows map[string]*headlessWindow
}

func (h *headlessHost) Open(options windowing.Options, onClose func() bool) (windowing.Window, bool) {
	win := &headlessWindow{onClose: onClose}
	if h.windows == nil {
		h.windows = map[string]*headlessWindow{}
	}
	h.windows[options.Name] = win
	return win, true
}

func (h *headlessHost) Find(name string) (windowing.Window, bool) {
	win, ok := h.windows[name]
	if !ok {
		return nil, false
	}
	return win, true
}

func (h *headlessHost) WorkArea() (windowing.Rect, bool) { return windowing.Rect{}, false }

func waitAppTask(t *testing.T, store task.TaskStore, id string, want task.Status) *task.Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		saved, err := store.Get(context.Background(), id)
		if err == nil && saved != nil && saved.Status == want {
			return saved
		}
		time.Sleep(20 * time.Millisecond)
	}
	saved, _ := store.Get(context.Background(), id)
	t.Fatalf("task %s never reached %s (last: %+v)", id, want, saved)
	return nil
}

func TestApp_TaskLifecycle(t *testing.T) {
	app, store, tmpDir := newTestApp(t)

	// 下载入口只有一条：登记进文件信息窗口队列，再提交建任务。这里用不可达地址，
	// 断言任务照样建得出来，并如实落到失败状态而不是静默消失。
	if _, err := app.TriggerDownload(window.DownloadRequest{
		URL:       "http://127.0.0.1:59999/test.bin",
		Filename:  "test.bin",
		Directory: tmpDir,
	}); err != nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}
	active := waitActiveItem(t, app, "test.bin")
	created, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   2,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo failed: %v", err)
	}
	if waitAppTask(t, store, created.ID, task.StatusError) == nil {
		t.Fatalf("expected the unreachable download to end in error")
	}

	tasks, err := app.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task in list, got %d", len(tasks))
	}
	if err := app.DeleteTask(created.ID, false); err != nil {
		t.Fatalf("DeleteTask failed: %v", err)
	}
	tasksAfter, _ := app.ListTasks()
	if len(tasksAfter) != 0 {
		t.Fatalf("expected 0 tasks after delete, got %d", len(tasksAfter))
	}
}

func TestApp_UnreachableURLKeepsEditableMetadata(t *testing.T) {
	app, _, tmpDir := newTestApp(t)

	conflictRes := app.CheckFileConflict(tmpDir, "dummy.txt")
	if conflictRes.Exists || conflictRes.SuggestedFilename != "dummy.txt" {
		t.Errorf("expected no conflict, got %v", conflictRes)
	}

	// Probe failure must still leave the dialog usable: unknown size, a filename the user can edit.
	probeRes, _ := app.ProbeURL("http://127.0.0.1:59999/dummy.txt")
	if probeRes == nil || probeRes.Filename != "dummy.txt" {
		t.Fatalf("expected filename dummy.txt in probe result, got %+v", probeRes)
	}
	if probeRes.TotalBytes != -1 {
		t.Errorf("expected unknown size (-1), got %d", probeRes.TotalBytes)
	}
}

func TestApp_PreDownloadCancelKeepsPausedTask(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	payload := make([]byte, 512*1024)
	ts := serveRangedPayload(payload, "", 3*time.Millisecond)
	defer ts.Close()

	// 提前下载由文件信息窗口的队列驱动：登记时设置打开这个开关，探测落地后自己起步。
	st := app.GetSettings()
	st.Download.PreDownload = true
	if _, err := app.UpdateSettings(st); err != nil {
		t.Fatalf("failed to enable pre-download: %v", err)
	}
	if _, err := app.TriggerDownload(window.DownloadRequest{
		URL:       ts.URL + "/flow.bin",
		Filename:  "flow.bin",
		Directory: tmpDir,
	}); err != nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}
	active := waitActiveItem(t, app, "flow.bin")
	preTaskID := waitPreDownloadTask(t, app, active.ID)
	waitAppTask(t, store, preTaskID, task.StatusDownloading)

	// 用户在文件信息窗口点取消：预下载停下、但保留在列表里（转为暂停），分片留着可续传。
	if err := app.CancelCurrentFileInfo(); err != nil {
		t.Fatalf("CancelCurrentFileInfo failed: %v", err)
	}

	tasks, err := app.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != preTaskID {
		t.Fatalf("expected the cancelled pre-download to stay listed, got %d tasks", len(tasks))
	}
	if tasks[0].Status != task.StatusPaused {
		t.Errorf("expected a paused task after cancel, got %s", tasks[0].Status)
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, "flow.bin.sheepget")); statErr != nil {
		t.Errorf("partial data should be kept for a later resume: %v", statErr)
	}
}

func TestApp_FileInfoDialogFlow(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	saveDir := filepath.Join(tmpDir, "confirmed")
	payload := make([]byte, 128*1024)
	for i := range payload {
		payload[i] = byte((i * 11) % 251)
	}
	ts := serveRangedPayload(payload, "", 0)
	defer ts.Close()
	// 1. Probe reports the server-provided name, size and type for the dialog.
	probe, err := app.ProbeURL(ts.URL + "/download")
	if err != nil {
		t.Fatalf("ProbeURL failed: %v", err)
	}
	if probe.Filename != "flow.bin" || probe.TotalBytes != int64(len(payload)) || probe.DuplicateTask != nil {
		t.Fatalf("unexpected probe result: %+v", probe)
	}

	// 2. An existing same-name file is reported so the dialog can ask instead of overwriting.
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		t.Fatalf("failed to prepare save dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(saveDir, "flow.bin"), []byte("pre-existing user file"), 0o644); err != nil {
		t.Fatalf("failed to seed conflicting file: %v", err)
	}
	conflict := app.CheckFileConflict(saveDir, "flow.bin")
	if !conflict.Exists || conflict.SuggestedFilename != "flow (1).bin" {
		t.Fatalf("expected a conflict with suggestion flow (1).bin, got %+v", conflict)
	}

	// 3. 用户选择「序号副本」：按这个名字登记，提前下载起步并完成，确认后成为正式任务。
	st := app.GetSettings()
	st.Download.PreDownload = true
	if _, err := app.UpdateSettings(st); err != nil {
		t.Fatalf("failed to enable pre-download: %v", err)
	}
	if _, err := app.TriggerDownload(window.DownloadRequest{
		URL:       ts.URL + "/download",
		Filename:  conflict.SuggestedFilename,
		Directory: saveDir,
	}); err != nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}
	active := waitActiveItem(t, app, conflict.SuggestedFilename)
	preTaskID := waitPreDownloadTask(t, app, active.ID)
	waitAppTask(t, store, preTaskID, task.StatusCompleted)

	confirmed, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  conflict.SuggestedFilename,
		Directory: saveDir,
		MaxConn:   4,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo failed: %v", err)
	}
	if confirmed.Filename != "flow (1).bin" || confirmed.Directory != saveDir {
		t.Fatalf("expected flow (1).bin in %s, got %s in %s", saveDir, confirmed.Filename, confirmed.Directory)
	}
	content, err := os.ReadFile(filepath.Join(saveDir, "flow (1).bin"))
	if err != nil {
		t.Fatalf("failed to read confirmed download: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("confirmed download does not match the served resource")
	}
	untouched, err := os.ReadFile(filepath.Join(saveDir, "flow.bin"))
	if err != nil || string(untouched) != "pre-existing user file" {
		t.Fatalf("the pre-existing file must not be replaced when the user chooses a numbered copy: %v %q", err, string(untouched))
	}

	// 4. The same URL probed again is reported as a duplicate link, with the confirmed task located.
	again, err := app.ProbeURL(ts.URL + "/download")
	if err != nil {
		t.Fatalf("ProbeURL failed: %v", err)
	}
	if again.DuplicateTask == nil || again.DuplicateTask.ID != confirmed.ID {
		t.Fatalf("expected the confirmed task to be reported as a duplicate, got %+v", again.DuplicateTask)
	}

	// 5. 用户为这次重复选定「序号副本」：得到另一份独立的成品，不占用已有文件名。
	if _, err := app.TriggerDownload(window.DownloadRequest{
		URL:       ts.URL + "/download",
		Filename:  "flow.bin",
		Directory: saveDir,
	}); err != nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}
	copyActive := waitActiveItem(t, app, "flow.bin")
	copyTask, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: copyActive.ID,
		URL:       copyActive.URL,
		Filename:  "flow.bin",
		Directory: saveDir,
		MaxConn:   2,
		Action:    duplicate.ActionCopy,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo with copy action failed: %v", err)
	}
	if copyTask.ID == confirmed.ID || copyTask.Filename == confirmed.Filename {
		t.Fatalf("expected a distinct task with a free name, got %s / %s", copyTask.ID, copyTask.Filename)
	}
}

func TestApp_WindowQueue_Lifecycle(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	payload := []byte("window queue lifecycle payload")
	ts := serveRangedPayload(payload, "", 0)
	defer ts.Close()

	// 1. Trigger download via queue
	resp, err := app.TriggerDownload(window.DownloadRequest{
		URL:       ts.URL + "/queue_item.bin",
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}
	if !resp.Handled || resp.Action != "enqueued" {
		t.Fatalf("expected enqueued response, got %+v", resp)
	}
	if app.fileInfoQueueLength() != 1 {
		t.Fatalf("expected queue length 1, got %d", app.fileInfoQueueLength())
	}

	var active *window.FileInfoItem
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		active, _ = app.GetActiveFileInfo()
		if active != nil && active.Filename == "flow.bin" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if active == nil {
		t.Fatalf("expected active file info, got nil")
	}
	if active.Filename != "flow.bin" {
		t.Errorf("expected filename flow.bin from Content-Disposition, got %s", active.Filename)
	}

	// 2. Submit active file info
	submittedTask, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   4,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo failed: %v", err)
	}
	if submittedTask == nil {
		t.Fatalf("expected created task from submission")
	}
	if app.fileInfoQueueLength() != 0 {
		t.Fatalf("queue should be empty after submission, got %d", app.fileInfoQueueLength())
	}

	// 3. Wait for completed task
	done := waitAppTask(t, store, submittedTask.ID, task.StatusCompleted)
	if done == nil || done.Downloaded != int64(len(payload)) {
		t.Fatalf("expected completed task with payload length %d, got %v", len(payload), done)
	}
}

func TestApp_ProgressWindow_SettingsAndSubmit(t *testing.T) {
	app, _, tmpDir := newTestApp(t)
	payload := []byte("progress window test payload")
	ts := serveRangedPayload(payload, "", 0)
	defer ts.Close()

	// Test 1: Submit with default settings (showProgressWindow: true)
	resp, err := app.TriggerDownload(window.DownloadRequest{
		URL:       ts.URL + "/progress_item_1.bin",
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}
	if !resp.Handled {
		t.Fatalf("expected handled response")
	}
	active, err := app.GetActiveFileInfo()
	if err != nil || active == nil {
		t.Fatalf("expected active file info, got %v", active)
	}

	task1, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   2,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo failed: %v", err)
	}
	if task1 == nil {
		t.Fatalf("expected created task")
	}

	// Test 2: Update settings to showProgressWindow: false
	currSettings := app.settings.Get()
	currSettings.Download.ShowProgressWindow = false
	_, err = app.settings.Update(currSettings)
	if err != nil {
		t.Fatalf("failed to update settings: %v", err)
	}

	resp2, err := app.TriggerDownload(window.DownloadRequest{
		URL:       ts.URL + "/progress_item_2.bin",
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("TriggerDownload 2 failed: %v", err)
	}
	if !resp2.Handled {
		t.Fatalf("expected handled response 2")
	}
	active2, err := app.GetActiveFileInfo()
	if err != nil || active2 == nil {
		t.Fatalf("expected active file info 2, got %v", active2)
	}

	task2, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: active2.ID,
		URL:       active2.URL,
		Filename:  active2.Filename,
		Directory: active2.Directory,
		MaxConn:   2,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo 2 failed: %v", err)
	}
	if task2 == nil {
		t.Fatalf("expected created task 2")
	}

	// Test 3: RetryProcessingTask interface boundary
	retryErr := app.RetryProcessingTask(task2.ID)
	if retryErr != nil {
		t.Logf("RetryProcessingTask returned (expected if task is in another state): %v", retryErr)
	}
}

func TestApp_CheckURLFilesExist_MultiCopiesInTaskList(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	ctx := context.Background()
	targetURL := "https://example.com/archive.zip"

	// Existing files on disk: archive.zip (base), archive (1).zip (copy 1)
	_ = os.WriteFile(filepath.Join(tmpDir, "archive.zip"), []byte("data0"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "archive (1).zip"), []byte("data1"), 0644)

	// Existing tasks in store: archive.zip (base), archive (1).zip (copy 1), archive (2).zip (copy 2)
	t0 := &task.Task{ID: "t0", URL: targetURL, Filename: "archive.zip", Directory: tmpDir, Status: task.StatusCompleted}
	t1 := &task.Task{ID: "t1", URL: targetURL, Filename: "archive (1).zip", Directory: tmpDir, Status: task.StatusCompleted}
	t2 := &task.Task{ID: "t2", URL: targetURL, Filename: "archive (2).zip", Directory: tmpDir, Status: task.StatusDownloading}
	_ = store.Save(ctx, t0)
	_ = store.Save(ctx, t1)
	_ = store.Save(ctx, t2)

	// CheckURLFilesExist should recognize the existing disk files and suggest archive (3).zip
	res := app.CheckURLFilesExist(targetURL, tmpDir, "archive.zip")
	if !res.Exists {
		t.Errorf("expected duplicate existence, got false")
	}
	if res.SuggestedFilename != "archive (3).zip" {
		t.Errorf("expected suggested archive (3).zip, got %s", res.SuggestedFilename)
	}

	// CheckFileConflict should also recognize the existing tasks and suggest archive (3).zip
	conflictRes := app.CheckFileConflict(tmpDir, "archive.zip")
	if !conflictRes.Exists {
		t.Errorf("expected conflict existence, got false")
	}
	if conflictRes.SuggestedFilename != "archive (3).zip" {
		t.Errorf("expected suggested archive (3).zip, got %s", conflictRes.SuggestedFilename)
	}
}
func TestApp_CheckURLFilesExist_ReadOnlySuggestion_AllMissing(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	ctx := context.Background()
	targetURL := "https://example.com/item.zip"

	// Base file exists on disk
	_ = os.WriteFile(filepath.Join(tmpDir, "item.zip"), []byte("base"), 0644)
	baseTask := &task.Task{ID: "t_base", URL: targetURL, Filename: "item.zip", Directory: tmpDir, Status: task.StatusCompleted}
	_ = store.Save(ctx, baseTask)

	// Copies 1-5 exist in store, but NONE exist on disk
	for i := 1; i <= 5; i++ {
		tName := fmt.Sprintf("item (%d).zip", i)
		tID := fmt.Sprintf("t_copy_%d", i)
		_ = store.Save(ctx, &task.Task{
			ID:        tID,
			URL:       targetURL,
			Filename:  tName,
			Directory: tmpDir,
			Status:    task.StatusCompleted,
		})
	}

	// CheckURLFilesExist should suggest item (1).zip WITHOUT deleting any tasks
	res := app.CheckURLFilesExist(targetURL, tmpDir, "item.zip")
	if !res.Exists {
		t.Errorf("expected base duplicate existence to be true, got false")
	}
	if res.SuggestedFilename != "item (1).zip" {
		t.Errorf("expected suggested item (1).zip, got %s", res.SuggestedFilename)
	}

	// Verify tasks in store: all 6 tasks MUST still exist at dialog preview time!
	beforeSubmit, _ := store.List(ctx)
	if len(beforeSubmit) != 6 {
		t.Fatalf("CheckURLFilesExist must be read-only! expected 6 tasks, got %d", len(beforeSubmit))
	}

	// When user resolves with "copy" strategy, stale copies 1-5 are deleted and new task is created
	newTask, err := app.manager.ResolveDuplicate(ctx, "t_base", "copy", tmpDir, "item.zip", 2)
	if err != nil {
		t.Fatalf("ResolveDuplicate copy failed: %v", err)
	}
	if newTask.Filename != "item (1).zip" {
		t.Errorf("expected new task item (1).zip, got %s", newTask.Filename)
	}

	afterSubmit, _ := store.List(ctx)
	if len(afterSubmit) != 2 {
		t.Fatalf("expected 2 tasks after copy resolve (base + new), got %d: %+v", len(afterSubmit), afterSubmit)
	}
}

func TestApp_CheckURLFilesExist_ReadOnlySuggestion_HoleMissing(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	ctx := context.Background()
	targetURL := "https://example.com/item.zip"

	// Base file exists on disk
	_ = os.WriteFile(filepath.Join(tmpDir, "item.zip"), []byte("base"), 0644)
	baseTask := &task.Task{ID: "t_base", URL: targetURL, Filename: "item.zip", Directory: tmpDir, Status: task.StatusCompleted}
	_ = store.Save(ctx, baseTask)

	// Copies 1, 2, 4, 5 exist on disk; copy 3 does NOT exist on disk
	for i := 1; i <= 5; i++ {
		tName := fmt.Sprintf("item (%d).zip", i)
		tID := fmt.Sprintf("t_copy_%d", i)
		if i != 3 {
			_ = os.WriteFile(filepath.Join(tmpDir, tName), []byte("copy"), 0644)
		}
		_ = store.Save(ctx, &task.Task{
			ID:        tID,
			URL:       targetURL,
			Filename:  tName,
			Directory: tmpDir,
			Status:    task.StatusCompleted,
		})
	}

	// CheckURLFilesExist suggests item (3).zip without deleting copy 3
	res := app.CheckURLFilesExist(targetURL, tmpDir, "item.zip")
	if !res.Exists {
		t.Errorf("expected existence to be true, got false")
	}
	if res.SuggestedFilename != "item (3).zip" {
		t.Errorf("expected suggested item (3).zip, got %s", res.SuggestedFilename)
	}

	c3Before, _ := store.Get(ctx, "t_copy_3")
	if c3Before == nil {
		t.Fatalf("copy 3 must not be deleted at preview time")
	}

	// Confirm download with copy strategy: copy 3 is removed, new task is item (3).zip
	newTask, err := app.manager.ResolveDuplicate(ctx, "t_base", "copy", tmpDir, "item.zip", 2)
	if err != nil {
		t.Fatalf("ResolveDuplicate copy failed: %v", err)
	}
	if newTask.Filename != "item (3).zip" {
		t.Errorf("expected item (3).zip, got %s", newTask.Filename)
	}

	c3After, _ := store.Get(ctx, "t_copy_3")
	if c3After != nil {
		t.Errorf("expected t_copy_3 to be deleted after copy resolve")
	}
}

func TestApp_CheckURLFilesExist_DiskFileNotExists_ReportsNotExistsAndDirectDownloadCleansOld(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	ctx := context.Background()
	targetURL := "https://example.com/no_disk_file.zip"

	// Old completed task in store, but disk file was deleted by user!
	oldTask := &task.Task{
		ID:        "t_old",
		URL:       targetURL,
		Filename:  "no_disk_file.zip",
		Directory: tmpDir,
		Status:    task.StatusCompleted,
	}
	_ = store.Save(ctx, oldTask)

	// Since file does NOT exist on disk, CheckURLFilesExist MUST return exists: false!
	res := app.CheckURLFilesExist(targetURL, tmpDir, "no_disk_file.zip")
	if res.Exists {
		t.Errorf("expected exists=false when disk file is deleted, got true")
	}
	if res.SuggestedFilename != "no_disk_file.zip" {
		t.Errorf("expected suggested filename no_disk_file.zip, got %s", res.SuggestedFilename)
	}

	// When user enqueues and confirms download for this request without choosing an action:
	// 目标位置没有成品文件，历史任务也只是残留记录，后端默认重新下载。
	enqResp, err := app.TriggerDownload(window.DownloadRequest{
		URL:       targetURL,
		Directory: tmpDir,
		Filename:  "no_disk_file.zip",
	})
	if err != nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}

	subTask, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: enqResp.RequestID,
		URL:       targetURL,
		Filename:  "no_disk_file.zip",
		Directory: tmpDir,
		MaxConn:   2,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo failed: %v", err)
	}
	if subTask == nil {
		t.Fatalf("expected created task")
	}

	// Verify the old stale task was deleted
	ot, _ := store.Get(ctx, "t_old")
	if ot != nil {
		t.Errorf("expected old stale task t_old to be deleted on direct download")
	}

	// Verify only the new task remains in store
	tasks, _ := store.List(ctx)
	if len(tasks) != 1 {
		t.Errorf("expected exactly 1 task in store, got %d", len(tasks))
	}
}

func TestApp_SetCategoryDirectory(t *testing.T) {
	app, _, tmpDir := newTestApp(t)
	customDir := filepath.Join(tmpDir, "custom_video")

	// Update builtin category directory
	err := app.SetCategoryDirectory("builtin-video", customDir)
	if err != nil {
		t.Fatalf("SetCategoryDirectory failed: %v", err)
	}

	settings := app.GetSettings()
	found := false
	for _, b := range settings.Download.BuiltinCategories {
		if b.ID == "builtin-video" {
			found = true
			if b.Directory != customDir {
				t.Errorf("expected %s, got %s", customDir, b.Directory)
			}
		}
	}
	if !found {
		t.Errorf("builtin-video not found in settings")
	}
}

func TestApp_CheckURLFilesExist_CrossDirectoryReuse(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	ctx := context.Background()
	targetURL := "https://example.com/reuse_target.bin"
	content := []byte("reuse-identical-payload-content-12345")

	dirOld := filepath.Join(tmpDir, "old_folder")
	_ = os.MkdirAll(dirOld, 0755)
	oldFilePath := filepath.Join(dirOld, "reuse_target.bin")
	_ = os.WriteFile(oldFilePath, content, 0644)

	oldTask := &task.Task{
		ID:         "task_old_complete",
		URL:        targetURL,
		Filename:   "reuse_target.bin",
		Directory:  dirOld,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(content)),
		Downloaded: int64(len(content)),
	}
	_ = store.Save(ctx, oldTask)

	dirNew := filepath.Join(tmpDir, "new_folder")
	_ = os.MkdirAll(dirNew, 0755)

	// 1. CheckURLFilesExist in dirNew should detect the identical file in dirOld
	res := app.CheckURLFilesExist(targetURL, dirNew, "reuse_target.bin")
	if res.Exists {
		t.Errorf("expected exists=false in dirNew, got true")
	}
	if !res.CanReuseExistingFile {
		t.Fatalf("expected CanReuseExistingFile=true")
	}
	if res.ExistingTaskID != oldTask.ID {
		t.Errorf("expected existing task ID %s, got %s", oldTask.ID, res.ExistingTaskID)
	}
	if res.ExistingPath != oldFilePath {
		t.Errorf("expected existing path %s, got %s", oldFilePath, res.ExistingPath)
	}

	// 2. Perform ReuseExistingFile
	reusedTask, err := app.manager.ReuseExistingFile(context.Background(), oldTask.ID, dirNew, "reuse_target.bin")
	if err != nil {
		t.Fatalf("ReuseExistingFile failed: %v", err)
	}
	if reusedTask.Directory != dirNew {
		t.Errorf("expected directory %s, got %s", dirNew, reusedTask.Directory)
	}
	if reusedTask.Status != task.StatusCompleted {
		t.Errorf("expected status completed, got %s", reusedTask.Status)
	}

	// Verify file was moved to dirNew
	newFilePath := filepath.Join(dirNew, "reuse_target.bin")
	if _, err := os.Stat(newFilePath); err != nil {
		t.Errorf("expected file to exist at %s, got error: %v", newFilePath, err)
	}
}

func TestApp_CheckURLFilesExist_MultiStaleDuplicates_AllCleaned(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	ctx := context.Background()
	targetURL := "https://example.com/multi_stale.bin"

	// Multiple duplicate tasks in store whose physical files are gone
	t1 := &task.Task{
		ID:        "t_stale_1",
		URL:       targetURL,
		Filename:  "multi_stale.bin",
		Directory: tmpDir,
		Status:    task.StatusCompleted,
	}
	t2 := &task.Task{
		ID:        "t_stale_2",
		URL:       targetURL,
		Filename:  "multi_stale.bin",
		Directory: tmpDir,
		Status:    task.StatusError,
	}
	_ = store.Save(ctx, t1)
	_ = store.Save(ctx, t2)

	enqResp, err := app.TriggerDownload(window.DownloadRequest{
		URL:       targetURL,
		Directory: tmpDir,
		Filename:  "multi_stale.bin",
	})
	if err != nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}

	// 界面提示「此前已下载过此链接，当前目录下无同名文件」，用户未另行选择动作。
	subTask, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: enqResp.RequestID,
		URL:       targetURL,
		Filename:  "multi_stale.bin",
		Directory: tmpDir,
		MaxConn:   2,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo failed: %v", err)
	}
	if subTask == nil {
		t.Fatalf("expected subTask created")
	}

	// All stale tasks should be cleaned!
	tasks, _ := store.List(ctx)
	if len(tasks) != 1 {
		t.Fatalf("expected only 1 task remaining, got %d", len(tasks))
	}
	if tasks[0].ID != subTask.ID {
		t.Errorf("expected remaining task to be %s, got %s", subTask.ID, tasks[0].ID)
	}
}

func TestApp_ToggleClipboardSettingsNoDeadlock(t *testing.T) {
	app, _, _ := newTestApp(t)

	// Rapidly toggle clipboard enabled back and forth
	for i := 0; i < 10; i++ {
		st := app.GetSettings()
		st.Clipboard.Enabled = !st.Clipboard.Enabled
		updated, err := app.UpdateSettings(st)
		if err != nil {
			t.Fatalf("UpdateSettings failed on iteration %d: %v", i, err)
		}
		if updated.Clipboard.Enabled != st.Clipboard.Enabled {
			t.Errorf("iteration %d: expected Clipboard.Enabled %v, got %v", i, st.Clipboard.Enabled, updated.Clipboard.Enabled)
		}
	}
}

// TestApp_ResolveDestination pins the single source of truth for the frontend: the file
// info window reads the matched category and directory from here instead of implementing
// the category rule a second time in TypeScript.
func TestApp_ResolveDestination(t *testing.T) {
	app, _, _ := newTestApp(t)

	for _, name := range []string{"clip.mp4", "readme.pdf", "unknown.zzz", "Makefile"} {
		got := app.ResolveDestination(name)
		if got.CategoryID == "" {
			t.Fatalf("expected a matched category for %q, got none", name)
		}
		if got.Directory == "" {
			t.Fatalf("expected a resolved directory for %q, got none", name)
		}
		if want := app.settings.Get().Download.ResolveCategoryDirectory(name); want != got.Directory {
			t.Fatalf("ResolveDestination(%q) directory %q disagrees with ResolveCategoryDirectory %q", name, got.Directory, want)
		}
	}
}

// seedHistoryElsewhere 在另一个目录放一条已完成的历史记录，模拟「手动选过目录、默认目录
// 后来改过、或文件被搬走过」之后，历史记录所在目录与这次解析出的保存目录不一致的情形。
// writeFile 为真时同时落下成品文件，用于验证不该被误删的情况。
func seedHistoryElsewhere(t *testing.T, store task.TaskStore, tmpDir, id, url, filename string, writeFile bool) *task.Task {
	t.Helper()
	historyDir := filepath.Join(tmpDir, "elsewhere")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("failed to create history dir: %v", err)
	}
	history := &task.Task{
		ID:        id,
		URL:       url,
		Filename:  filename,
		Directory: historyDir,
		Status:    task.StatusCompleted,
	}
	if err := store.Save(context.Background(), history); err != nil {
		t.Fatalf("failed to seed history: %v", err)
	}
	if writeFile {
		if err := os.WriteFile(filepath.Join(historyDir, filename), []byte("kept"), 0o644); err != nil {
			t.Fatalf("failed to seed history file: %v", err)
		}
	}
	return history
}

// confirmDirectTrigger 模拟浏览器/剪贴板触发（请求不带目录与文件名）并按界面收到的默认动作确认。
func confirmDirectTrigger(t *testing.T, app *App, url string) *task.Task {
	t.Helper()
	enqResp, err := app.TriggerDownload(window.DownloadRequest{URL: url})
	if err != nil || enqResp == nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}
	active, err := app.GetActiveFileInfo()
	if err != nil || active == nil {
		t.Fatalf("expected active item: %v", err)
	}
	resTask, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: enqResp.RequestID,
		URL:       url,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   2,
		Action:    active.DuplicateDecision.Default,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo failed: %v", err)
	}
	return resTask
}

// 实测复现：历史任务已完成、成品文件已被删除时，确认下载必须清掉那条历史记录；
// 清理不能取决于它当初恰好保存在哪个目录。
func TestApp_CompletedHistoryWithoutFile_CleansOldRecordRegardlessOfDirectory(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	ctx := context.Background()
	targetURL := "https://example.com/vanished.bin"

	history := seedHistoryElsewhere(t, store, tmpDir, "t_vanished", targetURL, "vanished.bin", false)

	if confirmDirectTrigger(t, app, targetURL) == nil {
		t.Fatal("expected a fresh download task")
	}

	if old, _ := store.Get(ctx, history.ID); old != nil {
		t.Errorf("成品文件已不在磁盘上的历史记录应当被清理，got %+v", old)
	}
}

// 反向保证：历史任务的文件仍在（哪怕是别的目录），那是用户自己的一份成品，不得清掉。
func TestApp_CompletedHistoryWithFileElsewhere_IsKept(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	ctx := context.Background()
	targetURL := "https://example.com/still_here.bin"

	history := seedHistoryElsewhere(t, store, tmpDir, "t_still_here", targetURL, "still_here.bin", true)

	if confirmDirectTrigger(t, app, targetURL) == nil {
		t.Fatal("expected a fresh download task")
	}

	if kept, _ := store.Get(ctx, history.ID); kept == nil {
		t.Errorf("文件仍存在的历史记录不应被清理")
	}
	if _, err := os.Stat(filepath.Join(history.Directory, history.Filename)); err != nil {
		t.Errorf("别的目录里的成品文件不应被动到: %v", err)
	}
}

// 勾选「同时删除磁盘文件」时成品与分片一并清掉；不勾选时只移除记录，磁盘文件保留。
func TestApp_DeleteTask_RemovesDiskFilesOnlyWhenRequested(t *testing.T) {
	ctx := context.Background()

	seedTaskWithFiles := func(t *testing.T, store task.TaskStore, tmpDir, id string) (*task.Task, []string) {
		t.Helper()
		dir := filepath.Join(tmpDir, "downloads-"+id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create download dir: %v", err)
		}
		tt := &task.Task{
			ID:         id,
			URL:        "https://example.com/" + id + ".bin",
			Filename:   id + ".bin",
			Directory:  dir,
			Status:     task.StatusCompleted,
			TotalBytes: 4,
			Downloaded: 4,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		if err := store.Save(ctx, tt); err != nil {
			t.Fatalf("failed to seed task: %v", err)
		}
		paths := []string{
			filepath.Join(dir, tt.Filename),
			filepath.Join(dir, tt.Filename+".sheepget"),
		}
		for _, path := range paths {
			if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
				t.Fatalf("failed to seed %s: %v", path, err)
			}
		}
		return tt, paths
	}

	t.Run("with disk files", func(t *testing.T) {
		app, store, tmpDir := newTestApp(t)
		tt, paths := seedTaskWithFiles(t, store, tmpDir, "del_disk")

		if err := app.DeleteTask(tt.ID, true); err != nil {
			t.Fatalf("DeleteTask failed: %v", err)
		}
		for _, path := range paths {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("expected %s to be removed, stat err = %v", path, err)
			}
		}
		if _, err := store.Get(ctx, tt.ID); err == nil {
			t.Error("task record should be removed")
		}
	})

	t.Run("records only", func(t *testing.T) {
		app, store, tmpDir := newTestApp(t)
		tt, paths := seedTaskWithFiles(t, store, tmpDir, "del_record")

		if err := app.DeleteTask(tt.ID, false); err != nil {
			t.Fatalf("DeleteTask failed: %v", err)
		}
		for _, path := range paths {
			if _, err := os.Stat(path); err != nil {
				t.Errorf("expected %s to be kept, stat err = %v", path, err)
			}
		}
		if _, err := store.Get(ctx, tt.ID); err == nil {
			t.Error("task record should be removed")
		}
	})
}

func TestApp_HandleHandover(t *testing.T) {
	app, _, _ := newTestApp(t)
	ctx := context.Background()

	st := app.GetSettings()
	st.Takeover.ExcludedSites = []string{"excluded.com"}
	_, err := app.UpdateSettings(st)
	if err != nil {
		t.Fatalf("failed to update settings: %v", err)
	}

	// 1. Excluded site handover -> rejected
	reqExcluded := &server.HandoverRequest{
		SourceType: "browser_takeover",
		URL:        "https://cdn.example.com/archive.zip",
		PageContext: server.PageContext{
			PageURL: "https://sub.excluded.com/download",
		},
	}
	resp, err := app.handleHandover(ctx, reqExcluded)
	if err != nil {
		t.Fatalf("handleHandover failed: %v", err)
	}
	if resp.Accepted || resp.Reason != "site_excluded" {
		t.Errorf("expected site_excluded rejection, got %+v", resp)
	}

	// 2. Normal site handover -> accepted and enqueued
	reqNormal := &server.HandoverRequest{
		SourceType:         "browser_takeover",
		URL:                "https://normal.com/archive.zip",
		FilenameSuggestion: "myarchive.zip",
		PageContext: server.PageContext{
			PageURL:  "https://normal.com/download.html",
			Referrer: "https://normal.com/index.html",
		},
		Credentials: &server.CredentialsPayload{
			Cookies: "session=xyz123",
			Headers: map[string]string{
				"User-Agent": "TestAgent",
			},
		},
	}
	resp, err = app.handleHandover(ctx, reqNormal)
	if err != nil {
		t.Fatalf("handleHandover failed: %v", err)
	}
	if !resp.Accepted || resp.QueueItemID == "" {
		t.Errorf("expected accepted handover, got %+v", resp)
	}

	activeItem, err := app.GetActiveFileInfo()
	if err != nil || activeItem == nil {
		t.Fatalf("failed to get active file info item: %v", err)
	}
	if activeItem.PageURL != "https://normal.com/download.html" {
		t.Errorf("expected activeItem.PageURL = %q, got %q", "https://normal.com/download.html", activeItem.PageURL)
	}

	createdTask, err := app.SubmitFileInfo(window.FileInfoSubmission{
		RequestID: activeItem.ID,
		URL:       activeItem.URL,
		Filename:  "myarchive.zip",
		Directory: activeItem.Directory,
		MaxConn:   4,
	})
	if err != nil {
		t.Fatalf("SubmitFileInfo failed: %v", err)
	}
	if createdTask.PageURL != "https://normal.com/download.html" {
		t.Errorf("expected createdTask.PageURL = %q, got %q", "https://normal.com/download.html", createdTask.PageURL)
	}

	// 交接带过来的请求上下文按同一份整理落到任务上：扩展给的自定义头保留，Cookie 与 Referer 补齐。
	if createdTask.RequestHeaders == nil {
		t.Fatalf("expected the handover request context to be stored on the task")
	}
	headers := createdTask.RequestHeaders.RawHeaders()
	for name, want := range map[string]string{
		"User-Agent": "TestAgent",
		"Cookie":     "session=xyz123",
		"Referer":    "https://normal.com/index.html",
	} {
		if headers[name] != want {
			t.Errorf("expected header %s=%q on the created task, got %q", name, want, headers[name])
		}
	}
}

func TestApp_ProgressWindowAlwaysOnTop(t *testing.T) {
	app, _, _ := newTestApp(t)

	if app.IsProgressWindowAlwaysOnTop() {
		t.Errorf("expected always on top to default to false")
	}

	// Toggle to true
	state := app.ToggleProgressWindowAlwaysOnTop()
	if !state || !app.IsProgressWindowAlwaysOnTop() {
		t.Errorf("expected always on top to toggle to true")
	}

	// Toggle back to false
	state = app.ToggleProgressWindowAlwaysOnTop()
	if state || app.IsProgressWindowAlwaysOnTop() {
		t.Errorf("expected always on top to toggle to false")
	}
}

func TestApp_SelectDirectory_AppNotInitialized(t *testing.T) {
	app, _, tmpDir := newTestApp(t)

	// app.getApp() returns nil in test environment
	_, err := app.SelectDirectory(tmpDir)
	if err == nil || !strings.Contains(err.Error(), "application not initialized") {
		t.Errorf("expected application not initialized error, got: %v", err)
	}
}

func TestApp_OpenFile_NonExistent(t *testing.T) {
	app, _, tmpDir := newTestApp(t)

	err := app.OpenFile(filepath.Join(tmpDir, "missing_file_xyz.bin"))
	if err == nil {
		t.Fatalf("expected error opening non-existent file")
	}
	if !strings.Contains(err.Error(), "文件不存在或已被删除") {
		t.Errorf("expected missing file error message, got: %v", err)
	}
}

func TestApp_LaunchAtStartup(t *testing.T) {
	app, _, _ := newTestApp(t)

	// 自启动的开关走设置这一条路：界面改设置，桌面端在设置落地时把系统注册同步过去。
	if app.GetSettings().General.LaunchAtStartup {
		t.Errorf("expected launch at startup to be off by default")
	}
	for _, enabled := range []bool{true, false} {
		current := app.GetSettings()
		current.General.LaunchAtStartup = enabled
		updated, err := app.UpdateSettings(current)
		if err != nil {
			t.Fatalf("UpdateSettings failed: %v", err)
		}
		if updated.General.LaunchAtStartup != enabled {
			t.Errorf("expected updated settings to report %v", enabled)
		}
		if app.GetSettings().General.LaunchAtStartup != enabled {
			t.Errorf("expected the stored setting to be %v", enabled)
		}
	}
}

func TestApp_GetStorageInfo(t *testing.T) {
	app, _, _ := newTestApp(t)

	info := app.GetStorageInfo()
	if info == nil {
		t.Fatalf("expected non-nil storage info")
	}
	if info["mode"] == "" || info["dataDir"] == "" {
		t.Errorf("expected mode and dataDir in storage info, got: %+v", info)
	}
}

func TestApp_ServerPortManagement(t *testing.T) {
	app, _, _ := newTestApp(t)

	status := app.GetServerStatus()
	if status.Port <= 0 {
		t.Errorf("expected positive server port, got %d", status.Port)
	}
	if app.GetSettings().General.ServerPort != status.Port {
		t.Errorf("expected settings General.ServerPort %d to match status.Port %d on startup", app.GetSettings().General.ServerPort, status.Port)
	}

	// Restart on a fresh available port
	testLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on test port: %v", err)
	}
	targetPort := testLn.Addr().(*net.TCPAddr).Port
	_ = testLn.Close()

	restartedPort, err := app.RestartServer(targetPort)
	if err != nil {
		t.Fatalf("RestartServer failed: %v", err)
	}
	if restartedPort != targetPort {
		t.Errorf("expected restarted port %d, got %d", targetPort, restartedPort)
	}
	if app.GetServerStatus().Port != targetPort {
		t.Errorf("expected GetServerStatus port %d, got %d", targetPort, app.GetServerStatus().Port)
	}
	if app.GetSettings().General.ServerPort != targetPort {
		t.Errorf("expected settings General.ServerPort %d, got %d", targetPort, app.GetSettings().General.ServerPort)
	}
}

func TestApp_GetServerStatus(t *testing.T) {
	app, _, _ := newTestApp(t)

	status := app.GetServerStatus()
	if !status.Running {
		t.Errorf("expected server to be running")
	}
	if status.Port <= 0 {
		t.Errorf("expected positive server port, got %d", status.Port)
	}
	if status.ConnectedCount < 0 {
		t.Errorf("expected non-negative connected count, got %d", status.ConnectedCount)
	}
}

// 日志开关：默认不落盘，打开后写文件，关掉后停止写入并释放文件句柄。
func TestApp_LoggingSwitch(t *testing.T) {
	app, _, tmpDir := newTestApp(t)

	logPath := filepath.Join(tmpDir, "logs", "sheepget.log")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("expected no log file before enabling logging, stat err = %v", err)
	}

	app.applyLogging(true)
	app.log().Info("enabled record")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected the log file after enabling logging: %v", err)
	}
	if !strings.Contains(string(content), "enabled record") {
		t.Fatalf("expected the record in the log file, got %q", content)
	}
	if app.log().Path() != logPath {
		t.Fatalf("expected the active log path %s, got %s", logPath, app.log().Path())
	}

	app.applyLogging(false)
	app.log().Info("disabled record")
	after, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read the log file: %v", err)
	}
	if strings.Contains(string(after), "disabled record") {
		t.Fatal("logging must stop once the switch is off")
	}
	if app.log().Path() != "" {
		t.Fatalf("expected no active log path after disabling, got %s", app.log().Path())
	}
}

// 「这个名字被占了」的占用来源里包含窗口队列已经发出的名字，但正在编辑的那一项不算占着
// 自己的名字——窗口里的冲突提示回答的是「除了它自己，还有谁占着」。
func TestApp_CheckFileConflict_ExcludesEditedItem(t *testing.T) {
	app, _, tmpDir := newTestApp(t)
	ctx := context.Background()
	enqueue := func(filename string) {
		t.Helper()
		if _, err := app.windowQueue.Enqueue(ctx, window.DownloadRequest{
			URL:       "http://127.0.0.1:1/" + filename,
			Filename:  filename,
			Directory: tmpDir,
		}); err != nil {
			t.Fatalf("enqueue %s failed: %v", filename, err)
		}
	}

	enqueue("report.pdf")
	own := app.CheckFileConflict(tmpDir, "report.pdf")
	if own.Exists || own.SuggestedFilename != "report.pdf" {
		t.Errorf("正在编辑的这一项不该和它自己的名字冲突，got %+v", own)
	}

	// 另一项占着 report (1).pdf：窗口问它时必须报冲突，并给出仍然可用的序号副本。
	enqueue("report (1).pdf")
	other := app.CheckFileConflict(tmpDir, "report (1).pdf")
	if !other.Exists {
		t.Errorf("另一个排队项占着的名字必须报冲突，got %+v", other)
	}
	if other.SuggestedFilename != "report (2).pdf" {
		t.Errorf("expected report (2).pdf, got %s", other.SuggestedFilename)
	}
}

// 主窗口的关闭策略：非轻量模式关到托盘（拦下这次关闭并隐藏），轻量模式放行让宿主销毁。
func TestApp_MainWindowClosePolicy(t *testing.T) {
	app, _, _ := newTestApp(t)
	host := &headlessHost{}
	app.declareWindows(host)

	if _, ok := app.windows.Show(winNameMain, "/"); !ok {
		t.Fatalf("expected the main window to be created")
	}
	win := host.windows[winNameMain]
	if !win.onClose() {
		t.Errorf("non-lightweight: closing the main window must be intercepted")
	}
	if win.visible {
		t.Errorf("non-lightweight: the intercepted close must hide the window into the tray")
	}

	st := app.GetSettings()
	st.General.LightweightMode = true
	if _, err := app.UpdateSettings(st); err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}
	win.visible = true
	if win.onClose() {
		t.Errorf("lightweight: the close must be allowed through so the host can destroy the window")
	}
}

// 关掉文件信息窗口就是「这次下载不要了」：拦下关闭，并把队列里的当前项取消掉。
func TestApp_FileInfoWindowCloseCancelsCurrent(t *testing.T) {
	app, _, tmpDir := newTestApp(t)
	host := &headlessHost{}
	app.declareWindows(host)

	if _, err := app.TriggerDownload(window.DownloadRequest{
		URL:       "http://127.0.0.1:59999/cancel.bin",
		Filename:  "cancel.bin",
		Directory: tmpDir,
	}); err != nil {
		t.Fatalf("TriggerDownload failed: %v", err)
	}
	waitActiveItem(t, app, "cancel.bin")

	if _, ok := app.windows.Show(winNameFileInfo, ""); !ok {
		t.Fatalf("expected the file info window to be created")
	}
	if cancel := host.windows[winNameFileInfo].onClose(); !cancel {
		t.Fatalf("closing the file info window must be intercepted")
	}
	if length := app.fileInfoQueueLength(); length != 0 {
		t.Errorf("closing the file info window must cancel the current item, %d left", length)
	}
}
