package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sheep-get/internal/config"
	"sheep-get/internal/engine"
	"sheep-get/internal/task"
	"sheep-get/internal/window"
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
	}
	winView := &wailsWindowView{
		getApp:      app.getApp,
		name:        "fileinfo",
		getSettings: settingsSvc.Get,
	}
	app.windowQueue = window.NewQueueController(mgr, settingsSvc, winView)
	app.windowQueue.SetOnShowCompleted(app.ShowProgressWindow)
	app.startup(context.Background())
	return app, store, tmpDir
}

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
	app, _, tmpDir := newTestApp(t)

	// Test AddTask with unreachable URL to verify error handling without panic
	created, err := app.AddTask("http://127.0.0.1:59999/test.bin", tmpDir, "test.bin", 2)
	if err != nil {
		t.Fatalf("unexpected error adding task: %v", err)
	}
	if created.Status != task.StatusError {
		t.Fatalf("expected status error, got %v", created.Status)
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

	preTask, err := app.StartPreDownload(ts.URL+"/flow.bin", tmpDir, "flow.bin", 2)
	if err != nil {
		t.Fatalf("StartPreDownload failed: %v", err)
	}
	waitAppTask(t, store, preTask.ID, task.StatusDownloading)

	if err := app.CancelPreDownload(preTask.ID); err != nil {
		t.Fatalf("CancelPreDownload failed: %v", err)
	}

	tasks, err := app.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != preTask.ID {
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
	ctx := context.Background()

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

	// 3. The user answers "add a number": pre-download runs under the chosen name and the original survives.
	preTask, err := app.StartPreDownload(ts.URL+"/download", saveDir, conflict.SuggestedFilename, 2)
	if err != nil {
		t.Fatalf("StartPreDownload failed: %v", err)
	}
	waitAppTask(t, store, preTask.ID, task.StatusCompleted)

	confirmed, err := app.ConfirmPreDownload(preTask.ID, saveDir, conflict.SuggestedFilename, 4)
	if err != nil {
		t.Fatalf("ConfirmPreDownload failed: %v", err)
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
	if again.DuplicateTask == nil || again.DuplicateTask.ID != preTask.ID {
		t.Fatalf("expected the confirmed task to be reported as a duplicate, got %+v", again.DuplicateTask)
	}

	// 5. "Save as a numbered copy" creates a separate task and does not reuse the existing name.
	copyTask, err := app.ResolveDuplicate(preTask.ID, "copy", saveDir, "flow.bin", 2)
	if err != nil {
		t.Fatalf("ResolveDuplicate copy failed: %v", err)
	}
	if copyTask.ID == preTask.ID || copyTask.Filename == confirmed.Filename {
		t.Fatalf("expected a distinct task with a free name, got %s / %s", copyTask.ID, copyTask.Filename)
	}

	// 6. Cancelling a completed task keeps it and its finished file.
	if err := app.CancelPreDownload(preTask.ID); err != nil {
		t.Fatalf("CancelPreDownload failed: %v", err)
	}
	completed, err := store.Get(ctx, preTask.ID)
	if err != nil || completed.Status != task.StatusCompleted {
		t.Fatalf("expected the completed task to survive cancel, got %+v", completed)
	}
	if _, err := os.Stat(filepath.Join(saveDir, "flow (1).bin")); err != nil {
		t.Fatalf("completed file must survive cancel: %v", err)
	}
}

func TestApp_ExpiredLinkRecovery(t *testing.T) {
	app, store, tmpDir := newTestApp(t)
	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = byte((i * 7) % 251)
	}
	const referer = "https://player.example/watch"
	ts := serveRangedPayload(payload, referer, 0)
	defer ts.Close()

	// A task whose link expired before any byte was written: the probe fails, the task survives.
	if _, err := app.ProbeURL(ts.URL + "/download"); err == nil {
		t.Fatalf("expected probing an expired link to fail")
	}
	expired, err := app.AddTask(ts.URL+"/download", tmpDir, "flow.bin", 2)
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}
	waitAppTask(t, store, expired.ID, task.StatusError)

	// Without the required request information the refreshed link cannot be verified.
	verification, err := app.CheckURLConsistency(expired.ID, ts.URL+"/download", nil)
	if err != nil {
		t.Fatalf("CheckURLConsistency failed: %v", err)
	}
	if verification.Consistent {
		t.Fatalf("expected the link to stay unverified without request info")
	}
	if verification.Reason == "" {
		t.Errorf("expected an explanation for the unverified link")
	}

	headers := map[string]string{"Referer": referer}
	verification, err = app.CheckURLConsistency(expired.ID, ts.URL+"/download", headers)
	if err != nil {
		t.Fatalf("CheckURLConsistency failed: %v", err)
	}
	if !verification.Consistent {
		t.Fatalf("expected the link to be verifiable with request info, got: %s", verification.Reason)
	}

	updated, err := app.UpdateTaskURL(expired.ID, ts.URL+"/download", headers)
	if err != nil {
		t.Fatalf("UpdateTaskURL failed: %v", err)
	}
	if updated.RequestHeaders["Referer"] != referer {
		t.Errorf("expected the request info to be stored on the task, got %v", updated.RequestHeaders)
	}

	// Updating the link continues the transfer rather than leaving the task paused.
	final := waitAppTask(t, store, expired.ID, task.StatusCompleted)
	if final.Downloaded != int64(len(payload)) {
		t.Errorf("expected %d downloaded bytes, got %d", len(payload), final.Downloaded)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "flow.bin"))
	if err != nil {
		t.Fatalf("failed to read recovered download: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("recovered download does not match the served resource")
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
	if app.GetFileInfoQueueLength() != 1 {
		t.Fatalf("expected queue length 1, got %d", app.GetFileInfoQueueLength())
	}

	active, err := app.GetActiveFileInfo()
	if err != nil || active == nil {
		t.Fatalf("expected active file info, got %v, err: %v", active, err)
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
	if app.GetFileInfoQueueLength() != 0 {
		t.Fatalf("queue should be empty after submission, got %d", app.GetFileInfoQueueLength())
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
