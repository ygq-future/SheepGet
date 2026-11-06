// Package ratelimit 提供全局下载带宽速率限制器与速率受控的 io.Reader 包装。
package ratelimit

import (
	"context"
	"io"
	"sync"
	"time"
)

// Limiter 提供基于令牌桶的全局带宽速率限制。
// 线程安全，支持并发读取与动态调整速率。
type Limiter struct {
	mu          sync.Mutex
	limitKB     int
	bytesPerSec float64
	burst       float64
	tokens      float64
	lastTime    time.Time
}

// New 构造一个速率限制器。limitKB <= 0 表示不限速。
func New(limitKB int) *Limiter {
	l := &Limiter{}
	l.SetLimit(limitKB)
	return l
}

// SetLimit 动态更新限制速率 (KB/s)。<= 0 表示不限速。
func (l *Limiter) SetLimit(limitKB int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.advanceLocked(now)

	if limitKB < 0 {
		limitKB = 0
	}
	l.limitKB = limitKB
	if limitKB > 0 {
		l.bytesPerSec = float64(limitKB) * 1024
		// 桶容量基准为 1 秒的配额，物理底线 64KB，物理上限 1MB
		burst := l.bytesPerSec
		if burst < 64*1024 {
			burst = 64 * 1024
		} else if burst > 1024*1024 {
			burst = 1024 * 1024
		}
		l.burst = burst
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
	} else {
		l.bytesPerSec = 0
		l.burst = 0
		l.tokens = 0
	}
	l.lastTime = now
}

// LimitKB 返回当前限制速率 (KB/s)。
func (l *Limiter) LimitKB() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limitKB
}

func (l *Limiter) advanceLocked(now time.Time) {
	if l.limitKB <= 0 {
		l.tokens = 0
		l.lastTime = now
		return
	}
	if l.lastTime.IsZero() {
		l.lastTime = now
		l.tokens = l.burst
		return
	}
	dt := now.Sub(l.lastTime).Seconds()
	if dt > 0 {
		l.tokens += dt * l.bytesPerSec
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.lastTime = now
	}
}

// ChunkSize 根据当前速率计算推荐的单次读取切片大小，
// 以平滑吞吐并避免单次读取耗费过长等待时间。
func (l *Limiter) ChunkSize(bufLen int) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.limitKB <= 0 || bufLen <= 0 {
		return bufLen
	}

	// 目标单次读取耗时约 50ms
	target := int(l.bytesPerSec * 0.05)
	if target < 1024 {
		target = 1024
	} else if target > 32*1024 {
		target = 32 * 1024
	}
	if target > bufLen {
		target = bufLen
	}
	return target
}

// Wait 为刚刚读取的 n 个字节等待消费配额。
// 若未开启限速或 n <= 0 则立即返回。
func (l *Limiter) Wait(ctx context.Context, n int) error {
	if n <= 0 {
		return nil
	}

	l.mu.Lock()
	if l.limitKB <= 0 {
		l.mu.Unlock()
		return nil
	}

	now := time.Now()
	l.advanceLocked(now)

	l.tokens -= float64(n)
	var waitDur time.Duration
	if l.tokens < 0 {
		waitSec := -l.tokens / l.bytesPerSec
		// 单次等待保护：防止过度透支导致长时间阻断
		if waitSec > 2.0 {
			waitSec = 2.0
			l.tokens = -2.0 * l.bytesPerSec
		}
		waitDur = time.Duration(waitSec * float64(time.Second))
	}
	l.mu.Unlock()

	if waitDur <= 0 {
		return nil
	}

	timer := time.NewTimer(waitDur)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Reader 返回一个包装了速率限制的 io.Reader。
func (l *Limiter) Reader(ctx context.Context, r io.Reader) io.Reader {
	if l == nil {
		return r
	}
	return &limitedReader{
		ctx: ctx,
		r:   r,
		l:   l,
	}
}

type limitedReader struct {
	ctx context.Context
	r   io.Reader
	l   *Limiter
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if lr.l == nil || lr.l.LimitKB() <= 0 {
		return lr.r.Read(p)
	}

	maxChunk := lr.l.ChunkSize(len(p))
	n, err := lr.r.Read(p[:maxChunk])
	if n > 0 {
		if wErr := lr.l.Wait(lr.ctx, n); wErr != nil {
			return n, wErr
		}
	}
	return n, err
}
