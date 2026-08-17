package alert

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/store"
)

// newSilenceTestEngine 构建带 DB（单渠道）与 Notify 计数器的引擎。
func newSilenceTestEngine(t *testing.T) (*Engine, *int) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Alert{}, &model.Notification{}, &model.NotificationGroup{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Notification{ID: 1, Name: "ch", Type: "webhook", URL: "http://127.0.0.1:1/x"}).Error; err != nil {
		t.Fatal(err)
	}
	notifyCount := 0
	e := &Engine{db: db, Notify: func(n *model.Notification, title, content string, ownerID int64, vars map[string]string) {
		notifyCount++
	}}
	return e, &notifyCount
}

func testState() store.State {
	h := store.NewHub()
	h.Upsert(&model.Server{ID: 1, Name: "srv"})
	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{CPU: 99, MemUsed: 1, MemTotal: 1})
	return *h.Get(1)
}

func TestSilencedAtBoundaries(t *testing.T) {
	e := &Engine{}
	now := time.Now()
	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)
	a := &model.Alert{SilenceFrom: &from, SilenceTo: &to}

	if !e.silencedAt(a, now) {
		t.Fatal("inside window should be silenced")
	}
	if e.silencedAt(a, now.Add(-2*time.Hour)) {
		t.Fatal("before silence_from should not be silenced")
	}
	if e.silencedAt(a, now.Add(2*time.Hour)) {
		t.Fatal("after silence_to should not be silenced")
	}
	// 无结束时间 = 不静默
	if e.silencedAt(&model.Alert{SilenceFrom: &from}, now) {
		t.Fatal("nil silence_to should not silence")
	}
	// from 为空 = 从现在起
	onlyTo := &model.Alert{SilenceTo: &to}
	if !e.silencedAt(onlyTo, now) {
		t.Fatal("nil silence_from with future to should silence")
	}
	// 恰好到结束时刻 = 不再静默
	if e.silencedAt(onlyTo, to) {
		t.Fatal("at silence_to should not be silenced")
	}
}

func TestSilenceSuppressesNotification(t *testing.T) {
	e, count := newSilenceTestEngine(t)
	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	a := &model.Alert{ID: 1, Name: "cpu", Metric: "cpu", Notify: true, WebhookID: 1,
		SilenceFrom: &from, SilenceTo: &to}
	// 静默窗口内触发 → 不通知
	e.notify(a, testState(), 99, "triggered", true, false)
	if *count != 0 {
		t.Fatalf("silenced alert should not notify, got %d", *count)
	}
	// 窗口结束 → 恢复通知
	a.SilenceFrom = nil
	past := time.Now().Add(-time.Hour)
	a.SilenceTo = &past
	e.notify(a, testState(), 99, "triggered", true, false)
	if *count != 1 {
		t.Fatalf("alert outside silence window should notify, got %d", *count)
	}
}

func TestAckSuppressesNotification(t *testing.T) {
	e, count := newSilenceTestEngine(t)
	a := &model.Alert{ID: 1, Name: "cpu", Metric: "cpu", Notify: true, WebhookID: 1}
	// 未确认 → 通知
	e.notify(a, testState(), 99, "triggered", true, false)
	if *count != 1 {
		t.Fatalf("unacked alert should notify, got %d", *count)
	}
	// 已确认 → 不再通知
	now := time.Now()
	a.AckedAt = &now
	a.AckedBy = "alice"
	e.notify(a, testState(), 99, "triggered", true, false)
	if *count != 1 {
		t.Fatalf("acked alert should not notify, got %d", *count)
	}
	// 取消确认 → 恢复通知
	a.AckedAt = nil
	e.notify(a, testState(), 99, "triggered", true, false)
	if *count != 2 {
		t.Fatalf("unacked alert should notify again, got %d", *count)
	}
}

func TestClearAckPersistsToDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Alert{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Create(&model.Alert{ID: 1, Name: "a", Metric: "cpu", AckedAt: &now, AckedBy: "alice"}).Error; err != nil {
		t.Fatal(err)
	}
	var a model.Alert
	db.First(&a)
	e := &Engine{db: db}
	e.clearAck(&a)
	if a.AckedAt != nil || a.AckedBy != "" {
		t.Fatalf("in-memory ack not cleared: %+v", a)
	}
	var persisted model.Alert
	db.First(&persisted)
	if persisted.AckedAt != nil || persisted.AckedBy != "" {
		t.Fatalf("DB ack not cleared: %+v", persisted)
	}
	// 未确认的规则清除为无操作
	e.clearAck(&persisted)
}

// TestAckNotRequiredForRecovery 恢复通知不受确认抑制（引擎先清 ack 再发恢复通知，
// 见 checkOnce 恢复分支），此处验证 notify 对已清 ack 的规则正常发恢复通知。
func TestAckNotRequiredForRecovery(t *testing.T) {
	e, count := newSilenceTestEngine(t)
	a := &model.Alert{ID: 1, Name: "cpu", Metric: "cpu", Notify: true, WebhookID: 1}
	e.notify(a, testState(), 0, "recovered", true, false)
	if *count != 1 {
		t.Fatalf("recovery should notify, got %d", *count)
	}
}
