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
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/hls"
	"sheep-get/internal/task"
)

// serveRanged serves payload with Resume/Range support. delay slows every 4KiB write so tests can
// observe mid-transfer state; referer, when non-empty, makes the server answer 403 unless the
// request carries that Referer — standing in for a link whose authorization expired.
func serveRanged(payload []byte, delay time.Duration, referer string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if referer != "" && r.Header.Get("Referer") != referer {
			w.WriteHeader(http.StatusForbidden)
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
			writeSlow(w, payload, delay)
			return
		}

		rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.Split(rangeSpec, "-")
		start, _ := strconv.ParseInt(parts[0], 10, 64)
		end := int64(len(payload)) - 1
		if len(parts) > 1 && parts[1] != "" {
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
		writeSlow(w, payload[start:end+1], delay)
	}))
}

func writeSlow(w http.ResponseWriter, data []byte, delay time.Duration) {
	offset := 0
	for offset < len(data) {
		chunkEnd := min(offset+4096, len(data))
		if _, err := w.Write(data[offset:chunkEnd]); err != nil {
			return
		}
		offset = chunkEnd
		if delay > 0 {
			time.Sleep(delay)
		}
	}
}

func newTestManager(t *testing.T, payload []byte, delay time.Duration, referer string) (*task.FileTaskStore, *engine.Manager, *httptest.Server) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	ts := serveRanged(payload, delay, referer)
	downloader := engine.NewHTTPDownloader(ts.Client())
	mgr := engine.NewManager(store, downloader, engine.Config{MaxActiveTasks: 2})
	t.Cleanup(func() {
		mgr.Close()
		ts.Close()
	})
	return store, mgr, ts
}

// waitForStatus polls the persisted task until it reaches one of the wanted statuses.
func waitForStatus(t *testing.T, store task.TaskStore, id string, timeout time.Duration, want ...task.Status) *task.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		saved, err := store.Get(context.Background(), id)
		if err == nil && saved != nil {
			for _, candidate := range want {
				if saved.Status == candidate {
					return saved
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	saved, _ := store.Get(context.Background(), id)
	t.Fatalf("task %s did not reach %v in %s (last: %+v)", id, want, timeout, saved)
	return nil
}

// waitForDownloaded polls the persisted task until it reports at least one downloaded byte.
// 进度写入磁盘按 500ms 或 1MB 节流，因此「状态变为下载中」与「已下载字节可见」之间有一段窗口，
// 测试要等的是后者，不能假定状态一到就有字节。
func waitForDownloaded(t *testing.T, store task.TaskStore, id string, timeout time.Duration) *task.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		saved, err := store.Get(context.Background(), id)
		if err == nil && saved != nil && saved.Downloaded > 0 {
			return saved
		}
		time.Sleep(20 * time.Millisecond)
	}
	saved, _ := store.Get(context.Background(), id)
	t.Fatalf("task %s did not report any downloaded byte in %s (last: %+v)", id, timeout, saved)
	return nil
}

func TestProbe_MetadataAndContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", "1024")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"v1.0"`)
		w.Header().Set("Content-Disposition", `attachment; filename="testfile.zip"`)
		if r.Method == http.MethodHead || r.Header.Get("Range") != "" {
			if r.Header.Get("Range") != "" {
				w.Header().Set("Content-Range", "bytes 0-0/1024")
				w.WriteHeader(http.StatusPartialContent)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(make([]byte, 1024))
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	info, err := downloader.Probe(context.Background(), ts.URL+"/download", nil)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	if info.Filename != "testfile.zip" {
		t.Errorf("expected filename testfile.zip, got %s", info.Filename)
	}
	if info.TotalBytes != 1024 {
		t.Errorf("expected total bytes 1024, got %d", info.TotalBytes)
	}
	if info.ContentType != "application/zip" {
		t.Errorf("expected contentType application/zip, got %s", info.ContentType)
	}
	if !info.Resumable {
		t.Errorf("expected resumable true")
	}
	if info.ETag != `"v1.0"` {
		t.Errorf("expected ETag \"v1.0\", got %s", info.ETag)
	}
}

func TestProbe_UnknownSize(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write([]byte("streaming content"))
	}))
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	info, err := downloader.Probe(context.Background(), ts.URL+"/stream.txt", nil)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	if info.TotalBytes != -1 {
		t.Errorf("expected total bytes -1, got %d", info.TotalBytes)
	}
	if info.Filename != "stream.txt" {
		t.Errorf("expected filename stream.txt, got %s", info.Filename)
	}
}

