// 滑动窗口延迟统计（P1：延迟分位数）。
//
// 探测频率低（每分钟 1-2 次）时，单个分钟桶内的样本不足以计算分位数，
// 因此哨兵在内存中为每服务维护最近 DelayWindowSize 次成功探测的延迟样本
// （跨分钟），每轮探测后更新窗口并计算 p50/p95/p99/stddev/jitter，
// 分钟桶落库时写入当前窗口快照。
package sentinel

import (
	"math"
	"sort"
)

const (
	// DelayWindowSize 滑动窗口容量：每服务最多保留的延迟样本数（毫秒）。
	DelayWindowSize = 60
	// DelayMinSamples 分位数有意义的最小样本数；不足时视为无数据（API 返回 null）。
	DelayMinSamples = 30
)

// DelayWindow 每服务最近 N 次成功探测的延迟样本，按到达顺序保存。
type DelayWindow struct {
	samples []int
}

// Add 追加一个延迟样本；超出容量时丢弃最旧样本。
func (w *DelayWindow) Add(delay int) {
	if delay < 0 {
		delay = 0
	}
	w.samples = append(w.samples, delay)
	if len(w.samples) > DelayWindowSize {
		w.samples = w.samples[len(w.samples)-DelayWindowSize:]
	}
}

// Len 当前窗口样本数。
func (w *DelayWindow) Len() int { return len(w.samples) }

// Percentile 计算有序样本的第 q 百分位（q ∈ [0,100]，线性插值，四舍五入）。
// 空样本返回 0；单样本返回该值。
func Percentile(sorted []int, q float64) int {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	pos := q / 100 * float64(n-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return int(math.Round(float64(sorted[lo]) + frac*float64(sorted[hi]-sorted[lo])))
}

// Snapshot 计算当前窗口的 p50/p95/p99、标准差（总体）与抖动
// （相邻样本差绝对值的均值）。样本数不足 DelayMinSamples 时返回 ok=false，
// 各统计值为 0。
func (w *DelayWindow) Snapshot() (p50, p95, p99, stddev, jitter int, ok bool) {
	if len(w.samples) < DelayMinSamples {
		return 0, 0, 0, 0, 0, false
	}
	sorted := make([]int, len(w.samples))
	copy(sorted, w.samples)
	sort.Ints(sorted)
	return Percentile(sorted, 50), Percentile(sorted, 95), Percentile(sorted, 99),
		stddevOf(w.samples), jitterOf(w.samples), true
}

// stddevOf 总体标准差（四舍五入到毫秒）。
func stddevOf(vals []int) int {
	n := len(vals)
	if n == 0 {
		return 0
	}
	mean := 0.0
	for _, v := range vals {
		mean += float64(v)
	}
	mean /= float64(n)
	var sq float64
	for _, v := range vals {
		d := float64(v) - mean
		sq += d * d
	}
	return int(math.Round(math.Sqrt(sq / float64(n))))
}

// jitterOf 相邻样本差绝对值的均值（四舍五入到毫秒）。
func jitterOf(vals []int) int {
	n := len(vals)
	if n < 2 {
		return 0
	}
	sum := 0.0
	for i := 1; i < n; i++ {
		d := float64(vals[i] - vals[i-1])
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return int(math.Round(sum / float64(n-1)))
}
