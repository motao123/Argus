package sla

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(&model.Metric{}, &model.MaintenanceWindow{}); err != nil {
		t.Fatal(err)
	}
	return gdb
}

// seedMetrics 写入 server 在 [from, to) 内每个整分钟一条指标行。
func seedMetrics(t *testing.T, gdb *gorm.DB, serverID int64, from, to time.Time) {
	t.Helper()
	var rows []model.Metric
	for ts := from.Truncate(time.Minute); ts.Before(to); ts = ts.Add(time.Minute) {
		rows = append(rows, model.Metric{ServerID: serverID, TS: ts.Unix(), Granularity: 60, CPU: 1})
	}
	if err := gdb.CreateInBatches(rows, 200).Error; err != nil {
		t.Fatal(err)
	}
}

func TestComputeMonth(t *testing.T) {
	gdb := newTestDB(t)
	// 2026-07 全月指标（31 天 = 44640 分钟）
	month := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 31, 23, 59, 0, 0, time.UTC)
	seedMetrics(t, gdb, 1, month, now)

	m := ComputeMonth(gdb, 1, time.Time{}, month, now)
	if m.EligibleMinutes != 44639 {
		t.Fatalf("eligible = %d, want 44639", m.EligibleMinutes)
	}
	if m.UptimeMinutes != 44639 {
		t.Fatalf("uptime = %d, want 44639", m.UptimeMinutes)
	}
	if m.Availability == nil || *m.Availability != 100 {
		t.Fatalf("availability = %v, want 100", m.Availability)
	}

	// 维护排除：8 月前 3 天全维护 → 分母与分子同时扣除
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	nowAug := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	seedMetrics(t, gdb, 1, aug, nowAug)
	win := &model.MaintenanceWindow{
		Title: "mv", ServerIDs: "1",
		StartAt: aug, EndAt: aug.Add(72 * time.Hour), // 8-01 ~ 8-04（72h）
	}
	if err := gdb.Create(win).Error; err != nil {
		t.Fatal(err)
	}
	m2 := ComputeMonth(gdb, 1, time.Time{}, aug, nowAug)
	// 8 月 1-9 日共 9*1440 = 12960 分钟；维护 72h = 4320 分钟（含 8-04 前 3 小时?）
	// 维护 8-01 00:00 → 8-04 00:00 = 72h = 4320 分钟。
	if m2.MaintenanceMinutes != 4320 {
		t.Fatalf("maintenance = %d, want 4320", m2.MaintenanceMinutes)
	}
	if m2.EligibleMinutes != 12960-4320 {
		t.Fatalf("eligible = %d, want %d", m2.EligibleMinutes, 12960-4320)
	}
	if m2.UptimeMinutes != m2.EligibleMinutes {
		t.Fatalf("uptime = %d, want %d (全部数据分钟都应计入)", m2.UptimeMinutes, m2.EligibleMinutes)
	}
	if m2.Availability == nil || *m2.Availability != 100 {
		t.Fatalf("availability = %v, want 100", m2.Availability)
	}

	// 缺口：8-05 全天无数据 → 可用率 = 8640/12960-4320-1440...
	// 8-01~8-09 数据分钟 = 12960 - 1440(8-05) = 11520
	// 剔除维护 4320（全部在 8-01~8-03 有数据区间内）→ uptime 11520-4320=7200, eligible 8640
	if err := gdb.Where("ts >= ? AND ts < ?", time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC).Unix(), time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC).Unix()).
		Delete(&model.Metric{}, "server_id = ?", 1).Error; err != nil {
		t.Fatal(err)
	}
	m3 := ComputeMonth(gdb, 1, time.Time{}, aug, nowAug)
	if m3.EligibleMinutes != 8640 {
		t.Fatalf("eligible = %d, want 8640", m3.EligibleMinutes)
	}
	if m3.UptimeMinutes != 7200 {
		t.Fatalf("uptime = %d, want 7200", m3.UptimeMinutes)
	}
	want := round2(7200.0 / 8640.0 * 100)
	if m3.Availability == nil || *m3.Availability != want {
		t.Fatalf("availability = %v, want %v", m3.Availability, want)
	}
}

func TestComputeMonthNoEligible(t *testing.T) {
	gdb := newTestDB(t)
	month := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	// 全月维护 → 无计入分钟
	if err := gdb.Create(&model.MaintenanceWindow{
		Title: "all", ServerIDs: "1",
		StartAt: month, EndAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	m := ComputeMonth(gdb, 1, time.Time{}, month, now)
	if m.EligibleMinutes != 0 {
		t.Fatalf("eligible = %d, want 0", m.EligibleMinutes)
	}
	if m.Availability != nil {
		t.Fatalf("availability should be nil, got %v", *m.Availability)
	}
}

func TestServerCreatedMidMonth(t *testing.T) {
	gdb := newTestDB(t)
	month := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC) // 月中创建
	now := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	seedMetrics(t, gdb, 7, created, now)

	m := ComputeMonth(gdb, 7, created, month, now)
	wantEligible := int64(now.Sub(created) / time.Minute) // 4 天
	if m.EligibleMinutes != wantEligible {
		t.Fatalf("eligible = %d, want %d", m.EligibleMinutes, wantEligible)
	}
	if m.Availability == nil || *m.Availability != 100 {
		t.Fatalf("availability = %v, want 100", m.Availability)
	}
}

func TestApplySLO(t *testing.T) {
	// 99% < 99.9% → 不达标
	m := Month{Month: "2026-07", EligibleMinutes: 100, UptimeMinutes: 99}
	avail := round2(99.0)
	m.Availability = &avail
	ApplySLO(&m, 99.9)
	if m.SloMet == nil || *m.SloMet {
		t.Fatalf("slo_met should be false (99 < 99.9)")
	}
	// 99.95% >= 99.9% → 达标
	m2 := Month{EligibleMinutes: 10000, UptimeMinutes: 9995}
	avail2 := round2(99.95)
	m2.Availability = &avail2
	ApplySLO(&m2, 99.9)
	if m2.SloMet == nil || !*m2.SloMet {
		t.Fatalf("slo_met should be true (99.95 >= 99.9)")
	}
	// 目标 0 = 未启用 → slo_met 为 nil
	m3 := Month{EligibleMinutes: 100, UptimeMinutes: 100}
	avail3 := 100.0
	m3.Availability = &avail3
	ApplySLO(&m3, 0)
	if m3.SloMet != nil {
		t.Fatal("slo_met should be nil when SLO disabled")
	}
}

func TestSeries(t *testing.T) {
	gdb := newTestDB(t)
	// 当前 2026-08：近 3 个月 = 6/7/8 月
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	seedMetrics(t, gdb, 1, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))
	series := Series(gdb, 1, time.Time{}, now, 3)
	if len(series) != 3 {
		t.Fatalf("len = %d, want 3", len(series))
	}
	if series[0].Month != "2026-06" || series[1].Month != "2026-07" || series[2].Month != "2026-08" {
		t.Fatalf("months = %s,%s,%s", series[0].Month, series[1].Month, series[2].Month)
	}
	for _, m := range series {
		if m.Availability == nil || *m.Availability != 100 {
			t.Fatalf("month %s availability = %v, want 100", m.Month, m.Availability)
		}
	}
}
