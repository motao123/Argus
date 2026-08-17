package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifier"
)

// newReportsEnv 报告/到期提醒专用测试环境（单连接内存库）。
func newReportsEnv(t *testing.T) *Server {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(
		&model.Server{}, &model.Transfer{}, &model.TrafficReport{},
		&model.Notification{}, &model.Setting{}, &model.NotificationDelivery{},
	); err != nil {
		t.Fatal(err)
	}
	return &Server{DB: gdb, Notifier: notifier.NewQueue(gdb)}
}

func reportUnix(t *testing.T, value string) int64 {
	t.Helper()
	v, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return v.Unix()
}

// ---- 计划触发 ----

func TestTrafficReportDue(t *testing.T) {
	mon0930 := time.Date(2024, 3, 11, 9, 30, 0, 0, time.UTC) // 周一 09:30

	// daily：任意日期，仅 hour 命中
	cfg := &model.TrafficReport{Period: ReportPeriodDaily, Hour: 9, Enabled: true}
	if !trafficReportDue(cfg, mon0930) {
		t.Error("daily should be due at 09:30")
	}
	if trafficReportDue(cfg, mon0930.Add(time.Hour)) {
		t.Error("daily should not be due at 10:30")
	}
	cfg.Enabled = false
	if trafficReportDue(cfg, mon0930) {
		t.Error("disabled report should never be due")
	}
	cfg.Enabled = true
	cfg.Period = "" // 旧配置无 period → 按 daily 处理
	if !trafficReportDue(cfg, mon0930) {
		t.Error("empty period should default to daily")
	}

	// weekly：weekday+hour
	cfg = &model.TrafficReport{Period: ReportPeriodWeekly, Hour: 9, Weekday: 1, Enabled: true}
	if !trafficReportDue(cfg, mon0930) {
		t.Error("weekly Monday 09:30 should be due")
	}
	if trafficReportDue(cfg, time.Date(2024, 3, 12, 9, 30, 0, 0, time.UTC)) {
		t.Error("weekly Tuesday 09:30 should not be due")
	}
	cfg.Weekday = 0 // 周日
	if !trafficReportDue(cfg, time.Date(2024, 3, 10, 9, 30, 0, 0, time.UTC)) {
		t.Error("weekly Sunday 09:30 should be due")
	}

	// monthly：day+hour
	cfg = &model.TrafficReport{Period: ReportPeriodMonthly, Hour: 9, Day: 15, Enabled: true}
	if !trafficReportDue(cfg, time.Date(2024, 3, 15, 9, 30, 0, 0, time.UTC)) {
		t.Error("monthly day 15 09:30 should be due")
	}
	if trafficReportDue(cfg, time.Date(2024, 3, 14, 9, 30, 0, 0, time.UTC)) {
		t.Error("monthly day 14 should not be due")
	}
	if trafficReportDue(cfg, time.Date(2024, 3, 15, 8, 30, 0, 0, time.UTC)) {
		t.Error("monthly wrong hour should not be due")
	}
}

func TestValidateTrafficReport(t *testing.T) {
	valid := []struct {
		period             string
		hour, weekday, day int
	}{
		{ReportPeriodDaily, 9, 0, 0},
		{ReportPeriodWeekly, 0, 0, 0},   // 周日 0 点合法
		{ReportPeriodWeekly, 23, 6, 1},  // 周六 23 点合法
		{ReportPeriodMonthly, 9, 0, 1},  // 每月 1 号
		{ReportPeriodMonthly, 9, 0, 28}, // 每月 28 号（上限）
	}
	for _, v := range valid {
		if err := validateTrafficReport(v.period, v.hour, v.weekday, v.day); err != nil {
			t.Errorf("validate(%+v) unexpected error: %v", v, err)
		}
	}
	invalid := []struct {
		period             string
		hour, weekday, day int
	}{
		{"fortnightly", 9, 0, 0},
		{ReportPeriodDaily, 24, 0, 0},
		{ReportPeriodDaily, -1, 0, 0},
		{ReportPeriodWeekly, 9, 7, 0},
		{ReportPeriodWeekly, 9, -1, 0},
		{ReportPeriodMonthly, 9, 0, 0},
		{ReportPeriodMonthly, 9, 0, 29},
	}
	for _, v := range invalid {
		if err := validateTrafficReport(v.period, v.hour, v.weekday, v.day); err == nil {
			t.Errorf("validate(%+v) should fail", v)
		}
	}
}

