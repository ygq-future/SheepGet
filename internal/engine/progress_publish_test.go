package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

// marshalListener 模拟 Wails 的事件派发：载荷被交给另一条 goroutine 序列化。
// 真实事件总线就是这样工作的（application.EventProcessor.Emit → mailbox → 新起 goroutine
// 里 json.Marshal），因此「交给监听者的对象在之后还会被改写」是一次真正的数据竞争。
type marshalListener struct {
	mu          sync.Mutex
	downloading *task.Task
	done        chan struct{}
	wg          sync.WaitGroup
}

func newMarshalListener() *marshalListener {
	return &marshalListener{done: make(chan struct{})}
}

func (l *marshalListener) OnTaskUpdated(t *task.Task) {
	l.mu.Lock()
	if l.downloading == nil && t.Status == task.StatusDownloading {
		l.downloading = t
	}
	l.mu.Unlock()

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		for {
			select {
			case <-l.done:
				return
			default:
			}
			_, _ = json.Marshal(t)
			time.Sleep(200 * time.Microsecond)
		}
	}()
}

func (l *marshalListener) stop() {
	close(l.done)
	l.wg.Wait()
}

func (l *marshalListener) downloadingPayload() *task.Task {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.downloading
}

// blockingPublishSink 让第一次发布停在原地，用来观察「发布还在进行中」时传输是否仍在前进。
type blockingPublishSink struct {
	firstStarted  chan struct{}
	firstRelease  chan struct{}
	secondStarted chan struct{}

	mu    sync.Mutex
	calls int
}

func (s *blockingPublishSink) WantsSnapshot() bool { return true }

func (s *blockingPublishSink) PublishSnapshot(*task.Task) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()

	switch call {
	case 1:
		close(s.firstStarted)
		<-s.firstRelease
	case 2:
		close(s.secondStarted)
	}
}

// TestHTTPDownloader_PublishDoesNotHoldChunkLock 固定一条契约：进度发布（落盘 + 广播）不发生在
// 分片元数据锁内。
//
// 发布要写全库 JSON 并回调监听者，压在 coord.mu 上时所有分片工作协程都会跟着停摆——一个慢磁盘
// 就能把整条传输按成单线程。这里让第一次发布卡住：若锁仍被它持有，第二个工作协程永远交不出
// 自己的副本，用例超时失败。
func TestHTTPDownloader_PublishDoesNotHoldChunkLock(t *testing.T) {
	payload := make([]byte, 4<<20)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	ts := serveRanged(payload, 0, "")
	defer ts.Close()

	tmpDir := t.TempDir()
	sink := &blockingPublishSink{
		firstStarted:  make(chan struct{}),
		firstRelease:  make(chan struct{}),
		secondStarted: make(chan struct{}),
	}
	dlTask := &task.Task{
		ID:             "task-publish-lock",
		URL:            ts.URL + "/big.bin",
		Filename:       "big.bin",
		Directory:      tmpDir,
		TotalBytes:     int64(len(payload)),
		Resumable:      true,
		MaxConcurrency: 4,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	downloader := engine.NewHTTPDownloader(ts.Client())
	errCh := make(chan error, 1)
	go func() { errCh <- downloader.Download(ctx, dlTask, sink) }()

	select {
	case <-sink.firstStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("expected the transfer to publish progress")
	}

	select {
	case <-sink.secondStarted:
	case <-time.After(2 * time.Second):
		close(sink.firstRelease)
		<-errCh
		t.Fatal("chunk workers stalled behind an in-flight publish: PublishSnapshot must run outside coord.mu")
	}
	close(sink.firstRelease)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("download failed: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("download did not finish")
	}

	got, err := os.ReadFile(filepath.Join(tmpDir, "big.bin"))
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("downloaded content does not match the served payload")
	}
}

