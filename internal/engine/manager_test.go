package engine_test

import (
	"context"
	"errors"
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
	"sheep-get/internal/hls"
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
	if t1.FailurePhase != task.FailurePhaseTransfer {
		t.Fatalf("expected transfer failure phase, got %q", t1.FailurePhase)
	}

	// Verify task is persisted in store and can be retrieved
	saved, err := store.Get(ctx, t1.ID)
	if err != nil || saved.Status != task.StatusError {
		t.Fatalf("expected task to be saved in store with StatusError: %v", err)
	}
	if saved.FailurePhase != task.FailurePhaseTransfer {
		t.Fatalf("expected persisted transfer failure phase, got %q", saved.FailurePhase)
	}
}

// TestManager_RetryContractByFailurePhase pins the contract the UI uses to choose a retry
// action: 传输失败走 Retry，处理失败只能走 RetryProcessing，避免按错误文案猜测失败类型。
func TestManager_RetryContractByFailurePhase(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))

	mgr := engine.NewManager(store, nil, engine.Config{MaxActiveTasks: 2})
	defer mgr.Close()

	ctx := context.Background()

	// 传输阶段失败：不能按处理失败重试。
	transferFailed, err := mgr.AddTask(ctx, "http://127.0.0.1:54321/nonexistent.bin", tmpDir, "t.bin", 2)
	if err != nil {
		t.Fatalf("AddTask should not fail outright, got err: %v", err)
	}
	if err := mgr.RetryProcessing(ctx, transferFailed.ID); !errors.Is(err, engine.ErrNotProcessingFailure) {
		t.Fatalf("expected ErrNotProcessingFailure, got %v", err)
	}
	if err := mgr.Retry(ctx, transferFailed.ID); errors.Is(err, engine.ErrProcessingRetryRequired) {
		t.Fatalf("transfer failure must remain retryable as a transfer, got %v", err)
	}

	// 处理阶段失败：不能重新传输，只能重试处理。分片已经就绪是这条契约的前提——重新传输
	// 既没必要，也会把处理失败留下的分片一起丢掉。
	processingFailed := &task.Task{
		ID:           "processing-failed",
		URL:          "https://example.com/media.m3u8",
		Filename:     "media.mp4",
		Directory:    tmpDir,
		Status:       task.StatusError,
		FailurePhase: task.FailurePhaseProcessing,
		ErrorMsg:     "mux failed",
		Media:        &hls.Source{PlaylistURL: "https://example.com/media.m3u8"},
		MediaInputs:  &hls.Inputs{Segments: []string{"seg_00000.ts"}},
		TransferDone: true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := store.Save(ctx, processingFailed); err != nil {
		t.Fatalf("failed to seed processing failure: %v", err)
	}

	if err := mgr.Retry(ctx, processingFailed.ID); !errors.Is(err, engine.ErrProcessingRetryRequired) {
		t.Fatalf("expected ErrProcessingRetryRequired, got %v", err)
	}
	if err := mgr.RetryProcessing(ctx, processingFailed.ID); err != nil {
		// 分片与清单都在任务里，重试处理不联网、不需要用户再提供任何东西。
		t.Fatalf("expected processing retry to be accepted, got %v", err)
	}

	// 没有处理阶段的任务（普通 HTTP 传输，或分片已不在）不能被当作「仅重试处理」。
	if err := mgr.RetryProcessing(ctx, transferFailed.ID); !errors.Is(err, engine.ErrNotProcessingFailure) {
		t.Fatalf("expected ErrNotProcessingFailure, got %v", err)
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
