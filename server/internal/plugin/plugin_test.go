package plugin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---- 测试辅助 ----

// fakeResolver 测试用 DNS 解析器。
type fakeResolver struct{ m map[string][]net.IPAddr }

func (f fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if ips, ok := f.m[host]; ok {
		return ips, nil
	}
	return nil, fmt.Errorf("no such host: %s", host)
}

func ipAddr(s string) net.IPAddr { return net.IPAddr{IP: net.ParseIP(s)} }

// dialTo 把任何目标地址拨号重定向到测试服务器（SSRF 校验发生在 dial 之前）。
func dialTo(ts *httptest.Server) func(ctx context.Context, network, addr string) (net.Conn, error) {
	u, _ := url.Parse(ts.URL)
	target := u.Host
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, target)
	}
}

// writePlugin 在插件根目录写入一个插件（manifest + plugin.js）。
func writePlugin(t *testing.T, dir, name, manifest, js string) {
	t.Helper()
	pdir := filepath.Join(dir, name)
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "plugin.js"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newManager 创建并 Load 插件管理器。
func newManager(t *testing.T, dir string, resolver LookupIPResolver) *Manager {
	t.Helper()
	m := New(dir)
	m.Resolver = resolver
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	return m
}

// dialToServer 注入拨号器：所有目标重定向到本地测试服务器。
func dialToServer(m *Manager, ts *httptest.Server) { m.Dial = dialTo(ts) }

// waitFor 轮询等待条件成立。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout waiting for: " + msg)
}

// pluginSnapshot 通过公开 API 获取插件状态快照。
func pluginSnapshot(t *testing.T, m *Manager, name string) *Plugin {
	t.Helper()
	p, ok := m.Get(name)
	if !ok {
		t.Fatalf("plugin %s not found", name)
	}
	return p
}

// hasLog 插件日志中是否存在包含子串的条目。
func hasLog(m *Manager, name, substr string) bool {
	p, ok := m.Get(name)
	if !ok {
		return false
	}
	return snapshotHasLog(p, substr)
}

func snapshotHasLog(p *Plugin, substr string) bool {
	for _, l := range p.Logs {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// logCount 插件日志中包含子串的条数。
func logCount(m *Manager, name, substr string) int {
	p, ok := m.Get(name)
	if !ok {
		return 0
	}
	n := 0
	for _, l := range p.Logs {
		if strings.Contains(l, substr) {
			n++
		}
	}
	return n
}

// ---- 权限：未声明/未批准拒绝、批准后可用、撤销即时生效 ----

func TestFetchPermissionDeniedThenApproved(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"net","version":"1.0.0","description":"d","cron":"","events":[],"permissions":{"allow_fetch":true,"fetch_domains":["safe.test"],"allow_notify":false,"approved":false}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }))
	defer srv.Close()
	resolver := fakeResolver{m: map[string][]net.IPAddr{"safe.test": {ipAddr("93.184.216.34")}}}
	js := `const r = fetch("http://safe.test/x"); console.log("run:" + (r === undefined ? "DENIED" : r));`
	writePlugin(t, dir, "net", manifest, js)

	m := newManager(t, dir, resolver)
	if _, ok := m.Get("net"); !ok {
		t.Fatal("plugin not loaded")
	}
	// 未批准：拒绝
	if err := m.Run("net"); err != nil {
		t.Fatal(err)
	}
	p := pluginSnapshot(t, m, "net")
	if !snapshotHasLog(p, "run:DENIED") {
		t.Fatalf("unapproved fetch should be denied, logs: %v", p.Logs)
	}
	// 批准后：允许（返回响应文本）
	if !m.SetApproved("net", true) {
		t.Fatal("approve failed")
	}
	dialToServer(m, srv)
	if err := m.Run("net"); err != nil {
		t.Fatal(err)
	}
	p = pluginSnapshot(t, m, "net")
	if !snapshotHasLog(p, "run:OK") {
		t.Fatalf("approved fetch should succeed, logs: %v", p.Logs)
	}
	// 重启后状态持久化
	m2 := New(dir)
	m2.Resolver = resolver
	dialToServer(m2, srv)
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	if !pluginSnapshot(t, m2, "net").Permissions.Approved {
		t.Fatal("approved state should persist across restart")
	}
}

