package engine_test

import (
	"context"
	"encoding/json"
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
	if _, err := mgr.AddTaskWithHeaders(ctx, ts.URL+"/big.bin", tmpDir, "big.bin", 4, nil); err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		tasks, listErr := mgr.List(ctx)
		if listErr != nil {
			t.Fatalf("failed to list tasks: %v", listErr)
		}
		if len(tasks) == 1 && tasks[0].Status == task.StatusCompleted {
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