func TestOccupancy_DiskNames(t *testing.T) {
	tmpDir := t.TempDir()
	occupancy := engine.Occupancy{}

	if occupancy.Taken(tmpDir, "sample.txt") || occupancy.Suggest(tmpDir, "sample.txt") != "sample.txt" {
		t.Errorf("expected a free name, got taken=%v suggest=%s", occupancy.Taken(tmpDir, "sample.txt"), occupancy.Suggest(tmpDir, "sample.txt"))
	}

	writeFile(t, filepath.Join(tmpDir, "sample.txt"))

	if !occupancy.Taken(tmpDir, "sample.txt") || occupancy.Suggest(tmpDir, "sample.txt") != "sample (1).txt" {
		t.Errorf("expected sample (1).txt, got taken=%v suggest=%s", occupancy.Taken(tmpDir, "sample.txt"), occupancy.Suggest(tmpDir, "sample.txt"))
	}

	writeFile(t, filepath.Join(tmpDir, "sample (1).txt"))

	if got := occupancy.Suggest(tmpDir, "sample.txt"); got != "sample (2).txt" {
		t.Errorf("expected sample (2).txt, got %s", got)
	}

	writeFile(t, filepath.Join(tmpDir, "README"))

	if !occupancy.Taken(tmpDir, "README") || occupancy.Suggest(tmpDir, "README") != "README (1)" {
		t.Errorf("expected README (1), got taken=%v suggest=%s", occupancy.Taken(tmpDir, "README"), occupancy.Suggest(tmpDir, "README"))
	}
}

func TestOccupancy_SheepgetTemporaryFile(t *testing.T) {
	tmpDir := t.TempDir()
	// If a downloading task has created sample.txt.sheepget, it should be treated as taken.
	writeFile(t, filepath.Join(tmpDir, "sample.txt.sheepget"))

	occupancy := engine.Occupancy{}
	if !occupancy.Taken(tmpDir, "sample.txt") || occupancy.Suggest(tmpDir, "sample.txt") != "sample (1).txt" {
		t.Errorf("expected sample.txt.sheepget to cause conflict, got taken=%v suggest=%s", occupancy.Taken(tmpDir, "sample.txt"), occupancy.Suggest(tmpDir, "sample.txt"))
	}
}

// 「这个名字被占了」只有一条规则，三类来源都在里面：磁盘、正在进行中的任务、以及已经发给
// 排队项的名字。已完成且成品文件已不在磁盘上的记录不算占用。
func TestOccupancy_Sources(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 1})
	defer mgr.Close()
	ctx := context.Background()

	writeFile(t, filepath.Join(tmpDir, "base.zip"))
	_ = store.Save(ctx, &task.Task{ID: "active", Filename: "base (2).zip", Directory: tmpDir, Status: task.StatusDownloading})
	_ = store.Save(ctx, &task.Task{ID: "stale", Filename: "base (3).zip", Directory: tmpDir, Status: task.StatusCompleted})
	reserved := func(dir, name string) bool {
		return engine.SamePath(dir, tmpDir) && name == "base (1).zip"
	}

	occupancy := mgr.Occupancy(ctx, reserved)
	for name, reason := range map[string]string{
		"base.zip":     "磁盘上的成品文件",
		"base (2).zip": "正在进行中的任务",
		"base (1).zip": "排队项已经发出的名字",
	} {
		if !occupancy.Taken(tmpDir, name) {
			t.Errorf("%s 应算占用：%s", name, reason)
		}
	}
	if occupancy.Taken(tmpDir, "base (3).zip") {
		t.Error("已完成且成品文件已不在磁盘上的记录不该算占用")
	}
	if occupancy.Taken(filepath.Join(tmpDir, "other"), "base.zip") {
		t.Error("另一个目录下的同名文件不该算占用")
	}

	suggested := occupancy.Suggest(tmpDir, "base.zip")
	if suggested != "base (3).zip" {
		t.Errorf("expected base (3).zip (跳过占用中的 1、2), got %s", suggested)
	}
	if occupancy.Taken(tmpDir, suggested) {
		t.Errorf("建议名 %s 自己不能是被占用的名字", suggested)
	}
}

