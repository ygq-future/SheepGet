package task_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sheep-get/internal/credentials"
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

func TestFileTaskStore_RequestCredentialsPersistenceAndMasking(t *testing.T) {
	tmpDir := t.TempDir()
	storeFile := filepath.Join(tmpDir, "tasks.json")

	store, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	rawHeaders := map[string]string{
		"User-Agent":    "TestBrowser/1.0",
		"Referer":       "https://example.com",
		"Cookie":        "session_id=secret12345; auth=ok",
		"Authorization": "Bearer super-secret-token",
	}
	t1 := &task.Task{
		ID:             "task-cred",
		URL:            "https://example.com/file.zip",
		Filename:       "file.zip",
		Directory:      tmpDir,
		Status:         task.StatusPaused,
		RequestHeaders: credentials.New(rawHeaders),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := store.Save(ctx, t1); err != nil {
		t.Fatalf("failed to save task: %v", err)
	}

	// 1. Verify Wails/Event serialization is masked
	data, err := json.Marshal(t1)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if strings.Contains(jsonStr, "secret12345") || strings.Contains(jsonStr, "super-secret-token") {
		t.Fatalf("sensitive credentials leaked into JSON serialization: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, "Bearer ****") {
		t.Fatalf("expected Bearer mask in JSON: %s", jsonStr)
	}

	// 2. Verify reloaded task from disk retains raw credentials for downloading
	store2, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		t.Fatalf("failed to reload store: %v", err)
	}
	reloaded, err := store2.Get(ctx, "task-cred")
	if err != nil {
		t.Fatalf("failed to get reloaded task: %v", err)
	}
	if reloaded.RequestHeaders == nil {
		t.Fatal("expected reloaded RequestHeaders to not be nil")
	}
	reloadedRaw := reloaded.RequestHeaders.RawHeaders()
	if reloadedRaw["Cookie"] != rawHeaders["Cookie"] {
		t.Errorf("expected raw cookie %q, got %q", rawHeaders["Cookie"], reloadedRaw["Cookie"])
	}
	if reloadedRaw["Authorization"] != rawHeaders["Authorization"] {
		t.Errorf("expected raw authorization %q, got %q", rawHeaders["Authorization"], reloadedRaw["Authorization"])
	}
}
