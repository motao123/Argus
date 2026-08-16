package plugin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginPermissionDeniedThenApproved(t *testing.T) {
	dir := t.TempDir()
	// 插件：声明 allow_fetch 但未批准
	pdir := filepath.Join(dir, "test")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"test","version":"1.0.0","description":"d","permissions":{"allow_fetch":true,"approved":false}}`
	if err := os.WriteFile(filepath.Join(pdir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }))
	defer srv.Close()
	if err := os.WriteFile(filepath.Join(pdir, "plugin.js"), []byte(`const r = fetch("`+srv.URL+`"); console.log("run:" + (r === undefined ? "DENIED" : "OK"))`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(dir)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	p := m.plugins["test"]
	if p == nil {
		t.Fatal("plugin not loaded")
	}
	if !p.Permissions.AllowFetch || p.Permissions.Approved {
		t.Fatal("expect allow_fetch true, approved false")
	}
	// 未批准：fetch 调用返回 undefined（拒绝）
	if err := m.Run("test"); err != nil {
		t.Fatal(err)
	}
	if len(p.Logs) < 2 || !strings.HasSuffix(p.Logs[len(p.Logs)-1], "run:DENIED") {
		t.Fatalf("unapproved fetch should be denied, logs: %v", p.Logs)
	}
	// 批准后：fetch 可用（返回响应文本）
	if !m.SetApproved("test", true) {
		t.Fatal("approve failed")
	}
	if !p.Permissions.Approved {
		t.Fatal("approved not set")
	}
	if err := m.Run("test"); err != nil {
		t.Fatal(err)
	}
	if len(p.Logs) < 2 || !strings.HasSuffix(p.Logs[len(p.Logs)-1], "run:OK") {
		t.Fatalf("approved fetch should succeed, logs: %v", p.Logs)
	}
	// 重启后状态持久化
	m2 := New(dir)
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	if !m2.plugins["test"].Permissions.Approved {
		t.Fatal("approved state should persist across restart")
	}
}
