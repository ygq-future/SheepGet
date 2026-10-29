package hls

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestSegmentProgressPerSegmentWorkDoesNotScaleWithSegmentCount 固定进度记账的分配特征：
// 记一个分片完成是常数开销，与清单分片数无关。
//
// 旧实现每次回调都复制整张位图，4096 分片的清单因此每完成一个分片就分配 4KB；这条用例在那种
// 写法下会直接失败（AllocsPerRun 不再是 0）。
func TestSegmentProgressPerSegmentWorkDoesNotScaleWithSegmentCount(t *testing.T) {
	for _, segments := range []int{8, 4096} {
		progress := newSegmentProgress(segments)
		allocs := testing.AllocsPerRun(100, func() {
			progress.markComplete(0, 512)
			progress.resetDelta()
		})
		if allocs != 0 {
			t.Fatalf("%d 分片的清单每完成一个分片分配了 %.1f 次，期望 0", segments, allocs)
		}
	}

	progress := newSegmentProgress(8)
	progress.markComplete(3, 100)
	progress.markComplete(5, 250)
	if got := progress.delta(); len(got) != 2 || got[0] != 3 || got[1] != 5 {
		t.Fatalf("delta = %v, want [3 5]", got)
	}
	if progress.downloaded != 350 || progress.completed != 2 {
		t.Fatalf("downloaded/completed = %d/%d, want 350/2", progress.downloaded, progress.completed)
	}
	if !progress.done[3] || !progress.done[5] || progress.done[0] {
		t.Fatalf("done flags = %v, want only 3 and 5", progress.done)
	}

	progress.resetDelta()
	if got := progress.delta(); len(got) != 0 {
		t.Fatalf("delta after reset = %v, want empty", got)
	}
}

// TestSegmentFlowGateYieldsSlotDuringRetryDelay 固定限流重试的关键语义：退避期间必须让出
// 并发名额。否则收缩后的上限对已经在飞的分片毫无作用，其他分片会继续撞 429，限流分片
// 也会把自己的重试次数耗光——Windows CI 上的 429 分片失败就是这么来的。
func TestSegmentFlowGateYieldsSlotDuringRetryDelay(t *testing.T) {
	ctx := context.Background()
	gate := newSegmentFlowGate(1)
	if !gate.acquire(ctx) {
		t.Fatal("first acquire failed")
	}

	yieldErr := make(chan error, 1)
	go func() { yieldErr <- gate.yield(ctx, 150*time.Millisecond) }()

	acquired := make(chan bool, 1)
	go func() { acquired <- gate.acquire(ctx) }()
	select {
	case ok := <-acquired:
		if !ok {
			t.Fatal("acquire reported a closed gate")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("yield kept the slot during the retry delay; the shrunk limit never takes effect")
	}
	gate.release()

	select {
	case err := <-yieldErr:
		if err != nil {
			t.Fatalf("yield did not restore the slot: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("yield did not re-acquire the slot after the delay")
	}
	gate.release()
	if gate.active != 0 {
		t.Fatalf("expected every slot to be returned, got %d active", gate.active)
	}
	gate.close()
}

// TestSegmentFlowGateYieldReportsClosedGate 确认 gate 关闭后 yield 返回错误，调用方据此
// 结束本次重试，而不是把 0 字节当成成功。
func TestSegmentFlowGateYieldReportsClosedGate(t *testing.T) {
	gate := newSegmentFlowGate(2)
	if !gate.acquire(context.Background()) {
		t.Fatal("acquire failed")
	}
	gate.close()
	if err := gate.yield(context.Background(), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from a closed gate, got %v", err)
	}
}
