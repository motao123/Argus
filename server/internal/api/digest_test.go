package api

import (
	"testing"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/tdigest"
)

// TestAggregateMetricsPercentiles 验证查询层：无 digest 的历史数据不输出分位数，
// 有 digest 时合并后输出 cpu_p50/p95/p99。
func TestAggregateMetricsPercentiles(t *testing.T) {
	// 历史数据：无 digest
	points := aggregateMetrics([]model.Metric{
		{ServerID: 1, TS: 100, Granularity: 60, CPU: 10},
		{ServerID: 1, TS: 160, Granularity: 60, CPU: 20},
	}, 60)
	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(points))
	}
	if _, ok := points[0]["cpu_p95"]; ok {
		t.Fatal("historical row without digest should not emit cpu_p95")
	}
	// 带 digest：合并后输出分位数
	d := tdigest.New(0)
	for i := 0; i < 100; i++ {
		d.Add(float64(i))
	}
	withDigest := []model.Metric{
		{ServerID: 1, TS: 100, Granularity: 60, CPU: 50, Samples: 100, Digest: d.Encode()},
		{ServerID: 1, TS: 100, Granularity: 60, CPU: 50, Samples: 100, Digest: d.Encode()},
	}
	points = aggregateMetrics(withDigest, 60)
	if len(points) != 1 {
		t.Fatalf("expected 1 merged point, got %d", len(points))
	}
	p95, ok := points[0]["cpu_p95"].(float64)
	if !ok || p95 < 92 || p95 > 98 {
		t.Errorf("merged cpu_p95 = %v, want ~95", points[0]["cpu_p95"])
	}
	if _, ok := points[0]["cpu_p50"]; !ok {
		t.Error("cpu_p50 missing")
	}
	if _, ok := points[0]["cpu_p99"]; !ok {
		t.Error("cpu_p99 missing")
	}
}
