package window

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"sheep-get/internal/config"
	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

type mockWindowView struct {
	mu        sync.Mutex
	shown     bool
	hidden    bool
	focused   bool
	emitted   []emittedEvent
	showCount int
	hideCount int
}

type emittedEvent struct {
	Name string
	Data any
}

func (m *mockWindowView) Show() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shown = true
	m.hidden = false
	m.showCount++
}

func (m *mockWindowView) Hide() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shown = false
	m.hidden = true
	m.hideCount++
}

func (m *mockWindowView) Focus() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.focused = true
}

func (m *mockWindowView) Emit(event string, data any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitted = append(m.emitted, emittedEvent{Name: event, Data: data})
}

type testSettingsProvider struct {
	settings config.Settings
}

func (s *testSettingsProvider) Get() config.Settings {
	return s.settings
}

func setupTestQueue(t *testing.T, policy config.DuplicateURLPolicy) (*QueueController, *mockWindowView, *engine.Manager, task.TaskStore, string) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 3})
	t.Cleanup(mgr.Close)

	settings := config.DefaultSettings(tmpDir, filepath.Join(tmpDir, "temp"))
	settings.Download.DuplicateURLPolicy = policy
	settings.Download.PreDownload = false
	settings.Download.DefaultConnectionsPerTask = 4
	settingsProvider := &testSettingsProvider{settings: settings}

	winView := &mockWindowView{}
	qc := NewQueueController(mgr, settingsProvider, winView)

	return qc, winView, mgr, store, tmpDir
}

func TestQueueController_SingleRequestConfirmAndCancel(t *testing.T) {
	ctx := context.Background()
	qc, winView, _, _, tmpDir := setupTestQueue(t, config.DuplicatePolicyPrompt)

	resp, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       "https://example.com/file1.zip",
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if !resp.Handled || resp.Action != "enqueued" {
		t.Fatalf("expected enqueued response, got %+v", resp)
	}

	if !winView.shown {
		t.Fatalf("expected window to be shown on first enqueue")
	}

	active, err := qc.GetActive()
	if err != nil || active == nil {
		t.Fatalf("expected active item, got %v, err: %v", active, err)
	}
	if active.Filename != "file1.zip" {
		t.Errorf("expected filename file1.zip, got %s", active.Filename)
	}

	// Confirm this request
	confirmedTask, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   4,
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if confirmedTask == nil || confirmedTask.Filename != "file1.zip" {
		t.Fatalf("unexpected confirmed task: %+v", confirmedTask)
	}

	if !winView.hidden {
		t.Fatalf("expected window to be hidden when queue becomes empty")
	}
	if qc.QueueLength() != 0 {
		t.Fatalf("expected queue length 0, got %d", qc.QueueLength())
	}
}

