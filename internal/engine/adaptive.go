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
	ConservativeMaxCap       = 4
)

// AdaptiveConcurrency 控制多连接分块下载的动态并发窗口。
// 它通过慢启动探测（Slow Start）与限流自适应，在服务端发生 503/429 或限流时冻结或逐步回退并发，
// 并将失败连接就地降级隔离，避免由于配置的高并发把服务器打满而导致整个任务失败。
type AdaptiveConcurrency struct {
	mu               sync.Mutex
	maxLimit         int           // 当前通道允许的最大并发（受限于 transportCeiling 及本轮退避）
	transportCeiling int           // 跨代理通道传递的并发天花板（当某通道建立连接后因并发过高被限制时，下一通道严格 < throttledAt）
	currentLimit     int           // 当前允许的最大并发活跃数
	hasEstablished   bool          // 当前通道是否曾成功建立并收到过数据流（区分"死代理"与"并发超限"）
	everThrottled    bool          // 任务生命周期内是否已经遭遇过源站限流或并发封禁
	throttledAt      int           // 当前通道首次触发限流/故障时的并发水位
	successCount     int           // 连续成功请求计数
	lastBackoff      time.Time     // 上次触发限流降级的时间
	cooldown         time.Duration // 降级后的探测冷却时间
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
		maxLimit:         max,
		transportCeiling: max,
		currentLimit:     initial,
		cooldown:         DefaultAdaptiveCooldown,
	}
}

// CurrentLimit 返回当前允许的并发上限。
func (ac *AdaptiveConcurrency) CurrentLimit() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.currentLimit
}

// MaxLimit 返回当前收敛后的最大并发天花板。
func (ac *AdaptiveConcurrency) MaxLimit() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.maxLimit
}

// ThrottledAt 返回本轮通道首次遭遇限流或连接异常时的并发水位（未触发时为 0）。
func (ac *AdaptiveConcurrency) ThrottledAt() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.throttledAt
}

// HasEverThrottled 报告该任务是否曾遭遇过源站限流或并发封禁。
// 一旦为 true，协调器将限制激进的慢块拆分与首包瞬间扩容，避免再次冲爆源站。
func (ac *AdaptiveConcurrency) HasEverThrottled() bool {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.everThrottled
}

