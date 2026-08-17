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

// fakeClock 可控时钟（注入 Engine.nowFn）。
type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time          { return f.t }
func (f *fakeClock) advance(d time.Duration) { f.t = f.t.Add(d) }

// notifyRecord 捕获一次通知投递（渠道 + 事件类型）。
type notifyRecord struct {
	channelID int64
	event     string
}

// repeatEnv 重复/升级测试环境：默认渠道 ch1（ID=1）、升级渠道 ch2（ID=2），
// 服务器 node-1（ID=1）CPU=99（触发 cpu max=90 规则）。
type repeatEnv struct {
	t      *testing.T
	db     *gorm.DB
	hub    *store.Hub
	engine *Engine
	clock  *fakeClock
	recs   *[]notifyRecord
}

func newRepeatEnv(t *testing.T, a *model.Alert) *repeatEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Alert{}, &model.Notification{}, &model.NotificationGroup{},
		&model.NotificationDelivery{}, &model.AlertState{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Notification{ID: 1, Name: "ch1", Type: "webhook", URL: "http://127.0.0.1:1/a", OwnerID: a.OwnerID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Notification{ID: 2, Name: "ch2", Type: "webhook", URL: "http://127.0.0.1:1/b", OwnerID: a.OwnerID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(a).Error; err != nil {
		t.Fatal(err)
	}
	hub := store.NewHub()
	hub.Upsert(&model.Server{ID: 1, Name: "node-1", OwnerID: a.OwnerID})
	hub.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{CPU: 99})
	recs := &[]notifyRecord{}
	clock := &fakeClock{t: time.Now()}
	e := NewEngine(db, hub)
	e.nowFn = clock.now
	e.Notify = func(n *model.Notification, title, content string, ownerID int64, vars map[string]string) {
		*recs = append(*recs, notifyRecord{channelID: n.ID, event: vars["event"]})
	}
	return &repeatEnv{t: t, db: db, hub: hub, engine: e, clock: clock, recs: recs}
}

// events 返回已捕获事件序列（仅事件名）。
func (env *repeatEnv) events() []string {
	out := make([]string, 0, len(*env.recs))
	for _, r := range *env.recs {
		out = append(out, r.event)
	}
	return out
}

// triggerOnce 推进到触发：首轮登记开始时间，再推进 duration 秒后触发。
func (env *repeatEnv) triggerOnce(duration int) {
	env.engine.checkOnce()
	env.clock.advance(time.Duration(duration+1) * time.Second)
	env.engine.checkOnce()
}

func cpuAlert(max float64, m *model.Alert) *model.Alert {
	if m == nil {
		m = &model.Alert{}
	}
	m.Name = "cpu-rule"
	m.Metric = "cpu"
	m.Max = &max
	m.Duration = 3
	m.Notify = true
	m.WebhookID = 1
	m.Enabled = true
	return m
}

// TestEngineRepeatInterval 告警持续期间每 RepeatMinutes 分钟重发一次（event=repeat），
// 未到间隔不重发。
func TestEngineRepeatInterval(t *testing.T) {
	a := cpuAlert(90, &model.Alert{RepeatMinutes: 1})
	env := newRepeatEnv(t, a)

	env.triggerOnce(a.Duration)
	if got := env.events(); len(got) != 1 || got[0] != "triggered" {
		t.Fatalf("after trigger = %v, want [triggered]", got)
	}

	// 未到 1 分钟：不重发
	env.clock.advance(30 * time.Second)
	env.engine.checkOnce()
	if got := env.events(); len(got) != 1 {
		t.Fatalf("early repeat: %v, want only [triggered]", got)
	}

	// 达到 1 分钟：重发一次
	env.clock.advance(31 * time.Second)
	env.engine.checkOnce()
	if got := env.events(); len(got) != 2 || got[1] != "repeat" {
		t.Fatalf("after repeat interval = %v, want [triggered repeat]", got)
	}

	// 再过 1 分钟：再次重发
	env.clock.advance(time.Minute)
	env.engine.checkOnce()
	if got := env.events(); len(got) != 3 || got[2] != "repeat" {
		t.Fatalf("after second interval = %v, want [triggered repeat repeat]", got)
	}

	// 重复/升级进度已持久化（重启恢复用）
	var st model.AlertState
	if err := env.db.Where("alert_id = ? AND server_id = ?", a.ID, 1).First(&st).Error; err != nil {
		t.Fatalf("persisted state missing: %v", err)
	}
	if st.LastNotifyAt.IsZero() || st.EscalatedAt != nil {
		t.Fatalf("persisted state = %+v", st)
	}
}

// TestEngineRepeatStopsOnRecovery 恢复后发送 recovered 且停止重复，并清除持久化状态。
func TestEngineRepeatStopsOnRecovery(t *testing.T) {
	a := cpuAlert(90, &model.Alert{RepeatMinutes: 1})
	env := newRepeatEnv(t, a)

	env.triggerOnce(a.Duration)
	env.clock.advance(time.Minute)
	env.engine.checkOnce()
	if got := env.events(); len(got) != 2 || got[1] != "repeat" {
		t.Fatalf("before recovery = %v", got)
	}

	// 恢复：CPU 回到正常区间
	env.hub.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{CPU: 50})
	env.clock.advance(time.Second)
	env.engine.checkOnce()
	if got := env.events(); len(got) != 3 || got[2] != "recovered" {
		t.Fatalf("after recovery = %v, want [triggered repeat recovered]", got)
	}

	// 恢复后不再重复
	env.clock.advance(10 * time.Minute)
	env.engine.checkOnce()
	if got := env.events(); len(got) != 3 {
		t.Fatalf("repeats after recovery = %v, want 3 total", got)
	}
	var n int64
	env.db.Model(&model.AlertState{}).Count(&n)
	if n != 0 {
		t.Fatalf("persisted states after recovery = %d, want 0", n)
	}
}

