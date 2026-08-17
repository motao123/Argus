package notifier

import (
	"sync"
	"time"
)

// rateBucket 单渠道令牌桶。
// 令牌按 RateLimitPerMin/60 个/秒 持续补充（等价于每分钟补充 RateLimitPerMin 个），
// 桶容量为 BurstLimit（瞬时突发上限）。令牌不足时不允许发送，调用方把投递
// 排到 next_retry 稍后重试（进入既有重试路径，不丢弃）。
type rateBucket struct {
	ratePerMin int       // 每分钟补充令牌数（>0 才启用）
	burst      int       // 桶容量（>0 才启用）
	tokens     float64   // 当前可用令牌（可为小数）
	last       time.Time // 上次补充时间
}

// rateLimiter 每渠道独立桶的限流器（进程内状态；渠道配置变更时自动重置桶）。
// 并发安全：Enqueue/worker 可能在不同 goroutine 触发发送。
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*rateBucket
}

// allow 尝试为渠道 channelID 消耗一个令牌。
// 返回 (wait, true) 表示放行；返回 (wait, false) 表示超限，wait 为预计
// 下一次有令牌可用的等待时长（供 next_retry 调度）。
// 限流未启用（ratePerMin<=0 或 burst<=0，即 0=不限）时始终放行。
func (rl *rateLimiter) allow(channelID int64, ratePerMin, burst int, now time.Time) (time.Duration, bool) {
	if ratePerMin <= 0 || burst <= 0 {
		return 0, true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.buckets == nil {
		rl.buckets = map[int64]*rateBucket{}
	}
	b, ok := rl.buckets[channelID]
	if !ok || b.ratePerMin != ratePerMin || b.burst != burst {
		// 新渠道或限流配置变更：重置为满桶，立即放行第一条。
		b = &rateBucket{ratePerMin: ratePerMin, burst: burst, tokens: float64(burst) - 1, last: now}
		rl.buckets[channelID] = b
		return 0, true
	}
	// 持续补充：按经过时间累计令牌，封顶桶容量。
	if elapsed := now.Sub(b.last); elapsed >= 0 {
		b.tokens += elapsed.Seconds() * (float64(ratePerMin) / 60)
		if b.tokens > float64(burst) {
			b.tokens = float64(burst)
		}
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return 0, true
	}
	wait := time.Duration((1 - b.tokens) / (float64(ratePerMin) / 60) * float64(time.Second))
	return wait, false
}