// 「建议名」与「序号副本名」是同一个占用判定给出的两个答案，语义不同：建议名在原名可用时
// 就是原名（没有冲突就没什么要避让的），序号副本名则永远是编号名（这个动作要的就是另一份成品）。
func TestOccupancy_NumberedCopyAlwaysNumbers(t *testing.T) {
	tmpDir := t.TempDir()
	occupancy := engine.Occupancy{}

	if got := occupancy.Suggest(tmpDir, "free.zip"); got != "free.zip" {
		t.Errorf("expected the free name itself, got %s", got)
	}
	if got := occupancy.NumberedCopy(tmpDir, "free.zip"); got != "free (1).zip" {
		t.Errorf("expected free (1).zip, got %s", got)
	}

	writeFile(t, filepath.Join(tmpDir, "free (1).zip"))
	if got := occupancy.NumberedCopy(tmpDir, "free.zip"); got != "free (2).zip" {
		t.Errorf("expected free (2).zip, got %s", got)
	}
}

func TestManager_Occupancy_MultiCopies(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 1})
	defer mgr.Close()
	ctx := context.Background()
	// 1. Add base task and two copy tasks into store, with disk files present
	writeFile(t, filepath.Join(tmpDir, "test.zip"))
	writeFile(t, filepath.Join(tmpDir, "test (1).zip"))
	t0 := &task.Task{ID: "t0", URL: "http://example.com/test.zip", Filename: "test.zip", Directory: tmpDir, Status: task.StatusCompleted}
	t1 := &task.Task{ID: "t1", URL: "http://example.com/test.zip", Filename: "test (1).zip", Directory: tmpDir, Status: task.StatusCompleted}
	t2 := &task.Task{ID: "t2", URL: "http://example.com/test.zip", Filename: "test (2).zip", Directory: tmpDir, Status: task.StatusDownloading}
	_ = store.Save(ctx, t0)
	_ = store.Save(ctx, t1)
	_ = store.Save(ctx, t2)

	// 占用判定要认出磁盘文件与正在进行中的任务，给出 test (3).zip
	copyName := mgr.Occupancy(ctx, nil).Suggest(tmpDir, "test.zip")
	if copyName != "test (3).zip" {
		t.Errorf("expected test (3).zip, got %s", copyName)
	}

	// Even if passed "test (1).zip", it should still recognize existing (1) and (2) and return (3)
	copyName2 := mgr.Occupancy(ctx, nil).Suggest(tmpDir, "test (1).zip")
	if copyName2 != "test (3).zip" {
		t.Errorf("expected test (3).zip, got %s", copyName2)
	}
}