// OnStreamEstablished 在一个分块连接成功建立并开始拉流时调用（收到首包数据触发）。
// 若尚未遭遇过源站并发封禁，允许收到首包时平滑向上扩容 1 个槽位；
// 若此前已经触发过源站并发封禁（everThrottled == true），则不再在收到首包时瞬间连环扩容，而是交由 OnSuccess 在分片完成时稳步推进。
func (ac *AdaptiveConcurrency) OnStreamEstablished() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.hasEstablished = true
	if ac.everThrottled || ac.currentLimit >= ac.maxLimit {
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

	// 一旦任务在当前源站遭遇过并发限流或连接封禁（everThrottled == true），
	// 锁定安全巡航并发，禁止再盲目向上扩容，防止爬升到临界点再次将代理 IP 打封禁。
	if ac.everThrottled || ac.currentLimit >= ac.maxLimit {
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

// FreezeOnFailure 在遇到单次连接故障或临时网络中断时调用，冻结当前并发窗口，防止继续盲目扩容。
func (ac *AdaptiveConcurrency) FreezeOnFailure() {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.lastBackoff = time.Now()
	ac.successCount = 0
}

// CapMaxLimit 将允许的最大并发上限收敛至不超过 capLimit（最低保底为 1）。
// 用于遭遇连接重置风暴或切换备用传输通道后，防止并发再次无限爬升触发源站封禁。
func (ac *AdaptiveConcurrency) CapMaxLimit(capLimit int) int {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	if capLimit < 1 {
		capLimit = 1
	}
	if ac.maxLimit > capLimit {
		ac.maxLimit = capLimit
	}
	if ac.currentLimit > ac.maxLimit {
		ac.currentLimit = ac.maxLimit
	}
	return ac.maxLimit
}

// OnRateLimit 在请求遇到服务端限流（如 503 Service Unavailable、429 Too Many Requests）或连接重置风暴时调用。
// 策略：
//  1. 首次遇到异常时停止扩容并保持/冻结当前并发数量；
//  2. 若在时间窗口（冷却期）内持续遇到限流或连接被拒，则逐步回退并发（每次减 1），直到保底单线程（1）；
//     同时将任务的最大并发天花板收敛至安全水位，避免冷却期过后再次冲高并发触发源站封禁。
func (ac *AdaptiveConcurrency) OnRateLimit() int {
	return ac.OnRateLimitWithActive(0)
}

// OnRateLimitWithActive 在请求遇到服务端限流或连接异常时调用，activeWorkers 为故障发生时的实际活跃连接数。
// 首次在当前通道遇到异常时，记录触发限制的水位 throttledAt = max(currentLimit, activeWorkers)。
func (ac *AdaptiveConcurrency) OnRateLimitWithActive(activeWorkers int) int {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	now := time.Now()
	// 仅当当前通道曾成功建立连接拉流后才发生故障时，记录触发源站限制的并发水位；
	// 若某个代理一上来连首包都没通就直接 502/超时，属于代理自身不可用而非并发超限。
	if ac.hasEstablished && ac.throttledAt == 0 {
		ac.throttledAt = max(ac.currentLimit, activeWorkers)
		ac.everThrottled = true
	}
	// 若在冷却时间窗口内再次触发限流，说明当前并发数依然过高，继续回退 1 个槽位直到单线程
	if !ac.lastBackoff.IsZero() && now.Sub(ac.lastBackoff) < ac.cooldown {
		if ac.currentLimit > 1 {
			ac.currentLimit--
		}
		safeCap := max(ac.currentLimit, ConservativeMaxCap)
		if ac.throttledAt > 1 && ac.throttledAt-1 < safeCap {
			safeCap = ac.throttledAt - 1
		}
		if ac.maxLimit > safeCap {
			ac.maxLimit = safeCap
		}
	}

	ac.lastBackoff = now
	ac.successCount = 0
	return ac.currentLimit
}

// OnTransportSwitched 在传输中途成功切换到下一个代理或直连通道时调用：
//  1. 若上一通道曾成功拉流但因并发冲高到 throttledAt 被限制，则将新通道的 maxLimit 折半收敛至安全水位
//     min(throttledAt/2, ConservativeMaxCap)（严格 < throttledAt，最低保底为 1），并锁定不再向上扩容；
//  2. 若上一通道一上来就是死代理（从未建立过有效数据流），则不扣减 transportCeiling，保留完整并发上限；
//  3. 清空旧通道的退避冷却与故障计数，并将 currentLimit 恢复至合理的起步并发窗口（不超过新 maxLimit）。
func (ac *AdaptiveConcurrency) OnTransportSwitched(activeAtFailure int) (throttledAt, newCurrent, newMax int) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	triggerLevel := 0
	if ac.hasEstablished {
		ac.everThrottled = true
		triggerLevel = max(ac.throttledAt, activeAtFailure)
		if triggerLevel > 1 {
			newCap := min(triggerLevel/2, ConservativeMaxCap)
			if newCap < 1 {
				newCap = 1
			}
			if ac.transportCeiling > newCap {
				ac.transportCeiling = newCap
			}
		} else if triggerLevel == 1 {
			ac.transportCeiling = 1
		}
	}
	if ac.transportCeiling < 1 {
		ac.transportCeiling = 1
	}
	ac.maxLimit = ac.transportCeiling

	// 新通道起步并发：若已获知源站上限，则直接锁定在安全巡航并发 maxLimit（<= ConservativeMaxCap）；否则以 2 慢启动
	startLimit := min(ac.maxLimit, 2)
	if ac.everThrottled {
		startLimit = min(ac.maxLimit, ConservativeMaxCap)
	}
	if startLimit < 1 {
		startLimit = 1
	}
	ac.currentLimit = startLimit
	ac.hasEstablished = false
	ac.throttledAt = 0
	ac.successCount = 0
	ac.lastBackoff = time.Time{}
	return triggerLevel, ac.currentLimit, ac.maxLimit
}
