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

type pairedProgressObserver struct {
	mu      sync.Mutex
	samples []task.Task
}

func (o *pairedProgressObserver) OnTaskUpdated(t *task.Task) {
	if t == nil {
		return
	}
	o.mu.Lock()
	o.samples = append(o.samples, *t)
	o.mu.Unlock()
}

func TestManager_PairedMediaTotalBytesAndMonotonicProgress(t *testing.T) {
	// 复用合法 fMP4 样本构造双路流：video 含完整媒体轨道，audio 含另一份数据（仅验证流下载+进度单调）。
	initBytes, err := os.ReadFile(filepath.Join("..", "media", "testdata", "fmp4_init.mp4"))
	if err != nil {
		t.Fatalf("read fmp4_init.mp4: %v", err)
	}
	seg0Bytes, err := os.ReadFile(filepath.Join("..", "media", "testdata", "fmp4_seg0.m4s"))
	if err != nil {
		t.Fatalf("read fmp4_seg0.m4s: %v", err)
	}
	videoPayload := append(append([]byte{}, initBytes...), seg0Bytes...)
	videoPayload = append(videoPayload, make([]byte, 512*1024)...)
	// 让两个文件大小明显不同，确保不是把同一条流算两遍。
	audioPayload := append(append([]byte{}, initBytes...), seg0Bytes...)
	audioPayload = append(audioPayload, make([]byte, 256*1024)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload []byte
		switch r.URL.Path {
		case "/video.m4s":
			payload = videoPayload
		case "/audio.m4s":
			payload = audioPayload
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.WriteHeader(http.StatusOK)
			writeSlow(w, payload, 5*time.Millisecond)
			return
		}
		spec := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.Split(spec, "-")
		start, _ := strconv.ParseInt(parts[0], 10, 64)
		end := int64(len(payload)) - 1
		if len(parts) > 1 && parts[1] != "" {
			if p, parseErr := strconv.ParseInt(parts[1], 10, 64); parseErr == nil && p < end {
				end = p
			}
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		writeSlow(w, payload[start:end+1], 5*time.Millisecond)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(srv.Client()), engine.Config{
		MaxActiveTasks:            2,
		TempDirectory:             filepath.Join(tmpDir, "temp"),
		DefaultConnectionsPerTask: 1,
	})
	defer mgr.Close()

	obs := &pairedProgressObserver{}
	mgr.AddListener(obs)

	ctx := context.Background()
	created, err := mgr.CreateTask(ctx, engine.CreateRequest{
		URL:       srv.URL + "/video.m4s",
		AudioURL:  srv.URL + "/audio.m4s",
		Directory: filepath.Join(tmpDir, "out"),
		Filename:  "paired.mp4",
		MaxConn:   4,
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	wantTotal := int64(len(videoPayload) + len(audioPayload))
	if created.TotalBytes != wantTotal {
		t.Fatalf("CreateTask TotalBytes = %d, want %d (video=%d audio=%d)",
			created.TotalBytes, wantTotal, len(videoPayload), len(audioPayload))
	}

	// fMP4 样本两份都含音视频轨，所以 Processing 阶段会按预期拒绝同轨合并或失败，
	// 这里主要检验 Transfer 阶段的进度与 TotalBytes 在整个传输过程从未回退/反弹。
	final := waitForStatus(t, store, created.ID, 15*time.Second, task.StatusCompleted, task.StatusError)
	if final.Downloaded != wantTotal {
		t.Fatalf("final Downloaded = %d, want %d", final.Downloaded, wantTotal)
	}
	if final.TotalBytes != wantTotal {
		t.Fatalf("final TotalBytes = %d, want %d", final.TotalBytes, wantTotal)
	}

	obs.mu.Lock()
	samples := append([]task.Task(nil), obs.samples...)
	obs.mu.Unlock()

	var maxRatio float64
	for i, s := range samples {
		if s.Status != task.StatusDownloading {
			continue
		}
		if s.TotalBytes != wantTotal {
			t.Fatalf("snapshot[%d] TotalBytes = %d, want constant %d (progress bounce!)", i, s.TotalBytes, wantTotal)
		}
		ratio := float64(s.Downloaded) / float64(s.TotalBytes)
		if ratio+1e-9 < maxRatio {
			t.Fatalf("snapshot[%d] progress bounced backward: %.4f < %.4f (downloaded=%d total=%d)",
				i, ratio, maxRatio, s.Downloaded, s.TotalBytes)
		}
		maxRatio = ratio
	}
	var sawMultiChunks bool
	for _, s := range samples {
		if len(s.Chunks) > 1 {
			sawMultiChunks = true
		}
	}
	if !sawMultiChunks {
		t.Fatalf("expected multi-chunk transfer when MaxConn=4 and Resumable=true, got no multi-chunk snapshot")
	}
}
