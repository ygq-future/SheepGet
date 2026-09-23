package stallwatch_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"sheep-get/internal/stallwatch"
)

// blockingReader 模拟「连接还在、数据不来」：第一次读就阻塞，直到 ctx 取消。
type blockingReader struct {
	ctx context.Context
}

func (r *blockingReader) Read(p []byte) (int, error) {
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func TestWatch_CancelsIdleRequest(t *testing.T) {
	parent := context.Background()
	ctx, watch := stallwatch.New(parent, 50*time.Millisecond)
	defer watch.Stop()

	_, err := io.ReadAtLeast(watch.Reader(&blockingReader{ctx: ctx}), make([]byte, 8), 8)
	if err == nil {
		t.Fatal("expected the idle request to be aborted")
	}
	if !watch.Stalled() {
		t.Fatal("expected the watch to report a stall")
	}
	classified := watch.Err(parent, err)
	if !errors.Is(classified, stallwatch.ErrStalled) {
		t.Fatalf("expected ErrStalled, got %v", classified)
	}
}

// 慢但仍在发字节的连接不该被判成停摆：限速服务器是合法场景。
func TestWatch_AllowsSlowButActiveStream(t *testing.T) {
	parent := context.Background()
	_, watch := stallwatch.New(parent, 120*time.Millisecond)
	defer watch.Stop()

	body := watch.Reader(io.NopCloser(strings.NewReader(strings.Repeat("x", 64))))
	written := 0
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	buf := make([]byte, 8)
	for written < 64 {
		<-ticker.C
		n, err := body.Read(buf)
		written += n
		if err != nil {
			t.Fatalf("slow stream was cut off after %d bytes: %v", written, err)
		}
		if watch.Stalled() {
			t.Fatalf("slow stream flagged as stalled after %d bytes", written)
		}
	}
	if written != 64 {
		t.Fatalf("expected 64 bytes, got %d", written)
	}
}

// 父 ctx 取消（暂停、退出）优先于停摆：调用方必须据此把它当取消传递下去，而不是当网络故障。
func TestWatch_ParentCancelWinsOverStall(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx, watch := stallwatch.New(parent, 50*time.Millisecond)
	defer watch.Stop()

	time.AfterFunc(10*time.Millisecond, cancel)
	_, err := io.ReadAtLeast(watch.Reader(&blockingReader{ctx: ctx}), make([]byte, 8), 8)
	if err == nil {
		t.Fatal("expected the request to be aborted")
	}
	classified := watch.Err(parent, err)
	if !errors.Is(classified, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", classified)
	}
	if errors.Is(classified, stallwatch.ErrStalled) {
		t.Fatal("a user cancel must not be reported as a stall")
	}
}

// Stop 必须释放这次请求的 ctx：不取消的话，父 ctx 会一路挂着成千上万个已结束请求的死节点。
func TestWatch_StopReleasesRequestContext(t *testing.T) {
	parent := context.Background()
	reqCtx, watch := stallwatch.New(parent, time.Hour)
	watch.Stop()
	if reqCtx.Err() == nil {
		t.Fatal("expected the request context to be released after Stop")
	}
	if watch.Stalled() {
		t.Fatal("a released request must not be reported as stalled")
	}
}

// 看门狗覆盖「等待响应头」阶段：还没拿到响应就算停摆，同样被斩断。
func TestWatch_CoversHeaderWait(t *testing.T) {
	parent := context.Background()
	ctx, watch := stallwatch.New(parent, 40*time.Millisecond)
	defer watch.Stop()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected the header wait to be aborted by the watchdog")
	}
	if !watch.Stalled() {
		t.Fatal("expected the watch to report a stall during the header wait")
	}
}