func TestManager_NumberedCopy_CleanMissingCopies_ScenarioAllMissing(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 1})
	defer mgr.Close()
	ctx := context.Background()

	// Base file exists on disk
	targetURL := "http://example.com/item.zip"
	writeFile(t, filepath.Join(tmpDir, "item.zip"))
	baseTask := &task.Task{ID: "base", URL: targetURL, Filename: "item.zip", Directory: tmpDir, Status: task.StatusCompleted}
	_ = store.Save(ctx, baseTask)

	// Copies 1 to 5 exist in store, but NONE exist on disk
	for i := 1; i <= 5; i++ {
		tName := fmt.Sprintf("item (%d).zip", i)
		tID := fmt.Sprintf("copy_%d", i)
		_ = store.Save(ctx, &task.Task{
			ID:        tID,
			URL:       targetURL,
			Filename:  tName,
			Directory: tmpDir,
			Status:    task.StatusCompleted,
		})
	}

	// 1. 只读预览（占用判定）认出副本 (1)，且不删除任何任务
	suggested := mgr.Occupancy(ctx, nil).Suggest(tmpDir, "item.zip")
	if suggested != "item (1).zip" {
		t.Errorf("expected suggested copy item (1).zip, got %s", suggested)
	}
	beforeConfirm, _ := store.List(ctx)
	if len(beforeConfirm) != 6 {
		t.Fatalf("preview should not delete tasks, expected 6 tasks, got %d", len(beforeConfirm))
	}

	// 2. When user confirms download with "copy" strategy (ResolveDuplicate), it cleans stale copies and creates copy (1)
	newTask, err := mgr.ResolveDuplicate(ctx, baseTask.ID, "copy", tmpDir, "item.zip", 2)
	if err != nil {
		t.Fatalf("ResolveDuplicate copy failed: %v", err)
	}
	if newTask.Filename != "item (1).zip" {
		t.Errorf("expected new task filename item (1).zip, got %s", newTask.Filename)
	}

	// Verify copies 1-5 tasks were removed, only base and newTask remain
	remaining, err := store.List(ctx)
	if err != nil {
		t.Fatalf("store.List failed: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 tasks remaining (base + newTask), got %d: %+v", len(remaining), remaining)
	}
	baseFound, newFound := false, false
	for _, rem := range remaining {
		if rem.ID == "base" {
			baseFound = true
		}
		if rem.ID == newTask.ID {
			newFound = true
		}
	}
	if !baseFound || !newFound {
		t.Errorf("expected base and newTask to remain in store, got %+v", remaining)
	}
}
func TestManager_NumberedCopy_CleanMissingCopies_ScenarioHoleMissing(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 1})
	defer mgr.Close()
	ctx := context.Background()

	targetURL := "http://example.com/item.zip"
	writeFile(t, filepath.Join(tmpDir, "item.zip"))
	baseTask := &task.Task{ID: "base", URL: targetURL, Filename: "item.zip", Directory: tmpDir, Status: task.StatusCompleted}
	_ = store.Save(ctx, baseTask)

	// Copies 1, 2, 4, 5 exist on disk and in store; Copy 3 is missing on disk
	for i := 1; i <= 5; i++ {
		tName := fmt.Sprintf("item (%d).zip", i)
		tID := fmt.Sprintf("copy_%d", i)
		if i != 3 {
			writeFile(t, filepath.Join(tmpDir, tName))
		}
		_ = store.Save(ctx, &task.Task{
			ID:        tID,
			URL:       targetURL,
			Filename:  tName,
			Directory: tmpDir,
			Status:    task.StatusCompleted,
		})
	}

	suggested := mgr.Occupancy(ctx, nil).Suggest(tmpDir, "item.zip")
	if suggested != "item (3).zip" {
		t.Errorf("expected suggested copy item (3).zip, got %s", suggested)
	}
	c3Before, _ := store.Get(ctx, "copy_3")
	if c3Before == nil {
		t.Fatalf("copy_3 should not be deleted before user confirms download")
	}

	// 2. When user confirms download with "copy" strategy, copy_3 is deleted and new task is item (3).zip
	newTask, err := mgr.ResolveDuplicate(ctx, baseTask.ID, "copy", tmpDir, "item.zip", 2)
	if err != nil {
		t.Fatalf("ResolveDuplicate copy failed: %v", err)
	}
	if newTask.Filename != "item (3).zip" {
		t.Errorf("expected new task filename item (3).zip, got %s", newTask.Filename)
	}

	// Verify copy_3 was deleted, others preserved
	c3, _ := store.Get(ctx, "copy_3")
	if c3 != nil {
		t.Errorf("expected copy_3 to be deleted, but still found in store")
	}
	for _, idx := range []int{1, 2, 4, 5} {
		c, _ := store.Get(ctx, fmt.Sprintf("copy_%d", idx))
		if c == nil {
			t.Errorf("expected copy_%d to be preserved", idx)
		}
	}
}

