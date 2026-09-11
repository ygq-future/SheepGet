package main

import (
	"context"
	"path/filepath"
	"testing"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

func TestApp_TaskLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	storeFile := filepath.Join(tmpDir, "tasks.json")
	store, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	downloader := engine.NewHTTPDownloader(nil)
	mgr := engine.NewManager(store, downloader, engine.Config{MaxActiveTasks: 2})
	defer mgr.Close()

	app := &App{
		manager: mgr,
		store:   store,
	}
	app.startup(context.Background())

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
