package plugin

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---- argus.kv：每插件命名空间 + 大小限制 ----

func TestKVIsolation(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "p1",
		`{"name":"p1","version":"1.0.0","description":"d","permissions":{}}`,
		`argus.kv.set("shared", "v1"); argus.kv.set("only1", "x"); console.log("p1:" + argus.kv.get("shared") + ":" + argus.kv.get("only1"));`)
	writePlugin(t, dir, "p2",
		`{"name":"p2","version":"1.0.0","description":"d","permissions":{}}`,
		`argus.kv.set("shared", "v2"); console.log("p2:" + argus.kv.get("shared") + ":" + (argus.kv.get("only1") === undefined ? "NA" : "LEAK"));`)
	m := newManager(t, dir, nil)
	if err := m.Run("p1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Run("p2"); err != nil {
		t.Fatal(err)
	}
	if !hasLog(m, "p1", "p1:v1:x") {
		t.Fatalf("p1 kv wrong, logs: %v", pluginSnapshot(t, m, "p1").Logs)
	}
	if !hasLog(m, "p2", "p2:v2:NA") {
		t.Fatalf("p2 kv must be isolated (same key, no cross-plugin leak), logs: %v", pluginSnapshot(t, m, "p2").Logs)
	}
}

func TestKVSizeLimits(t *testing.T) {
	dir := t.TempDir()
	// 大值（> 4KiB）写入失败；超过 64 键失败
	big := strings.Repeat("A", 5000)
	writePlugin(t, dir, "kvlim",
		`{"name":"kvlim","version":"1.0.0","description":"d","permissions":{}}`,
		`var ok = argus.kv.set("big", "`+big+`");
console.log("big:" + ok);
argus.kv.set("okey", "value");
console.log("roundtrip:" + argus.kv.get("okey"));
var allOk = true;
for (var i = 0; i < 70; i++) { if (!argus.kv.set("k" + i, "v")) { allOk = false; break; } }
console.log("overflow:" + allOk);`)
	m := newManager(t, dir, nil)
	if err := m.Run("kvlim"); err != nil {
		t.Fatal(err)
	}
	p := pluginSnapshot(t, m, "kvlim")
	if !snapshotHasLog(p, "big:false") {
		t.Fatalf("oversized value should be rejected, logs: %v", p.Logs)
	}
	if !snapshotHasLog(p, "overflow:false") {
		t.Fatalf("kv key cap should be enforced, logs: %v", p.Logs)
	}
	if !snapshotHasLog(p, "roundtrip:value") {
		t.Fatalf("normal kv set/get should work, logs: %v", p.Logs)
	}
}

// ---- argus.notify：成功与宿主 panic 隔离 ----

func TestNotifyHostAPI(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "noti",
		`{"name":"noti","version":"1.0.0","description":"d","permissions":{"allow_notify":true,"approved":true}}`,
		`var a = argus.notify(42, "t1", "c1"); console.log("notify42:" + a);`)
	writePlugin(t, dir, "noti2",
		`{"name":"noti2","version":"1.0.0","description":"d","permissions":{"allow_notify":true,"approved":true}}`,
		`var b = argus.notify(7, "t", "c"); console.log("notify7:" + b); console.log("after");`)
	m := newManager(t, dir, nil)
	var gotID int64
	var gotTitle, gotContent string
	m.NotifyFunc = func(id int64, title, content string) error {
		gotID, gotTitle, gotContent = id, title, content
		return nil
	}
	if err := m.Run("noti"); err != nil {
		t.Fatal(err)
	}
	if gotID != 42 || gotTitle != "t1" || gotContent != "c1" {
		t.Fatalf("notify args wrong: %d %q %q", gotID, gotTitle, gotContent)
	}
	if !hasLog(m, "noti", "notify42:true") {
		t.Fatalf("notify should succeed, logs: %v", pluginSnapshot(t, m, "noti").Logs)
	}

	// 宿主 NotifyFunc panic：不崩服务，返回 false
	m.NotifyFunc = func(id int64, title, content string) error {
		panic("host notify exploded")
	}
	if err := m.Run("noti2"); err != nil {
		t.Fatalf("host panic must not escape to the manager: %v", err)
	}
	p2 := pluginSnapshot(t, m, "noti2")
	if !snapshotHasLog(p2, "notify7:false") || !snapshotHasLog(p2, "after") {
		t.Fatalf("host panic should be isolated (notify=false, script continues), logs: %v", p2.Logs)
	}
}

