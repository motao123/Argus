package alert

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/store"
)

// captureNotify 捕获 notify 回调的标题/正文/变量表。
type captureNotify struct {
	titles   []string
	contents []string
	vars     []map[string]string
}

// newTemplateTestEngine 构造带单渠道 DB 与捕获回调的引擎。
func newTemplateTestEngine(t *testing.T) (*Engine, *captureNotify) {
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
	cap := &captureNotify{}
	e := &Engine{db: db, Notify: func(n *model.Notification, title, content string, ownerID int64, vars map[string]string) {
		cap.titles = append(cap.titles, title)
		cap.contents = append(cap.contents, content)
		cap.vars = append(cap.vars, vars)
	}}
	return e, cap
}

// templateState 服务器运行时状态（在线、含 IP/平台信息）。
func templateState() store.State {
	h := store.NewHub()
	h.Upsert(&model.Server{ID: 7, Name: "node-1"})
	h.SetReport(7, protocol.HostInfo{
		Hostname: "node-1", IP: "1.2.3.4", IPv4: "1.2.3.4", IPv6: "fe80::7", Platform: "linux",
	}, &protocol.ReportParams{CPU: 95})
	return *h.Get(7)
}

// TestNotifyDefaultTemplate 无自定义模板时用默认格式（title/content 与变量表）。
func TestNotifyDefaultTemplate(t *testing.T) {
	e, cap := newTemplateTestEngine(t)
	max := 90.0
	a := &model.Alert{ID: 1, Name: "cpu-rule", Metric: "cpu", Max: &max, Notify: true, WebhookID: 1}
	e.notify(a, templateState(), 95, "triggered", true, false)

	if len(cap.titles) != 1 || cap.titles[0] != "[Argus] node-1 triggered" {
		t.Errorf("title = %v", cap.titles)
	}
	if len(cap.contents) != 1 || cap.contents[0] != "cpu-rule: cpu = 95.00" {
		t.Errorf("content = %v", cap.contents)
	}
	v := cap.vars[0]
	for k, want := range map[string]string{
		"event": "triggered", "rule": "cpu-rule", "metric": "cpu",
		"value": "95.00", "threshold": "90.00", "server.name": "node-1",
		"server.id": "7", "server.ip": "1.2.3.4", "server.ipv4": "1.2.3.4",
		"server.ipv6": "fe80::7", "server.platform": "linux", "server.online": "online",
	} {
		if v[k] != want {
			t.Errorf("vars[%q] = %q, want %q", k, v[k], want)
		}
	}
	if v["title"] != "[Argus] node-1 triggered" || v["content"] != "cpu-rule: cpu = 95.00" {
		t.Errorf("vars title/content mismatch: %q / %q", v["title"], v["content"])
	}
}

// TestNotifyDefaultTemplateRecovered 恢复通知默认格式。
func TestNotifyDefaultTemplateRecovered(t *testing.T) {
	e, cap := newTemplateTestEngine(t)
	a := &model.Alert{ID: 1, Name: "cpu-rule", Metric: "cpu", Notify: true, WebhookID: 1}
	e.notify(a, templateState(), 50, "recovered", true, false)
	if len(cap.contents) != 1 || cap.contents[0] != "node-1: cpu-rule back to normal" {
		t.Errorf("recovered content = %v", cap.contents)
	}
	if cap.vars[0]["event"] != "recovered" {
		t.Errorf("recovered event = %q", cap.vars[0]["event"])
	}
}

