package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

func setupTestManager(t *testing.T) (*engine.Manager, task.TaskStore, string) {
	t.Helper()
	tmpDir := t.TempDir()
	storeFile := filepath.Join(tmpDir, "tasks.json")
	store, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, nil, engine.Config{MaxActiveTasks: 3})
	return mgr, store, tmpDir
}

func TestCleanup_OlderTasks(t *testing.T) {
	mgr, store, tmpDir := setupTestManager(t)
	ctx := context.Background()

	now := time.Now()
	oldTime := now.Add(-35 * 24 * time.Hour)
	recentTime := now.Add(-5 * 24 * time.Hour)

	// Task 1: Old completed task with file
	f1 := filepath.Join(tmpDir, "old.zip")
	if err := os.WriteFile(f1, []byte("old-zip-content-12345"), 0644); err != nil {
		t.Fatal(err)
	}
	t1 := &task.Task{
		ID:         "task-old-1",
		Filename:   "old.zip",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len("old-zip-content-12345")),
		CreatedAt:  oldTime,
		UpdatedAt:  oldTime,
	}
	if err := store.Save(ctx, t1); err != nil {
		t.Fatal(err)
	}

	// Task 2: Old but active task (Downloading) - should NEVER be cleaned
	t2 := &task.Task{
		ID:         "task-old-active",
		Filename:   "downloading.zip",
		Directory:  tmpDir,
		Status:     task.StatusDownloading,
		TotalBytes: 100,
		CreatedAt:  oldTime,
		UpdatedAt:  oldTime,
	}
	if err := store.Save(ctx, t2); err != nil {
		t.Fatal(err)
	}

	// Task 3: Recent completed task
	f3 := filepath.Join(tmpDir, "recent.zip")
	if err := os.WriteFile(f3, []byte("recent-zip-content"), 0644); err != nil {
		t.Fatal(err)
	}
	t3 := &task.Task{
		ID:         "task-recent",
		Filename:   "recent.zip",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len("recent-zip-content")),
		CreatedAt:  recentTime,
		UpdatedAt:  recentTime,
	}
	if err := store.Save(ctx, t3); err != nil {
		t.Fatal(err)
	}

	// Scan with 30 days threshold
	scanRes, err := mgr.ScanCleanup(ctx, engine.CleanupScanOptions{
		OlderThanDays: 30,
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if len(scanRes.OlderTasks) != 1 {
		t.Fatalf("expected 1 older task, got %d", len(scanRes.OlderTasks))
	}
	if scanRes.OlderTasks[0].ID != "task-old-1" {
		t.Errorf("expected task-old-1, got %s", scanRes.OlderTasks[0].ID)
	}
	if scanRes.OlderFilesBytes != int64(len("old-zip-content-12345")) {
		t.Errorf("expected %d bytes, got %d", len("old-zip-content-12345"), scanRes.OlderFilesBytes)
	}
	if scanRes.TotalCleanableTasks != 1 {
		t.Errorf("expected 1 cleanable task in totals, got %d", scanRes.TotalCleanableTasks)
	}

	// Execute without deleting disk file
	execRes, err := mgr.ExecuteCleanup(ctx, engine.CleanupExecuteOptions{
		DeleteOlderTasks:     true,
		DeleteOlderDiskFiles: false,
		OlderThanDays:        30,
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if execRes.DeletedTaskCount != 1 {
		t.Errorf("expected 1 task deleted, got %d", execRes.DeletedTaskCount)
	}
	if execRes.DeletedFileCount != 0 {
		t.Errorf("expected 0 files deleted, got %d", execRes.DeletedFileCount)
	}
	// Verify file still exists on disk
	if _, err := os.Stat(f1); err != nil {
		t.Errorf("file f1 should still exist: %v", err)
	}
}

func TestCleanup_Duplicates_NumberedCopies(t *testing.T) {
	mgr, store, tmpDir := setupTestManager(t)
	ctx := context.Background()

	content := []byte("identical-video-stream-data-payload")

	// Base file
	fOrig := filepath.Join(tmpDir, "video.mp4")
	if err := os.WriteFile(fOrig, content, 0644); err != nil {
		t.Fatal(err)
	}
	tOrig := &task.Task{
		ID:         "task-orig",
		Filename:   "video.mp4",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(content)),
		CreatedAt:  time.Now().Add(-10 * time.Hour),
		UpdatedAt:  time.Now().Add(-10 * time.Hour),
	}
	_ = store.Save(ctx, tOrig)

	// Numbered copy 1
	fCopy1 := filepath.Join(tmpDir, "video (1).mp4")
	if err := os.WriteFile(fCopy1, content, 0644); err != nil {
		t.Fatal(err)
	}
	tCopy1 := &task.Task{
		ID:         "task-copy-1",
		Filename:   "video (1).mp4",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(content)),
		CreatedAt:  time.Now().Add(-5 * time.Hour),
		UpdatedAt:  time.Now().Add(-5 * time.Hour),
	}
	_ = store.Save(ctx, tCopy1)

	// Numbered copy 2
	fCopy2 := filepath.Join(tmpDir, "video (2).mp4")
	if err := os.WriteFile(fCopy2, content, 0644); err != nil {
		t.Fatal(err)
	}
	tCopy2 := &task.Task{
		ID:         "task-copy-2",
		Filename:   "video (2).mp4",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(content)),
		CreatedAt:  time.Now().Add(-1 * time.Hour),
		UpdatedAt:  time.Now().Add(-1 * time.Hour),
	}
	_ = store.Save(ctx, tCopy2)

	// Scan duplicates
	scanRes, err := mgr.ScanCleanup(ctx, engine.CleanupScanOptions{
		CheckDuplicates: true,
	})
	if err != nil {
		t.Fatalf("scan duplicates failed: %v", err)
	}

	if len(scanRes.DuplicateGroups) != 1 {
		t.Fatalf("expected 1 duplicate group, got %d", len(scanRes.DuplicateGroups))
	}
	group := scanRes.DuplicateGroups[0]
	if group.OriginalTask == nil || group.OriginalTask.ID != "task-orig" {
		t.Fatalf("expected OriginalTask to be task-orig, got %+v", group.OriginalTask)
	}
	if len(group.DuplicateTasks) != 2 {
		t.Fatalf("expected 2 duplicate tasks, got %d", len(group.DuplicateTasks))
	}

	// Execute duplicate cleanup
	execRes, err := mgr.ExecuteCleanup(ctx, engine.CleanupExecuteOptions{
		DeleteDuplicates: true,
	})
	if err != nil {
		t.Fatalf("execute cleanup failed: %v", err)
	}
	if execRes.DeletedTaskCount != 2 {
		t.Errorf("expected 2 tasks deleted, got %d", execRes.DeletedTaskCount)
	}
	if execRes.DeletedFileCount != 2 {
		t.Errorf("expected 2 files deleted, got %d", execRes.DeletedFileCount)
	}
	if execRes.FreedBytes != int64(len(content)*2) {
		t.Errorf("expected freed bytes %d, got %d", len(content)*2, execRes.FreedBytes)
	}

	// Verify original file exists and copies are gone
	if _, err := os.Stat(fOrig); err != nil {
		t.Errorf("original file should still exist: %v", err)
	}
	if _, err := os.Stat(fCopy1); !os.IsNotExist(err) {
		t.Errorf("copy1 file should be deleted")
	}
	if _, err := os.Stat(fCopy2); !os.IsNotExist(err) {
		t.Errorf("copy2 file should be deleted")
	}
	// Verify original task exists in store and copy tasks are gone
	if _, err := store.Get(ctx, "task-orig"); err != nil {
		t.Errorf("task-orig should exist in store: %v", err)
	}
	if _, err := store.Get(ctx, "task-copy-1"); err == nil {
		t.Errorf("task-copy-1 should be deleted from store")
	}
	if _, err := store.Get(ctx, "task-copy-2"); err == nil {
		t.Errorf("task-copy-2 should be deleted from store")
	}
}