// ---- argus.getServers：脱敏只读 ----

func TestGetServersRedacted(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "gs",
		`{"name":"gs","version":"1.0.0","description":"d","permissions":{}}`,
		`var list = argus.getServers();
console.log("count:" + list.length);
if (list.length > 0) {
  console.log("first:" + list[0].id + ":" + list[0].name + ":" + list[0].online + ":" + (list[0].secret === undefined ? "NOSECRET" : "LEAK") + ":" + (list[0].price === undefined ? "NOPRICE" : "LEAK"));
}`)
	m := newManager(t, dir, nil)
	m.ServerSource = func() []ServerView {
		return []ServerView{
			{ID: 1, Name: "web-01", Online: true, IP: "1.2.3.4"},
			{ID: 2, Name: "db-01", Online: false},
		}
	}
	if err := m.Run("gs"); err != nil {
		t.Fatal(err)
	}
	p := pluginSnapshot(t, m, "gs")
	if !snapshotHasLog(p, "count:2") {
		t.Fatalf("getServers count wrong, logs: %v", p.Logs)
	}
	if !snapshotHasLog(p, "first:1:web-01:true:NOSECRET:NOPRICE") {
		t.Fatalf("getServers must be redacted (no secret/price), logs: %v", p.Logs)
	}
}

// ---- panic / timeout 不崩服务 ----

func TestTimeoutDoesNotKillService(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "hang",
		`{"name":"hang","version":"1.0.0","description":"d","permissions":{}}`,
		`while (true) {}`)
	writePlugin(t, dir, "good",
		`{"name":"good","version":"1.0.0","description":"d","permissions":{}}`,
		`console.log("hello")`)
	m := newManager(t, dir, nil)
	m.RunTimeout = 300 * time.Millisecond

	start := time.Now()
	err := m.Run("hang")
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout took too long")
	}
	// 超时后服务仍可用
	if err := m.Run("good"); err != nil {
		t.Fatalf("manager should remain functional after timeout: %v", err)
	}
	if !hasLog(m, "good", "hello") {
		t.Fatal("good plugin should run after timeout")
	}
	// 再次运行 hang 依旧干净超时（无状态污染）
	start = time.Now()
	if err := m.Run("hang"); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("second timeout run: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("second timeout took too long")
	}
	if pluginSnapshot(t, m, "hang").LastStatus != "error" {
		t.Fatalf("hang plugin last_status should be error, got %q", pluginSnapshot(t, m, "hang").LastStatus)
	}
}

func TestJSPanicDoesNotKillService(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "boom",
		`{"name":"boom","version":"1.0.0","description":"d","permissions":{}}`,
		`throw new Error("boom");`)
	writePlugin(t, dir, "good",
		`{"name":"good","version":"1.0.0","description":"d","permissions":{}}`,
		`console.log("still-alive")`)
	m := newManager(t, dir, nil)
	err := m.Run("boom")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected JS exception, got: %v", err)
	}
	if err := m.Run("good"); err != nil {
		t.Fatalf("manager should survive JS panic: %v", err)
	}
	if !hasLog(m, "good", "still-alive") {
		t.Fatal("good plugin should still run")
	}
}

// ---- 事件 hook：异步、载荷、未声明不触发、禁止并发重入 ----

