package engine_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

type mockListener struct {
	mu      sync.Mutex
	updates []*task.Task
}

func (l *mockListener) OnTaskUpdated(t *task.Task) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cloned := *t
	l.updates = append(l.updates, &cloned)
}

func TestManager_QueueAndConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	storeFile := filepath.Join(tmpDir, "tasks.json")
	store, err := task.NewFileTaskStore(storeFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	payload := make([]byte, 512*1024) // 512KB

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
			parts := strings.Split(rangeSpec, "-")
			start, _ := strconv.ParseInt(parts[0], 10, 64)
			end, _ := strconv.ParseInt(parts[1], 10, 64)
			if end >= int64(len(payload)) {
				end = int64(len(payload)) - 1
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write(payload[start : end+1])
		} else {
			w.WriteHeader(http.StatusOK)
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write(payload)
		}
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	// MaxActiveTasks = 2
	mgr := engine.NewManager(store, downloader, engine.Config{MaxActiveTasks: 2})
	defer mgr.Close()

	listener := &mockListener{}
	mgr.AddListener(listener)

	ctx := context.Background()

	// Add 3 tasks: 1 and 2 should run, 3 should wait in queue
	t1, err := mgr.AddTask(ctx, ts.URL+"/file1", tmpDir, "t1.bin", 2)
	if err != nil {
		t.Fatalf("add t1 failed: %v", err)
	}
	t2, err := mgr.AddTask(ctx, ts.URL+"/file2", tmpDir, "t2.bin", 2)
	if err != nil {
		t.Fatalf("add t2 failed: %v", err)
	}
	t3, err := mgr.AddTask(ctx, ts.URL+"/file3", tmpDir, "t3.bin", 2)
	if err != nil {
		t.Fatalf("add t3 failed: %v", err)
	}

	// Wait for all 3 tasks to complete
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task3, err := store.Get(ctx, t3.ID)
		if err == nil && task3.Status == task.StatusCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	for _, tID := range []string{t1.ID, t2.ID, t3.ID} {
		res, err := store.Get(ctx, tID)
		if err != nil {
			t.Fatalf("failed to get task %s: %v", tID, err)
		}
		if res.Status != task.StatusCompleted {
			t.Fatalf("expected task %s to be completed, got %v", tID, res.Status)
		}
		filePath := filepath.Join(tmpDir, res.Filename)
		if fi, err := os.Stat(filePath); err != nil || fi.Size() != int64(len(payload)) {
			t.Fatalf("file %s size mismatch: %v", filePath, err)
		}
	}
}

func TestManager_ImmediateErrorKeepsTask(t *testing.T) {
	// A03: 手动确认过的零进度失败任务仍保留错误和重试入口
	tmpDir := t.TempDir()
	storeFile := filepath.Join(tmpDir, "tasks.json")
	store, _ := task.NewFileTaskStore(storeFile)

	mgr := engine.NewManager(store, nil, engine.Config{MaxActiveTasks: 2})
	defer mgr.Close()

	ctx := context.Background()
	// Non-existent server URL
	t1, err := mgr.AddTask(ctx, "http://127.0.0.1:54321/nonexistent.bin", tmpDir, "error.bin", 2)
	if err != nil {
		t.Fatalf("AddTask should not fail outright, got err: %v", err)
	}
	if t1.Status != task.StatusError {
		t.Fatalf("expected task to be in StatusError, got %v", t1.Status)
	}
	if t1.ErrorMsg == "" {
		t.Fatalf("expected non-empty ErrorMsg")
	}

	// Verify task is persisted in store and can be retrieved
	saved, err := store.Get(ctx, t1.ID)
	if err != nil || saved.Status != task.StatusError {
		t.Fatalf("expected task to be saved in store with StatusError: %v", err)
	}
}

func TestManager_DuplicateURLDetection(t *testing.T) {
	tmpDir := t.TempDir()
	storeFile := filepath.Join(tmpDir, "tasks.json")
	store, _ := task.NewFileTaskStore(storeFile)

	mgr := engine.NewManager(store, nil, engine.Config{MaxActiveTasks: 2})
	defer mgr.Close()

	ctx := context.Background()
	_, err := mgr.AddTask(ctx, "http://127.0.0.1:54321/dup.bin", tmpDir, "dup.bin", 2)
	if err != nil {
		t.Fatalf("first AddTask failed: %v", err)
	}

	// Second add with same URL should be rejected
	_, err = mgr.AddTask(ctx, "http://127.0.0.1:54321/dup.bin", tmpDir, "dup2.bin", 2)
	if err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("expected duplicate URL error, got %v", err)
	}
}
