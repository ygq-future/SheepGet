package window

import (
	"context"
	"testing"
	"time"

	"sheep-get/internal/config"
	"sheep-get/internal/events"
)

// recordingWindowView 记录窗口调用的先后次序。blockShow 为真时 Show 会一直阻塞到 release
// 被关闭为止，用来复现「主线程被占住」：Wails 的 Show / Focus / Hide / Emit 都会把调用同步
// 派发回主线程并等它执行完（application.InvokeSync 没有超时），主线程不空，它们就不返回。
type recordingWindowView struct {
	blockShow   bool
	showStarted chan struct{}
	release     chan struct{}
	ops         chan string
}

func newRecordingWindowView() *recordingWindowView {
	return newWindowView(false)
}

func newBlockedWindowView() *recordingWindowView {
	return newWindowView(true)
}

func newWindowView(blockShow bool) *recordingWindowView {
	view := &recordingWindowView{
		blockShow:   blockShow,
		showStarted: make(chan struct{}, 1),
		release:     make(chan struct{}),
		ops:         make(chan string, 16),
	}
	if !blockShow {
		close(view.release)
	}
	return view
}

func (v *recordingWindowView) Show() {
	if v.blockShow {
		select {
		case v.showStarted <- struct{}{}:
		default:
		}
		<-v.release
	}
	v.ops <- "show"
}

func (v *recordingWindowView) Hide()  { v.ops <- "hide" }
func (v *recordingWindowView) Focus() { v.ops <- "focus" }

func (v *recordingWindowView) Emit(event string, _ any) { v.ops <- "emit:" + event }

// waitForOps 按次序收集 n 次窗口调用；超时即报错，避免用例在窗口调用丢失时静默挂住。
func (v *recordingWindowView) waitForOps(t *testing.T, n int) []string {
	t.Helper()
	got := make([]string, 0, n)
	deadline := time.After(2 * time.Second)
	for len(got) < n {
		select {
		case op := <-v.ops:
			got = append(got, op)
		case <-deadline:
			t.Fatalf("expected %d window calls, got %v", n, got)
		}
	}
	return got
}

// 交接响应不能等着窗口显示完成。扩展只等 2.5 秒（HANDOVER_TIMEOUT_MS），一旦超时它会按
// no_response 把下载还给浏览器，而桌面端其实已经受理了这次请求——用户于是同时得到浏览器
// 的一份下载和桌面端的一条任务，而两边都以为自己是唯一一份。
func TestQueueController_EnqueueDoesNotWaitForWindow(t *testing.T) {
	ctx := context.Background()
	view := newBlockedWindowView()
	qc, _, _, tmpDir := setupQueueWithView(t, config.DuplicatePolicyPrompt, view, newAsyncOps())

	enqueued := make(chan struct{})
	go func() {
		defer close(enqueued)
		if _, err := qc.Enqueue(ctx, DownloadRequest{URL: "https://example.com/blocked.zip", Directory: tmpDir}); err != nil {
			t.Errorf("enqueue failed: %v", err)
		}
	}()

	select {
	case <-view.showStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("window was never shown")
	}

	// 窗口仍卡在显示上，交接响应此时必须已经返回。
	select {
	case <-enqueued:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Enqueue did not return while the window was blocked: the handover response would exceed the extension's 2.5s timeout")
	}

	close(view.release)
}

// 第二个请求（以及读队列、取消）也不能被第一个请求卡住的窗口显示挡住：窗口调用在锁内时，
// 队列锁会一直握在那个阻塞的调用上，每一次交接都要排队等它——超时后同样是静默还给浏览器。
func TestQueueController_QueueStaysUsableWhileWindowIsBlocked(t *testing.T) {
	ctx := context.Background()
	view := newBlockedWindowView()
	qc, _, _, tmpDir := setupQueueWithView(t, config.DuplicatePolicyPrompt, view, newAsyncOps())

	go func() {
		if _, err := qc.Enqueue(ctx, DownloadRequest{URL: "https://example.com/first.zip", Directory: tmpDir}); err != nil {
			t.Errorf("first enqueue failed: %v", err)
		}
	}()
	select {
	case <-view.showStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("window was never shown")
	}

	secondEnqueued := make(chan struct{})
	go func() {
		defer close(secondEnqueued)
		if _, err := qc.Enqueue(ctx, DownloadRequest{URL: "https://example.com/second.zip", Directory: tmpDir}); err != nil {
			t.Errorf("second enqueue failed: %v", err)
		}
	}()
	select {
	case <-secondEnqueued:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("a second handover waited for the first one's blocked window call")
	}

	activeRead := make(chan struct{})
	go func() {
		defer close(activeRead)
		if _, err := qc.GetActive(); err != nil {
			t.Errorf("GetActive failed: %v", err)
		}
	}()
	select {
	case <-activeRead:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reading the active item waited for a blocked window call")
	}

	close(view.release)
}

// 窗口副作用必须按提交次序执行。界面上的当前项要与队列里的 activeIndex 对应：次序一旦错乱
// （例如第二个请求先投递），用户可能在错的那一项上点确认，确认的是另一条链接。
func TestQueueController_WindowCallsKeepSubmissionOrder(t *testing.T) {
	ctx := context.Background()
	view := newRecordingWindowView()
	qc, _, _, tmpDir := setupQueueWithView(t, config.DuplicatePolicyPrompt, view, newAsyncOps())

	// 不带链接的请求不会触发后台探测，窗口调用因此只有登记与推进两种，次序完全确定。
	if _, err := qc.Enqueue(ctx, DownloadRequest{Directory: tmpDir}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if err := qc.CancelCurrent(ctx); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}

	got := view.waitForOps(t, 4)
	want := []string{"show", "focus", "emit:" + events.FileInfoNext, "hide"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("window calls out of order: got %v, want %v", got, want)
		}
	}
}