func TestCleanup_Duplicates_DifferentNamesAndSameSizeDifferentHash(t *testing.T) {
	mgr, store, tmpDir := setupTestManager(t)
	ctx := context.Background()

	contentA := []byte("identical-content-1234567890")
	contentB := []byte("different-content-1234567890") // same size, different hash

	// Task A1 (earlier)
	fA1 := filepath.Join(tmpDir, "file_a.iso")
	_ = os.WriteFile(fA1, contentA, 0644)
	tA1 := &task.Task{
		ID:         "task-a1",
		Filename:   "file_a.iso",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(contentA)),
		CreatedAt:  time.Now().Add(-5 * time.Hour),
	}
	_ = store.Save(ctx, tA1)

	// Task A2 (later, completely different filename, but identical content)
	fA2 := filepath.Join(tmpDir, "backup_archive.iso")
	_ = os.WriteFile(fA2, contentA, 0644)
	tA2 := &task.Task{
		ID:         "task-a2",
		Filename:   "backup_archive.iso",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(contentA)),
		CreatedAt:  time.Now().Add(-1 * time.Hour),
	}
	_ = store.Save(ctx, tA2)

	// Task B (same size as contentA, but different content/MD5)
	fB := filepath.Join(tmpDir, "file_b.iso")
	_ = os.WriteFile(fB, contentB, 0644)
	tB := &task.Task{
		ID:         "task-b",
		Filename:   "file_b.iso",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(contentB)),
		CreatedAt:  time.Now().Add(-2 * time.Hour),
	}
	_ = store.Save(ctx, tB)

	// Scan duplicates
	scanRes, err := mgr.ScanCleanup(ctx, engine.CleanupScanOptions{
		CheckDuplicates: true,
	})
	if err != nil {
		t.Fatalf("scan duplicates failed: %v", err)
	}

	if len(scanRes.DuplicateGroups) != 1 {
		t.Fatalf("expected exactly 1 duplicate group, got %d", len(scanRes.DuplicateGroups))
	}
	group := scanRes.DuplicateGroups[0]
	// Since filenames have no numbered copy relation, the earlier created task-a1 is original
	if group.OriginalTask.ID != "task-a1" {
		t.Errorf("expected OriginalTask to be task-a1, got %s", group.OriginalTask.ID)
	}
	if len(group.DuplicateTasks) != 1 || group.DuplicateTasks[0].ID != "task-a2" {
		t.Errorf("expected DuplicateTasks to contain task-a2, got %+v", group.DuplicateTasks)
	}
}

