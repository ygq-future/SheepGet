package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sheep-get/internal/hls"
	"sheep-get/internal/task"
)

// TestRunHLSTransferMarksEverySegmentDone 固定 HLS 分片位图的来源：进度回调只带增量，
// 任务位图必须靠把每个增量并进去得到完整结果——界面正是按这张位图把进度条画成分段条。
func TestRunHLSTransferMarksEverySegmentDone(t *testing.T) {
	const segments = 5

	var playlist strings.Builder
	playlist.WriteString("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-VERSION:3\n")
	for i := range segments {
		fmt.Fprintf(&playlist, "#EXTINF:6.000,\nseg%d.ts\n", i)
	}
	playlist.WriteString("#EXT-X-ENDLIST\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte(playlist.String()))
			return
		}
		_, _ = w.Write([]byte("segment payload"))
	}))
	defer srv.Close()

	mgr, _ := newFinalizeTestManager(t)
	playlistURL := srv.URL + "/media.m3u8"
	tk := &task.Task{
		ID:             "hls-progress",
		Filename:       "clip.mp4",
		Directory:      t.TempDir(),
		MaxConcurrency: 3,
		Media:          &hls.Source{PlaylistURL: playlistURL, Variant: hls.Variant{URI: playlistURL}},
	}

	inputs, err := mgr.runHLSTransfer(context.Background(), tk, nil)
	if err != nil {
		t.Fatalf("runHLSTransfer: %v", err)
	}
	if len(inputs.Segments) != segments {
		t.Fatalf("expected %d segments, got %d", segments, len(inputs.Segments))
	}
	if tk.SegmentsTotal != segments || tk.SegmentsDone != segments {
		t.Fatalf("segment counts = %d/%d, want %d/%d", tk.SegmentsDone, tk.SegmentsTotal, segments, segments)
	}
	if len(tk.SegmentDone) != segments {
		t.Fatalf("bitmap length = %d, want %d", len(tk.SegmentDone), segments)
	}
	for i, done := range tk.SegmentDone {
		if !done {
			t.Fatalf("segment %d was never marked done: %v", i, tk.SegmentDone)
		}
	}
}
