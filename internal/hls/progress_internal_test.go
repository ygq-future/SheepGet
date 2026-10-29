package hls

import "testing"

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
