package window

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sheep-get/internal/config"
	"sheep-get/internal/engine"
	"sheep-get/internal/task"
)

// 队列锁的适用范围：锁内只做内存与本地元数据，会走网络的引擎调用一律在锁外。
//
// 真实引擎里这些调用不受队列控制：ProbeURL 会重试三次、还带一次 HEAD 兜底，HTTP 客户端
// 本身没有超时（Timeout: 0），一个黑洞地址就能把它卡上几十秒。锁被它握住的这段时间里，
// 读当前项、取消、下一次交接全都要排队——而浏览器交接只等 2.5 秒（HANDOVER_TIMEOUT_MS），
// 超时后扩展按「没收到响应」把下载还给浏览器，桌面端其实已经受理，用户拿到两份下载。

// blockingEngine 是一个可以停在指定方法里的引擎。blocked 列出的方法会一直等到 release
// 被调用为止；其余方法立即返回。它同时记录调用次序与关键入参，便于断言提交最终走了哪条路径。
type blockingEngine struct {
	mu        sync.Mutex
	calls     []string
	blocks    map[string]chan struct{}
	entered   map[string]chan struct{}
	cancelled []string
	confirmed string
}

func newBlockingEngine(blocked ...string) *blockingEngine {
	e := &blockingEngine{
		blocks:  make(map[string]chan struct{}),
		entered: make(map[string]chan struct{}),
	}
	for _, name := range blocked {
		e.blocks[name] = make(chan struct{})
		e.entered[name] = make(chan struct{}, 8)
	}
	return e
}

// enter 记录一次调用；该方法若在被点名的名单里，就在这里等到放行。
func (e *blockingEngine) enter(name string) {
	e.mu.Lock()
	e.calls = append(e.calls, name)
	entered := e.entered[name]
	block := e.blocks[name]
	e.mu.Unlock()

	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		<-block
	}
}

func (e *blockingEngine) waitFor(t *testing.T, name string) {
	t.Helper()
	select {
	case <-e.entered[name]:
	case <-time.After(2 * time.Second):
		t.Fatalf("engine never reached %s", name)
	}
}

// release 放行该方法：通道关闭后，后续同名调用也不再阻塞。
func (e *blockingEngine) release(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ch, ok := e.blocks[name]; ok {
		close(ch)
		delete(e.blocks, name)
	}
}

func (e *blockingEngine) called(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.calls {
		if c == name {
			return true
		}
	}
	return false
}

func (e *blockingEngine) cancelledIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.cancelled...)
}

func (e *blockingEngine) confirmedTaskID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.confirmed
}

func (e *blockingEngine) ProbeURL(context.Context, string) (*engine.ProbeResult, error) {
	e.enter("ProbeURL")
	return &engine.ProbeResult{TotalBytes: 1024, Resumable: true}, nil
}

func (e *blockingEngine) FindDuplicateTask(context.Context, string) (*task.Task, error) {
	e.enter("FindDuplicateTask")
	return nil, nil
}

func (e *blockingEngine) AddTask(context.Context, string, string, string, int) (*task.Task, error) {
	e.enter("AddTask")
	return &task.Task{ID: "task_manual"}, nil
}

func (e *blockingEngine) AddTaskWithHeaders(context.Context, string, string, string, int, map[string]string) (*task.Task, error) {
	e.enter("AddTaskWithHeaders")
	return &task.Task{ID: "task_manual"}, nil
}

func (e *blockingEngine) StartPreDownload(context.Context, string, string, string, int) (*task.Task, error) {
	e.enter("StartPreDownload")
	return &task.Task{ID: "task_pre"}, nil
}

func (e *blockingEngine) StartPreDownloadWithHeaders(context.Context, string, string, string, int, map[string]string) (*task.Task, error) {
	e.enter("StartPreDownloadWithHeaders")
	return &task.Task{ID: "task_pre"}, nil
}

func (e *blockingEngine) ConfirmPreDownload(_ context.Context, taskID, _, _ string, _ int) (*task.Task, error) {
	e.enter("ConfirmPreDownload")
	e.mu.Lock()
	e.confirmed = taskID
	e.mu.Unlock()
	return &task.Task{ID: taskID}, nil
}