// ---- 周/月汇总 ----

func TestBuildTrafficReportWeekly(t *testing.T) {
	s := newReportsEnv(t)
	now := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC) // 周五
	// 本周窗口：[2024-03-08T10:00, 2024-03-15T10:00)
	sumSrv := model.Server{Name: "sum-srv", Secret: "sec-sum-srv", TrafficAccounting: "sum"}
	maxSrv := model.Server{Name: "max-srv", Secret: "sec-max-srv", TrafficAccounting: "max"}
	if err := s.DB.Create(&sumSrv).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&maxSrv).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Transfer{
		{ServerID: sumSrv.ID, Ts: reportUnix(t, "2024-03-09T00:00:00Z"), In: 100, Out: 50},
		{ServerID: sumSrv.ID, Ts: reportUnix(t, "2024-03-14T23:00:00Z"), In: 10, Out: 20},
		{ServerID: sumSrv.ID, Ts: reportUnix(t, "2024-03-08T09:00:00Z"), In: 999, Out: 999}, // 窗口起点之前
		{ServerID: sumSrv.ID, Ts: reportUnix(t, "2024-03-15T10:30:00Z"), In: 999, Out: 999}, // now 之后
		{ServerID: maxSrv.ID, Ts: reportUnix(t, "2024-03-10T00:00:00Z"), In: 30, Out: 70},
	}
	if err := s.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	title, content, err := buildTrafficReport(s.DB, ReportPeriodWeekly, now)
	if err != nil {
		t.Fatal(err)
	}
	if title != "[Argus] 流量周报" {
		t.Fatalf("title=%q", title)
	}
	if !strings.HasPrefix(content, "本周流量报告\n") {
		t.Fatalf("content=%q", content)
	}
	for _, want := range []string{"sum-srv: ↓110 B ↑70 B · 计费 180 B", "max-srv: ↓30 B ↑70 B · 计费 70 B"} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q: %q", want, content)
		}
	}
	if strings.Contains(content, "999") {
		t.Errorf("weekly window leaked out-of-window rows: %q", content)
	}
}

func TestBuildTrafficReportMonthly(t *testing.T) {
	s := newReportsEnv(t)
	now := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	// 本月窗口：[2024-03-01T00:00, 2024-03-15T10:00)
	sv := model.Server{Name: "month-srv", Secret: "sec-month-srv", TrafficAccounting: "in"}
	if err := s.DB.Create(&sv).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Transfer{
		{ServerID: sv.ID, Ts: reportUnix(t, "2024-03-02T00:00:00Z"), In: 100, Out: 50},
		{ServerID: sv.ID, Ts: reportUnix(t, "2024-03-14T23:00:00Z"), In: 10, Out: 20},
		{ServerID: sv.ID, Ts: reportUnix(t, "2024-02-29T23:00:00Z"), In: 999, Out: 999}, // 上月末（月窗口外）
		{ServerID: sv.ID, Ts: reportUnix(t, "2024-03-15T11:00:00Z"), In: 999, Out: 999}, // now 之后
	}
	if err := s.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	title, content, err := buildTrafficReport(s.DB, ReportPeriodMonthly, now)
	if err != nil {
		t.Fatal(err)
	}
	if title != "[Argus] 流量月报" {
		t.Fatalf("title=%q", title)
	}
	if !strings.HasPrefix(content, "本月流量报告\n") {
		t.Fatalf("content=%q", content)
	}
	// accounting=in → accounted 只看入向
	if !strings.Contains(content, "month-srv: ↓110 B ↑70 B · 计费 110 B") {
		t.Errorf("content=%q", content)
	}
	if strings.Contains(content, "999") {
		t.Errorf("monthly window leaked out-of-window rows: %q", content)
	}
}