func TestQueueController_FIFO_SmoothTransitions(t *testing.T) {
	ctx := context.Background()
	qc, winView, _, _, tmpDir := setupTestQueue(t, config.DuplicatePolicyPrompt)

	// Enqueue 3 requests concurrently/sequentially
	_, err := qc.Enqueue(ctx, DownloadRequest{URL: "https://example.com/item1.mp4", Directory: tmpDir})
	if err != nil {
		t.Fatalf("enqueue 1 failed: %v", err)
	}
	_, err = qc.Enqueue(ctx, DownloadRequest{URL: "https://example.com/item2.mp4", Directory: tmpDir})
	if err != nil {
		t.Fatalf("enqueue 2 failed: %v", err)
	}
	_, err = qc.Enqueue(ctx, DownloadRequest{URL: "https://example.com/item3.mp4", Directory: tmpDir})
	if err != nil {
		t.Fatalf("enqueue 3 failed: %v", err)
	}

	if qc.QueueLength() != 3 {
		t.Fatalf("expected 3 items in queue, got %d", qc.QueueLength())
	}

	// Active should be item 1
	active, _ := qc.GetActive()
	if active.Filename != "item1.mp4" {
		t.Fatalf("expected active item1.mp4, got %s", active.Filename)
	}
	if active.QueueIndex != 1 || active.QueueTotal != 3 {
		t.Fatalf("expected queue 1/3, got %d/%d", active.QueueIndex, active.QueueTotal)
	}

	// Confirm item 1 -> Should smoothly transition to item 2 without closing window
	_, err = qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   4,
	})
	if err != nil {
		t.Fatalf("submit item 1 failed: %v", err)
	}

	if winView.hidden {
		t.Fatalf("window should NOT be hidden when queue still has items")
	}

	active2, _ := qc.GetActive()
	if active2.Filename != "item2.mp4" {
		t.Fatalf("expected active item2.mp4, got %s", active2.Filename)
	}
	if active2.QueueIndex != 1 || active2.QueueTotal != 2 {
		t.Fatalf("expected queue 1/2, got %d/%d", active2.QueueIndex, active2.QueueTotal)
	}

	// Cancel item 2 -> Should transition to item 3
	err = qc.CancelCurrent(ctx)
	if err != nil {
		t.Fatalf("cancel item 2 failed: %v", err)
	}

	if winView.hidden {
		t.Fatalf("window should NOT be hidden when item 3 remains")
	}

	active3, _ := qc.GetActive()
	if active3.Filename != "item3.mp4" {
		t.Fatalf("expected active item3.mp4, got %s", active3.Filename)
	}

	// Cancel item 3 -> Queue is now empty, window hides!
	err = qc.CancelCurrent(ctx)
	if err != nil {
		t.Fatalf("cancel item 3 failed: %v", err)
	}

	if !winView.hidden {
		t.Fatalf("window should be hidden after last item is cancelled")
	}
	if qc.QueueLength() != 0 {
		t.Fatalf("expected queue length 0, got %d", qc.QueueLength())
	}
}

func TestQueueController_DuplicatePolicy_SkipShowCompleted(t *testing.T) {
	ctx := context.Background()
	qc, winView, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicySkipShowDone)

	// Save a completed task
	targetURL := "https://example.com/already_done.iso"
	completedTask := &task.Task{
		ID:         "task_completed_1",
		URL:        targetURL,
		Filename:   "already_done.iso",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: 1024,
		Downloaded: 1024,
	}
	if err := store.Save(ctx, completedTask); err != nil {
		t.Fatalf("failed to seed completed task: %v", err)
	}

	var showCompletedTarget string
	qc.SetOnShowCompleted(func(taskID string) {
		showCompletedTarget = taskID
	})

	// Enqueue same URL
	_, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       targetURL,
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Opens completed dialog and enqueues FileInfo without suppressing window
	if showCompletedTarget != completedTask.ID {
		t.Fatalf("expected onShowCompleted callback with %s, got %s", completedTask.ID, showCompletedTarget)
	}
	if !winView.shown {
		t.Fatalf("fileinfo window should be shown so user can re-download if desired")
	}
	if qc.QueueLength() != 1 {
		t.Fatalf("queue should contain 1 item, got %d", qc.QueueLength())
	}
}

func TestQueueController_DuplicatePolicy_NumberedCopy(t *testing.T) {
	ctx := context.Background()
	qc, _, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicyNumberedCopy)

	targetURL := "https://example.com/doc.pdf"
	existingTask := &task.Task{
		ID:         "task_existing_1",
		URL:        targetURL,
		Filename:   "doc.pdf",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: 2048,
	}
	_ = store.Save(ctx, existingTask)

	resp, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       targetURL,
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if resp.Action != "enqueued" {
		t.Fatalf("expected enqueued, got %s", resp.Action)
	}

	active, _ := qc.GetActive()
	if active.DuplicateTask == nil {
		t.Fatalf("expected DuplicateTask to be populated")
	}
	// Under NumberedCopy, suggested filename should have (1)
	if active.Filename != "doc (1).pdf" {
		t.Errorf("expected suggested filename 'doc (1).pdf', got '%s'", active.Filename)
	}

	// Confirm as copy
	newTask, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID:         active.ID,
		URL:               active.URL,
		Filename:          active.Filename,
		Directory:         active.Directory,
		MaxConn:           4,
		DuplicateStrategy: "copy",
	})
	if err != nil {
		t.Fatalf("submit copy failed: %v", err)
	}
	if newTask.Filename != "doc (1).pdf" {
		t.Errorf("expected new task filename 'doc (1).pdf', got %s", newTask.Filename)
	}
}

