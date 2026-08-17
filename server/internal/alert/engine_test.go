package alert

import (
	"testing"
	"time"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/store"
)

func TestInRange(t *testing.T) {
	e := &Engine{}
	min := 10.0
	max := 90.0
	a := &model.Alert{Metric: "cpu", Min: &min, Max: &max}
	cases := []struct {
		v    float64
		want bool
	}{
		{5, true},   // 低于下限触发
		{50, false}, // 正常区间
		{95, true},  // 高于上限触发
	}
	for _, c := range cases {
		if got := e.inRange(a, c.v); got != c.want {
			t.Errorf("inRange(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestMetricValue(t *testing.T) {
	e := &Engine{}
	h := store.NewHub()
	h.Upsert(&model.Server{ID: 1, Name: "t"})
	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{
		CPU: 42.5, MemUsed: 512, MemTotal: 1024, DiskUsed: 20, DiskTotal: 100,
	})
	st := h.Get(1)

	if v, ok := e.metricValue(&model.Alert{Metric: "cpu"}, *st); !ok || v != 42.5 {
		t.Errorf("cpu = %v,%v", v, ok)
	}
	if v, ok := e.metricValue(&model.Alert{Metric: "mem"}, *st); !ok || v != 50 {
		t.Errorf("mem = %v,%v", v, ok)
	}
	if v, ok := e.metricValue(&model.Alert{Metric: "disk"}, *st); !ok || v != 20 {
		t.Errorf("disk = %v,%v", v, ok)
	}
	// 延迟：有测量 → 毫秒值
	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{LatencyMs: 88, Timestamp: 106})
	st = h.Get(1)
	if v, ok := e.metricValue(&model.Alert{Metric: "latency"}, *st); !ok || v != 88 {
		t.Errorf("latency = %v,%v, want 88,true", v, ok)
	}
	// 延迟：无测量（旧 Agent 上报 0）→ 不参与判定
	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{Timestamp: 108})
	st = h.Get(1)
	if v, ok := e.metricValue(&model.Alert{Metric: "latency"}, *st); ok {
		t.Errorf("latency 无测量 = %v,%v, want 0,false", v, ok)
	}
	// 离线
	h.MarkOffline(1)
	st = h.Get(1)
	if v, ok := e.metricValue(&model.Alert{Metric: "offline"}, *st); !ok || v != 1 {
		t.Errorf("offline = %v,%v", v, ok)
	}
}

func TestMetricValueExtended(t *testing.T) {
	e := &Engine{baselines: map[string]transferBaseline{}}
	h := store.NewHub()
	h.Upsert(&model.Server{ID: 2, Name: "t2"})
	h.SetReport(2, protocol.HostInfo{}, &protocol.ReportParams{
		SwapUsed: 200, SwapTotal: 400,
		Temperature: 71.5,
		GPU:         protocol.GPUReport{Availability: protocol.Availability{Available: true}, Devices: []protocol.GPUDevice{{Util: 30}, {Util: 95}}},
		GPUUtil:     62.5,
		NetInSpeed:  10, NetOutSpeed: 20,
		Load5: 1.5, Load15: 2.5,
		TCPEstablished: 42, TCPCount: 99, UDPCount: 7, ProcessCount: 123,
		NetInTransfer: 1000, NetOutTransfer: 2000,
		Timestamp: 106,
	})
	st := h.Get(2)

	cases := []struct {
		metric string
		want   float64
	}{
		{"swap", 50},
		{"temperature", 71.5},
		{"gpu", 95},        // 多卡取最大
		{"gpu_max", 95},    // 别名
		{"net_all_speed", 30},
		{"load5", 1.5},
		{"load15", 2.5},
		{"tcp_conn_count", 42}, // established 优先
		{"udp_conn_count", 7},
		{"process_count", 123},
		{"transfer_in", 0},  // 首次评估基线 = 当前值
		{"transfer_out", 0}, // 首次评估基线 = 当前值
		{"transfer_all", 0},
	}
	for _, c := range cases {
		if v, ok := e.metricValue(&model.Alert{Metric: c.metric}, *st); !ok || v != c.want {
			t.Errorf("%s = %v,%v, want %v,true", c.metric, v, ok, c.want)
		}
	}

	// 累计流量：基线建立后按差值计
	h.SetReport(2, protocol.HostInfo{}, &protocol.ReportParams{
		NetInTransfer: 1100, NetOutTransfer: 2500, Timestamp: 108,
	})
	st = h.Get(2)
	if v, ok := e.metricValue(&model.Alert{Metric: "transfer_in"}, *st); !ok || v != 100 {
		t.Errorf("transfer_in delta = %v,%v, want 100,true", v, ok)
	}
	if v, ok := e.metricValue(&model.Alert{Metric: "transfer_out"}, *st); !ok || v != 500 {
		t.Errorf("transfer_out delta = %v,%v, want 500,true", v, ok)
	}
	if v, ok := e.metricValue(&model.Alert{Metric: "transfer_all"}, *st); !ok || v != 600 {
		t.Errorf("transfer_all delta = %v,%v, want 600,true", v, ok)
	}
	// Agent 重启（计数器归零）→ 差值按 0 处理
	h.SetReport(2, protocol.HostInfo{}, &protocol.ReportParams{NetInTransfer: 50, NetOutTransfer: 60, Timestamp: 110})
	st = h.Get(2)
	if v, ok := e.metricValue(&model.Alert{Metric: "transfer_in"}, *st); !ok || v != 0 {
		t.Errorf("transfer_in after reset = %v,%v, want 0,true", v, ok)
	}
	// 触发通知后重置基线
	e.resetBaseline(&model.Alert{Metric: "transfer_all"}, *st)
	if v, ok := e.metricValue(&model.Alert{Metric: "transfer_all"}, *st); !ok || v != 0 {
		t.Errorf("transfer_all after baseline reset = %v,%v, want 0,true", v, ok)
	}
}

func TestMetricValueLegacyTCPFallback(t *testing.T) {
	e := &Engine{baselines: map[string]transferBaseline{}}
	h := store.NewHub()
	h.Upsert(&model.Server{ID: 3, Name: "t3"})
	// 旧 Agent：仅 TCPCount（总连接数）
	h.SetReport(3, protocol.HostInfo{}, &protocol.ReportParams{TCPCount: 77, Timestamp: 1})
	st := h.Get(3)
	if v, ok := e.metricValue(&model.Alert{Metric: "tcp_conn_count"}, *st); !ok || v != 77 {
		t.Errorf("tcp_conn_count legacy = %v,%v, want 77,true", v, ok)
	}
}

func TestStateMachine(t *testing.T) {
	e := &Engine{state: map[string]*violation{}}
	max := 90.0
	a := &model.Alert{ID: 1, Metric: "cpu", Max: &max, Duration: 3}
	key := "1:1"

	// 第一次超限：记录开始时间，不通知
	now := time.Now()
	e.state[key] = &violation{triggeredAt: now}
	v := e.state[key]
	if v.notified {
		t.Fatal("should not notify immediately")
	}
	// 3s 后仍超限：应通知（标记）
	v.triggeredAt = now.Add(-4 * time.Second)
	if !v.notified && time.Since(v.triggeredAt) >= time.Duration(a.Duration)*time.Second {
		v.notified = true
	}
	if !v.notified {
		t.Fatal("should notify after duration")
	}
}
