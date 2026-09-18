package window

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"sheep-get/internal/config"
	"sheep-get/internal/duplicate"
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
	// 「序号副本」要避让的是目标位置上真实存在的成品文件。
	if err := os.WriteFile(filepath.Join(tmpDir, "doc.pdf"), []byte("original"), 0o644); err != nil {
		t.Fatalf("failed to seed finished file: %v", err)
	}

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
	if active.DuplicateDecision.Default != duplicate.ActionCopy {
		t.Fatalf("default = %q, want %q", active.DuplicateDecision.Default, duplicate.ActionCopy)
	}
	// Under NumberedCopy, suggested filename should have (1)
	if active.Filename != "doc (1).pdf" {
		t.Errorf("expected suggested filename 'doc (1).pdf', got '%s'", active.Filename)
	}

	// Confirm as copy
	newTask, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   4,
		Action:    duplicate.ActionCopy,
	})
	if err != nil {
		t.Fatalf("submit copy failed: %v", err)
	}
	if newTask.Filename != "doc (1).pdf" {
		t.Errorf("expected new task filename 'doc (1).pdf', got %s", newTask.Filename)
	}
}

// 目标位置没有成品文件时，「序号副本」无从避让：既不该预置编号名称，
// 也不该把副本当成这次的动作，否则界面显示的名字与实际落点会不一致。
func TestQueueController_NumberedCopyWithoutFinishedFileIsNotACopy(t *testing.T) {
	ctx := context.Background()
	qc, _, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicyNumberedCopy)

	targetURL := "https://example.com/gone.pdf"
	// 历史任务已完成，但成品文件已被删除或移走。
	_ = store.Save(ctx, &task.Task{
		ID:        "task_gone",
		URL:       targetURL,
		Filename:  "gone.pdf",
		Directory: tmpDir,
		Status:    task.StatusCompleted,
	})

	if _, err := qc.Enqueue(ctx, DownloadRequest{URL: targetURL, Directory: tmpDir}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	active, _ := qc.GetActive()

	if active.Filename != "gone.pdf" {
		t.Errorf("没有成品文件可避让时不应预置编号名称，got %q", active.Filename)
	}
	if active.DuplicateDecision.Default != duplicate.ActionRedownload {
		t.Errorf("default = %q, want %q", active.DuplicateDecision.Default, duplicate.ActionRedownload)
	}
	if len(active.DuplicateDecision.Options) != 0 {
		t.Errorf("options = %v, want 空：没有成品文件时不提供序号副本", active.DuplicateDecision.Options)
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
	if err := os.WriteFile(filepath.Join(tmpDir, "prompt_dup.zip"), []byte("original"), 0o644); err != nil {
		t.Fatalf("failed to seed finished file: %v", err)
	}

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
	// 「询问」的意义就在于必须由用户选：给出三个选项，但不预置任何一项。
	if active.DuplicateDecision.Case != duplicate.CaseDestinationOccupied {
		t.Fatalf("case = %q, want %q", active.DuplicateDecision.Case, duplicate.CaseDestinationOccupied)
	}
	if len(active.DuplicateDecision.Options) != 3 {
		t.Fatalf("options = %v, want 三个动作", active.DuplicateDecision.Options)
	}
	if active.DuplicateDecision.Default != "" {
		t.Fatalf("default = %q, want 空（询问策略必须先由用户选择）", active.DuplicateDecision.Default)
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
	if task.RequestHeaders == nil || task.RequestHeaders.RawHeaders()["Authorization"] != "Bearer token123" {
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
	_ = os.WriteFile(filepath.Join(tmpDir, "manual_dup.bin"), make([]byte, 8192), 0644)

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

func TestQueueController_CategoryDirectoryResolution_AndManualPriority(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	store, _ := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 3})
	t.Cleanup(mgr.Close)

	settings := config.DefaultSettings(tmpDir, filepath.Join(tmpDir, "temp"))
	// Custom category for books (.epub)
	booksDir := filepath.Join(tmpDir, "Books")
	settings.Download.CustomCategories = []config.CategoryConfig{
		{
			ID:         "custom-books",
			Name:       "电子书",
			Directory:  booksDir,
			Extensions: []string{"epub"},
		},
	}

	win := &mockWindowView{}
	qc := NewQueueController(mgr, &testSettingsProvider{settings: settings}, win)

	// 1. Request with empty directory for .mp4 -> should automatically resolve to built-in "视频" directory!
	videoResp, err := qc.Enqueue(ctx, DownloadRequest{
		URL:      "http://example.com/movie.mp4",
		Filename: "movie.mp4",
	})
	if err != nil || !videoResp.Handled {
		t.Fatalf("enqueue video failed: %v", err)
	}
	item1, _ := qc.GetActive()
	expectedVideoDir := filepath.Join(tmpDir, "Videos")
	if item1.Directory != expectedVideoDir {
		t.Errorf("expected category directory %s, got %s", expectedVideoDir, item1.Directory)
	}
	_ = qc.CancelCurrent(ctx)

	// 2. Request with empty directory for .epub -> should resolve to custom "电子书" directory!
	bookResp, err := qc.Enqueue(ctx, DownloadRequest{
		URL:      "http://example.com/novel.epub",
		Filename: "novel.epub",
	})
	if err != nil || !bookResp.Handled {
		t.Fatalf("enqueue book failed: %v", err)
	}
	item2, _ := qc.GetActive()
	if item2.Directory != booksDir {
		t.Errorf("expected custom category directory %s, got %s", booksDir, item2.Directory)
	}
	_ = qc.CancelCurrent(ctx)

	// 3. Request with explicit MANUAL directory -> manual directory MUST take precedence!
	manualDir := filepath.Join(tmpDir, "ManualFolder")
	manualResp, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       "http://example.com/clip.mp4",
		Filename:  "clip.mp4",
		Directory: manualDir, // user manually specified directory
	})
	if err != nil || !manualResp.Handled {
		t.Fatalf("enqueue manual failed: %v", err)
	}
	item3, _ := qc.GetActive()
	if item3.Directory != manualDir {
		t.Errorf("expected manual directory %s to take precedence, got %s", manualDir, item3.Directory)
	}
}