func TestEventHooks(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "hooker",
		`{"name":"hooker","version":"1.0.0","description":"d","events":["onAlert","onServerOnline"],"permissions":{}}`,
		`function onAlert(p) { console.log("onAlert:" + p.alert + ":" + p.server_id + ":" + p.kind); }
function onServerOnline(p) { console.log("online:" + p.server_id); }`)
	m := newManager(t, dir, nil)

	m.FireEvent("onAlert", map[string]any{"alert": "CPU-High", "server_id": int64(7), "kind": "triggered"})
	waitFor(t, 3*time.Second, func() bool { return hasLog(m, "hooker", "onAlert:CPU-High:7:triggered") }, "onAlert hook fired")
	if pluginSnapshot(t, m, "hooker").RunCount != 1 {
		t.Fatalf("hook run should be counted, got %d", pluginSnapshot(t, m, "hooker").RunCount)
	}
	if pluginSnapshot(t, m, "hooker").LastStatus != "ok" {
		t.Fatalf("hook run status should be ok, got %q", pluginSnapshot(t, m, "hooker").LastStatus)
	}

	m.FireEvent("onServerOnline", map[string]any{"server_id": int64(3)})
	waitFor(t, 3*time.Second, func() bool { return hasLog(m, "hooker", "online:3") }, "onServerOnline hook fired")

	// 未声明的事件不触发
	m.FireEvent("onServerOffline", map[string]any{"server_id": int64(9)})
	time.Sleep(300 * time.Millisecond)
	if hasLog(m, "hooker", "onServerOffline") {
		t.Fatal("undeclared event must not fire")
	}
}

func TestHookNoConcurrentReentry(t *testing.T) {
	dir := t.TempDir()
	// onAlert 先打日志再死循环（模拟慢 hook）
	writePlugin(t, dir, "slow",
		`{"name":"slow","version":"1.0.0","description":"d","events":["onAlert"],"permissions":{}}`,
		`function onAlert(p) { console.log("enter"); while (true) {} }`)
	m := newManager(t, dir, nil)
	m.RunTimeout = 400 * time.Millisecond

	payload := map[string]any{"alert": "x"}
	// 第一次触发：进入运行中状态
	m.FireEvent("onAlert", payload)
	waitFor(t, 3*time.Second, func() bool { return hasLog(m, "slow", "enter") }, "first hook entered")

	// 并发重入被阻止：第二次触发被跳过（busy）
	m.FireEvent("onAlert", payload)
	waitFor(t, 3*time.Second, func() bool { return hasLog(m, "slow", "SKIP onAlert") }, "concurrent reentry blocked")

	// 等待第一次超时结束后，第三次触发可再次进入
	waitFor(t, 5*time.Second, func() bool {
		if pluginSnapshot(t, m, "slow").Running {
			return false
		}
		m.FireEvent("onAlert", payload)
		return true
	}, "first hook finished")
	waitFor(t, 3*time.Second, func() bool { return logCount(m, "slow", "enter") >= 2 }, "hook runs again after timeout")
}

// ---- 持久化：重启保留日志 / KV / 运行状态 ----

func TestRestartPreservesLogsStateAndKV(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }))
	defer srv.Close()
	manifest := `{"name":"persist","version":"1.0.0","description":"d","events":["onAlert"],"permissions":{"allow_fetch":true,"fetch_domains":["safe.test"],"approved":true}}`
	writePlugin(t, dir, "persist", manifest,
		`argus.kv.set("token", "abc123");
function onAlert(p) { console.log("hook:" + p.alert); }
console.log("boot");`)
	resolver := fakeResolver{m: map[string][]net.IPAddr{"safe.test": {ipAddr("93.184.216.34")}}}

	m := newManager(t, dir, resolver)
	dialToServer(m, srv)
	if err := m.Run("persist"); err != nil {
		t.Fatal(err)
	}
	// 触发一个 hook 事件产生额外日志
	m.FireEvent("onAlert", map[string]any{"alert": "mem"})
	waitFor(t, 3*time.Second, func() bool { return hasLog(m, "persist", "hook:mem") }, "hook log")

	// 重启（全新 Manager 实例，同一目录）
	m2 := New(dir)
	m2.Resolver = resolver
	dialToServer(m2, srv)
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	p := pluginSnapshot(t, m2, "persist")
	if p == nil {
		t.Fatal("plugin missing after restart")
	}
	if !p.Permissions.Approved {
		t.Fatal("approved state should persist across restart")
	}
	if !p.Enabled {
		t.Fatal("enabled state should persist across restart")
	}
	if p.RunCount < 2 {
		t.Fatalf("run_count should persist across restart, got %d", p.RunCount)
	}
	if p.LastStatus != "ok" {
		t.Fatalf("last_status should persist, got %q", p.LastStatus)
	}
	if len(p.Logs) == 0 || !snapshotHasLog(p, "boot") || !snapshotHasLog(p, "hook:mem") {
		t.Fatalf("logs should persist across restart, logs: %v", p.Logs)
	}
	if v, ok := m2.kvGet("persist", "token"); !ok || v != "abc123" {
		t.Fatalf("kv should persist across restart, got %q ok=%v", v, ok)
	}
}

