package hls_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"sheep-get/internal/hls"
)

// BenchmarkDownloadProgressReporting 观察分片数上千时进度汇报的分配量。
//
// 进度回调只带「自上次以来的完成序号」，因此每个分片完成都不产生与分片总数成比例的分配；
// 位图整份复制的写法在这里会表现为每分片一次的 O(N) 分配（1024 分片约 1MB/轮）。
func BenchmarkDownloadProgressReporting(b *testing.B) {
	const segments = 1024
	origin := newFakeOrigin(b)
	names := make([]string, segments)
	for i := range names {
		names[i] = fmt.Sprintf("seg%d.ts", i)
	}
	parsed, err := hls.Parse(mustURL(b, origin.url("/media.m3u8")), []byte(origin.mediaPlaylist(names...)))
	if err != nil {
		b.Fatalf("解析清单失败: %v", err)
	}
	pl := parsed.Media

	bitmap := make([]bool, segments)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		dir, dirErr := os.MkdirTemp("", "hls-progress-bench-")
		if dirErr != nil {
			b.Fatalf("无法创建目录: %v", dirErr)
		}
		b.StartTimer()

		_, err := newTestFetcher().Download(context.Background(), pl, dir, 8,
			func(_, _ int64, _, _ int, completedIndices []int) {
				for _, idx := range completedIndices {
					bitmap[idx] = true
				}
			})
		if err != nil {
			b.Fatalf("下载失败: %v", err)
		}

		b.StopTimer()
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			b.Fatalf("无法清理目录: %v", rmErr)
		}
		b.StartTimer()
	}
}