func (e *blockingEngine) CancelPreDownload(_ context.Context, taskID string) error {
	e.enter("CancelPreDownload")
	e.mu.Lock()
	e.cancelled = append(e.cancelled, taskID)
	e.mu.Unlock()
	return nil
}

func (e *blockingEngine) ResolveDuplicate(_ context.Context, taskID, _, _, _ string, _ int) (*task.Task, error) {
	e.enter("ResolveDuplicate")
	return &task.Task{ID: taskID}, nil
}

func (e *blockingEngine) NumberedCopyName(_ context.Context, _, filename string) (string, error) {
	e.enter("NumberedCopyName")
	return filename, nil
}

func (e *blockingEngine) ReuseExistingFile(_ context.Context, taskID, _, _ string) (*task.Task, error) {
	e.enter("ReuseExistingFile")
	return &task.Task{ID: taskID}, nil
}

func setupQueueWithEngine(t *testing.T, eng DownloadEngine, view WindowView, runOps windowOps) (*QueueController, string) {
	t.Helper()
	tmpDir := t.TempDir()
	settings := config.DefaultSettings(tmpDir, filepath.Join(tmpDir, "temp"))
	settings.Download.DuplicateURLPolicy = config.DuplicatePolicyPrompt
	settings.Download.PreDownload = false
	settings.Download.DefaultConnectionsPerTask = 4
	return newQueueController(eng, &testSettingsProvider{settings: settings}, view, runOps), tmpDir
}

// assertQueueResponsive 断言队列的基本操作没有被某次引擎调用挡住。500ms 是刻意的宽限：
// 这些操作只碰内存和本地元数据，正常情况下是微秒级，只有锁被引擎调用握着才会走到这里。
func assertQueueResponsive(t *testing.T, qc *QueueController, ctx context.Context, dir string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := qc.Enqueue(ctx, DownloadRequest{Directory: dir}); err != nil {
			t.Errorf("enqueue while the engine was busy failed: %v", err)
		}
		if _, err := qc.GetActive(); err != nil {
			t.Errorf("reading the active item while the engine was busy failed: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queue operations waited for a blocked engine call")
	}
}

func hasItem(items []*FileInfoItem, id string) bool {
	for _, it := range items {
		if it.ID == id {
			return true
		}
	}
	return false
}