// TestQueueController_ItemCarriesResolvedCategory pins that the file info window receives
// the matched category from the backend instead of re-implementing the category rule.
func TestQueueController_ItemCarriesResolvedCategory(t *testing.T) {
	ctx := context.Background()
	qc, _, _, _, tmpDir := setupTestQueue(t, config.DuplicatePolicyPrompt)

	if _, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       "https://example.com/clip.mp4",
		Directory: tmpDir,
	}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	active, err := qc.GetActive()
	if err != nil || active == nil {
		t.Fatalf("expected active item, got %v, err: %v", active, err)
	}
	if active.CategoryID == "" {
		t.Fatalf("expected the queued item to carry the matched category")
	}
	// 分类只决定目录：手动指定目录时目录保持用户选择，命中分类仍然给出。
	if active.Directory != tmpDir {
		t.Fatalf("expected the manual directory to win, got %q", active.Directory)
	}

	if _, err := qc.Enqueue(ctx, DownloadRequest{URL: "https://example.com/other.zip"}); err != nil {
		t.Fatalf("enqueue without directory failed: %v", err)
	}
	items := qc.GetQueueItems()
	last := items[len(items)-1]
	if last.Directory == "" {
		t.Fatalf("expected the category rule to resolve a directory when none was given")
	}
}

