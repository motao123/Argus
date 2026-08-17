package plugin

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSSRFScenarios fetch 的 SSRF 防护各场景：
// 回环/私网/链路本地（含云元数据 169.254.169.254）/组播/未指定、
// DNS 解析到内网、非 http(s) scheme、重定向复查、正常公网请求放行。
func TestSSRFScenarios(t *testing.T) {
	// 重定向服务：/redir → priv.test（内网）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, "http://priv.test/y", http.StatusFound)
			return
		}
		w.Write([]byte("BODY"))
	}))
	defer srv.Close()

	resolver := fakeResolver{m: map[string][]net.IPAddr{
		"safe.test":   {ipAddr("93.184.216.34")},
		"priv.test":   {ipAddr("10.0.0.5")},
		"meta.test":   {ipAddr("169.254.169.254")},
		"link.test":   {ipAddr("169.254.1.1")},
		"mcast.test":  {ipAddr("224.0.0.1")},
		"local.test":  {ipAddr("127.0.0.1")},
		"unspec.test": {ipAddr("0.0.0.0")},
	}}

	cases := []struct {
		name string
		url  string
		want string // 期望日志内容（含 "DENIED" 或 "OK"）
	}{
		{"loopback literal", "http://127.0.0.1:8080/x", "DENIED"},
		{"private literal", "http://10.1.2.3/x", "DENIED"},
		{"private literal 192.168", "http://192.168.1.1/x", "DENIED"},
		{"metadata literal", "http://169.254.169.254/latest/meta-data/", "DENIED"},
		{"link-local literal", "http://169.254.3.4/x", "DENIED"},
		{"multicast literal", "http://224.0.0.1/x", "DENIED"},
		{"unspecified literal", "http://0.0.0.0/x", "DENIED"},
		{"localhost name", "http://localhost/x", "DENIED"},
		{"dns to private", "http://priv.test/x", "DENIED"},
		{"dns to metadata", "http://meta.test/latest/meta-data/", "DENIED"},
		{"dns to link-local", "http://link.test/x", "DENIED"},
		{"dns to multicast", "http://mcast.test/x", "DENIED"},
		{"dns to loopback", "http://local.test/x", "DENIED"},
		{"dns to unspecified", "http://unspec.test/x", "DENIED"},
		{"non-http scheme", "ftp://safe.test/x", "DENIED"},
		{"userinfo", "http://user:pass@safe.test/x", "DENIED"},
		{"redirect to private", "http://safe.test/redir", "DENIED"},
		{"public ok", "http://safe.test/x", "OK"},
	}

	manifest := `{"name":"ssrf","version":"1.0.0","description":"d","permissions":{"allow_fetch":true,"fetch_domains":["safe.test","priv.test","meta.test","link.test","mcast.test","local.test","unspec.test","localhost"],"approved":true}}`

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writePlugin(t, dir, "ssrf", manifest,
				`const r = fetch("`+tc.url+`"); console.log("RESULT:" + (r === undefined ? "DENIED" : r));`)
			m := newManager(t, dir, resolver)
			dialToServer(m, srv)
			if err := m.Run("ssrf"); err != nil {
				t.Fatalf("run: %v", err)
			}
			p := pluginSnapshot(t, m, "ssrf")
			found := false
			for _, l := range p.Logs {
				if !strings.Contains(l, "RESULT:") {
					continue
				}
				found = true
				if tc.want == "OK" {
					if !strings.Contains(l, "RESULT:BODY") {
						t.Fatalf("expected OK body, logs: %v", p.Logs)
					}
				} else if !strings.Contains(l, "RESULT:DENIED") {
					t.Fatalf("expected DENIED, logs: %v", p.Logs)
				}
			}
			if !found {
				t.Fatalf("no RESULT log, logs: %v", p.Logs)
			}
		})
	}
}

// TestSSRFDomainAllowlist 域名白名单：白名单外主机即使公网也拒绝。
func TestSSRFDomainAllowlist(t *testing.T) {
	dir := t.TempDir()
	// 只允许 safe.test；请求 other.test（公网解析）应被白名单拒绝
	manifest := `{"name":"wl","version":"1.0.0","description":"d","permissions":{"allow_fetch":true,"fetch_domains":["safe.test"],"approved":true}}`
	writePlugin(t, dir, "wl", manifest,
		`const r = fetch("http://other.test/x"); console.log("RESULT:" + (r === undefined ? "DENIED" : r));`)
	resolver := fakeResolver{m: map[string][]net.IPAddr{
		"other.test": {ipAddr("93.184.216.34")},
		"safe.test":  {ipAddr("93.184.216.34")},
	}}
	m := newManager(t, dir, resolver)
	if err := m.Run("wl"); err != nil {
		t.Fatal(err)
	}
	if !hasLog(m, "wl", "RESULT:DENIED") {
		t.Fatalf("off-allowlist host should be denied, logs: %v", pluginSnapshot(t, m, "wl").Logs)
	}
}

// TestSSRFSubdomainAllowlist 子域匹配：bar.safe.test 命中 safe.test 白名单。
func TestSSRFSubdomainAllowlist(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"name":"sub","version":"1.0.0","description":"d","permissions":{"allow_fetch":true,"fetch_domains":["safe.test"],"approved":true}}`
	writePlugin(t, dir, "sub", manifest,
		`const r = fetch("http://bar.safe.test/x"); console.log("RESULT:" + (r === undefined ? "DENIED" : r));`)
	resolver := fakeResolver{m: map[string][]net.IPAddr{"bar.safe.test": {ipAddr("93.184.216.34")}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }))
	defer srv.Close()
	m := newManager(t, dir, resolver)
	dialToServer(m, srv)
	if err := m.Run("sub"); err != nil {
		t.Fatal(err)
	}
	if !hasLog(m, "sub", "RESULT:OK") {
		t.Fatalf("subdomain should match allowlist, logs: %v", pluginSnapshot(t, m, "sub").Logs)
	}
}
