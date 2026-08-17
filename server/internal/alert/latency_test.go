package alert

import (
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/store"
)

// TestLatencyAlertTriggers 验证 latency 指标走现有引擎（min/max + duration）：
// 上报延迟持续超过阈值后触发通知状态。
func TestLatencyAlertTriggers(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(
		&model.Server{}, &model.Alert{}, &model.AlertState{}, &model.Notification{},
		&model.MaintenanceWindow{},
	); err != nil {
		t.Fatal(err)
	}
	srv := &model.Server{Name: "node-1", Secret: "x"}
	if err := gdb.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	max := 50.0
	rule := &model.Alert{Name: "高延迟", Metric: "latency", Max: &max, Duration: 0, Enabled: true}
	if err := gdb.Create(rule).Error; err != nil {
		t.Fatal(err)
	}

	st := store.NewHub()
	st.Upsert(srv)
	st.SetReport(srv.ID, protocol.HostInfo{}, &protocol.ReportParams{LatencyMs: 120, Timestamp: 100})

	e := NewEngine(gdb, st)
	key := fmt.Sprintf("%d:%d", rule.ID, srv.ID)
	e.checkOnce() // 第一轮：记录触发开始时间
	e.checkOnce() // 第二轮：持续达 duration(0) → 标记已通知

	e.mu.Lock()
	v := e.state[key]
	e.mu.Unlock()
	if v == nil || !v.notified {
		t.Fatalf("latency 超阈值后应触发通知，state = %+v", v)
	}

	// 延迟回落至阈值内 → 恢复（清除状态）
	st.SetReport(srv.ID, protocol.HostInfo{}, &protocol.ReportParams{LatencyMs: 10, Timestamp: 200})
	e.checkOnce()
	e.mu.Lock()
	v = e.state[key]
	e.mu.Unlock()
	if v == nil || !v.recovering {
		t.Fatalf("latency 回落应进入恢复，state = %+v", v)
	}
}

// TestLatencyAlertNoDataSkips 验证无测量（旧 Agent 上报 0）不参与判定：
// 不会因 0 值误触发低于下限的规则。
func TestLatencyAlertNoDataSkips(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(
		&model.Server{}, &model.Alert{}, &model.AlertState{}, &model.Notification{},
		&model.MaintenanceWindow{},
	); err != nil {
		t.Fatal(err)
	}
	srv := &model.Server{Name: "node-1", Secret: "x"}
	if err := gdb.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	min := 100.0 // 低于下限触发：若把"无数据 0"当 0ms 会误触发
	rule := &model.Alert{Name: "延迟过低", Metric: "latency", Min: &min, Duration: 0, Enabled: true}
	if err := gdb.Create(rule).Error; err != nil {
		t.Fatal(err)
	}

	st := store.NewHub()
	st.Upsert(srv)
	st.SetReport(srv.ID, protocol.HostInfo{}, &protocol.ReportParams{Timestamp: 100}) // 旧 Agent：无延迟

	e := NewEngine(gdb, st)
	e.checkOnce()
	e.mu.Lock()
	defer e.mu.Unlock()
	for k := range e.state {
		t.Fatalf("无测量不应产生任何触发状态, key=%s", k)
	}
}