// TestNotifyRuleTemplateOverride 规则自定义模板覆盖默认格式：
// 首行为标题、其余为正文，支持全部变量。
func TestNotifyRuleTemplateOverride(t *testing.T) {
	e, cap := newTemplateTestEngine(t)
	min := 20.0
	a := &model.Alert{
		ID: 1, Name: "mem-rule", Metric: "mem", Min: &min, Notify: true, WebhookID: 1,
		Template: "{{event}}|{{server.name}}|{{server.ipv4}}\n{{rule}} {{metric}}={{value}} 阈值 {{threshold}} 时间 {{time}}",
	}
	e.notify(a, templateState(), 10, "triggered", true, false)

	if len(cap.titles) != 1 {
		t.Fatal("no notification captured")
	}
	if cap.titles[0] != "triggered|node-1|1.2.3.4" {
		t.Errorf("template title = %q", cap.titles[0])
	}
	content := cap.contents[0]
	if len(content) == 0 || content[:len("mem-rule mem=10.00 阈值 20.00 时间 ")] != "mem-rule mem=10.00 阈值 20.00 时间 " {
		t.Errorf("template content = %q", content)
	}
}

// TestNotifyRuleTemplateMissingVars 模板中未提供的变量渲染为空字符串。
func TestNotifyRuleTemplateMissingVars(t *testing.T) {
	e, cap := newTemplateTestEngine(t)
	a := &model.Alert{
		ID: 1, Name: "offline-rule", Metric: "offline", Notify: true, WebhookID: 1,
		Template: "{{server.name}} [{{server.ipv6}}] 阈值={{threshold}} 未知={{nope}}",
	}
	e.notify(a, templateState(), 1, "triggered", true, false)

	if len(cap.contents) != 1 {
		t.Fatal("no notification captured")
	}
	// offline 无阈值 → 空；未知变量 → 空
	if cap.contents[0] != "node-1 [fe80::7] 阈值= 未知=" {
		t.Errorf("content = %q", cap.contents[0])
	}
	// 单行模板 → 内容为该行，标题用默认
	if cap.titles[0] != "[Argus] node-1 triggered" {
		t.Errorf("single-line template title = %q", cap.titles[0])
	}
}

// TestNotifyRuleTemplateEmptyFallsBack 空白模板回退默认格式。
func TestNotifyRuleTemplateEmptyFallsBack(t *testing.T) {
	e, cap := newTemplateTestEngine(t)
	a := &model.Alert{ID: 1, Name: "cpu-rule", Metric: "cpu", Notify: true, WebhookID: 1, Template: "   \n "}
	e.notify(a, templateState(), 95, "triggered", true, false)
	if len(cap.titles) != 1 || cap.titles[0] != "[Argus] node-1 triggered" {
		t.Errorf("blank template title = %v", cap.titles)
	}
	if cap.contents[0] != "cpu-rule: cpu = 95.00" {
		t.Errorf("blank template content = %v", cap.contents)
	}
}

// TestOfflineNotifyCtxVars 离线哨兵构造统一上下文（event=offline、服务器变量）。
func TestOfflineNotifyCtxVars(t *testing.T) {
	env := newOfflineSentinelEnv(t)
	cap := &captureNotify{}
	s := NewOfflineSentinel(env.db, env.st)
	s.Notify = func(n *model.Notification, title, content string, ownerID int64, vars map[string]string) {
		cap.titles = append(cap.titles, title)
		cap.contents = append(cap.contents, content)
		cap.vars = append(cap.vars, vars)
	}
	s.check() // 首次：开始计时
	waitThreshold()
	s.check() // 超过阈值 → 通知
	if len(cap.vars) != 1 {
		t.Fatalf("offline notifications = %d, want 1", len(cap.vars))
	}
	if cap.titles[0] != "[Argus] 服务器离线 node-1" {
		t.Errorf("offline title = %q", cap.titles[0])
	}
	for k, want := range map[string]string{
		"event": "offline", "metric": "offline", "value": "1 秒",
		"server.name": "node-1", "server.id": "1", "server.online": "offline",
		"title": "[Argus] 服务器离线 node-1", "content": "node-1 已离线超过 1 秒",
	} {
		if cap.vars[0][k] != want {
			t.Errorf("vars[%q] = %q, want %q", k, cap.vars[0][k], want)
		}
	}
	// 恢复 → event=online
	env.st.SetOnline(env.srv.ID)
	s.check()
	if len(cap.vars) != 2 || cap.vars[1]["event"] != "online" {
		t.Errorf("online vars = %v", cap.vars)
	}
}
