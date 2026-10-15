package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/hls"
	"sheep-get/internal/sys"
	"sheep-get/internal/task"
)

func TestHLSVariantFilename(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		variant  hls.Variant
		expected string
	}{
		{
			name:     "standard 420p",
			base:     "video.mp4",
			variant:  hls.Variant{Height: 420},
			expected: "video_420p.mp4",
		},
		{
			name:     "standard 1080p with m3u8 extension",
			base:     "stream.m3u8",
			variant:  hls.Variant{Height: 1080},
			expected: "stream_1080p.mp4",
		},
		{
			name:     "already contains same resolution tag",
			base:     "movie_720p.mp4",
			variant:  hls.Variant{Height: 720},
			expected: "movie_720p.mp4",
		},
		{
			name:     "swapping resolution tag from 720p to 1080p",
			base:     "movie_720p.mp4",
			variant:  hls.Variant{Height: 1080},
			expected: "movie_1080p.mp4",
		},
		{
			name:     "empty base filename defaults to media",
			base:     "",
			variant:  hls.Variant{Height: 420},
			expected: "media_420p.mp4",
		},
		{
			name:     "variant with name 480p instead of height",
			base:     "sample.mp4",
			variant:  hls.Variant{Name: "480p"},
			expected: "sample_480p.mp4",
		},
		{
			name:     "no resolution info preserves base name",
			base:     "sample.mp4",
			variant:  hls.Variant{},
			expected: "sample.mp4",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := engine.HLSVariantFilename(tc.base, tc.variant)
			if got != tc.expected {
				t.Errorf("HLSVariantFilename(%q, %+v) = %q; want %q", tc.base, tc.variant, got, tc.expected)
			}
		})
	}
}

// TestManager_HLSProcessingPreFlightDiskSpaceCheck 验证 P1-3 HLS 合并处理前置空间检查：
// 当分片已就绪进入媒体处理阶段时，若剩余空间不足以容纳合并产物，提前报错并完整保留已有分片供清理空间后重试。
func TestManager_HLSProcessingPreFlightDiskSpaceCheck(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	mgr := engine.NewManager(store, nil, engine.Config{MaxActiveTasks: 1})
	defer mgr.Close()

	dlTask := &task.Task{
		ID:           "hls-disk-check",
		URL:          "https://example.com/video.m3u8",
		Filename:     "video.mp4",
		Directory:    tmpDir,
		Status:       task.StatusError,
		FailurePhase: task.FailurePhaseProcessing,
		TransferDone: true,
		Media: &hls.Source{
			PlaylistURL: "https://example.com/video.m3u8",
			Segments:    1,
		},
		MediaInputs: &hls.Inputs{
			Segments: []string{"seg_00000.ts"},
		},
	}
	segDir := mgr.GetPartPath(dlTask) + ".segments"
	if err := os.MkdirAll(segDir, 0o755); err != nil {
		t.Fatalf("mkdir segDir: %v", err)
	}
	segPath := filepath.Join(segDir, "seg_00000.ts")
	if err := os.WriteFile(segPath, make([]byte, 4096), 0o644); err != nil {
		t.Fatalf("write segment: %v", err)
	}
	if err := store.Save(context.Background(), dlTask); err != nil {
		t.Fatalf("save task: %v", err)
	}

	// 模拟磁盘空间仅剩 512 字节（小于 4096 字节分片体积）
	restore := sys.SetAvailableDiskSpaceForTest(func(dir string) (uint64, error) {
		return 512, nil
	})
	defer restore()

	if err := mgr.RetryProcessing(context.Background(), dlTask.ID); err != nil {
		t.Fatalf("RetryProcessing: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.Get(context.Background(), dlTask.ID)
		if got != nil && got.Status == task.StatusError {
			if got.FailurePhase != task.FailurePhaseProcessing {
				t.Fatalf("expected FailurePhaseProcessing, got %q", got.FailurePhase)
			}
			if !strings.Contains(got.ErrorMsg, sys.ErrInsufficientDiskSpace.Error()) {
				t.Fatalf("expected insufficient disk space error message, got %q", got.ErrorMsg)
			}
			// 确认分片未被误删
			if _, statErr := os.Stat(segPath); statErr != nil {
				t.Fatalf("expected segment file to be preserved on disk space failure, got: %v", statErr)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for HLS processing disk space failure")
}
