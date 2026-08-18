package plugin

import (
	"fmt"
	"testing"
	"time"
)

// TestHostRPCRegisterDispatch 验证 argus.registerRPC：方法注册 + HTTP 调用派发返回结果。
func TestHostRPCRegisterDispatch(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"rpc-demo","version":"1.0.0","permissions":{"allow_rpc":true,"approved":true}}`
	js := `
argus.registerRPC("echo", function(p){ return {ok:true, echoed:p && p.msg || ""}; });
argus.registerRPC("add", function(p){ return (p.a||0) + (p.b||0); });
`
	writePlugin(t, dir, "rpc-demo", manifest, js)
	m := newManager(t, dir, nil)
	if !m.Has("rpc-demo") {
		t.Fatal("plugin not loaded")
	}
	// 先运行一次以填充 RPC 展示元数据
	if err := m.Run("rpc-demo"); err != nil {
		t.Fatal(err)
	}
	// 列表应展示 RPC 方法
	rpcs := m.RPCs("rpc-demo")
	if len(rpcs) != 2 {
		t.Fatalf("RPCs() = %v, want 2 methods", rpcs)
	}
	// 派发调用
	res, err := m.CallRPC("rpc-demo", "echo", map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := res.(map[string]any)
	if !ok || obj["ok"] != true || obj["echoed"] != "hi" {
		t.Fatalf("echo result = %#v", res)
	}
	sum, err := m.CallRPC("rpc-demo", "add", map[string]any{"a": 2, "b": 3})
	if err != nil || fmt.Sprintf("%v", sum) != "5" {
		t.Fatalf("add result = %v, err=%v", sum, err)
	}
	// 未注册方法 → 错误
	if _, err := m.CallRPC("rpc-demo", "missing", nil); err == nil {
		t.Fatal("missing method should error")
	}
}

// TestHostRPCPermissionDenied 验证未声明 allow_rpc / 未批准时 registerRPC 被拒。
func TestHostRPCPermissionDenied(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"no-rpc","version":"1.0.0","permissions":{"approved":true}}`
	js := `argus.registerRPC("x", function(p){ return "ok"; });`
	writePlugin(t, dir, "no-rpc", manifest, js)
	m := newManager(t, dir, nil)
	// registerRPC 返回 false → 不注册；调用应失败
	if _, err := m.CallRPC("no-rpc", "x", nil); err == nil {
		t.Fatal("call without allow_rpc should fail")
	}
}

// TestHostRoute 验证 argus.route：注册 + DispatchRoute 派发返回响应。
func TestHostRoute(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"route-demo","version":"1.0.0","permissions":{"allow_routes":true,"approved":true}}`
	js := `
argus.route("GET", "/hello", function(req, res){
  res.statusCode = 200;
  res.headers = {"content-type":"text/plain; charset=utf-8"};
  res.body = "hello " + (req.query.name && req.query.name[0] || "world");
});
`
	writePlugin(t, dir, "route-demo", manifest, js)
	m := newManager(t, dir, nil)
	if err := m.Run("route-demo"); err != nil {
		t.Fatal(err)
	}
	if routes := m.Routes("route-demo"); len(routes) != 1 || routes[0] != "GET /hello" {
		t.Fatalf("Routes() = %v", routes)
	}
	res, err := m.DispatchRoute("route-demo", "GET", "/hello", &RouteRequest{Method: "GET", Path: "/hello", Query: map[string][]string{"name": {"argus"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || res.Body != "hello argus" {
		t.Fatalf("route result = %+v", res)
	}
	if res.Headers["content-type"] == "" {
		t.Fatal("content-type header missing")
	}
	// 未注册路径 → 404 错误
	if _, err := m.DispatchRoute("route-demo", "GET", "/nope", &RouteRequest{}); err == nil {
		t.Fatal("unregistered path should error")
	}
}

// TestHostScriptCron 验证 argus.cron：注册并触发一次（@every 1s）。
func TestHostScriptCron(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"cron-demo","version":"1.0.0","permissions":{"allow_cron":true,"approved":true}}`
	js := `
argus.kv.set("hits", "0");
argus.cron("@every 1s", function(){ var n = parseInt(argus.kv.get("hits")||"0",10)||0; argus.kv.set("hits", String(n+1)); });
`
	writePlugin(t, dir, "cron-demo", manifest, js)
	m := newManager(t, dir, nil)
	m.Start()
	defer m.Stop()
	// 手动运行一次注册调度
	if err := m.Run("cron-demo"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool {
		v, _ := m.kvGet("cron-demo", "hits")
		return v != "0"
	}, "argus.cron should fire at least once")
}

