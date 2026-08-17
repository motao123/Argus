package alert

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifier"
	"github.com/motao123/Argus/server/internal/store"
)

// offlineSentinelEnv 离线哨兵测试环境：内存库 + 已离线服务器。
type offlineSentinelEnv struct {
	db      *gorm.DB
	st      *store.Hub
	srv     *model.Server
	webhook model.Notification
	queue   *notifier.Queue
}

func newOfflineSentinelEnv(t *testing.T) *offlineSentinelEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(
		&model.Server{}, &model.OfflineNotify{}, &model.Notification{},
		&model.MaintenanceWindow{}, &model.NotificationDelivery{},
	); err != nil {
		t.Fatal(err)
	}
	srv := &model.Server{Name: "node-1", Secret: "x"}
	if err := gdb.Create(srv).Error; err != nil {
		t.Fatal(err)
	}
	webhook := model.Notification{Name: "hook", Type: "webhook", URL: "http://127.0.0.1:1/dead", Method: "POST", Body: "{}", OwnerID: 1}
	if err := gdb.Create(&webhook).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.OfflineNotify{WebhookID: webhook.ID, OfflineAfter: 1, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	st := store.NewHub()
	st.Upsert(srv)
	st.MarkOffline(srv.ID)

	// 用持久队列记录送达（Notify 回调注入），避免真实 HTTP 发送干扰断言。
	queue := notifier.NewQueue(gdb)
	queue.BackoffBase = time.Second

	return &offlineSentinelEnv{db: gdb, st: st, srv: srv, webhook: webhook, queue: queue}
}

// sentinel 构造注入 Notify 回调的哨兵。
func (e *offlineSentinelEnv) sentinel() *OfflineSentinel {
	s := NewOfflineSentinel(e.db, e.st)
	s.Notify = func(n *model.Notification, title, content string, ownerID int64, vars map[string]string) {
		_ = e.queue.EnqueueCtx(n, title, content, ownerID, vars)
	}
	return s
}

func (e *offlineSentinelEnv) deliveries() int64 {
	var n int64
	e.db.Model(&model.NotificationDelivery{}).Count(&n)
	return n
}

// eventually 轮询等待条件成立（通知经 goroutine 异步入队）。
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func itoa(v int64) string {
	return fmt.Sprintf("%d", v)
}

// waitThreshold 等待超过离线阈值（OfflineAfter=1s）。
func waitThreshold() { time.Sleep(1100 * time.Millisecond) }

// TestOfflineSentinelSkipsMaintenance 维护窗口内不产生离线通知；窗口结束后照常判定。
func TestOfflineSentinelSkipsMaintenance(t *testing.T) {
	env := newOfflineSentinelEnv(t)
	now := time.Now()
	// 覆盖该服务器的维护窗口：当前生效
	if err := env.db.Create(&model.MaintenanceWindow{
		Title: "mv", ServerIDs: itoa(env.srv.ID),
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	s := env.sentinel()
	s.check() // 首次：维护内 → 跳过
	waitThreshold()
	s.check() // 阈值已过：维护内 → 仍跳过
	time.Sleep(100 * time.Millisecond)
	if n := env.deliveries(); n != 0 {
		t.Fatalf("deliveries during maintenance = %d, want 0", n)
	}

	// 维护窗口结束 → 应触发离线通知
	if err := env.db.Model(&model.MaintenanceWindow{}).Where("title = ?", "mv").
		Update("end_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	s.check() // 首次脱离维护：开始计时
	waitThreshold()
	s.check() // 阈值通过 → 通知
	eventually(t, "offline delivery after maintenance", func() bool { return env.deliveries() == 1 })
}

// TestOfflineSentinelNotifiesWithoutMaintenance 无维护窗口时行为不变（回归）。
func TestOfflineSentinelNotifiesWithoutMaintenance(t *testing.T) {
	env := newOfflineSentinelEnv(t)
	s := env.sentinel()
	s.check()
	waitThreshold()
	s.check()
	eventually(t, "offline delivery", func() bool { return env.deliveries() == 1 })
}

// TestOfflineSentinelRecoversAfterMaintenance 维护前已通知的服务器，
// 维护期间恢复不重复通知，维护结束后补发恢复通知。
func TestOfflineSentinelRecoversAfterMaintenance(t *testing.T) {
	env := newOfflineSentinelEnv(t)
	s := env.sentinel()

	// 先离线通知（无维护）
	s.check()
	waitThreshold()
	s.check()
	eventually(t, "initial offline delivery", func() bool { return env.deliveries() == 1 })

	// 进入维护并恢复上线：不应在维护期间发恢复通知
	now := time.Now()
	if err := env.db.Create(&model.MaintenanceWindow{
		Title: "mv", ServerIDs: itoa(env.srv.ID),
		StartAt: now.Add(-time.Hour), EndAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	env.st.SetOnline(env.srv.ID)
	s.check()
	s.check()
	time.Sleep(100 * time.Millisecond)
	if n := env.deliveries(); n != 1 {
		t.Fatalf("deliveries during maintenance = %d, want 1", n)
	}

	// 维护结束：恢复通知补发
	if err := env.db.Model(&model.MaintenanceWindow{}).Where("title = ?", "mv").
		Update("end_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	s.check()
	eventually(t, "recovery delivery after maintenance", func() bool { return env.deliveries() == 2 })
}
