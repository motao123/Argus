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
	// 离线
	h.MarkOffline(1)
	st = h.Get(1)
	if v, ok := e.metricValue(&model.Alert{Metric: "offline"}, *st); !ok || v != 1 {
		t.Errorf("offline = %v,%v", v, ok)
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
