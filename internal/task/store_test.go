package task_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"sheep-get/internal/task"
)

func TestFileTaskStore(t *testing.T) {
	tmpDir := t.TempDir()
	storeFile := filepath.Join(tmpDir, "tasks.json")

	store, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	task1 := &task.Task{
		ID:         "task-1",
		URL:        "https://example.com/test.bin",
		Filename:   "test.bin",
		Directory:  tmpDir,
		TotalBytes: 1024,
		Downloaded: 512,
		Status:     task.StatusDownloading,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := store.Save(ctx, task1); err != nil {
		t.Fatalf("failed to save task: %v", err)
	}

	got, err := store.Get(ctx, "task-1")
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if got.ID != task1.ID || got.Downloaded != 512 {
		t.Fatalf("unexpected task content: %+v", got)
	}

	// Reload store and verify downloading status is converted to paused on startup
	store2, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		t.Fatalf("failed to reload store: %v", err)
	}
	reloaded, err := store2.Get(ctx, "task-1")
	if err != nil {
		t.Fatalf("failed to get reloaded task: %v", err)
	}
	if reloaded.Status != task.StatusPaused {
		t.Fatalf("expected StatusPaused on reload, got %v", reloaded.Status)
	}

	// Test Delete
	if err := store2.Delete(ctx, "task-1"); err != nil {
		t.Fatalf("failed to delete task: %v", err)
	}
	if _, err := store2.Get(ctx, "task-1"); err != task.ErrTaskNotFound {
		t.Fatalf("expected ErrTaskNotFound after delete, got %v", err)
	}
}