// TestEngineEscalationSwitchesChannel 持续超过升级延迟后：
// 发送 event=escalated 到升级渠道，此后重复通知（event=repeat）改发升级渠道。
func TestEngineEscalationSwitchesChannel(t *testing.T) {
	a := cpuAlert(90, &model.Alert{
		RepeatMinutes: 5, EscalateToChannelID: 2, EscalateAfterMinutes: 10,
	})
	env := newRepeatEnv(t, a)

	env.triggerOnce(a.Duration) // triggered → ch1
	if len(*env.recs) != 1 || (*env.recs)[0].channelID != 1 {
		t.Fatalf("triggered recs = %+v", *env.recs)
	}

	// 未到升级延迟：重复仍走原渠道
	env.clock.advance(5 * time.Minute)
	env.engine.checkOnce()
	if len(*env.recs) != 2 || (*env.recs)[1].event != "repeat" || (*env.recs)[1].channelID != 1 {
		t.Fatalf("pre-escalation repeat = %+v", *env.recs)
	}

	// 达到升级延迟：escalated → ch2（同一轮重复也切到 ch2）
	env.clock.advance(5 * time.Minute)
	env.engine.checkOnce()
	if len(*env.recs) != 4 {
		t.Fatalf("escalation recs = %+v", *env.recs)
	}
	if (*env.recs)[2].event != "escalated" || (*env.recs)[2].channelID != 2 {
		t.Fatalf("escalated = %+v, want ch2", (*env.recs)[2])
	}
	if (*env.recs)[3].event != "repeat" || (*env.recs)[3].channelID != 2 {
		t.Fatalf("post-escalation repeat = %+v, want ch2", (*env.recs)[3])
	}

	// 后续重复持续走升级渠道
	env.clock.advance(5 * time.Minute)
	env.engine.checkOnce()
	last := (*env.recs)[len(*env.recs)-1]
	if last.event != "repeat" || last.channelID != 2 {
		t.Fatalf("later repeat = %+v, want ch2 repeat", last)
	}

	// 升级状态已持久化
	var st model.AlertState
	if err := env.db.Where("alert_id = ? AND server_id = ?", a.ID, 1).First(&st).Error; err != nil {
		t.Fatalf("persisted state missing: %v", err)
	}
	if st.EscalatedAt == nil {
		t.Fatalf("escalated_at not persisted: %+v", st)
	}
}