func TestCleanup_MissingFiles(t *testing.T) {
	mgr, store, tmpDir := setupTestManager(t)
	ctx := context.Background()

	// Task with missing file
	tMissing := &task.Task{
		ID:        "task-missing",
		Filename:  "nonexistent.zip",
		Directory: tmpDir,
		Status:    task.StatusCompleted,
		CreatedAt: time.Now(),
	}
	_ = store.Save(ctx, tMissing)

	// Task with existing file
	fExist := filepath.Join(tmpDir, "exist.zip")
	_ = os.WriteFile(fExist, []byte("exist"), 0644)
	tExist := &task.Task{
		ID:        "task-exist",
		Filename:  "exist.zip",
		Directory: tmpDir,
		Status:    task.StatusCompleted,
		CreatedAt: time.Now(),
	}
	_ = store.Save(ctx, tExist)

	// Scan missing files
	scanRes, err := mgr.ScanCleanup(ctx, engine.CleanupScanOptions{
		CheckMissingFiles: true,
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if len(scanRes.MissingTasks) != 1 || scanRes.MissingTasks[0].ID != "task-missing" {
		t.Fatalf("expected task-missing, got %+v", scanRes.MissingTasks)
	}

	// Execute cleanup missing tasks
	execRes, err := mgr.ExecuteCleanup(ctx, engine.CleanupExecuteOptions{
		DeleteMissingTasks: true,
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if execRes.DeletedTaskCount != 1 {
		t.Errorf("expected 1 task deleted, got %d", execRes.DeletedTaskCount)
	}
	if _, err := store.Get(ctx, "task-missing"); err == nil {
		t.Errorf("task-missing should be deleted from store")
	}
	if _, err := store.Get(ctx, "task-exist"); err != nil {
		t.Errorf("task-exist should still exist in store")
	}
}

func TestCleanup_SameFileMultipleTasks(t *testing.T) {
	mgr, store, tmpDir := setupTestManager(t)
	ctx := context.Background()

	content := []byte("shared-disk-file-content")
	fPath := filepath.Join(tmpDir, "shared.bin")
	_ = os.WriteFile(fPath, content, 0644)

	// Task 1: pointing to shared.bin
	t1 := &task.Task{
		ID:         "task-1",
		Filename:   "shared.bin",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(content)),
		CreatedAt:  time.Now().Add(-2 * time.Hour),
	}
	_ = store.Save(ctx, t1)

	// Task 2: also pointing to shared.bin (duplicate task record for same file)
	t2 := &task.Task{
		ID:         "task-2",
		Filename:   "shared.bin",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(content)),
		CreatedAt:  time.Now().Add(-1 * time.Hour),
	}
	_ = store.Save(ctx, t2)

	// Duplicate cleanup should delete task-2, but KEEP the physical file!
	execRes, err := mgr.ExecuteCleanup(ctx, engine.CleanupExecuteOptions{
		DeleteDuplicates: true,
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if execRes.DeletedTaskCount != 1 {
		t.Errorf("expected 1 task deleted, got %d", execRes.DeletedTaskCount)
	}
	if execRes.DeletedFileCount != 0 {
		t.Errorf("expected 0 files deleted (since it is the same file as original), got %d", execRes.DeletedFileCount)
	}
	if _, err := os.Stat(fPath); err != nil {
		t.Errorf("shared file on disk MUST still exist: %v", err)
	}
}

func TestCleanup_CombinedDeletions(t *testing.T) {
	mgr, store, tmpDir := setupTestManager(t)
	ctx := context.Background()

	content := []byte("combined-test-payload")

	// File 1: Old and has a duplicate
	fOrig := filepath.Join(tmpDir, "doc.pdf")
	_ = os.WriteFile(fOrig, content, 0644)
	tOrig := &task.Task{
		ID:         "task-doc-orig",
		Filename:   "doc.pdf",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(content)),
		CreatedAt:  time.Now().Add(-40 * 24 * time.Hour),
	}
	_ = store.Save(ctx, tOrig)

	// File 2: Copy of File 1 (also old)
	fCopy := filepath.Join(tmpDir, "doc (1).pdf")
	_ = os.WriteFile(fCopy, content, 0644)
	tCopy := &task.Task{
		ID:         "task-doc-copy",
		Filename:   "doc (1).pdf",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: int64(len(content)),
		CreatedAt:  time.Now().Add(-40 * 24 * time.Hour),
	}
	_ = store.Save(ctx, tCopy)

	// File 3: Missing task
	tMissing := &task.Task{
		ID:        "task-missing",
		Filename:  "lost.pdf",
		Directory: tmpDir,
		Status:    task.StatusCompleted,
		CreatedAt: time.Now().Add(-40 * 24 * time.Hour),
	}
	_ = store.Save(ctx, tMissing)

	// Execute combined cleanup: older + duplicates + missing
	execRes, err := mgr.ExecuteCleanup(ctx, engine.CleanupExecuteOptions{
		DeleteOlderTasks:     true,
		DeleteOlderDiskFiles: true,
		OlderThanDays:        30,
		DeleteDuplicates:     true,
		DeleteMissingTasks:   true,
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	// All 3 tasks should be deleted without double-counting
	if execRes.DeletedTaskCount != 3 {
		t.Errorf("expected 3 tasks deleted, got %d", execRes.DeletedTaskCount)
	}
	if execRes.DeletedFileCount != 2 {
		t.Errorf("expected 2 physical files deleted, got %d", execRes.DeletedFileCount)
	}
	if execRes.FreedBytes != int64(len(content)*2) {
		t.Errorf("expected %d freed bytes, got %d", len(content)*2, execRes.FreedBytes)
	}
}