func TestManager_ResolveDuplicate_RedownloadCleansOldTask(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 1})
	defer mgr.Close()
	ctx := context.Background()

	targetURL := "http://example.com/file.bin"
	writeFile(t, filepath.Join(tmpDir, "file.bin"))
	oldTask := &task.Task{
		ID:        "old_task",
		URL:       targetURL,
		Filename:  "file.bin",
		Directory: tmpDir,
		Status:    task.StatusCompleted,
	}
	_ = store.Save(ctx, oldTask)

	// When user resolves with "redownload", the old task should be deleted and a new task created
	newTask, err := mgr.ResolveDuplicate(ctx, oldTask.ID, "redownload", tmpDir, "file.bin", 2)
	if err != nil {
		t.Fatalf("ResolveDuplicate redownload failed: %v", err)
	}
	if newTask.ID == oldTask.ID {
		t.Fatalf("expected new task with distinct ID")
	}

	// Verify old_task is gone from store
	ot, _ := store.Get(ctx, oldTask.ID)
	if ot != nil {
		t.Errorf("expected old_task to be deleted on redownload")
	}

	// Verify newTask is in store
	nt, _ := store.Get(ctx, newTask.ID)
	if nt == nil {
		t.Errorf("expected new task in store")
	}

	tasks, _ := store.List(ctx)
	if len(tasks) != 1 {
		t.Errorf("expected exactly 1 task in store, got %d", len(tasks))
	}
}
func TestManager_ResolveDuplicate_PreservesHLSMedia(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 1})
	defer mgr.Close()
	ctx := context.Background()

	targetURL := "https://example.com/master.m3u8"
	oldTask := &task.Task{
		ID:         "hls_old",
		URL:        targetURL,
		Filename:   "video.mp4",
		Directory:  tmpDir,
		Status:     task.StatusCompleted,
		TotalBytes: 50 * 1024 * 1024,
		Media: &hls.Source{
			PlaylistURL: targetURL,
			Variant:     hls.Variant{URI: "https://example.com/480.m3u8", Height: 480},
		},
	}
	_ = store.Save(ctx, oldTask)

	// Redownload with a new probe (720p)
	newProbe := &engine.ProbeResult{
		URL:        targetURL,
		TotalBytes: 100 * 1024 * 1024,
		HLS: &engine.HLSProbe{
			IsHLS: true,
			Media: &hls.Source{
				PlaylistURL: targetURL,
				Variant:     hls.Variant{URI: "https://example.com/720.m3u8", Height: 720},
			},
		},
	}

	newTask, err := mgr.ResolveDuplicateFromProbe(ctx, oldTask.ID, "redownload", tmpDir, "video.mp4", 2, newProbe)
	if err != nil {
		t.Fatalf("ResolveDuplicateFromProbe failed: %v", err)
	}
	if !newTask.IsHLS() {
		t.Fatalf("expected newTask.IsHLS() to be true, got false")
	}
	if newTask.Media == nil || newTask.Media.Variant.Height != 720 {
		t.Fatalf("expected 720p variant in newTask.Media, got %+v", newTask.Media)
	}
	if newTask.TotalBytes != 100*1024*1024 {
		t.Fatalf("expected 100MB TotalBytes, got %d", newTask.TotalBytes)
	}
}
func writeFile(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("failed to close %s: %v", path, err)
	}
}

func TestManager_ProbeURLAndDuplicateDetection(t *testing.T) {
	payload := []byte("duplicate detection payload")
	store, mgr, ts := newTestManager(t, payload, 0, "")
	ctx := context.Background()

	result, err := mgr.ProbeURL(ctx, ts.URL+"/file.bin", nil)
	if err != nil {
		t.Fatalf("ProbeURL failed: %v", err)
	}
	if result.DuplicateTask != nil {
		t.Errorf("expected no duplicate task initially, got %v", result.DuplicateTask)
	}
	if result.TotalBytes != int64(len(payload)) || result.Filename != "file.bin" {
		t.Errorf("expected probed metadata for file.bin (%d bytes), got %s (%d bytes)", len(payload), result.Filename, result.TotalBytes)
	}

	createdTask, err := mgr.AddTask(ctx, ts.URL+"/file.bin", t.TempDir(), "file.bin", 2)
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	result2, err := mgr.ProbeURL(ctx, ts.URL+"/file.bin", nil)
	if err != nil {
		t.Fatalf("ProbeURL 2 failed: %v", err)
	}
	if result2.DuplicateTask == nil || result2.DuplicateTask.ID != createdTask.ID {
		t.Fatalf("expected duplicate task %s, got %v", createdTask.ID, result2.DuplicateTask)
	}
	_ = store
}

func TestManager_ResolveDuplicateTask(t *testing.T) {
	tmpDir := t.TempDir()
	payload := []byte("duplicate strategies payload")
	store, mgr, ts := newTestManager(t, payload, 0, "")
	ctx := context.Background()

	task1, err := mgr.AddTask(ctx, ts.URL+"/dup.bin", tmpDir, "dup.bin", 2)
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}
	_ = mgr.Pause(ctx, task1.ID)
	waitForStatus(t, store, task1.ID, 3*time.Second, task.StatusPaused, task.StatusCompleted)

	continuedTask, err := mgr.ResolveDuplicate(ctx, task1.ID, "continue", tmpDir, "dup.bin", 2)
	if err != nil {
		t.Fatalf("resolve duplicate 'continue' failed: %v", err)
	}
	if continuedTask.ID != task1.ID {
		t.Errorf("expected continue to reuse task %s, got %s", task1.ID, continuedTask.ID)
	}

	copyTask, err := mgr.ResolveDuplicate(ctx, task1.ID, "copy", tmpDir, "dup.bin", 2)
	if err != nil {
		t.Fatalf("resolve duplicate 'copy' failed: %v", err)
	}
	if copyTask.ID == task1.ID {
		t.Errorf("expected copy to create a distinct task")
	}
	if copyTask.Filename != "dup (1).bin" {
		t.Errorf("expected filename dup (1).bin, got %s", copyTask.Filename)
	}

	redownloadedTask, err := mgr.ResolveDuplicate(ctx, task1.ID, "redownload", tmpDir, "dup.bin", 2)
	if err != nil {
		t.Fatalf("resolve duplicate 'redownload' failed: %v", err)
	}
	if redownloadedTask.ID == task1.ID {
		t.Errorf("expected redownload/overwrite to create a new distinct task, got same ID %s", task1.ID)
	}
	if redownloadedTask.Downloaded != 0 {
		t.Errorf("expected redownload to reset progress, got %d", redownloadedTask.Downloaded)
	}
}