func TestBuildTrafficReportDailyDefault(t *testing.T) {
	s := newReportsEnv(t)
	now := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	sv := model.Server{Name: "day-srv", Secret: "sec-day-srv"}
	if err := s.DB.Create(&sv).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Transfer{
		{ServerID: sv.ID, Ts: reportUnix(t, "2024-03-15T01:00:00Z"), In: 100, Out: 50},
		{ServerID: sv.ID, Ts: reportUnix(t, "2024-03-14T23:00:00Z"), In: 999, Out: 999}, // 昨日（日窗口外）
	}
	if err := s.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	title, content, err := buildTrafficReport(s.DB, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if title != "[Argus] 流量日报" {
		t.Fatalf("title=%q", title)
	}
	if !strings.HasPrefix(content, "今日流量报告\n") {
		t.Fatalf("content=%q", content)
	}
	if !strings.Contains(content, "day-srv: ↓100 B ↑50 B · 计费 150 B") {
		t.Errorf("content=%q", content)
	}
	if strings.Contains(content, "999") {
		t.Errorf("daily window leaked yesterday rows: %q", content)
	}
}

// ---- 到期提醒天数 ----

func TestExpireNotifyDaysSetting(t *testing.T) {
	s := newReportsEnv(t)
	hook := model.Notification{Name: "hook", Type: "webhook", URL: "http://127.0.0.1:1/hook"}
	if err := s.DB.Create(&hook).Error; err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(5 * 24 * time.Hour)
	sv := model.Server{Name: "exp-srv", Secret: "sec-exp-srv", ExpireAt: &exp}
	if err := s.DB.Create(&sv).Error; err != nil {
		t.Fatal(err)
	}

	// 默认 3 天：5 天后到期 → 不触发
	s.RunExpireCheck()
	var deliveries int64
	s.DB.Model(&model.NotificationDelivery{}).Count(&deliveries)
	if deliveries != 0 {
		t.Fatalf("default 3 days should not fire for 5-day expiry, got %d deliveries", deliveries)
	}

	// 设置 7 天 → 触发
	if err := s.DB.Create(&model.Setting{Key: SettingExpireNotifyDays, Value: "7"}).Error; err != nil {
		t.Fatal(err)
	}
	if got := s.expireNotifyDays(); got != 7 {
		t.Fatalf("expireNotifyDays=%d, want 7", got)
	}
	s.RunExpireCheck()
	s.DB.Model(&model.NotificationDelivery{}).Count(&deliveries)
	if deliveries != 1 {
		t.Fatalf("want 1 delivery with 7-day setting, got %d", deliveries)
	}
	var d model.NotificationDelivery
	if err := s.DB.First(&d).Error; err != nil {
		t.Fatal(err)
	}
	if d.Title != "[Argus] 服务器到期提醒" {
		t.Errorf("title=%q", d.Title)
	}
	if !strings.Contains(d.Content, "exp-srv") {
		t.Errorf("content=%q", d.Content)
	}

	// 越界/非法值回退默认 3
	for _, bad := range []string{"99", "0", "-2", "abc"} {
		s.DB.Model(&model.Setting{}).Where("key = ?", SettingExpireNotifyDays).Update("value", bad)
		if got := s.expireNotifyDays(); got != 3 {
			t.Errorf("expireNotifyDays with %q = %d, want default 3", bad, got)
		}
	}
	// 边界值 1 与 30 合法
	s.DB.Model(&model.Setting{}).Where("key = ?", SettingExpireNotifyDays).Update("value", "1")
	if got := s.expireNotifyDays(); got != 1 {
		t.Errorf("expireNotifyDays=1 got %d", got)
	}
	s.DB.Model(&model.Setting{}).Where("key = ?", SettingExpireNotifyDays).Update("value", "30")
	if got := s.expireNotifyDays(); got != 30 {
		t.Errorf("expireNotifyDays=30 got %d", got)
	}
}

// ---- 端到端：命中计划 → 通知持久队列 ----

func TestRunScheduledReportsEnqueuesWeekly(t *testing.T) {
	s := newReportsEnv(t)
	hook := model.Notification{Name: "hook", Type: "webhook", URL: "http://127.0.0.1:1/hook"}
	if err := s.DB.Create(&hook).Error; err != nil {
		t.Fatal(err)
	}
	cfg := model.TrafficReport{WebhookID: hook.ID, Period: ReportPeriodWeekly, Hour: 9, Weekday: 1, Enabled: true}
	if err := s.DB.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}
	sv := model.Server{Name: "srv1", Secret: "sec-srv1"}
	if err := s.DB.Create(&sv).Error; err != nil {
		t.Fatal(err)
	}
	mon0930 := time.Date(2024, 3, 11, 9, 30, 0, 0, time.UTC) // 周一
	if err := s.DB.Create(&model.Transfer{ServerID: sv.ID, Ts: reportUnix(t, "2024-03-10T00:00:00Z"), In: 100, Out: 50}).Error; err != nil {
		t.Fatal(err)
	}

	// 命中 → 入队（发送会因 URL 不可达而进入重试，记录仍在）
	s.RunScheduledReports(mon0930)
	var deliveries int64
	s.DB.Model(&model.NotificationDelivery{}).Count(&deliveries)
	if deliveries != 1 {
		t.Fatalf("want 1 delivery, got %d", deliveries)
	}
	var d model.NotificationDelivery
	if err := s.DB.First(&d).Error; err != nil {
		t.Fatal(err)
	}
	if d.Title != "[Argus] 流量周报" {
		t.Errorf("title=%q", d.Title)
	}
	if !strings.Contains(d.Content, "srv1: ↓100 B ↑50 B · 计费 150 B") {
		t.Errorf("content=%q", d.Content)
	}

	// 周二同一时刻 → 不触发
	s.RunScheduledReports(mon0930.Add(24 * time.Hour))
	s.DB.Model(&model.NotificationDelivery{}).Count(&deliveries)
	if deliveries != 1 {
		t.Fatalf("Tuesday should not fire, got %d deliveries", deliveries)
	}

	// 未配置渠道/未启用 → 不触发
	s.DB.Model(&cfg).Update("enabled", false)
	s.RunScheduledReports(time.Date(2024, 3, 18, 9, 30, 0, 0, time.UTC))
	s.DB.Model(&model.NotificationDelivery{}).Count(&deliveries)
	if deliveries != 1 {
		t.Fatalf("disabled should not fire, got %d deliveries", deliveries)
	}
}