// TestEngineStateRestoredAfterRestart 重启后（新引擎 + 同一 DB）：
// 不重复发送 triggered，重复节奏与升级渠道从持久化状态恢复；恢复后清除。
func TestEngineStateRestoredAfterRestart(t *testing.T) {
	a := cpuAlert(90, &model.Alert{
		RepeatMinutes: 5, EscalateToChannelID: 2, EscalateAfterMinutes: 10,
	})
	env := newRepeatEnv(t, a)

	env.triggerOnce(a.Duration) // triggered → ch1
	env.clock.advance(5 * time.Minute)
	env.engine.checkOnce() // repeat → ch1
	env.clock.advance(5 * time.Minute)
	env.engine.checkOnce() // escalated + repeat → ch2
	if len(*env.recs) != 4 {
		t.Fatalf("before restart recs = %+v", *env.recs)
	}

	// 模拟重启：同 DB 新建引擎（loadStates 恢复状态），时钟继续。
	recs2 := &[]notifyRecord{}
	e2 := NewEngine(env.db, env.hub)
	e2.nowFn = env.clock.now
	e2.Notify = func(n *model.Notification, title, content string, ownerID int64, vars map[string]string) {
		*recs2 = append(*recs2, notifyRecord{channelID: n.ID, event: vars["event"]})
	}

	// 重启后未到重复点：不产生任何通知（尤其不重发 triggered）
	env.clock.advance(time.Second)
	e2.checkOnce()
	if len(*recs2) != 0 {
		t.Fatalf("notifications right after restart = %+v, want none", *recs2)
	}

	// 推进到下一个重复点：重复发往升级渠道（升级状态已恢复）
	env.clock.advance(4*time.Minute + 59*time.Second)
	e2.checkOnce()
	if len(*recs2) != 1 || (*recs2)[0].event != "repeat" || (*recs2)[0].channelID != 2 {
		t.Fatalf("repeat after restart = %+v, want ch2 repeat", *recs2)
	}

	// 重启后恢复：发 recovered 并清除持久化状态
	env.hub.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{CPU: 50})
	env.clock.advance(time.Second)
	e2.checkOnce()
	if len(*recs2) != 2 || (*recs2)[1].event != "recovered" {
		t.Fatalf("recovery after restart = %+v", *recs2)
	}
	var n int64
	env.db.Model(&model.AlertState{}).Count(&n)
	if n != 0 {
		t.Fatalf("persisted states after recovery = %d, want 0", n)
	}
}

// TestEngineEscalationOwnerMismatch 升级渠道 owner 与规则不匹配时：
// 不发送 escalated、不切换渠道，重复通知持续走原渠道。
func TestEngineEscalationOwnerMismatch(t *testing.T) {
	a := cpuAlert(90, &model.Alert{
		OwnerID: 100, RepeatMinutes: 5, EscalateToChannelID: 2, EscalateAfterMinutes: 10,
	})
	env := newRepeatEnv(t, a)
	// 把升级渠道 ch2 改归他人所有（owner 不匹配）
	if err := env.db.Model(&model.Notification{}).Where("id = ?", 2).Update("owner_id", 101).Error; err != nil {
		t.Fatal(err)
	}

	env.triggerOnce(a.Duration) // triggered → ch1
	env.clock.advance(10 * time.Minute)
	env.engine.checkOnce() // 升级被拒（不投递），重复仍走 ch1
	env.clock.advance(5 * time.Minute)
	env.engine.checkOnce()

	for _, r := range *env.recs {
		if r.event == "escalated" {
			t.Fatalf("escalated sent despite owner mismatch: %+v", *env.recs)
		}
		if r.channelID == 2 {
			t.Fatalf("escalate channel used despite owner mismatch: %+v", *env.recs)
		}
	}
	if len(*env.recs) != 3 || (*env.recs)[1].event != "repeat" || (*env.recs)[2].event != "repeat" {
		t.Fatalf("recs = %+v, want [triggered repeat repeat] on ch1", *env.recs)
	}
	// 未升级：escalated_at 不落库
	var st model.AlertState
	if err := env.db.Where("alert_id = ? AND server_id = ?", a.ID, 1).First(&st).Error; err != nil {
		t.Fatalf("persisted state missing: %v", err)
	}
	if st.EscalatedAt != nil {
		t.Fatalf("escalated_at persisted despite owner mismatch: %+v", st)
	}
}
