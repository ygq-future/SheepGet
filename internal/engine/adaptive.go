package engine

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// RateLimitError 表示因服务端限流（429 或 503 等）引起的临时连接被拒。
// 自适应并发机制将其识别为并发降级信号，将其与普通网络故障区分开。
type RateLimitError struct {
	Status string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("server rate limited (%s): scaling down concurrency", e.Status)
}

// IsRateLimitError 检查目标错误是否属于服务端限流错误。
func IsRateLimitError(err error) bool {
	var rle *RateLimitError
	return errors.As(err, &rle)
}

const (
	DefaultProbeInitialLimit = 1
	DefaultAdaptiveCooldown  = 2 * time.Second
	SuccessesPerScaleUp      = 2
)

// AdaptiveConcurrency 控制多连接分块下载的动态并发窗口。
// 它通过慢启动探测（Slow Start）与限流自适应，在服务端发生 503/429 或限流时冻结或逐步回退并发，
// 并将失败连接就地降级隔离，避免由于配置的高并发把服务器打满而导致整个任务失败。
type AdaptiveConcurrency struct {
	mu           sync.Mutex
	maxLimit     int           // 用户或任务配置的硬性最大并发
	currentLimit int           // 当前允许的最大并发活跃数
	successCount int           // 连续成功请求计数
	lastBackoff  time.Time     // 上次触发限流降级的时间
	cooldown     time.Duration // 降级后的探测冷却时间
}

// NewAdaptiveConcurrency 创建一个自适应并发控制器。
// max 为任务允许的上限，initial 为初始探测并发数（通常为 1，若 initial > max 则截断至 max）。
func NewAdaptiveConcurrency(max, initial int) *AdaptiveConcurrency {
	if max < 1 {
		max = 1
	}
	if initial < 1 {
		initial = DefaultProbeInitialLimit
	}
	if initial > max {
		initial = max
	}
	return &AdaptiveConcurrency{
		maxLimit:     max,
		currentLimit: initial,
		cooldown:     DefaultAdaptiveCooldown,
	}
}

// CurrentLimit 返回当前允许的并发上限。
func (ac *AdaptiveConcurrency) CurrentLimit() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.currentLimit
}

// OnStreamEstablished 在一个分块连接成功建立并开始拉流时调用（收到首包数据触发）。
// 只要未处于降级冷却期，平滑向上扩容 1 个并发槽位，快速达到 maxLimit。
func (ac *AdaptiveConcurrency) OnStreamEstablished() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.currentLimit >= ac.maxLimit {
		return ac.currentLimit
	}

	// 若处于降级冷却期，不尝试扩容
	if !ac.lastBackoff.IsZero() && time.Since(ac.lastBackoff) < ac.cooldown {
		return ac.currentLimit
	}

	ac.currentLimit++
	return ac.currentLimit
}

// OnSuccess 在一个分块或请求成功传输后被调用，推动窗口在冷却期过后平稳扩容。
func (ac *AdaptiveConcurrency) OnSuccess() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.currentLimit >= ac.maxLimit {
		return
	}

	// 若处于降级冷却期，不尝试扩容
	if !ac.lastBackoff.IsZero() && time.Since(ac.lastBackoff) < ac.cooldown {
		return
	}

	ac.successCount++
	// 每平稳完成若干次请求，尝试向上探测增加 1 个并发槽位
	if ac.successCount >= SuccessesPerScaleUp {
		ac.successCount = 0
		ac.currentLimit++
		if ac.currentLimit > ac.maxLimit {
			ac.currentLimit = ac.maxLimit
		}
	}
}

// FreezeOnFailure 在遇到连接故障或临时网络中断时调用，冻结当前并发窗口，防止继续盲目扩容。
func (ac *AdaptiveConcurrency) FreezeOnFailure() {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.lastBackoff = time.Now()
	ac.successCount = 0
}

// OnRateLimit 在请求遇到服务端限流（如 503 Service Unavailable、429 Too Many Requests）时调用。
// 策略：
// 1. 首次遇到异常时停止扩容并保持/冻结当前并发数量；
// 2. 若在时间窗口（冷却期）内持续遇到限流，则逐步回退并发（每次减 1），直到保底单线程（1）。
func (ac *AdaptiveConcurrency) OnRateLimit() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	now := time.Now()
	// 若在冷却时间窗口内再次触发限流，说明当前并发数依然过高，继续回退 1 个槽位直到单线程
	if !ac.lastBackoff.IsZero() && now.Sub(ac.lastBackoff) < ac.cooldown {
		if ac.currentLimit > 1 {
			ac.currentLimit--
		}
	}

	ac.lastBackoff = now
	ac.successCount = 0
	return ac.currentLimit
}