// 目标位置没有成品文件、历史任务尚未完成时，界面不提供「重新下载」与「序号副本」，
// 确认后必须接着已有分片续传：历史记录与已下载进度都得留着。
// 这条回归锁住的是「界面自行兜底成 redownload，把分片删掉从 0 重下」的缺陷。
func TestQueueController_UnfinishedHistoryResumesInsteadOfDiscardingProgress(t *testing.T) {
	ctx := context.Background()

	for _, policy := range []config.DuplicateURLPolicy{
		config.DuplicatePolicyPrompt,
		config.DuplicatePolicySkipShowCompleted,
		config.DuplicatePolicyContinueOverwrite,
		config.DuplicatePolicyNumberedCopy,
	} {
		t.Run(string(policy), func(t *testing.T) {
			qc, _, _, store, tmpDir := setupTestQueue(t, policy)

			targetURL := "https://example.com/half_done.bin"
			pausedTask := &task.Task{
				ID:         "task_half_done",
				URL:        targetURL,
				Filename:   "half_done.bin",
				Directory:  tmpDir,
				Status:     task.StatusPaused,
				TotalBytes: 8192,
				Downloaded: 4096,
			}
			if err := store.Save(ctx, pausedTask); err != nil {
				t.Fatalf("failed to seed paused task: %v", err)
			}

			var showCompletedFor string
			qc.SetOnShowCompleted(func(taskID string) { showCompletedFor = taskID })

			if _, err := qc.Enqueue(ctx, DownloadRequest{URL: targetURL, Directory: tmpDir}); err != nil {
				t.Fatalf("enqueue failed: %v", err)
			}

			active, err := qc.GetActive()
			if err != nil || active == nil {
				t.Fatalf("expected active item, got %v, err: %v", active, err)
			}
			if active.DuplicateDecision.Case != duplicate.CaseHistoryUnfinished {
				t.Fatalf("case = %q, want %q", active.DuplicateDecision.Case, duplicate.CaseHistoryUnfinished)
			}
			if len(active.DuplicateDecision.Options) != 0 {
				t.Errorf("options = %v, want 空：没有成品文件时重下与副本都无意义", active.DuplicateDecision.Options)
			}
			if active.DuplicateDecision.Default != duplicate.ActionContinue {
				t.Errorf("default = %q, want %q", active.DuplicateDecision.Default, duplicate.ActionContinue)
			}
			// 「跳过并显示完成」只在历史任务真正完成时才会唤起完成区域。
			if showCompletedFor != "" {
				t.Errorf("未完成的历史任务不该唤起完成区域，收到 %q", showCompletedFor)
			}

			// 用户不另行选择动作，直接确认。
			resTask, err := qc.Submit(ctx, FileInfoSubmission{
				RequestID: active.ID,
				URL:       active.URL,
				Filename:  active.Filename,
				Directory: active.Directory,
				MaxConn:   4,
			})
			if err != nil {
				t.Fatalf("submit failed: %v", err)
			}
			if resTask.ID != pausedTask.ID {
				t.Errorf("expected to resume %s, got new task %s（历史进度被丢弃重下）", pausedTask.ID, resTask.ID)
			}

			kept, err := store.Get(ctx, pausedTask.ID)
			if err != nil || kept == nil {
				t.Fatalf("history task must survive for resume, got %v (err %v)", kept, err)
			}
			if kept.Downloaded != 4096 {
				t.Errorf("已下载进度被重置：downloaded = %d, want 4096", kept.Downloaded)
			}
		})
	}
}

