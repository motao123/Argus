// Package tdigest 实现 Dunning & Ertl 的 merging 变体 t-digest，
// 用于指标分位数（p50/p95/p99）的近似计算（算法对齐 komari pkg/metric/tdigest.go）。
//
// t-digest 是面向流式数据的紧凑分位数摘要：把样本折叠为一组质心
// （mean, weight），中位数附近质心稀疏、尾部密集，从而在固定内存下
// 保持尾部百分位的高精度。多个 digest 可无损 Merge（rollup 降采样依赖）。
package tdigest

import (
	"encoding/binary"
	"errors"
	"math"
	"sort"
)

// DefaultCompression 默认压缩比：越高精度越大、内存越大。
const DefaultCompression = 30.0

// digestMagic / digestVersion 是二进制编码魔数与版本。
const (
	digestMagic0  = 'T'
	digestMagic1  = 'D'
	digestVersion = 1
)

// centroid 一个质心：mean 为样本均值，weight 为样本数。
type centroid struct {
	mean   float64
	weight float64
}

// TDigest 分位数摘要。
type TDigest struct {
	compression float64
	centroids   []centroid
	count       float64
	min, max    float64
	processed   bool
}

// New 创建指定压缩比的 digest（compression<=1 时用默认值）。
func New(compression float64) *TDigest {
	if compression <= 1 {
		compression = DefaultCompression
	}
	return &TDigest{compression: compression, min: math.Inf(1), max: math.Inf(-1), processed: true}
}

// processThreshold 触发压缩的未处理质心缓冲上限；process() 会把它压缩回
// ~compression 个质心。
func (t *TDigest) processThreshold() int { return int(8*t.compression) + 16 }

// Add 追加一个样本。
func (t *TDigest) Add(x float64) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return
	}
	t.centroids = append(t.centroids, centroid{mean: x, weight: 1})
	t.count++
	if x < t.min {
		t.min = x
	}
	if x > t.max {
		t.max = x
	}
	t.processed = false
	if len(t.centroids) > t.processThreshold() {
		t.process()
	}
}

// Merge 把另一个 digest 的质心并入（rollup 父桶合成）。
func (t *TDigest) Merge(o *TDigest) {
	if o == nil || o.count == 0 {
		return
	}
	t.centroids = append(t.centroids, o.centroids...)
	t.count += o.count
	if o.min < t.min {
		t.min = o.min
	}
	if o.max > t.max {
		t.max = o.max
	}
	t.processed = false
	if len(t.centroids) > t.processThreshold() {
		t.process()
	}
}

// process 按均值排序，在合并后权重低于分位数相关大小限制
// 4*N*q*(1-q)/compression 时合并相邻质心。该限制在 q=0.5 附近宽松、
// 尾部收紧到接近零；`limit<1 && proposed<=1` 保证极端尾部单点也能合并，
// 使质心数收敛到 ~compression。
func (t *TDigest) process() {
	if t.processed {
		return
	}
	if len(t.centroids) == 0 {
		t.processed = true
		return
	}
	sort.Slice(t.centroids, func(i, j int) bool { return t.centroids[i].mean < t.centroids[j].mean })
	total := t.count
	merged := t.centroids[:0]
	cur := t.centroids[0]
	weightBefore := 0.0
	for i := 1; i < len(t.centroids); i++ {
		next := t.centroids[i]
		proposed := cur.weight + next.weight
		// 合并后质心中心的 CDF 位置
		q := (weightBefore + proposed/2) / total
		limit := 4 * total * q * (1 - q) / t.compression
		if proposed <= limit || limit < 1 && proposed <= 1 {
			// 加权均值更新保持质心均值精确
			cur.mean += next.weight * (next.mean - cur.mean) / proposed
			cur.weight = proposed
		} else {
			merged = append(merged, cur)
			weightBefore += cur.weight
			cur = next
		}
	}
	merged = append(merged, cur)
	t.centroids = merged
	t.processed = true
}

