package ratelimit_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"sheep-get/internal/ratelimit"
)

func TestLimiter_ZeroLimit_Unlimited(t *testing.T) {
	lim := ratelimit.New(0)
	if lim.LimitKB() != 0 {
		t.Fatalf("expected 0, got %d", lim.LimitKB())
	}

	start := time.Now()
	ctx := context.Background()
	for range 10 {
		if err := lim.Wait(ctx, 100*1024); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("unlimited should be near-instant, took %v", elapsed)
	}
}

func TestLimiter_Wait_Throttling(t *testing.T) {
	// 限速 200 KB/s
	// 读取 100 KB，由于初始桶有 burst，第一次可能很快；
	// 随后再读 200 KB，应该花费约 1 秒。
	lim := ratelimit.New(200)

	ctx := context.Background()
	// 消耗掉初始 burst
	_ = lim.Wait(ctx, 200*1024)

	start := time.Now()
	// 再读 100 KB，理论耗时约 500ms
	err := lim.Wait(ctx, 100*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 350*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Fatalf("expected elapsed around 500ms, got %v", elapsed)
	}
}

func TestLimiter_Reader_Wrapper(t *testing.T) {
	lim := ratelimit.New(300) // 300 KB/s

	data := bytes.Repeat([]byte("1234567890ABCDEF"), 10*1024) // 160 KB
	src := bytes.NewReader(data)
	ctx := context.Background()
	reader := lim.Reader(ctx, src)

	buf := make([]byte, 8*1024)
	var out bytes.Buffer

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("unexpected read error: %v", err)
		}
	}

	if !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("read content mismatch")
	}
}

func TestLimiter_ContextCanceled(t *testing.T) {
	lim := ratelimit.New(50) // 50 KB/s

	ctx, cancel := context.WithCancel(context.Background())
	// 消费初始 burst
	_ = lim.Wait(ctx, 100*1024)

	// 异步取消
	time.AfterFunc(30*time.Millisecond, cancel)

	err := lim.Wait(ctx, 50*1024)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestLimiter_DynamicAdjustment(t *testing.T) {
	lim := ratelimit.New(100)
	if lim.LimitKB() != 100 {
		t.Fatalf("expected 100, got %d", lim.LimitKB())
	}

	// 动态调整为 0（不限速）
	lim.SetLimit(0)
	if lim.LimitKB() != 0 {
		t.Fatalf("expected 0, got %d", lim.LimitKB())
	}

	ctx := context.Background()
	start := time.Now()
	_ = lim.Wait(ctx, 500*1024)
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("after disabling limit, wait should be instant")
	}

	// 调整为负数，应自愈为 0
	lim.SetLimit(-50)
	if lim.LimitKB() != 0 {
		t.Fatalf("expected 0 for negative input, got %d", lim.LimitKB())
	}
}

func TestLimiter_ConcurrentAccess(t *testing.T) {
	lim := ratelimit.New(1000) // 1000 KB/s
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				_ = lim.Wait(ctx, 1024)
			}
		}()
	}
	wg.Wait()
}