// 提交会联网（补一次同链接探测，然后建任务或确认预下载）。这段时间里队列必须仍然可用，
// 取消也必须立刻给出答复——而不是排在引擎调用后面，等它把 2.5 秒的交接预算耗光。
func TestQueueController_SubmitDoesNotHoldQueueLockWhileEngineWorks(t *testing.T) {
	ctx := context.Background()
	eng := newBlockingEngine("ProbeURL", "AddTaskWithHeaders")
	qc, tmpDir := setupQueueWithEngine(t, eng, newRecordingWindowView(), newAsyncOps())

	// 不带链接的登记不会触发后台探测，这里要控制的只有提交这条路径。
	reg, err := qc.Enqueue(ctx, DownloadRequest{Directory: tmpDir})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	submitted := make(chan error, 1)
	go func() {
		_, err := qc.Submit(ctx, FileInfoSubmission{
			RequestID: reg.RequestID,
			URL:       "https://example.com/slow.zip",
			Filename:  "slow.zip",
			Directory: tmpDir,
			MaxConn:   4,
		})
		submitted <- err
	}()

	eng.waitFor(t, "ProbeURL")
	assertQueueResponsive(t, qc, ctx, tmpDir)

	eng.release("ProbeURL")
	eng.waitFor(t, "AddTaskWithHeaders")
	assertQueueResponsive(t, qc, ctx, tmpDir)

	// 提交中这一项不能被取消：要么让它走完，要么明确告诉用户「正在提交」。
	// 无论如何都不能让这次取消排在引擎调用后面，最后落到队列里的下一项头上。
	if err := qc.CancelCurrent(ctx); !errors.Is(err, ErrSubmitInProgress) {
		t.Fatalf("cancelling a submission in progress = %v, want ErrSubmitInProgress", err)
	}

	eng.release("AddTaskWithHeaders")
	select {
	case err := <-submitted:
		if err != nil {
			t.Fatalf("submit failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("submit never returned after the engine was released")
	}

	if hasItem(qc.GetQueueItems(), reg.RequestID) {
		t.Fatal("the submitted item stayed in the queue")
	}
}

// 预下载启动也是联网调用（StartPreDownloadWithHeaders 内部会探测 URL）。它一旦挪出锁外，
// 提交就可能撞上「任务已经在启动、但 PreDownloadTaskID 还是空的」这一刻：此时走「新建任务」
// 会在同一链接上建出第二份。这一项正在启动预下载时，提交必须等它落地，再走确认预下载。
func TestQueueController_SubmitWaitsForPreDownloadStart(t *testing.T) {
	ctx := context.Background()
	eng := newBlockingEngine("ProbeURL", "StartPreDownloadWithHeaders")
	qc, tmpDir := setupQueueWithEngine(t, eng, newRecordingWindowView(), newAsyncOps())

	preDownload := true
	reg, err := qc.Enqueue(ctx, DownloadRequest{
		URL:         "https://example.com/pre.zip",
		Directory:   tmpDir,
		PreDownload: &preDownload,
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	eng.release("ProbeURL")
	eng.waitFor(t, "StartPreDownloadWithHeaders")

	submitted := make(chan error, 1)
	go func() {
		_, err := qc.Submit(ctx, FileInfoSubmission{
			RequestID: reg.RequestID,
			URL:       "https://example.com/pre.zip",
			Filename:  "pre.zip",
			Directory: tmpDir,
			MaxConn:   4,
		})
		submitted <- err
	}()

	// 给提交一次机会去建第二份任务；放行之前它本该还等着。
	time.Sleep(150 * time.Millisecond)
	select {
	case err := <-submitted:
		t.Fatalf("submit returned while the pre-download was still starting: %v", err)
	default:
	}

	eng.release("StartPreDownloadWithHeaders")
	select {
	case err := <-submitted:
		if err != nil {
			t.Fatalf("submit failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("submit never returned after the pre-download start was released")
	}

	if eng.called("AddTaskWithHeaders") {
		t.Fatal("提交另建了一份任务，而不是确认已经在跑的预下载")
	}
	if got := eng.confirmedTaskID(); got != "task_pre" {
		t.Fatalf("confirmed pre-download task = %q, want task_pre", got)
	}
}

// 预下载启动期间用户按了取消：这一项必须真的走掉，而它启动出来的任务不能留下来自己跑——
// 那就是一份用户从未确认过的下载。启动完成后发现这一项已经不在队列里，就该把它收掉。
func TestQueueController_CancelDuringPreDownloadStartLeavesNoTask(t *testing.T) {
	ctx := context.Background()
	eng := newBlockingEngine("ProbeURL", "StartPreDownloadWithHeaders")
	qc, tmpDir := setupQueueWithEngine(t, eng, newRecordingWindowView(), newAsyncOps())

	preDownload := true
	if _, err := qc.Enqueue(ctx, DownloadRequest{
		URL:         "https://example.com/pre.zip",
		Directory:   tmpDir,
		PreDownload: &preDownload,
	}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	eng.release("ProbeURL")
	eng.waitFor(t, "StartPreDownloadWithHeaders")

	cancelled := make(chan error, 1)
	go func() { cancelled <- qc.CancelCurrent(ctx) }()
	select {
	case err := <-cancelled:
		if err != nil {
			t.Fatalf("cancel failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("取消排在预下载启动后面返回：这一项最后会落到队列里的下一项头上")
	}
	if got := qc.QueueLength(); got != 0 {
		t.Fatalf("queue length after cancel = %d, want 0", got)
	}

	eng.release("StartPreDownloadWithHeaders")

	deadline := time.After(2 * time.Second)
	for {
		if ids := eng.cancelledIDs(); len(ids) > 0 {
			if ids[0] != "task_pre" {
				t.Fatalf("cancelled pre-download = %q, want task_pre", ids[0])
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("取消之后，已经启动的预下载任务仍然留着")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