// TestHostConfig 验证 argus.config：manifest 默认值 + SetConfig 覆盖 + 合并。
func TestHostConfig(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"cfg-demo","version":"1.0.0","permissions":{"approved":true},
"configuration":[{"key":"interval","label":"Interval","type":"number","default":60},
{"key":"verbose","label":"Verbose","type":"boolean","default":false},
{"key":"region","label":"Region","type":"select","default":"cn","options":["cn","us"]}]}`
	js := `globalThis.__cfg = JSON.stringify(argus.config());`
	writePlugin(t, dir, "cfg-demo", manifest, js)
	m := newManager(t, dir, nil)
	cfg := m.Config("cfg-demo")
	if cfg["interval"] != float64(60) || cfg["verbose"] != false || cfg["region"] != "cn" {
		t.Fatalf("defaults = %#v", cfg)
	}
	// 覆盖（类型强制）
	if err := m.SetConfig("cfg-demo", map[string]any{"interval": "90", "verbose": "true", "region": "us", "unknown": "x"}); err != nil {
		t.Fatal(err)
	}
	cfg = m.Config("cfg-demo")
	if cfg["interval"] != float64(90) || cfg["verbose"] != true || cfg["region"] != "us" {
		t.Fatalf("after override = %#v", cfg)
	}
	if _, ok := cfg["unknown"]; ok {
		t.Fatal("unknown config key should be dropped")
	}
}

// TestHostHTMLInject 验证 html_head/html_body 仅对启用 + 批准插件生效。
func TestHostHTMLInject(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"inj-demo","version":"1.0.0","permissions":{"approved":true},"html_head":"<style>.x{}</style>","html_body":"<script>init()</script>"}`
	writePlugin(t, dir, "inj-demo", manifest, jsStub())
	m := newManager(t, dir, nil)
	if head, body := m.HTMLInject(); head != "<style>.x{}</style>\n" || body != "<script>init()</script>\n" {
		t.Fatalf("inject = %q / %q", head, body)
	}
	// 停用后不再注入
	m.SetEnabled("inj-demo", false)
	if head, _ := m.HTMLInject(); head != "" {
		t.Fatalf("disabled plugin should not inject, got %q", head)
	}
}

// TestHostScriptCronDuplicate 验证 argus.cron 不重复调度（多次运行只调度一次）。
func TestHostScriptCronDuplicate(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"dup-cron","version":"1.0.0","permissions":{"allow_cron":true,"approved":true}}`
	js := `
argus.kv.set("hits", "0");
argus.cron("@every 1s", function(){ var n = parseInt(argus.kv.get("hits")||"0",10)||0; argus.kv.set("hits", String(n+1)); });
`
	writePlugin(t, dir, "dup-cron", manifest, js)
	m := newManager(t, dir, nil)
	m.Start()
	defer m.Stop()
	for i := 0; i < 3; i++ {
		if err := m.Run("dup-cron"); err != nil {
			t.Fatal(err)
		}
	}
	m.mu.Lock()
	count := len(m.scriptCron)
	m.mu.Unlock()
	if count != 1 {
		t.Fatalf("scriptCron entries = %d, want 1 (no duplicate scheduling)", count)
	}
	waitFor(t, 5*time.Second, func() bool {
		v, _ := m.kvGet("dup-cron", "hits")
		return v != "0"
	}, "cron should fire")
}

func jsStub() string { return "// stub\n" }