func TestQueueController_FileConflictDetection(t *testing.T) {
	ctx := context.Background()
	qc, _, _, _, tmpDir := setupTestQueue(t, config.DuplicatePolicyPrompt)

	// Create an existing file on disk
	conflictFile := filepath.Join(tmpDir, "report.pdf")
	if err := os.WriteFile(conflictFile, []byte("existing"), 0o644); err != nil {
		t.Fatalf("failed to create dummy file: %v", err)
	}

	_, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       "https://example.com/report.pdf",
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	active, _ := qc.GetActive()
	if !active.FileConflict {
		t.Errorf("expected FileConflict true for existing report.pdf on disk")
	}
	if active.SuggestedFilename != "report (1).pdf" {
		t.Errorf("expected SuggestedFilename 'report (1).pdf', got '%s'", active.SuggestedFilename)
	}
}

func TestQueueController_DuplicatePolicy_Prompt(t *testing.T) {
	ctx := context.Background()
	qc, winView, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicyPrompt)

	targetURL := "https://example.com/prompt_dup.zip"
	existingTask := &task.Task{
		ID:         "task_prompt_dup",
		URL:        targetURL,
		Filename:   "prompt_dup.zip",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: 5000,
	}
	_ = store.Save(ctx, existingTask)

	resp, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       targetURL,
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if resp.Action != "enqueued" {
		t.Fatalf("expected action enqueued, got %s", resp.Action)
	}
	if !winView.shown {
		t.Fatalf("window should be shown under prompt policy")
	}

	active, _ := qc.GetActive()
	if active.DuplicateTask == nil || active.DuplicateTask.ID != existingTask.ID {
		t.Fatalf("expected duplicate task %s, got %v", existingTask.ID, active.DuplicateTask)
	}
	if active.DuplicatePolicy != config.DuplicatePolicyPrompt {
		t.Fatalf("expected duplicate policy prompt, got %s", active.DuplicatePolicy)
	}
}

func TestQueueController_UnreachableURL_Allows_Manual_Confirmation(t *testing.T) {
	ctx := context.Background()
	qc, winView, _, _, tmpDir := setupTestQueue(t, config.DuplicatePolicyPrompt)

	// Non-routable port to guarantee probe fails
	unreachableURL := "http://127.0.0.1:54321/unreachable.bin"
	resp, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       unreachableURL,
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("enqueue should succeed even if probe fails: %v", err)
	}
	if !resp.Handled || resp.Action != "enqueued" {
		t.Fatalf("expected enqueued action, got %+v", resp)
	}
	if !winView.shown {
		t.Fatalf("window should be shown")
	}

	active, _ := qc.GetActive()
	if active.TotalBytes != -1 {
		t.Errorf("expected totalBytes -1 for unreachable url, got %d", active.TotalBytes)
	}
	if active.Filename != "unreachable.bin" {
		t.Errorf("expected filename unreachable.bin, got %s", active.Filename)
	}

	// User can still submit manually!
	confirmed, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   2,
	})
	if err != nil {
		t.Fatalf("submission should succeed for unreachable URL: %v", err)
	}
	if confirmed == nil || confirmed.Filename != "unreachable.bin" {
		t.Fatalf("expected confirmed task, got %+v", confirmed)
	}
}

