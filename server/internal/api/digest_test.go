package api

import (
	"testing"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/tdigest"
)

// TestAggregateMetricsPercentiles 验证查询层：无 digest 的历史数据不输出分位数，
// 有 digest 时合并后输出 cpu_p50/p95/p99。
func TestAggregateMetricsExtendedFieldsUseLatestSnapshot(t *testing.T) {
	points := aggregateMetrics([]model.Metric{
		{ID: 2, TS: 160, CPU: 30, Load5: 6, Load15: 12, LatencyMs: 40, SwapUsed: 20, SwapTotal: 100, Uptime: 200, NetInTransfer: 1200, NetOutTransfer: 2400, GPUMemUsed: 512, GPUMemTotal: 1024, GPUDevices: `[{"name":"new"}]`},
		{ID: 1, TS: 100, CPU: 10, Load5: 2, Load15: 4, LatencyMs: 0, SwapUsed: 10, SwapTotal: 100, Uptime: 100, NetInTransfer: 1000, NetOutTransfer: 2000, GPUMemUsed: 256, GPUMemTotal: 1024, GPUDevices: `[{"name":"old"}]`},
	}, 300)
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}
	point := points[0]
	if point["load5"] != float64(4) || point["load15"] != float64(8) || point["latency_ms"] != float64(40) {
		t.Fatalf("averaged historical values wrong: %+v", point)
	}
	if point["swap_used"] != uint64(20) || point["uptime"] != uint64(200) || point["net_in_transfer"] != uint64(1200) || point["net_out_transfer"] != uint64(2400) {
		t.Fatalf("latest counters wrong: %+v", point)
	}
	if point["gpu_mem_used"] != uint64(512) || point["gpu_devices"] != `[{"name":"new"}]` {
		t.Fatalf("latest GPU snapshot wrong: %+v", point)
	}
}

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
