// Package stallwatch 提供读取看门狗：一次请求连续一段时间没有任何字节到达时，取消这次请求。
//
// 大文件的传输不该有总时长上限——在慢速服务器上传很久是合法的；但对端静默挂住（连接还在、
// 数据不再来）时不产生任何错误，读会一直阻塞，任务于是既不到达也不失败。判据因此取
// 「多久没有新字节」：停摆连接的特征是字节完全停止，仍在发字节的慢速服务器不会触发。
//
// 看门狗不会自己重试：它只把这次请求归类成停摆（或父 ctx 取消），由调用方按既有策略处置——
// 传输侧据此从当前偏移重连，分片侧据此走它的重试。
package stallwatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// ErrStalled 表示响应在读取途中停摆：连续 timeout 内没有任何字节到达。
// 它与 context.Canceled 的区分至关重要——后者意味着暂停或退出，重试逻辑必须放过；
// 停摆是网络或服务器的故障，按可重试的失败处理。
var ErrStalled = errors.New("服务器在传输途中停止发送数据")

// Watch 挂在一次请求上：从发起请求到读完响应体，只要连续 timeout 没有进展就取消这次请求。
// 覆盖两个阶段——等待响应头，以及读取响应体——因为「发出响应头之后停住」是最难缠的卡死形态。
type Watch struct {
	timeout time.Duration
	cancel  context.CancelFunc
	timer   *time.Timer
	fired   atomic.Bool
}

// New 给 ctx 套上看门狗，返回这次请求应当使用的 ctx 与看门狗本身。
// 调用方负责 defer watch.Stop()，并在拿到错误后用 watch.Err 归类。
func New(ctx context.Context, timeout time.Duration) (context.Context, *Watch) {
	if timeout <= 0 {
		return ctx, &Watch{timeout: timeout}
	}
	reqCtx, cancel := context.WithCancel(ctx)
	watch := &Watch{timeout: timeout, cancel: cancel}
	watch.timer = time.AfterFunc(timeout, func() {
		watch.fired.Store(true)
		cancel()
	})
	return reqCtx, watch
}

// Touch 记一次进展（例如响应头到达），把空闲计时从此刻重新开始。
func (w *Watch) Touch() {
	if w.timer != nil && !w.fired.Load() {
		w.timer.Reset(w.timeout)
	}
}

// Reader 包住响应体：每读到字节就把空闲计时重新开始。
func (w *Watch) Reader(r io.Reader) io.Reader {
	if w.timer == nil {
		return r
	}
	return &watchedReader{r: r, watch: w}
}

// Stalled 报告这次请求是否因空闲而被斩断。读到错误后立即查询，据此换用 ErrStalled。
func (w *Watch) Stalled() bool { return w.fired.Load() }

// Stop 结束这次请求的看门狗。它同时取消这次请求的 ctx：请求已经读完，但 ctx 不取消就会
// 一直挂在父 ctx 的子节点表里——一次大文件传输有成百上千次请求，那些死节点会一直堆到任务结束。
func (w *Watch) Stop() {
	if w.timer != nil {
		w.timer.Stop()
	}
	if w.cancel != nil {
		w.cancel()
	}
}

// Err 把一次失败归类。父 ctx 自己取消（暂停、退出）优先——那必须继续当作取消传递下去；
// 其次是停摆；两者都不是时原样返回。
func (w *Watch) Err(parent context.Context, err error) error {
	if perr := parent.Err(); perr != nil {
		return perr
	}
	if w.Stalled() {
		return fmt.Errorf("%w（%s 内没有收到任何数据）", ErrStalled, w.timeout)
	}
	return err
}

type watchedReader struct {
	r     io.Reader
	watch *Watch
}

func (wr *watchedReader) Read(p []byte) (int, error) {
	// Reset 与看门狗回调分属两个 goroutine，极端交错时可能提前取消一次请求；
	// 那只是多走一遍重试，不影响正确性，为此加锁不值得。
	n, err := wr.r.Read(p)
	if n > 0 {
		wr.watch.Touch()
	}
	return n, err
}
