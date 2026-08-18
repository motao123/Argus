package tdigest

import (
	"math"
	"math/rand"
	"testing"
)

// TestPercentileAccuracy 验证均匀分布的分位数精度（误差 < 2%）。
func TestPercentileAccuracy(t *testing.T) {
	d := New(DefaultCompression)
	rng := rand.New(rand.NewSource(42))
	n := 100000
	// 均匀分布 [0, 100)
	for i := 0; i < n; i++ {
		d.Add(rng.Float64() * 100)
	}
	cases := []struct {
		p, want float64
	}{
		{50, 50}, {95, 95}, {99, 99}, {0.1, 0.1},
	}
	for _, c := range cases {
		got := d.Percentile(c.p)
		if math.Abs(got-c.want) > 2.0 {
			t.Errorf("p%v = %.2f, want ~%.2f", c.p, got, c.want)
		}
	}
	if d.Count() != float64(n) {
		t.Errorf("count = %v, want %d", d.Count(), n)
	}
	if d.Min() < 0 || d.Min() >= 1 || d.Max() >= 100 {
		t.Errorf("min/max = %v/%v", d.Min(), d.Max())
	}
}

// TestPercentileSkew 验证偏态分布尾部精度（尾部质心密集 → 高精度）。
func TestPercentileSkew(t *testing.T) {
	d := New(DefaultCompression)
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 50000; i++ {
		// 指数分布（大量小值 + 少量大值）
		d.Add(math.Pow(10, rng.Float64()*4))
	}
	// 真值：10^q 的分位数就是 10^q
	for _, q := range []float64{0.5, 0.9, 0.95, 0.99} {
		got := d.Quantile(q)
		want := math.Pow(10, q*4)
		// 对数误差 < 0.3（数量级内）
		if math.Abs(math.Log10(got)-math.Log10(want)) > 0.3 {
			t.Errorf("q%v = %.3e, want ~%.3e", q, got, want)
		}
	}
}

// TestMerge 验证两个 digest 合并后分位数与整体一致。
func TestMerge(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	a, b := New(30), New(30)
	for i := 0; i < 5000; i++ {
		a.Add(rng.Float64() * 100)
		b.Add(50 + rng.Float64()*50)
	}
	merged := New(30)
	merged.Merge(a)
	merged.Merge(b)
	// 混合分布：a ~ U[0,100)（5000 样本），b ~ U[50,100)（5000 样本）。
	// 真实 p50 = 50 + 0.25*10000/150 ≈ 66.7（[50,100) 区间密度 150/单位）。
	if p50 := merged.Percentile(50); p50 < 60 || p50 > 72 {
		t.Errorf("merged p50 = %.2f, want ~66.7", p50)
	}
	if merged.Count() != a.Count()+b.Count() {
		t.Errorf("merged count = %v, want %v", merged.Count(), a.Count()+b.Count())
	}
}

// TestEncodeDecode 验证二进制编码往返。
func TestEncodeDecode(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	d := New(25)
	for i := 0; i < 20000; i++ {
		d.Add(rng.Float64() * 100)
	}
	raw := d.Encode()
	got, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Count() != d.Count() {
		t.Errorf("round-trip count = %v, want %v", got.Count(), d.Count())
	}
	for _, p := range []float64{50, 90, 95, 99} {
		g, w := got.Percentile(p), d.Percentile(p)
		if math.Abs(g-w) > 1.0 {
			t.Errorf("round-trip p%v = %.2f, want %.2f", p, g, w)
		}
	}
	// 非法数据
	if _, err := Decode([]byte("xx")); err == nil {
		t.Fatal("bad magic should error")
	}
	// 空 digest 往返
	e := New(0)
	gotEmpty, err := Decode(e.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if gotEmpty.Count() != 0 {
		t.Fatal("empty digest round-trip should have count 0")
	}
}

// TestAddNaN 验证 NaN 被忽略。
func TestAddNaN(t *testing.T) {
	d := New(0)
	d.Add(math.NaN())
	d.Add(1)
	if d.Count() != 1 {
		t.Fatalf("NaN should be ignored, count = %v", d.Count())
	}
}
