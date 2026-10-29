package engine

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"sheep-get/internal/task"
)

func newFinalizeTestManager(t *testing.T) (*Manager, *task.FileTaskStore) {
	t.Helper()
	store, err := task.NewFileTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := NewManager(store, NewHTTPDownloader(nil), Config{MaxActiveTasks: 1})
	t.Cleanup(mgr.Close)
	return mgr, store
}

func TestFinalizeRunDoesNotResurrectDeletedTask(t *testing.T) {
	mgr, store := newFinalizeTestManager(t)
	ctx := context.Background()
	if err := store.Save(ctx, &task.Task{ID: "deleted", Status: task.StatusProcessing}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if err := store.Delete(ctx, "deleted"); err != nil {
		t.Fatalf("delete task: %v", err)
	}

	saved, err := mgr.finalizeRun(ctx, &task.Task{ID: "deleted", Status: task.StatusCompleted})
	if err != nil {
		t.Fatalf("finalizeRun: %v", err)
	}
	if saved {
		t.Fatal("a deleted task must not be written back")
	}
	if _, err := store.Get(ctx, "deleted"); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("expected task to stay deleted, got %v", err)
	}
}

func TestDeleteMarksTombstoneForRunningTask(t *testing.T) {
	mgr, store := newFinalizeTestManager(t)
	ctx := context.Background()
	if err := store.Save(ctx, &task.Task{ID: "running", Status: task.StatusProcessing}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	mgr.mu.Lock()
	mgr.executingTasks["running"] = func() {}
	mgr.mu.Unlock()

	if err := mgr.Delete(ctx, "running"); err != nil {
		t.Fatalf("delete running task: %v", err)
	}
	if !mgr.taskDeleted("running") {
		t.Fatal("deleting a running task must leave a tombstone for the in-flight writer")
	}

	saved, err := mgr.finalizeRun(ctx, &task.Task{ID: "running", Status: task.StatusCompleted})
	if err != nil {
		t.Fatalf("finalizeRun: %v", err)
	}
	if saved {
		t.Fatal("the in-flight run must not resurrect a deleted task")
	}
}

func TestPauseCancelsProcessingRun(t *testing.T) {
	mgr, store := newFinalizeTestManager(t)
	ctx := context.Background()
	runCtx, cancelRun := context.WithCancel(context.Background())
	if err := store.Save(ctx, &task.Task{ID: "processing", Status: task.StatusProcessing}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	// 传输结束后 activeTasks 已经释放，只有 executingTasks 还握着取消句柄。
	mgr.mu.Lock()
	mgr.executingTasks["processing"] = cancelRun
	mgr.mu.Unlock()

	if err := mgr.Pause(ctx, "processing"); err != nil {
		t.Fatalf("pause processing task: %v", err)
	}
	select {
	case <-runCtx.Done():
	default:
		t.Fatal("pausing a processing task must cancel its run context")
	}
	got, err := store.Get(ctx, "processing")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != task.StatusPaused {
		t.Fatalf("expected paused, got %q", got.Status)
	}
}

func TestFinalizeRunPreservesUserStatus(t *testing.T) {
	for _, status := range []task.Status{task.StatusPaused, task.StatusQueued} {
		t.Run(string(status), func(t *testing.T) {
			mgr, store := newFinalizeTestManager(t)
			ctx := context.Background()
			if err := store.Save(ctx, &task.Task{ID: "task", Status: status}); err != nil {
				t.Fatalf("seed task: %v", err)
			}

			saved, err := mgr.finalizeRun(ctx, &task.Task{ID: "task", Status: task.StatusCompleted})
			if err != nil {
				t.Fatalf("finalizeRun: %v", err)
			}
			if !saved {
				t.Fatal("the task still exists; the run outcome must be persisted")
			}
			got, err := store.Get(ctx, "task")
			if err != nil {
				t.Fatalf("get task: %v", err)
			}
			if got.Status != status {
				t.Fatalf("user status %q must win over the run outcome, got %q", status, got.Status)
			}
		})
	}
}

type countingListener struct {
	events atomic.Int32
}

func (l *countingListener) OnTaskUpdated(*task.Task) {
	l.events.Add(1)
}

func TestProgressSinkDropsSnapshotsForDeletedTask(t *testing.T) {
	mgr, store := newFinalizeTestManager(t)
	ctx := context.Background()
	listener := &countingListener{}
	mgr.AddListener(listener)

	mgr.mu.Lock()
	mgr.deletedTasks["gone"] = struct{}{}
	mgr.mu.Unlock()

	sink := newProgressSink(ctx, mgr, "gone", 0, time.Now())
	sink.PublishSnapshot(&task.Task{ID: "gone", Status: task.StatusDownloading, Downloaded: 1024})

	if listener.events.Load() != 0 {
		t.Fatal("a deleted task must not be published to listeners")
	}
	if _, err := store.Get(ctx, "gone"); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("a deleted task must not be persisted by a late snapshot, got %v", err)
	}
}
