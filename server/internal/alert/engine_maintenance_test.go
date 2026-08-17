package alert

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifier"
	"github.com/motao123/Argus/server/internal/store"
)

// TestEngineOfflineMetricSkipsMaintenance offline 报警规则在维护窗口内不触发，
// 窗口结束后照常触发（避免维护期误报）。
func TestEngineOfflineMetricSkipsMaintenance(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(
		&model.Server{}, &model.Alert{}, &model.Notification{}, &model.NotificationDelivery{}, &model.MaintenanceWindow{},
	); err != nil {
		t.Fatal(err)
	}
	srv := &model.Server{Name: "node-1", Secret: "x"}
	if err := gdb.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	hook := model.Notification{Name: "hook", Type: "webhook", URL: "http://127.0.0.1:1/dead", Method: "POST", Body: "{}"}
	if err := gdb.Create(&hook).Error; err != nil {
		t.Fatal(err)
	}
	min := 1.0
	alert := &model.Alert{Name: "offline-rule", Metric: "offline", Min: &min, Duration: 0, Notify: true, WebhookID: hook.ID, Enabled: true}
	if err := gdb.Create(alert).Error; err != nil {
		t.Fatal(err)
	}

	st := store.NewHub()
	st.Upsert(srv)
	st.MarkOffline(srv.ID)

	queue := notifier.NewQueue(gdb)
	e := NewEngine(gdb, st)
	e.Notify = func(n *model.Notification, title, content string, ownerID int64) {
		_ = queue.Enqueue(n, title, content, ownerID)
	}
	count := func() int64 {
		var n int64
		gdb.Model(&model.NotificationDelivery{}).Count(&n)
		return n
	}

	// 维护窗口生效：连续两轮判定均不触发
	now := time.Now()
	if err := gdb.Create(&model.MaintenanceWindow{
		Title: "mv", ServerIDs: itoa(srv.ID),
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	e.checkOnce()
	e.checkOnce()
	time.Sleep(100 * time.Millisecond)
	if n := count(); n != 0 {
		t.Fatalf("deliveries during maintenance = %d, want 0", n)
	}

	// 窗口结束：两轮判定后触发一次（不重复轰炸）
	if err := gdb.Model(&model.MaintenanceWindow{}).Where("title = ?", "mv").
		Update("end_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	e.checkOnce()
	e.checkOnce()
	eventually(t, "offline alert delivery", func() bool { return count() == 1 })
	e.checkOnce()
	e.checkOnce()
	time.Sleep(100 * time.Millisecond)
	if n := count(); n != 1 {
		t.Fatalf("deliveries after maintenance = %d, want 1 (单次触发)", n)
	}
}

// TestEngineOfflineMetricNoMaintenance 无维护窗口时 offline 规则行为不变（回归）。
func TestEngineOfflineMetricNoMaintenance(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(
		&model.Server{}, &model.Alert{}, &model.Notification{}, &model.NotificationDelivery{}, &model.MaintenanceWindow{},
	); err != nil {
		t.Fatal(err)
	}
	srv := &model.Server{Name: "node-1", Secret: "x"}
	if err := gdb.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	hook := model.Notification{Name: "hook", Type: "webhook", URL: "http://127.0.0.1:1/dead", Method: "POST", Body: "{}"}
	if err := gdb.Create(&hook).Error; err != nil {
		t.Fatal(err)
	}
	min := 1.0
	alert := &model.Alert{Name: "offline-rule", Metric: "offline", Min: &min, Duration: 0, Notify: true, WebhookID: hook.ID, Enabled: true}
	if err := gdb.Create(alert).Error; err != nil {
		t.Fatal(err)
	}
	st := store.NewHub()
	st.Upsert(srv)
	st.MarkOffline(srv.ID)

	queue := notifier.NewQueue(gdb)
	e := NewEngine(gdb, st)
	e.Notify = func(n *model.Notification, title, content string, ownerID int64) {
		_ = queue.Enqueue(n, title, content, ownerID)
	}
	e.checkOnce()
	e.checkOnce()
	eventually(t, "offline alert delivery", func() bool {
		var n int64
		gdb.Model(&model.NotificationDelivery{}).Count(&n)
		return n == 1
	})
}
