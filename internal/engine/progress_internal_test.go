package engine

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sheep-get/internal/task"
)

// TestProgressSinkReservesOneSnapshotPerInterval 固定发布窗口的预约语义：一帧只发一份快照。
//
// 分片工作协程在 coord.mu 内问「要不要」，拿到的肯定答复就是这一帧的唯一许可。发布本身已经挪到
// 锁外，若这里不预约，同一个窗口里会有多份副本同时落盘并广播。
func TestProgressSinkReservesOneSnapshotPerInterval(t *testing.T) {
	mgr, _ := newFinalizeTestManager(t)
	sink := newProgressSink(context.Background(), mgr, "task", 0, time.Now())

	const workers = 8
	var granted atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if sink.WantsSnapshot() {
				granted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := granted.Load(); got != 1 {
		t.Fatalf("expected exactly one worker to win the publish window, got %d", got)
	}

	// 窗口过去之后，下一帧仍然可以发布。
	time.Sleep(progressPublishInterval)
	if !sink.WantsSnapshot() {
		t.Fatal("expected the next publish window to be granted")
	}
}

// saveCountingStore 记录落盘次数，其余行为原样转发。
type saveCountingStore struct {
	task.TaskStore
	saves atomic.Int32
}

func (s *saveCountingStore) Save(ctx context.Context, t *task.Task) error {
	s.saves.Add(1)
	return s.TaskStore.Save(ctx, t)
}

func newSaveCountingManager(t *testing.T) (*Manager, *saveCountingStore) {
	t.Helper()
	base, err := task.NewFileTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	store := &saveCountingStore{TaskStore: base}
	mgr := NewManager(store, NewHTTPDownloader(nil), Config{MaxActiveTasks: 1})
	t.Cleanup(mgr.Close)
	return mgr, store
}

// TestProgressSinkPersistsAtSaveIntervalDuringFastPublishing 固定高速下载的落盘频率。
//
// 每个发布窗口（60ms）都能带上 MB 级进度时，落盘判据必须是「距上次落盘」而不是「这个窗口」；
// 否则每 60ms 就把全库 JSON 重写一遍。这里让每个窗口都前进 2MB，跨 180ms 只应发生第一次落盘。
func TestProgressSinkPersistsAtSaveIntervalDuringFastPublishing(t *testing.T) {
	mgr, store := newSaveCountingManager(t)
	sink := newProgressSink(context.Background(), mgr, "task", 0, time.Now())

	var downloaded int64
	sink.PublishSnapshot(&task.Task{ID: "task", Downloaded: downloaded})
	for range 3 {
		time.Sleep(progressPublishInterval)
		downloaded += 2 * progressSaveBytes
		sink.PublishSnapshot(&task.Task{ID: "task", Downloaded: downloaded})
	}

	if got := store.saves.Load(); got != 1 {
		t.Fatalf("expected one write inside the save window, got %d for %d bytes", got, downloaded)
	}
}

// TestProgressSinkWaitsForOneMegabyteBetweenSaves 固定 1MB 门槛的计量基准：距上次落盘的字节数。
//
// 时间条件已经满足、但只走了几百 KB 时不落盘；攒够 1MB 才补一次。若判据退回「每个窗口 1MB」，
// 这里的两次发布都会被算作达标，写入次数随之翻倍。
func TestProgressSinkWaitsForOneMegabyteBetweenSaves(t *testing.T) {
	mgr, store := newSaveCountingManager(t)
	sink := newProgressSink(context.Background(), mgr, "task", 0, time.Now())

	sink.PublishSnapshot(&task.Task{ID: "task", Downloaded: 0})
	if got := store.saves.Load(); got != 1 {
		t.Fatalf("expected the first snapshot to be persisted, got %d writes", got)
	}

	// 时间条件已满足，只差字节门槛。
	sink.mu.Lock()
	sink.lastSave = time.Now().Add(-2 * progressSaveInterval)
	sink.mu.Unlock()

	half := int64(progressSaveBytes / 2)
	sink.PublishSnapshot(&task.Task{ID: "task", Downloaded: half})
	if got := store.saves.Load(); got != 1 {
		t.Fatalf("expected no write before 1MB accumulated since the last save, got %d writes", got)
	}

	sink.PublishSnapshot(&task.Task{ID: "task", Downloaded: progressSaveBytes + half})
	if got := store.saves.Load(); got != 2 {
		t.Fatalf("expected a write once 1MB accumulated since the last save, got %d writes", got)
	}
}