// TestUndeclaredPermissionRejected 未声明的权限调用一律拒绝：
// 未声明 allow_fetch / fetch_domains 白名单、未声明 allow_notify。
func TestUndeclaredPermissionRejected(t *testing.T) {
	dir := t.TempDir()
	// 声明了 allow_fetch 但 fetch_domains 为空：白名单即拒绝
	writePlugin(t, dir, "nofetch",
		`{"name":"nofetch","version":"1.0.0","description":"d","permissions":{"allow_fetch":true,"fetch_domains":[],"approved":true}}`,
		`const r = fetch("http://safe.test/x"); console.log("fetch:" + (r === undefined ? "DENIED" : "OK"));`)
	// 完全未声明 allow_fetch
	writePlugin(t, dir, "undeclared",
		`{"name":"undeclared","version":"1.0.0","description":"d","permissions":{"approved":true}}`,
		`const r = fetch("http://safe.test/x"); console.log("fetch:" + (r === undefined ? "DENIED" : "OK"));`)
	// 未声明 allow_notify：argus.notify 拒绝
	writePlugin(t, dir, "nonotify",
		`{"name":"nonotify","version":"1.0.0","description":"d","permissions":{"approved":true}}`,
		`const ok = argus.notify(1, "t", "c"); console.log("notify:" + ok);`)
	// 声明 allow_notify 但未批准：拒绝
	writePlugin(t, dir, "notifyunapproved",
		`{"name":"notifyunapproved","version":"1.0.0","description":"d","permissions":{"allow_notify":true,"approved":false}}`,
		`const ok = argus.notify(1, "t", "c"); console.log("notify:" + ok);`)

	resolver := fakeResolver{m: map[string][]net.IPAddr{"safe.test": {ipAddr("93.184.216.34")}}}
	m := newManager(t, dir, resolver)

	for _, name := range []string{"nofetch", "undeclared"} {
		if err := m.Run(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !hasLog(m, name, "fetch:DENIED") {
			t.Fatalf("%s: undeclared fetch should be denied, logs: %v", name, pluginSnapshot(t, m, name).Logs)
		}
	}
	for _, name := range []string{"nonotify", "notifyunapproved"} {
		if err := m.Run(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !hasLog(m, name, "notify:false") {
			t.Fatalf("%s: undeclared/unapproved notify should be denied, logs: %v", name, pluginSnapshot(t, m, name).Logs)
		}
	}
}

// TestRevocationImmediate 撤销批准即时生效：同一实例运行中，
// 撤销后下一次 fetch 调用立即被拒绝（权限在每次调用时检查）。
func TestRevocationImmediate(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"live","version":"1.0.0","description":"d","permissions":{"allow_fetch":true,"fetch_domains":["safe.test"],"approved":true}}`
	firstHit := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case firstHit <- struct{}{}:
			// 第一次请求挂起，等待测试撤销权限
			select {
			case <-release:
			case <-time.After(5 * time.Second):
			}
		default:
		}
		w.Write([]byte("OK"))
	}))
	defer srv.Close()
	js := `
const u = "http://safe.test/x";
const r1 = fetch(u);
console.log("first:" + (r1 === undefined ? "DENIED" : "OK"));
const r2 = fetch(u);
console.log("second:" + (r2 === undefined ? "DENIED" : "OK"));
`
	writePlugin(t, dir, "live", manifest, js)
	resolver := fakeResolver{m: map[string][]net.IPAddr{"safe.test": {ipAddr("93.184.216.34")}}}
	m := newManager(t, dir, resolver)
	dialToServer(m, srv)

	done := make(chan error, 1)
	go func() { done <- m.Run("live") }()

	// 等待第一次 fetch 被服务端接住（权限已通过、请求进行中）
	waitFor(t, 5*time.Second, func() bool { return len(firstHit) > 0 }, "first fetch to reach server")
	// 运行中撤销批准
	if !m.SetApproved("live", false) {
		t.Fatal("revoke failed")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	p := pluginSnapshot(t, m, "live")
	if !snapshotHasLog(p, "first:OK") {
		t.Fatalf("first fetch (before revoke) should succeed, logs: %v", p.Logs)
	}
	if !snapshotHasLog(p, "second:DENIED") {
		t.Fatalf("second fetch (after revoke) should be denied immediately, logs: %v", p.Logs)
	}
}

// ---- allow_exec 明确拒绝 ----

func TestAllowExecRejected(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "evil",
		`{"name":"evil","version":"1.0.0","description":"d","permissions":{"allow_exec":true}}`,
		`console.log("pwned")`)
	writePlugin(t, dir, "good",
		`{"name":"good","version":"1.0.0","description":"d","permissions":{}}`,
		`console.log("hi")`)
	m := newManager(t, dir, nil)
	if m.Has("evil") {
		t.Fatal("plugin declaring allow_exec must be refused at load")
	}
	if !m.Has("good") {
		t.Fatal("normal plugin should load")
	}
}

// ---- 标准 5/6 段 cron 解析 ----

func TestCronParsing(t *testing.T) {
	valid := []string{
		"*/5 * * * *",   // 5 段（分钟级）
		"0 */5 * * * *", // 6 段（秒级）
		"30 2 * * *",
		"0 0 1 1 *",
		"@every 30s",
		"@daily",
		"@hourly",
		"@weekly",
		"",
	}
	for _, spec := range valid {
		if err := validateCron(spec); err != nil {
			t.Errorf("cron %q should be valid: %v", spec, err)
		}
	}
	invalid := []string{"61 * * * *", "* * *", "bad spec", "@every", "0 0 0 0 0 0 0"}
	for _, spec := range invalid {
		if err := validateCron(spec); err == nil {
			t.Errorf("cron %q should be invalid", spec)
		}
	}
}