// Count 样本数。
func (t *TDigest) Count() float64 { return t.count }

// Min / Max 样本最小/最大值。
func (t *TDigest) Min() float64 { return t.min }
func (t *TDigest) Max() float64 { return t.max }

// Quantile 估算指定分位数（q ∈ [0,1]）：头部锚定 min、质心间线性插值、
// 尾部锚定 max。
func (t *TDigest) Quantile(q float64) float64 {
	t.process()
	n := len(t.centroids)
	if n == 0 {
		return math.NaN()
	}
	if q <= 0 {
		return t.min
	}
	if q >= 1 {
		return t.max
	}
	if n == 1 {
		return t.centroids[0].mean
	}
	index := q * t.count

	// 头部：min 与第一个质心中心之间
	c0 := t.centroids[0]
	if index < c0.weight/2 {
		z := index / (c0.weight / 2)
		return t.min + (c0.mean-t.min)*z
	}
	weightSoFar := c0.weight / 2
	for i := 0; i < n-1; i++ {
		c := t.centroids[i]
		next := t.centroids[i+1]
		dw := (c.weight + next.weight) / 2
		if index < weightSoFar+dw {
			z := (index - weightSoFar) / dw
			return c.mean*(1-z) + next.mean*z
		}
		weightSoFar += dw
	}
	// 尾部：最后一个质心中心与 max 之间
	cl := t.centroids[n-1]
	z := (index - weightSoFar) / (cl.weight / 2)
	if z > 1 {
		z = 1
	}
	return cl.mean + (t.max-cl.mean)*z
}

// Percentile 便捷方法：0-100 的分位数。
func (t *TDigest) Percentile(p float64) float64 { return t.Quantile(p / 100) }

// Encode 序列化为紧凑小端二进制 blob：
// magic[2] version[1] compression[8] min[8] max[8] count[8] nCentroids[4]
// 后接 nCentroids * (mean[8] weight[8])。
func (t *TDigest) Encode() []byte {
	t.process()
	n := len(t.centroids)
	buf := make([]byte, 0, 3+8*4+4+n*16)
	buf = append(buf, digestMagic0, digestMagic1, digestVersion)
	var tmp [8]byte
	putF := func(f float64) {
		binary.LittleEndian.PutUint64(tmp[:], math.Float64bits(f))
		buf = append(buf, tmp[:]...)
	}
	putF(t.compression)
	putF(t.min)
	putF(t.max)
	putF(t.count)
	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], uint32(n))
	buf = append(buf, u32[:]...)
	for _, c := range t.centroids {
		putF(c.mean)
		putF(c.weight)
	}
	return buf
}

// Decode 从二进制还原 digest；nil/空 blob 返回空 digest（调用方可统一处理
// “未保存 sketch”）；非法数据返回错误。
func Decode(b []byte) (*TDigest, error) {
	if len(b) == 0 {
		return New(DefaultCompression), nil
	}
	if len(b) < 3+8*4+4 || b[0] != digestMagic0 || b[1] != digestMagic1 {
		return nil, errors.New("tdigest: invalid blob")
	}
	if b[2] != digestVersion {
		return nil, errors.New("tdigest: unsupported version")
	}
	off := 3
	getF := func() float64 {
		v := math.Float64frombits(binary.LittleEndian.Uint64(b[off : off+8]))
		off += 8
		return v
	}
	t := &TDigest{processed: true}
	t.compression = getF()
	t.min = getF()
	t.max = getF()
	t.count = getF()
	n := int(binary.LittleEndian.Uint32(b[off : off+4]))
	off += 4
	if n < 0 || n > 1<<20 || off+n*16 > len(b) {
		return nil, errors.New("tdigest: bad centroid count")
	}
	t.centroids = make([]centroid, 0, n)
	for i := 0; i < n; i++ {
		t.centroids = append(t.centroids, centroid{mean: getF(), weight: getF()})
	}
	return t, nil
}
