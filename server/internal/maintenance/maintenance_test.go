package maintenance

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
	// :memory: 库每个连接独立，单连接保证跨调用可见。
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(&model.MaintenanceWindow{}); err != nil {
		t.Fatal(err)
	}
	return gdb
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestActiveAtOneOff(t *testing.T) {
	w := &model.MaintenanceWindow{
		StartAt: mustTime("2026-08-10T02:00:00Z"),
		EndAt:   mustTime("2026-08-10T06:00:00Z"),
	}
	cases := []struct {
		at   string
		want bool
	}{
		{"2026-08-10T01:59:59Z", false},
		{"2026-08-10T02:00:00Z", true}, // 起始包含
		{"2026-08-10T05:59:59Z", true},
		{"2026-08-10T06:00:00Z", false}, // 结束不包含
		{"2026-08-11T03:00:00Z", false},
	}
	for _, c := range cases {
		if got := activeAt(w, mustTime(c.at)); got != c.want {
			t.Errorf("activeAt(%s) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestActiveAtRecurring(t *testing.T) {
	// 每周六 22:00 → 周日 02:00（跨午夜）。2026-08-15 是周六。
	w := &model.MaintenanceWindow{
		StartAt:   mustTime("2026-08-15T22:00:00Z"),
		EndAt:     mustTime("2026-08-16T02:00:00Z"),
		Recurring: true,
	}
	cases := []struct {
		at   string
		want bool
	}{
		{"2026-08-15T21:59:59Z", false}, // 窗口前
		{"2026-08-15T22:30:00Z", true},  // 周六晚
		{"2026-08-16T01:59:59Z", true},  // 跨午夜
		{"2026-08-16T02:00:00Z", false}, // 窗口后
		{"2026-08-22T23:00:00Z", true},  // 下周六（重复）
		{"2026-08-23T01:00:00Z", true},  // 下周日凌晨
		{"2026-08-17T12:00:00Z", false}, // 周一不生效
	}
	for _, c := range cases {
		if got := activeAt(w, mustTime(c.at)); got != c.want {
			t.Errorf("recurring activeAt(%s) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestOverlapMinutes(t *testing.T) {
	from := mustTime("2026-08-01T00:00:00Z")
	to := mustTime("2026-08-02T00:00:00Z")

	// 一次性窗口：部分重叠（01:00-01:10 → 10 分钟）
	w := &model.MaintenanceWindow{
		StartAt: mustTime("2026-08-01T01:00:00Z"),
		EndAt:   mustTime("2026-08-01T01:10:00Z"),
	}
	if got := overlapMinutes(w, from, to); got != 10 {
		t.Errorf("one-off overlap = %d, want 10", got)
	}

	// 完全不重叠
	w2 := &model.MaintenanceWindow{
		StartAt: mustTime("2026-08-02T01:00:00Z"),
		EndAt:   mustTime("2026-08-02T02:00:00Z"),
	}
	if got := overlapMinutes(w2, from, to); got != 0 {
		t.Errorf("disjoint overlap = %d, want 0", got)
	}

	// 重复窗口：每周一 00:00-02:00，8 月内 5 个周一（3/10/17/24/31）各 60 分钟
	// 窗口区间取 8-01 至 8-02（周一），共 60 分钟重叠。
	w3 := &model.MaintenanceWindow{
		StartAt:   mustTime("2026-08-01T00:00:00Z"), // 周六
		EndAt:     mustTime("2026-08-01T02:00:00Z"),
		Recurring: true,
	}
	// 3 号是周一：重复窗口按周六 00:00-02:00，8 月 1/8/15/22/29 五次，
	// 与 [8-01, 8-02) 只重叠 8-01 一次共 120 分钟。
	if got := overlapMinutes(w3, from, to); got != 120 {
		t.Errorf("recurring overlap = %d, want 120", got)
	}
}

func TestActiveServerIDs(t *testing.T) {
	gdb := newTestDB(t)
	sat := mustTime("2026-08-15T22:00:00Z")
	now := mustTime("2026-08-15T23:00:00Z")
	if err := gdb.Create(&model.MaintenanceWindow{
		Title: "svc-a", ServerIDs: "1,2",
		StartAt: sat, EndAt: mustTime("2026-08-16T02:00:00Z"), Recurring: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.MaintenanceWindow{
		Title: "svc-b", ServerIDs: "3",
		StartAt: mustTime("2026-08-15T20:00:00Z"), EndAt: mustTime("2026-08-15T22:30:00Z"),
	}).Error; err != nil {
		t.Fatal(err)
	}
	ids, coversAll, err := ActiveServerIDs(gdb, now)
	if err != nil {
		t.Fatal(err)
	}
	if coversAll {
		t.Fatal("coversAll should be false")
	}
	if len(ids) != 2 || !ids[1] || !ids[2] {
		t.Errorf("ids = %v, want {1,2}", ids)
	}

	// 覆盖全部服务器的窗口
	if err := gdb.Create(&model.MaintenanceWindow{
		Title: "global", ServerIDs: "",
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, coversAll, err = ActiveServerIDs(gdb, now)
	if err != nil {
		t.Fatal(err)
	}
	if !coversAll {
		t.Error("coversAll should be true")
	}
}

func TestCoveredMinutesWithDB(t *testing.T) {
	gdb := newTestDB(t)
	from := mustTime("2026-08-01T00:00:00Z")
	to := mustTime("2026-08-03T00:00:00Z")

	// 覆盖 server 1 的一次性窗口：8-01 02:00-02:30 → 30 分钟
	if err := gdb.Create(&model.MaintenanceWindow{
		Title: "a", ServerIDs: "1",
		StartAt: mustTime("2026-08-01T02:00:00Z"), EndAt: mustTime("2026-08-01T02:30:00Z"),
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 覆盖全部服务器的重复窗口：每天? 用周六 00:00-01:00 → 8-01 重叠 60 分钟
	if err := gdb.Create(&model.MaintenanceWindow{
		Title: "b", ServerIDs: "",
		StartAt: mustTime("2026-08-01T00:00:00Z"), EndAt: mustTime("2026-08-01T01:00:00Z"),
		Recurring: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 仅覆盖 server 2 的窗口
	if err := gdb.Create(&model.MaintenanceWindow{
		Title: "c", ServerIDs: "2",
		StartAt: mustTime("2026-08-02T10:00:00Z"), EndAt: mustTime("2026-08-02T11:00:00Z"),
	}).Error; err != nil {
		t.Fatal(err)
	}

	mins, err := CoveredMinutes(gdb, 1, from, to)
	if err != nil {
		t.Fatal(err)
	}
	// server 1: 窗口 a 30 分钟 + 窗口 b 60 分钟 = 90
	if mins != 90 {
		t.Errorf("server1 covered = %d, want 90", mins)
	}

	mins, err = CoveredMinutes(gdb, 2, from, to)
	if err != nil {
		t.Fatal(err)
	}
	// server 2: 窗口 b 60 分钟 + 窗口 c 60 分钟 = 120
	if mins != 120 {
		t.Errorf("server2 covered = %d, want 120", mins)
	}

	mins, err = CoveredMinutes(gdb, 3, from, to)
	if err != nil {
		t.Fatal(err)
	}
	// server 3（无专属窗口）: 仅全局窗口 b = 60
	if mins != 60 {
		t.Errorf("server3 covered = %d, want 60", mins)
	}
}

func TestIsActive(t *testing.T) {
	gdb := newTestDB(t)
	now := mustTime("2026-08-15T12:00:00Z")
	if err := gdb.Create(&model.MaintenanceWindow{
		Title: "x", ServerIDs: "1",
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	active, err := IsActive(gdb, 1, now)
	if err != nil || !active {
		t.Errorf("IsActive(1) = %v, %v; want true", active, err)
	}
	active, err = IsActive(gdb, 2, now)
	if err != nil || active {
		t.Errorf("IsActive(2) = %v, %v; want false", active, err)
	}
}
