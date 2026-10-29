package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