// ---- cron 调度：@every 与 onSchedule hook ----

func TestScheduledRunAndOnScheduleHook(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "ticker",
		`{"name":"ticker","version":"1.0.0","description":"d","cron":"@every 1s","events":["onSchedule"],"permissions":{}}`,
		`console.log("boot");
function onSchedule(p) { console.log("tick:" + (typeof p.time === "string")); }`)
	m := newManager(t, dir, nil)
	m.RunTimeout = 2 * time.Second
	m.Start()
	defer m.Stop()

	waitFor(t, 6*time.Second, func() bool { return hasLog(m, "ticker", "tick:true") }, "onSchedule hook via cron")
	if !hasLog(m, "ticker", "boot") {
		t.Fatalf("top-level script should run on scheduled invocation, logs: %v", pluginSnapshot(t, m, "ticker").Logs)
	}
	if pluginSnapshot(t, m, "ticker").RunCount < 1 {
		t.Fatal("scheduled runs should be counted")
	}

	// 停用后不再触发
	m.SetEnabled("ticker", false)
	before := pluginSnapshot(t, m, "ticker").RunCount
	time.Sleep(2300 * time.Millisecond)
	if pluginSnapshot(t, m, "ticker").RunCount != before {
		t.Fatalf("disabled plugin must not run on schedule: before=%d after=%d", before, pluginSnapshot(t, m, "ticker").RunCount)
	}
}

// ---- 手动运行与并发重入 ----

func TestRunRejectsConcurrentInvocation(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "busy",
		`{"name":"busy","version":"1.0.0","description":"d","permissions":{}}`,
		`console.log("start"); while (true) {}`)
	m := newManager(t, dir, nil)
	m.RunTimeout = 5 * time.Second

	done := make(chan error, 1)
	go func() { done <- m.Run("busy") }()
	waitFor(t, 3*time.Second, func() bool { return hasLog(m, "busy", "start") }, "first run started")

	// 并发第二次运行被拒绝（不等待超时）
	start := time.Now()
	err := m.Run("busy")
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("concurrent manual run should be rejected, got: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("concurrent run should fail fast")
	}
	if err := <-done; err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("first run should end with timeout, got: %v", err)
	}
}

// ---- 运行状态展示字段 ----

func TestPluginJSONStatusFields(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "st",
		`{"name":"st","version":"1.0.0","description":"d","events":["onAlert"],"permissions":{"allow_fetch":true,"fetch_domains":["a.test"],"allow_notify":true,"approved":false}}`,
		`console.log("hi")`)
	m := newManager(t, dir, nil)
	if err := m.Run("st"); err != nil {
		t.Fatal(err)
	}
	p := pluginSnapshot(t, m, "st")
	if p.LastStatus != "ok" || p.RunCount != 1 || p.LastRun == "" {
		t.Fatalf("status fields wrong: %+v", p)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, want := range []string{`"last_status"`, `"run_count"`, `"running"`, `"events"`, `"permissions"`, `"fetch_domains"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("JSON-exposed fields missing %q: %s", want, out)
		}
	}
}