// ---- HTTP API ----

func TestTrafficReportAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := newAuthzEnv(t)
	if err := e.srv.DB.AutoMigrate(&model.TrafficReport{}, &model.RevokedSession{}); err != nil {
		t.Fatal(err)
	}
	adminToken := e.token(t, e.admin)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.GET("/traffic-report", e.srv.getTrafficReport)
	authed.POST("/traffic-report", e.srv.saveTrafficReport)

	get := func() model.TrafficReport {
		req := httptest.NewRequest(http.MethodGet, "/traffic-report", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data model.TrafficReport `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Data
	}
	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/traffic-report", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// 无配置 → 默认 daily/9
	cfg := get()
	if cfg.Period != ReportPeriodDaily || cfg.Hour != 9 || cfg.Weekday != 1 || cfg.Day != 1 {
		t.Fatalf("defaults = %+v", cfg)
	}

	// 非法 period → 400
	if code := post(`{"webhook_id":1,"period":"hourly","enabled":true}`); code != http.StatusBadRequest {
		t.Fatalf("invalid period status=%d", code)
	}
	// weekly 越界 weekday → 400
	if code := post(`{"webhook_id":1,"period":"weekly","weekday":7,"hour":9}`); code != http.StatusBadRequest {
		t.Fatalf("invalid weekday status=%d", code)
	}
	// 合法 weekly → 200 并持久化
	if code := post(`{"webhook_id":1,"period":"weekly","weekday":5,"hour":18,"enabled":true}`); code != http.StatusOK {
		t.Fatalf("valid weekly status=%d", code)
	}
	cfg = get()
	if cfg.Period != ReportPeriodWeekly || cfg.Weekday != 5 || cfg.Hour != 18 || !cfg.Enabled {
		t.Fatalf("saved = %+v", cfg)
	}

	// 旧客户端（无 period）→ 兼容 daily
	if code := post(`{"webhook_id":2,"hour":7,"enabled":true}`); code != http.StatusOK {
		t.Fatalf("legacy save status=%d", code)
	}
	cfg = get()
	if cfg.Period != ReportPeriodDaily || cfg.Hour != 7 {
		t.Fatalf("legacy save = %+v", cfg)
	}
}