// TestManager_EventPayloadIsPrivateCopy 固定一条契约：事件载荷必须是自有副本。
//
// 传输期间任务的可变对象只属于传输侧（下载器与分片协调器），而事件载荷会被另一条 goroutine
// 序列化。两者共享同一个结构体时，序列化会读到正在被改写的分片——数据竞争（用 -race 可复现），
// 界面上也可能收到半更新的状态。本用例同时覆盖两件事：
//  1. 监听者在传输期间反复序列化载荷（-race 下暴露竞争）；
//  2. 任务随后完成时，已经发出的事件内容不得跟着变化（不依赖 -race 的确定性断言）。
func TestManager_EventPayloadIsPrivateCopy(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	payload := make([]byte, 2*1024*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	// 每次 4KiB 写延迟 1ms：传输持续上百毫秒，期间进度事件与分片改写必然重叠。
	ts := serveRanged(payload, time.Millisecond, "")
	defer ts.Close()

	downloader := engine.NewHTTPDownloader(ts.Client())
	mgr := engine.NewManager(store, downloader, engine.Config{MaxActiveTasks: 1})
	defer mgr.Close()

	listener := newMarshalListener()
	mgr.AddListener(listener)

	ctx := context.Background()
	if _, err := mgr.CreateTask(ctx, engine.CreateRequest{URL: ts.URL + "/big.bin", Directory: tmpDir, Filename: "big.bin", MaxConn: 4}); err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		tasks, listErr := mgr.List(ctx)
		if listErr != nil {
			t.Fatalf("failed to list tasks: %v", listErr)
		}
		if len(tasks) == 1 && tasks[0].Status == task.StatusCompleted {
			if tasks[0].ElapsedNano <= 0 {
				t.Errorf("completed task should record positive ElapsedNano, got %d", tasks[0].ElapsedNano)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("download did not finish in time: %+v", tasks)
		}
		time.Sleep(10 * time.Millisecond)
	}
	listener.stop()

	downloading := listener.downloadingPayload()
	if downloading == nil {
		t.Fatal("expected a downloading task update event")
	}
	// 事件发出后任务仍在被改写（进度前进、最终完成）。载荷若是同一个对象，这里读到的就会是
	// 结束状态——那正是竞态的另一面。
	if downloading.Status != task.StatusDownloading {
		t.Errorf("event payload changed after it was published: status = %q, want %q",
			downloading.Status, task.StatusDownloading)
	}
	if downloading.Downloaded >= int64(len(payload)) {
		t.Errorf("event payload changed after it was published: downloaded = %d, want less than %d",
			downloading.Downloaded, len(payload))
	}
}

func TestManager_AccumulatesElapsedNanoOnlyWhileRunning(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := task.NewFileTaskStore(filepath.Join(tmpDir, "tasks.json"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	payload := make([]byte, 512<<10)
	ts := serveRanged(payload, 2*time.Millisecond, "")
	defer ts.Close()

	mgr := engine.NewManager(store, engine.NewHTTPDownloader(nil), engine.Config{MaxActiveTasks: 1})
	defer mgr.Close()

	ctx := context.Background()
	created, err := mgr.CreateTask(ctx, engine.CreateRequest{URL: ts.URL + "/big.bin", Directory: tmpDir, Filename: "big.bin", MaxConn: 2})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	// Wait until some bytes are downloaded, then pause
	waitForDownloaded(t, store, created.ID, 5*time.Second)
	if err := mgr.Pause(ctx, created.ID); err != nil {
		t.Fatalf("failed to pause task: %v", err)
	}
	_ = waitForStatus(t, store, created.ID, 5*time.Second, task.StatusPaused, task.StatusCompleted)
	time.Sleep(50 * time.Millisecond)
	paused, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get paused task: %v", err)
	}
	if paused.ElapsedNano <= 0 {
		t.Fatalf("expected positive ElapsedNano after running, got %d", paused.ElapsedNano)
	}

	if paused.Status == task.StatusPaused {
		elapsedAtPause := paused.ElapsedNano
		time.Sleep(150 * time.Millisecond)
		afterIdle, getErr := store.Get(ctx, created.ID)
		if getErr != nil {
			t.Fatalf("failed to get task: %v", getErr)
		}
		if afterIdle.ElapsedNano != elapsedAtPause {
			t.Fatalf("paused task should not accumulate elapsed time: got %d, want %d", afterIdle.ElapsedNano, elapsedAtPause)
		}

		if err := mgr.Resume(ctx, created.ID); err != nil {
			t.Fatalf("failed to resume task: %v", err)
		}
		completed := waitForStatus(t, store, created.ID, 10*time.Second, task.StatusCompleted)
		if completed.ElapsedNano <= elapsedAtPause {
			t.Fatalf("expected resumed task to accumulate further ElapsedNano: got %d <= %d", completed.ElapsedNano, elapsedAtPause)
		}
	}
}