// 续传同样要收掉同链接的失效残留记录，否则「确认一次就把该链接清干净」的手感会丢。
func TestQueueController_ResumeAlsoCleansStaleDuplicateRecords(t *testing.T) {
	ctx := context.Background()
	qc, _, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicyContinueOverwrite)

	targetURL := "https://example.com/resume_clean.bin"
	pausedTask := &task.Task{
		ID:        "task_resume_clean",
		URL:       targetURL,
		Filename:  "resume_clean.bin",
		Directory: tmpDir,
		Status:    task.StatusPaused,
	}
	ghostCompleted := &task.Task{
		ID:        "task_ghost_completed",
		URL:       targetURL,
		Filename:  "resume_clean.bin",
		Directory: tmpDir,
		Status:    task.StatusCompleted,
	}
	ghostErrored := &task.Task{
		ID:        "task_ghost_errored",
		URL:       targetURL,
		Filename:  "resume_clean.bin",
		Directory: tmpDir,
		Status:    task.StatusError,
	}
	for _, seed := range []*task.Task{pausedTask, ghostCompleted, ghostErrored} {
		if err := store.Save(ctx, seed); err != nil {
			t.Fatalf("failed to seed %s: %v", seed.ID, err)
		}
	}

	if _, err := qc.Enqueue(ctx, DownloadRequest{URL: targetURL, Directory: tmpDir}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	active, _ := qc.GetActive()

	if _, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   4,
	}); err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	if _, err := store.Get(ctx, ghostCompleted.ID); err == nil {
		t.Errorf("同名但文件已不在磁盘上的残留记录应当被清理")
	}
	if _, err := store.Get(ctx, ghostErrored.ID); err == nil {
		t.Errorf("同名但文件已不在磁盘上的残留记录应当被清理")
	}
	if _, err := store.Get(ctx, pausedTask.ID); err != nil {
		t.Errorf("正在续传的历史任务不得被清理：%v", err)
	}
}

// 目标位置已有成品文件时才能谈「覆盖」；此时历史任务即使还没完成，默认动作也是续传——
// 续传完成后会把目标位置那个文件替换掉，所以「继续覆盖」这一项对两种子情况都成立。
func TestQueueController_OverwriteOptionOnlyExistsWithAFinishedFile(t *testing.T) {
	ctx := context.Background()
	qc, _, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicyContinueOverwrite)

	targetURL := "https://example.com/mixed.bin"
	// 历史任务未完成，但目标位置确实躺着同名成品文件（例如由其它工具或副本留下的）。
	_ = store.Save(ctx, &task.Task{
		ID:        "task_mixed",
		URL:       targetURL,
		Filename:  "mixed.bin",
		Directory: tmpDir,
		Status:    task.StatusPaused,
	})
	if err := os.WriteFile(filepath.Join(tmpDir, "mixed.bin"), []byte("present"), 0o644); err != nil {
		t.Fatalf("failed to seed finished file: %v", err)
	}

	if _, err := qc.Enqueue(ctx, DownloadRequest{URL: targetURL, Directory: tmpDir}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	active, _ := qc.GetActive()

	if active.DuplicateDecision.Case != duplicate.CaseDestinationOccupied {
		t.Fatalf("case = %q, want %q", active.DuplicateDecision.Case, duplicate.CaseDestinationOccupied)
	}
	if len(active.DuplicateDecision.Options) != 3 {
		t.Fatalf("options = %v, want 三个动作", active.DuplicateDecision.Options)
	}
	// 历史任务还没完成，目标位置的文件要留着，因此这一项执行续传而不是丢弃重下。
	if active.DuplicateDecision.Default != duplicate.ActionContinue {
		t.Errorf("default = %q, want %q", active.DuplicateDecision.Default, duplicate.ActionContinue)
	}
}

