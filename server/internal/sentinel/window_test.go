package sentinel

import "testing"

func TestPercentileOddAndEven(t *testing.T) {
	// 奇数个样本：直接取中间位置
	if got := Percentile([]int{1, 2, 3}, 50); got != 2 {
		t.Fatalf("p50 odd = %d, want 2", got)
	}
	// 偶数个样本：线性插值 2.5 → 四舍五入 3
	if got := Percentile([]int{1, 2, 3, 4}, 50); got != 3 {
		t.Fatalf("p50 even = %d, want 3", got)
	}
	if got := Percentile([]int{10, 20, 30, 40, 50, 60}, 50); got != 35 {
		t.Fatalf("p50 six = %d, want 35", got)
	}
	// 边界百分位
	if got := Percentile([]int{5, 6, 7, 8}, 0); got != 5 {
		t.Fatalf("p0 = %d, want 5", got)
	}
	if got := Percentile([]int{5, 6, 7, 8}, 100); got != 8 {
		t.Fatalf("p100 = %d, want 8", got)
	}
	// 空/单样本
	if got := Percentile(nil, 50); got != 0 {
		t.Fatalf("empty = %d, want 0", got)
	}
	if got := Percentile([]int{42}, 99); got != 42 {
		t.Fatalf("single = %d, want 42", got)
	}
}

func TestSnapshotRequiresMinSamples(t *testing.T) {
	w := &DelayWindow{}
	for i := 0; i < DelayMinSamples-1; i++ {
		w.Add(100)
	}
	if _, _, _, _, _, ok := w.Snapshot(); ok {
		t.Fatalf("snapshot with %d samples should be not ok", w.Len())
	}
	w.Add(100)
	if _, _, _, _, _, ok := w.Snapshot(); !ok {
		t.Fatalf("snapshot with %d samples should be ok", w.Len())
	}
}

func TestSnapshotQuantilesStddevJitter(t *testing.T) {
	// 30 个样本：15 个 0ms 与 15 个 100ms 交替 → p50=50、p95=p99=100、
	// 总体标准差 50、抖动（相邻差绝对值均值）= 100。
	w := &DelayWindow{}
	for i := 0; i < 15; i++ {
		w.Add(0)
		w.Add(100)
	}
	p50, p95, p99, stddev, jitter, ok := w.Snapshot()
	if !ok {
		t.Fatal("snapshot should be ok with 30 samples")
	}
	if p50 != 50 || p95 != 100 || p99 != 100 {
		t.Fatalf("percentiles = %d/%d/%d, want 50/100/100", p50, p95, p99)
	}
	if stddev != 50 {
		t.Fatalf("stddev = %d, want 50", stddev)
	}
	if jitter != 100 {
		t.Fatalf("jitter = %d, want 100", jitter)
	}

	// 恒定延迟：标准差与抖动均为 0。
	w2 := &DelayWindow{}
	for i := 0; i < DelayWindowSize; i++ {
		w2.Add(80)
	}
	_, _, _, stddev2, jitter2, _ := w2.Snapshot()
	if stddev2 != 0 || jitter2 != 0 {
		t.Fatalf("constant delay stddev/jitter = %d/%d, want 0/0", stddev2, jitter2)
	}
}

func TestWindowEvictsOldestBeyondCapacity(t *testing.T) {
	w := &DelayWindow{}
	for i := 1; i <= DelayWindowSize+10; i++ {
		w.Add(i)
	}
	if w.Len() != DelayWindowSize {
		t.Fatalf("len = %d, want %d", w.Len(), DelayWindowSize)
	}
	if w.samples[0] != 11 || w.samples[len(w.samples)-1] != 70 {
		t.Fatalf("window retained [%d..%d], want [11..70]", w.samples[0], w.samples[len(w.samples)-1])
	}
	// 负数延迟按 0 处理。
	w2 := &DelayWindow{}
	w2.Add(-5)
	if w2.samples[0] != 0 {
		t.Fatalf("negative delay clamped to 0, got %d", w2.samples[0])
	}
}
