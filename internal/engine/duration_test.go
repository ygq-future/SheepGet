package engine_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

// --- 测试用媒体样本（与 internal/mediainfo 的用例同构，但这里要发真的 HTTP 请求） ---

func sampleBox(typ string, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], typ)
	copy(out[8:], payload)
	return out
}

// sampleMP4 生成一个最小可解析的 mp4：mvhd 里的时长为 duration/timeScale 秒。
// fastStart 为真时 moov 在 mdat 之前，否则在文件尾（未做 faststart 的真实情况）。
func sampleMP4(timeScale, duration uint32, mdatSize int, fastStart bool) []byte {
	mvhd := make([]byte, 20)
	binary.BigEndian.PutUint32(mvhd[12:16], timeScale)
	binary.BigEndian.PutUint32(mvhd[16:20], duration)
	moov := sampleBox("moov", sampleBox("mvhd", mvhd))
	ftyp := sampleBox("ftyp", []byte("isom\x00\x00\x02\x00isomiso2"))
	mdat := sampleBox("mdat", make([]byte, mdatSize))

	if fastStart {
		return concatBytes(ftyp, moov, mdat)
	}
	return concatBytes(ftyp, mdat, moov)
}

func concatBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// rangeServer 提供支持范围请求的静态内容，并统计请求次数：
// 「时长探测发了几次请求」是这条路径的关键约束，要能直接断言。
func rangeServer(payload []byte, requests *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(requests, 1)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}
		spec := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.SplitN(spec, "-", 2)
		start, _ := strconv.ParseInt(parts[0], 10, 64)
		end := int64(len(payload)) - 1
		if len(parts) == 2 && parts[1] != "" {
			if parsed, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				end = parsed
			}
		}
		if start >= int64(len(payload)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
}

func newDurationProbeManager(t *testing.T, client *http.Client) *engine.Manager {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(client), engine.Config{MaxActiveTasks: 2})
	t.Cleanup(mgr.Close)
	return mgr
}

func TestProbeMediaDurationFastStart(t *testing.T) {
	payload := sampleMP4(1000, 185000, 4096, true)
	var requests int32
	ts := rangeServer(payload, &requests)
	defer ts.Close()

	mgr := newDurationProbeManager(t, ts.Client())
	seconds, ok := mgr.ProbeMediaDuration(context.Background(), ts.URL+"/clip.mp4", "clip.mp4", int64(len(payload)), nil)
	if !ok {
		t.Fatal("expected a duration for a faststart mp4")
	}
	if seconds != 185 {
		t.Errorf("duration = %v, want 185", seconds)
	}
	// moov 在文件头：一段就够，不该为它再读一次尾部。
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Errorf("range requests = %d, want 1", got)
	}
}

// moov 落在文件尾时读第二段；这也是「非 faststart 的常见 mp4」能得到时长的唯一路径。
func TestProbeMediaDurationNonFastStart(t *testing.T) {
	payload := sampleMP4(600, 600*90, 1<<20, false)
	var requests int32
	ts := rangeServer(payload, &requests)
	defer ts.Close()

	mgr := newDurationProbeManager(t, ts.Client())
	seconds, ok := mgr.ProbeMediaDuration(context.Background(), ts.URL+"/big.mp4", "big.mp4", int64(len(payload)), nil)
	if !ok {
		t.Fatal("expected a duration found in the tail of a non-faststart mp4")
	}
	if seconds != 90 {
		t.Errorf("duration = %v, want 90", seconds)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Errorf("range requests = %d, want 2", got)
	}
}

// 不认识的类型一次请求都不发：时长是附加信息，不该让每个下载都多走一趟网络。
func TestProbeMediaDurationSkipsUnsupportedTypes(t *testing.T) {
	var requests int32
	ts := rangeServer(sampleMP4(1000, 1000, 128, true), &requests)
	defer ts.Close()

	mgr := newDurationProbeManager(t, ts.Client())
	if _, ok := mgr.ProbeMediaDuration(context.Background(), ts.URL+"/setup.exe", "setup.exe", 4096, nil); ok {
		t.Error("an unsupported type must not report a duration")
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Errorf("range requests = %d, want 0", got)
	}
}

// 服务器忽略范围请求时，尾部片段只能靠读掉前面整个文件才拿得到；这种代价不能接受，
// 时长直接放弃，并且不能把整个文件拉下来。
func TestProbeMediaDurationGivesUpWhenServerIgnoresRange(t *testing.T) {
	payload := sampleMP4(1000, 60000, 1<<20, false)
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		// 完全无视 Range：每次都从头开始给整份内容。
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	mgr := newDurationProbeManager(t, ts.Client())
	if _, ok := mgr.ProbeMediaDuration(context.Background(), ts.URL+"/big.mp4", "big.mp4", int64(len(payload)), nil); ok {
		t.Error("expected no duration when the server ignores range requests")
	}
}

// 下载完成后时长由本地文件读出，写进任务里：任务列表显示的正是这个值。
func TestCompletedTaskGetsDurationFromLocalFile(t *testing.T) {
	payload := sampleMP4(1000, 125000, 1<<20, true)
	var requests int32
	ts := rangeServer(payload, &requests)
	defer ts.Close()

	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(ts.Client()), engine.Config{MaxActiveTasks: 2})
	defer mgr.Close()

	ctx := context.Background()
	created, err := mgr.AddTask(ctx, ts.URL+"/clip.mp4", tmpDir, "clip.mp4", 2)
	if err != nil {
		t.Fatalf("add task failed: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var finished *task.Task
	for time.Now().Before(deadline) {
		latest, err := store.Get(ctx, created.ID)
		if err == nil && latest.Status == task.StatusCompleted {
			finished = latest
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if finished == nil {
		t.Fatal("task did not complete in time")
	}
	if finished.Duration != 125 {
		t.Errorf("task duration = %v, want 125", finished.Duration)
	}
}

// 非媒体文件完成后不该被写上一个时长。
func TestCompletedTaskKeepsZeroDurationForNonMedia(t *testing.T) {
	payload := make([]byte, 64*1024)
	var requests int32
	ts := rangeServer(payload, &requests)
	defer ts.Close()

	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(ts.Client()), engine.Config{MaxActiveTasks: 2})
	defer mgr.Close()

	ctx := context.Background()
	created, err := mgr.AddTask(ctx, ts.URL+"/setup.exe", tmpDir, "setup.exe", 2)
	if err != nil {
		t.Fatalf("add task failed: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		latest, err := store.Get(ctx, created.ID)
		if err == nil && latest.Status == task.StatusCompleted {
			if latest.Duration != 0 {
				t.Errorf("non-media task duration = %v, want 0", latest.Duration)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task did not complete in time")
}