// 目标位置已有成品文件且策略是询问时没有默认动作：界面必须先问清楚，
// 后端拒绝没有动作的提交，而不是静默新建任务覆盖过去。
func TestQueueController_DestinationOccupiedRequiresUserChoice(t *testing.T) {
	ctx := context.Background()
	qc, _, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicyPrompt)

	targetURL := "https://example.com/occupied.bin"
	completedTask := &task.Task{
		ID:         "task_occupied",
		URL:        targetURL,
		Filename:   "occupied.bin",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: 4,
		Downloaded: 4,
	}
	if err := store.Save(ctx, completedTask); err != nil {
		t.Fatalf("failed to seed completed task: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "occupied.bin"), []byte("done"), 0o644); err != nil {
		t.Fatalf("failed to seed file: %v", err)
	}

	if _, err := qc.Enqueue(ctx, DownloadRequest{URL: targetURL, Directory: tmpDir}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	active, _ := qc.GetActive()

	if active.DuplicateDecision.Case != duplicate.CaseDestinationOccupied {
		t.Fatalf("case = %q, want %q", active.DuplicateDecision.Case, duplicate.CaseDestinationOccupied)
	}
	if len(active.DuplicateDecision.Options) != 3 {
		t.Fatalf("options = %v, want 三个动作", active.DuplicateDecision.Options)
	}
	if active.DuplicateDecision.Default != "" {
		t.Errorf("default = %q, want 空：询问策略必须由用户先选", active.DuplicateDecision.Default)
	}

	_, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   4,
	})
	if !errors.Is(err, ErrDuplicateChoiceRequired) {
		t.Fatalf("err = %v, want %v", err, ErrDuplicateChoiceRequired)
	}

	// 用户随后选「继续覆盖」：历史任务已完成且成品就在磁盘上，因此是覆盖重下。
	overwrite := active.DuplicateDecision.Options[1]
	if overwrite != duplicate.ActionRedownload {
		t.Fatalf("overwrite option = %q, want %q", overwrite, duplicate.ActionRedownload)
	}
	resTask, err := qc.Submit(ctx, FileInfoSubmission{
		RequestID: active.ID,
		URL:       active.URL,
		Filename:  active.Filename,
		Directory: active.Directory,
		MaxConn:   4,
		Action:    overwrite,
	})
	if err != nil {
		t.Fatalf("submit with explicit action failed: %v", err)
	}
	if resTask.ID == completedTask.ID {
		t.Errorf("已完成的历史任务应被覆盖重下，而不是沿用原任务")
	}
}

func TestQueueController_SubmitRejectsSubmissionForAnotherItem(t *testing.T) {
	ctx := context.Background()
	qc, _, _, store, tmpDir := setupTestQueue(t, config.DuplicatePolicyPrompt)

	if _, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       "https://example.com/first.zip",
		Directory: tmpDir,
		Filename:  "first.zip",
	}); err != nil {
		t.Fatalf("enqueue first failed: %v", err)
	}
	second, err := qc.Enqueue(ctx, DownloadRequest{
		URL:       "https://example.com/second.zip",
		Directory: tmpDir,
		Filename:  "second.zip",
	})
	if err != nil {
		t.Fatalf("enqueue second failed: %v", err)
	}

	first, err := qc.GetActive()
	if err != nil || first == nil {
		t.Fatalf("expected an active item, got %+v (err=%v)", first, err)
	}
	if first.ID == second.RequestID {
		t.Fatalf("test setup broken: both items share id %q", first.ID)
	}

	// 界面停在第一项，却带着第一项的落点去提交第二项：必须被拒绝，且不能改动队列。
	_, err = qc.Submit(ctx, FileInfoSubmission{
		RequestID: second.RequestID,
		URL:       first.URL,
		Filename:  first.Filename,
		Directory: first.Directory,
		MaxConn:   4,
	})
	if !errors.Is(err, ErrStaleFileInfoSubmission) {
		t.Fatalf("err = %v, want %v", err, ErrStaleFileInfoSubmission)
	}

	if qc.QueueLength() != 2 {
		t.Errorf("queue length = %d, want 2：被拒绝的提交不得推进队列", qc.QueueLength())
	}
	stillActive, err := qc.GetActive()
	if err != nil || stillActive == nil {
		t.Fatalf("expected active item to remain, got %+v (err=%v)", stillActive, err)
	}
	if stillActive.ID != first.ID {
		t.Errorf("active id = %q, want %q", stillActive.ID, first.ID)
	}
	tasks, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("tasks = %d, want 0：被拒绝的提交不得创建任务", len(tasks))
	}
}