func TestQueueController_RequestHeaders_Preserved(t *testing.T) {
	ctx := context.Background()
	qc, _, _, _, tmpDir := setupTestQueue(t, config.DuplicatePolicyPrompt)

	headers := map[string]string{
		"Authorization": "Bearer token123",
		"User-Agent":    "CustomAgent/1.0",
	}

	resp, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       "https://example.com/protected.zip",
		Directory: tmpDir,
		Headers:   headers,
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if !resp.Handled {
		t.Fatalf("expected handled enqueue")
	}

	active, err := qc.GetActive()
	if err != nil || active == nil {
		t.Fatalf("expected active item, got %v", active)
	}
	if active.Headers["Authorization"] != "Bearer token123" {
		t.Errorf("expected Authorization header preserved in active item, got %v", active.Headers)
	}

	task, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   2,
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if task.RequestHeaders["Authorization"] != "Bearer token123" {
		t.Errorf("expected task.RequestHeaders to carry Authorization header, got %v", task.RequestHeaders)
	}
}

func TestQueueController_DuplicatePolicy_FallbackOnOmittedStrategy(t *testing.T) {
	ctx := context.Background()
	qc, _, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicyContinueOverwrite)

	targetURL := "https://example.com/fallback_dup.bin"
	existingTask := &task.Task{
		ID:         "task_fallback_dup",
		URL:        targetURL,
		Filename:   "fallback_dup.bin",
		Directory:  tmpDir,
		Status:     task.StatusPaused,
		TotalBytes: 4096,
	}
	_ = store.Save(ctx, existingTask)

	_, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       targetURL,
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	active, _ := qc.GetActive()
	// Submit WITHOUT explicit strategy -> should fallback to ContinueOverwrite
	resTask, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   4,
	})
	if err != nil {
		t.Fatalf("submit with policy fallback failed: %v", err)
	}
	if resTask.ID != existingTask.ID {
		t.Errorf("expected resumed existing task %s, got %s", existingTask.ID, resTask.ID)
	}
}

func TestQueueController_ManualEmptyURL_DuplicateCompletedTask_Recognized(t *testing.T) {
	ctx := context.Background()
	qc, _, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicyContinueOverwrite)

	targetURL := "https://example.com/manual_dup.bin"
	existingTask := &task.Task{
		ID:         "task_manual_dup_completed",
		URL:        targetURL,
		Filename:   "manual_dup.bin",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: 8192,
	}
	_ = store.Save(ctx, existingTask)

	// 1. User opens new download with empty URL
	resp, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       "",
		Directory: tmpDir,
	})
	if err != nil {
		t.Fatalf("enqueue empty URL failed: %v", err)
	}
	if !resp.Handled {
		t.Fatalf("expected handled empty enqueue")
	}

	active, err := qc.GetActive()
	if err != nil || active == nil {
		t.Fatalf("expected active item for empty request")
	}
	if active.DuplicateTask != nil {
		t.Fatalf("initially duplicateTask should be nil before user enters URL")
	}

	// 2. User types in targetURL and submits with a new directory
	newDir := filepath.Join(tmpDir, "new_path")
	resTask, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       targetURL,
		Filename:  "manual_dup.bin",
		Directory: newDir,
		MaxConn:   4,
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	// Must recognize duplicate and create a new task for the new download session
	if resTask.ID == existingTask.ID {
		t.Errorf("expected duplicate overwrite to create a new task, but got same task %s", existingTask.ID)
	}
	if resTask.Directory != newDir {
		t.Errorf("expected updated directory %s, got %s", newDir, resTask.Directory)
	}

	// Total task count in store should be 2 (existing completed record + new task)
	allTasks, _ := store.List(ctx)
	if len(allTasks) != 2 {
		t.Fatalf("expected 2 tasks in store (original + new), got %d", len(allTasks))
	}
}