func TestManager_PreDownload_CancelInProgress(t *testing.T) {
	payload := make([]byte, 512*1024)
	store, mgr, ts := newTestManager(t, payload, 3*time.Millisecond, "")
	ctx := context.Background()

	tmpDir := t.TempDir()
	preTask, err := mgr.StartPreDownload(ctx, ts.URL+"/large.bin", tmpDir, "large.bin", 2)
	if err != nil {
		t.Fatalf("StartPreDownload failed: %v", err)
	}

	waitForStatus(t, store, preTask.ID, 3*time.Second, task.StatusDownloading)

	if err := mgr.CancelPreDownload(ctx, preTask.ID); err != nil {
		t.Fatalf("CancelPreDownload failed: %v", err)
	}

	saved, err := store.Get(ctx, preTask.ID)
	if err != nil {
		t.Fatalf("task should be preserved in store: %v", err)
	}
	if saved.Status != task.StatusPaused {
		t.Errorf("expected status paused, got %s", saved.Status)
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, "large.bin.sheepget")); statErr != nil {
		t.Errorf("partial data should be kept for resume: %v", statErr)
	}
}

func TestManager_PreDownload_ConfirmWithRenameAndDirChange(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "custom")
	payload := []byte("hello pre-download content")
	store, mgr, ts := newTestManager(t, payload, 0, "")
	ctx := context.Background()

	preTask, err := mgr.StartPreDownload(ctx, ts.URL+"/file.txt", tmpDir, "file.txt", 2)
	if err != nil {
		t.Fatalf("StartPreDownload failed: %v", err)
	}
	waitForStatus(t, store, preTask.ID, 3*time.Second, task.StatusCompleted)

	confirmedTask, err := mgr.ConfirmPreDownload(ctx, preTask.ID, subDir, "renamed.txt", 4)
	if err != nil {
		t.Fatalf("ConfirmPreDownload failed: %v", err)
	}
	if confirmedTask.Filename != "renamed.txt" || confirmedTask.Directory != subDir {
		t.Errorf("expected renamed.txt in %s, got %s in %s", subDir, confirmedTask.Filename, confirmedTask.Directory)
	}

	content, err := os.ReadFile(filepath.Join(subDir, "renamed.txt"))
	if err != nil {
		t.Fatalf("failed to read renamed file: %v", err)
	}
	if string(content) != string(payload) {
		t.Errorf("file content mismatch: %s", string(content))
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "file.txt")); !os.IsNotExist(err) {
		t.Errorf("old file should have been moved")
	}
}

