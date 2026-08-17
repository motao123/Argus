package notifyctx

import (
	"testing"
	"time"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/store"
)

// TestRenderVariables 模板变量替换：新增变量与 {{title}}/{{content}} 兼容。
func TestRenderVariables(t *testing.T) {
	ctx := &Ctx{
		Event:          "triggered",
		Title:          "[Argus] node-1 triggered",
		Content:        "cpu-rule: cpu = 95.00",
		Rule:           "cpu-rule",
		Metric:         "cpu",
		Value:          "95.00",
		Threshold:      "90.00",
		Time:           "2026-08-17 10:00:00",
		ServerName:     "node-1",
		ServerID:       7,
		ServerIP:       "1.2.3.4",
		ServerIPv4:     "1.2.3.4",
		ServerIPv6:     "fe80::1",
		ServerPlatform: "linux",
		ServerOnline:   "online",
	}
	tmpl := `{{event}}|{{server.name}}|{{server.id}}|{{server.ip}}|{{server.ipv4}}|{{server.ipv6}}|{{server.platform}}|{{server.online}}|{{rule}}|{{metric}}|{{value}}|{{threshold}}|{{time}}|{{title}}|{{content}}`
	want := "triggered|node-1|7|1.2.3.4|1.2.3.4|fe80::1|linux|online|cpu-rule|cpu|95.00|90.00|2026-08-17 10:00:00|[Argus] node-1 triggered|cpu-rule: cpu = 95.00"
	if got := ctx.Render(tmpl); got != want {
		t.Errorf("Render:\n got %q\nwant %q", got, want)
	}
}

// TestRenderMissingVars 未提供的变量渲染为空字符串。
func TestRenderMissingVars(t *testing.T) {
	ctx := &Ctx{Event: "offline", Title: "t", Content: "c"}
	tmpl := "[{{server.name}}] {{server.ipv4}} {{rule}}={{value}} ({{threshold}}) {{unknown}}"
	if got, want := ctx.Render(tmpl), "[]  = () "; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
	// 空模板 / 无占位符模板
	if got := ctx.Render(""); got != "" {
		t.Errorf("empty template = %q", got)
	}
	if got := ctx.Render("plain text"); got != "plain text" {
		t.Errorf("plain template = %q", got)
	}
	// 未闭合的 {{ 原样保留
	if got := ctx.Render("a {{b c"); got != "a {{b c" {
		t.Errorf("unclosed placeholder = %q", got)
	}
}

// TestRenderValuesNotReScanned 值中出现的 {{ 不二次替换（单遍扫描）。
func TestRenderValuesNotReScanned(t *testing.T) {
	vars := map[string]string{"a": "{{b}}", "b": "B"}
	if got := Render("{{a}}", vars); got != "{{b}}" {
		t.Errorf("Render = %q, want %q", got, "{{b}}")
	}
}

// TestFromState 服务器运行时状态填充 server.* 变量。
func TestFromState(t *testing.T) {
	h := store.NewHub()
	h.Upsert(&model.Server{ID: 9, Name: "srv-a"})
	h.SetReport(9, protocol.HostInfo{Hostname: "srv-a", IP: "10.0.0.1", IPv4: "10.0.0.1", IPv6: "2001:db8::1", Platform: "ubuntu"}, &protocol.ReportParams{})
	st := h.Get(9)
	ctx := (&Ctx{}).FromState(st)
	f := ctx.Flat()
	for k, want := range map[string]string{
		"server.name": "srv-a", "server.id": "9", "server.ip": "10.0.0.1",
		"server.ipv4": "10.0.0.1", "server.ipv6": "2001:db8::1",
		"server.platform": "ubuntu", "server.online": "online",
	} {
		if f[k] != want {
			t.Errorf("Flat[%s] = %q, want %q", k, f[k], want)
		}
	}
	// nil / 无 Server 不 panic
	(&Ctx{}).FromState(nil)
}

func TestMaskIPSetting(t *testing.T) {
	defer SetMaskIP(false)
	h := store.NewHub()
	h.Upsert(&model.Server{ID: 10, Name: "srv-b"})
	h.SetReport(10, protocol.HostInfo{Hostname: "srv-b", IP: "10.0.0.2", IPv4: "10.0.0.2", IPv6: "2001:db8::2", Platform: "debian"}, &protocol.ReportParams{})
	st := h.Get(10)

	SetMaskIP(true)
	ctx := (&Ctx{}).FromState(st)
	f := ctx.Flat()
	for _, k := range []string{"server.ip", "server.ipv4", "server.ipv6"} {
		if f[k] != "xxx.xxx.xxx.xxx" {
			t.Errorf("masked Flat[%s] = %q, want xxx.xxx.xxx.xxx", k, f[k])
		}
	}
	// 其他 server.* 字段不受打码影响
	if f["server.name"] != "srv-b" || f["server.platform"] != "debian" {
		t.Errorf("non-IP fields affected by mask: %v", f)
	}

	SetMaskIP(false)
	ctx = (&Ctx{}).FromState(st)
	if ctx.Flat()["server.ip"] != "10.0.0.2" {
		t.Errorf("mask off: server.ip = %q, want 10.0.0.2", ctx.Flat()["server.ip"])
	}
}

// TestEncodeDecodeRoundTrip 送达记录持久化的 JSON 往返。
func TestEncodeDecodeRoundTrip(t *testing.T) {
	ctx := &Ctx{Event: "triggered", ServerName: "n", ServerID: 3, Extras: map[string]string{"detail": "x"}}
	raw := ctx.Encode()
	got := Decode(raw)
	if got["event"] != "triggered" || got["server.name"] != "n" || got["server.id"] != "3" || got["detail"] != "x" {
		t.Errorf("round trip vars = %v", got)
	}
	// 空表 → 空串；Decode 空/非法 → nil
	if EncodeMap(nil) != "" {
		t.Error("EncodeMap(nil) should be empty")
	}
	if Decode("") != nil || Decode("{bad") != nil {
		t.Error("Decode should return nil for empty/invalid JSON")
	}
}

// TestFormatTime 时间格式化。
func TestFormatTime(t *testing.T) {
	got := FormatTime(time.Date(2026, 8, 17, 9, 5, 3, 0, time.Local))
	want := "2026-08-17 09:05:03"
	if got != want {
		t.Errorf("FormatTime = %q, want %q", got, want)
	}
}