func TestManager_PreDownload_ConfirmWhileDownloading(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "confirmed")
	payload := make([]byte, 256*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	store, mgr, ts := newTestManager(t, payload, 5*time.Millisecond, "")
	ctx := context.Background()

	preTask, err := mgr.StartPreDownload(ctx, ts.URL+"/slow.bin", tmpDir, "slow.bin", 2)
	if err != nil {
		t.Fatalf("StartPreDownload failed: %v", err)
	}

	// 要测的是「传输在途时确认」，所以等真实字节落库，而不是等状态翻成下载中。
	inFlight := waitForDownloaded(t, store, preTask.ID, 3*time.Second)
	if inFlight.Status != task.StatusDownloading {
		t.Fatalf("expected in-flight downloading, got %v", inFlight.Status)
	}

	confirmedTask, err := mgr.ConfirmPreDownload(ctx, preTask.ID, subDir, "confirmed.bin", 2)
	if err != nil {
		t.Fatalf("ConfirmPreDownload failed: %v", err)
	}
	if confirmedTask.Directory != subDir || confirmedTask.Filename != "confirmed.bin" {
		t.Fatalf("expected confirmed.bin in %s, got %s in %s", subDir, confirmedTask.Filename, confirmedTask.Directory)
	}

	waitForStatus(t, store, preTask.ID, 5*time.Second, task.StatusCompleted)

	content, err := os.ReadFile(filepath.Join(subDir, "confirmed.bin"))
	if err != nil {
		t.Fatalf("failed to read confirmed file: %v", err)
	}
	if len(content) != len(payload) {
		t.Fatalf("expected %d bytes, got %d", len(payload), len(content))
	}
	if string(content) != string(payload) {
		t.Fatalf("confirmed file content differs from the served resource")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "slow.bin.sheepget")); !os.IsNotExist(err) {
		t.Errorf("partial file should have been moved out of the dialog directory")
	}
}

func TestManager_PreDownload_CompletedThenCancel(t *testing.T) {
	tmpDir := t.TempDir()
	payload := []byte("completed data")
	store, mgr, ts := newTestManager(t, payload, 0, "")
	ctx := context.Background()

	preTask, err := mgr.StartPreDownload(ctx, ts.URL+"/done.bin", tmpDir, "done.bin", 2)
	if err != nil {
		t.Fatalf("StartPreDownload failed: %v", err)
	}
	waitForStatus(t, store, preTask.ID, 3*time.Second, task.StatusCompleted)

	if err := mgr.CancelPreDownload(ctx, preTask.ID); err != nil {
		t.Fatalf("CancelPreDownload failed: %v", err)
	}

	saved, err := store.Get(ctx, preTask.ID)
	if err != nil || saved.Status != task.StatusCompleted {
		t.Fatalf("task must remain completed after cancel, got %+v", saved)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "done.bin")); err != nil {
		t.Fatalf("completed file must not be removed on cancel: %v", err)
	}
}

func TestManager_CheckURLConsistency(t *testing.T) {
	payload := make([]byte, 2048)
	store, mgr, ts := newTestManager(t, payload, 0, "")
	ctx := context.Background()

	expired := &task.Task{
		ID:         "task_expired",
		URL:        "http://expired.example.com/file",
		Filename:   "file.bin",
		Directory:  t.TempDir(),
		TotalBytes: 2048,
		Downloaded: 512,
		Status:     task.StatusError,
		ErrorMsg:   "403 Forbidden: link expired",
		ETag:       `"version-1"`,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := store.Save(ctx, expired); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	// Same size, same ETag -> identity verified.
	sameETag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "2048")
		w.Header().Set("ETag", `"version-1"`)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-0/2048")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer sameETag.Close()

	res, err := mgr.CheckURLConsistency(ctx, expired.ID, sameETag.URL, nil)
	if err != nil {
		t.Fatalf("CheckURLConsistency failed: %v", err)
	}
	if !res.Consistent {
		t.Errorf("expected consistent=true for matching ETag, got reason: %s", res.Reason)
	}

	// Same size, different ETag -> refuse to resume.
	otherETag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "2048")
		w.Header().Set("ETag", `"version-2"`)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-0/2048")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer otherETag.Close()

	res, err = mgr.CheckURLConsistency(ctx, expired.ID, otherETag.URL, nil)
	if err != nil {
		t.Fatalf("CheckURLConsistency failed: %v", err)
	}
	if res.Consistent {
		t.Errorf("expected consistent=false for mismatched ETag")
	}

	// Same size, no validator on either side -> identity cannot be verified, so refuse to resume.
	noValidator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "2048")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-0/2048")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer noValidator.Close()

	res, err = mgr.CheckURLConsistency(ctx, expired.ID, noValidator.URL, nil)
	if err != nil {
		t.Fatalf("CheckURLConsistency failed: %v", err)
	}
	if res.Consistent {
		t.Errorf("expected consistent=false when no validator can confirm identity")
	}
	if res.Reason == "" {
		t.Errorf("expected an explanation when identity cannot be verified")
	}

	// Different size -> refuse to resume.
	res, err = mgr.CheckURLConsistency(ctx, expired.ID, ts.URL+"/file.bin", nil)
	if err != nil {
		t.Fatalf("CheckURLConsistency failed: %v", err)
	}
	if res.Consistent {
		t.Errorf("expected consistent=false for a size mismatch")
	}
}

func TestManager_UpdateTaskURLResumesWithRequestHeaders(t *testing.T) {
	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = byte((i * 3) % 251)
	}
	const referer = "https://player.example/watch"
	tmpDir := t.TempDir()
	store, mgr, ts := newTestManager(t, payload, 0, referer)
	ctx := context.Background()

	expired := &task.Task{
		ID:         "task_refresh",
		URL:        ts.URL + "/expired.bin",
		Filename:   "expired.bin",
		Directory:  tmpDir,
		TotalBytes: int64(len(payload)),
		Status:     task.StatusError,
		ErrorMsg:   "403 Forbidden",
		ETag:       `"v"`,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := store.Save(ctx, expired); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	// Without the required request context the refreshed link cannot even be probed.
	res, err := mgr.CheckURLConsistency(ctx, expired.ID, ts.URL+"/expired.bin", nil)
	if err != nil {
		t.Fatalf("CheckURLConsistency failed: %v", err)
	}
	if res.Consistent {
		t.Errorf("expected consistent=false while the request context is missing")
	}

	headers := map[string]string{"Referer": referer}
	res, err = mgr.CheckURLConsistency(ctx, expired.ID, ts.URL+"/expired.bin", headers)
	if err != nil {
		t.Fatalf("CheckURLConsistency failed: %v", err)
	}
	if !res.Consistent {
		t.Fatalf("expected the refreshed link to be usable with its request context, got: %s", res.Reason)
	}

	updated, err := mgr.UpdateTaskURL(ctx, expired.ID, ts.URL+"/expired.bin", headers)
	if err != nil {
		t.Fatalf("UpdateTaskURL failed: %v", err)
	}
	if updated.RequestHeaders == nil || updated.RequestHeaders.RawHeaders()["Referer"] != referer {
		t.Errorf("expected request headers to be stored on the task, got %v", updated.RequestHeaders)
	}
	if updated.ErrorMsg != "" {
		t.Errorf("expected the previous error to be cleared, got %s", updated.ErrorMsg)
	}

	// Updating the link must continue the download, and the transfer must carry the new headers.
	final := waitForStatus(t, store, expired.ID, 5*time.Second, task.StatusCompleted)
	if final.Downloaded != int64(len(payload)) {
		t.Errorf("expected %d downloaded bytes, got %d", len(payload), final.Downloaded)
	}
	content, err := os.ReadFile(filepath.Join(tmpDir, "expired.bin"))
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("downloaded file content differs from the served resource")
	}
}

func TestManager_ResetAndDownloadWithNewURL(t *testing.T) {
	oldPayload := make([]byte, 4096)
	newPayload := []byte("fresh resource after reset")
	tmpDir := t.TempDir()
	store, mgr, _ := newTestManager(t, oldPayload, 0, "")
	ctx := context.Background()

	freshServer := serveRanged(newPayload, 0, "")
	defer freshServer.Close()

	stale := &task.Task{
		ID:         "task_stale",
		URL:        "http://expired.example.com/old",
		Filename:   "stale.bin",
		Directory:  tmpDir,
		TotalBytes: int64(len(oldPayload)),
		Downloaded: 1024,
		Status:     task.StatusError,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := store.Save(ctx, stale); err != nil {
		t.Fatalf("failed to seed task: %v", err)
	}

	reset, err := mgr.ResetAndDownloadWithNewURL(ctx, stale.ID, freshServer.URL+"/stale.bin", nil)
	if err != nil {
		t.Fatalf("ResetAndDownloadWithNewURL failed: %v", err)
	}
	if reset.Downloaded != 0 {
		t.Errorf("expected progress to be reset, got %d", reset.Downloaded)
	}
	if reset.TotalBytes != int64(len(newPayload)) {
		t.Errorf("expected the new resource size %d, got %d", len(newPayload), reset.TotalBytes)
	}

	waitForStatus(t, store, stale.ID, 5*time.Second, task.StatusCompleted)
	content, err := os.ReadFile(filepath.Join(tmpDir, "stale.bin"))
	if err != nil {
		t.Fatalf("failed to read reset download: %v", err)
	}
	if string(content) != string(newPayload) {
		t.Fatalf("expected the new resource content, got %q", string(content))
	}
}
