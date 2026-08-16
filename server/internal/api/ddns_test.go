package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/ddns"
	"github.com/motao123/Argus/server/internal/model"
)

// ddnsDo 发起带身份和可选 RemoteAddr 的请求（RemoteAddr 模拟 API 调用者来源 IP）。
func ddnsDo(t *testing.T, e *authzTestEnv, method, path, token, body, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("{}")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.GET("/ddns", e.srv.listDDNS)
	authed.POST("/ddns", e.srv.createDDNS)
	authed.PUT("/ddns/:id", e.srv.updateDDNS)
	authed.DELETE("/ddns/:id", e.srv.deleteDDNS)
	authed.POST("/ddns/:id/test", e.srv.testDDNS)
	r.ServeHTTP(w, req)
	return w
}

func loadDDNSStates(t *testing.T, e *authzTestEnv, profileID int64) map[string]model.DDNSRecordState {
	t.Helper()
	var states []model.DDNSRecordState
	if err := e.srv.DB.Where("profile_id=?", profileID).Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	m := map[string]model.DDNSRecordState{}
	for _, st := range states {
		m[st.Domain+"|"+st.RecordType] = st
	}
	return m
}

// TestDDNSPartialFailureRetryAndStop 覆盖：部分失败（同 profile 内独立状态）、
// 429/500 → retrying + 指数退避、401 → stopped 停止自动重试、重启恢复、IP 变化自愈。
func TestDDNSPartialFailureRetryAndStop(t *testing.T) {
	e := newAuthzEnv(t)
	var hits atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !fail.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		dom := r.URL.Query().Get("domain")
		rt := r.URL.Query().Get("type")
		switch {
		case dom == "good.example" && rt == "A":
			w.WriteHeader(http.StatusOK)
		case dom == "good.example" && rt == "AAAA":
			w.WriteHeader(http.StatusTooManyRequests) // 429 → 临时失败 → retrying
		case dom == "bad.example" && rt == "A":
			w.WriteHeader(http.StatusInternalServerError) // 500 → 临时失败 → retrying
		default:
			w.WriteHeader(http.StatusUnauthorized) // 401 → stopped
		}
	}))
	defer ts.Close()
	e.srv.DDNS = ddns.NewClient(ts.Client())

	profile := model.DDNSProfile{
		OwnerID: e.alice.ID, ServerID: e.aliceS.ID, Name: "p", Provider: "webhook",
		RecordType: "dual", Domains: "good.example,bad.example", Enabled: true,
		WebhookMethod: "GET", WebhookHeaders: "{}",
		WebhookURL: ts.URL + "/u?domain={domain}&type={record_type}&ip={ip}",
	}
	if err := e.srv.DB.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.srv.syncDDNSRecords(&profile); err != nil {
		t.Fatal(err)
	}
	ips := map[string]string{"A": "192.0.2.77", "AAAA": "2001:db8::77"}

	// 第一轮：部分失败。
	e.srv.runDDNSProfile(&profile, ips, false)
	if got := hits.Load(); got != 4 {
		t.Fatalf("run1: %d provider calls, want 4", got)
	}
	states := loadDDNSStates(t, e, profile.ID)
	if s := states["good.example|A"]; s.Status != "success" || s.LastIP != "192.0.2.77" || s.RetryCount != 0 || s.NextRetry != nil || s.LastError != "" {
		t.Fatalf("good A: %+v", s)
	}
	for name, wantErr := range map[string]string{
		"good.example|AAAA": "429",
		"bad.example|A":     "500",
		"bad.example|AAAA":  "", // 401 是 unauthorized，见下方断言
	} {
		s := states[name]
		if s.LastAttempt == nil {
			t.Fatalf("%s: no last_attempt", name)
		}
		if s.LastError == "" {
			t.Fatalf("%s: no last_error", name)
		}
		if wantErr != "" && !strings.Contains(s.LastError, wantErr) {
			t.Fatalf("%s: error=%q want contains %q", name, s.LastError, wantErr)
		}
	}
	retry := states["good.example|AAAA"]
	if retry.Status != "retrying" || retry.RetryCount != 1 {
		t.Fatalf("good AAAA after run1: %+v", retry)
	}
	if d := retry.NextRetry.Sub(*retry.LastAttempt); d < 25*time.Second || d > 35*time.Second {
		t.Fatalf("good AAAA first backoff=%v, want ~30s", d)
	}
	stopped := states["bad.example|AAAA"]
	if stopped.Status != "stopped" || stopped.NextRetry != nil || !strings.Contains(stopped.LastError, ddns.ErrUnauthorized.Error()) {
		t.Fatalf("bad AAAA after run1: %+v", stopped)
	}

	// 第二轮：退避未到期 → 不重试（0 次调用，证明 next_retry 门控）。
	e.srv.runDDNSProfile(&profile, ips, false)
	if got := hits.Load(); got != 4 {
		t.Fatalf("run2 (backoff pending): %d provider calls, want 4 (next_retry 未到期)", got)
	}
	// 模拟时间流逝（重启后 next_retry 过期）：仅 retrying 记录重试，退避翻倍。
	past := time.Now().Add(-time.Minute)
	if err := e.srv.DB.Model(&model.DDNSRecordState{}).
		Where("profile_id=? AND status=?", profile.ID, "retrying").
		Update("next_retry", past).Error; err != nil {
		t.Fatal(err)
	}
	e.srv.runDDNSProfile(&profile, ips, false)
	if got := hits.Load(); got != 6 {
		t.Fatalf("run3: %d provider calls, want 6 (401/success 不重试)", got)
	}
	retry2 := loadDDNSStates(t, e, profile.ID)["good.example|AAAA"]
	if retry2.Status != "retrying" || retry2.RetryCount != 2 {
		t.Fatalf("good AAAA after run2: %+v", retry2)
	}
	if d := retry2.NextRetry.Sub(*retry2.LastAttempt); d < 55*time.Second || d > 65*time.Second {
		t.Fatalf("good AAAA second backoff=%v, want ~60s", d)
	}
	if s := loadDDNSStates(t, e, profile.ID)["bad.example|AAAA"]; s.Status != "stopped" {
		t.Fatalf("bad AAAA re-attempted after stop: %+v", s)
	}

	// 重启恢复：provider 恢复正常后，把过期重试置为过去并触发 RunDDNSRetries。
	e.srv.Store.Upsert(&e.aliceS).Host = protocol.HostInfo{IPv4: ips["A"], IPv6: ips["AAAA"]}
	fail.Store(false)
	if err := e.srv.DB.Model(&model.DDNSRecordState{}).
		Where("profile_id=? AND status=?", profile.ID, "retrying").
		Update("next_retry", past).Error; err != nil {
		t.Fatal(err)
	}
	e.srv.RunDDNSRetries()
	if got := hits.Load(); got != 8 {
		t.Fatalf("recovery: %d provider calls, want 8 (stopped 仍跳过)", got)
	}
	states = loadDDNSStates(t, e, profile.ID)
	for _, key := range []string{"good.example|A", "good.example|AAAA", "bad.example|A"} {
		if s := states[key]; s.Status != "success" || s.RetryCount != 0 || s.NextRetry != nil {
			t.Fatalf("%s after recovery: %+v", key, s)
		}
	}
	if s := states["bad.example|AAAA"]; s.Status != "stopped" {
		t.Fatalf("bad AAAA after recovery: %+v", s)
	}

	// IP 变化自愈：syncDDNSRecords 重置 stopped → pending，重新尝试并成功。
	e.srv.HandleServerIPChange(e.aliceS.ID, protocol.HostInfo{IPv4: "192.0.2.78", IPv6: "2001:db8::78"})
	if got := hits.Load(); got != 12 {
		t.Fatalf("ip change: %d provider calls, want 12 (全部 4 条更新)", got)
	}
	states = loadDDNSStates(t, e, profile.ID)
	for _, key := range []string{"good.example|A", "good.example|AAAA", "bad.example|A", "bad.example|AAAA"} {
		if s := states[key]; s.Status != "success" || s.NextRetry != nil {
			t.Fatalf("%s after ip change: %+v", key, s)
		}
	}
	if s := states["bad.example|AAAA"]; s.LastIP != "2001:db8::78" {
		t.Fatalf("bad AAAA last_ip=%q", s.LastIP)
	}
}

// TestDDNSTestUsesAgentIP 验证 /ddns/:id/test 使用 Agent 上报的 IP，而不是调用者来源 IP。
func TestDDNSTestUsesAgentIP(t *testing.T) {
	e := newAuthzEnv(t)
	var mu sync.Mutex
	seenIPs := map[string]bool{}
	var seenDomain atomic.Value
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenIPs[r.URL.Query().Get("ip")] = true
		mu.Unlock()
		seenDomain.Store(r.URL.Query().Get("domain"))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	e.srv.DDNS = ddns.NewClient(ts.Client())
	e.srv.Store.Upsert(&e.aliceS).Host = protocol.HostInfo{IPv4: "192.0.2.77", IPv6: "2001:db8::77", IP: "192.0.2.77"}

	tok := e.token(t, e.alice)
	w := ddnsDo(t, e, http.MethodPost, "/ddns", tok, fmt.Sprintf(`{"server_id":%d,"name":"p","provider":"webhook","record_type":"dual","domains":"h.example","webhook_url":%q,"webhook_method":"GET","webhook_headers":"{}","webhook_body":""}`, e.aliceS.ID, ts.URL+"/u?ip={ip}&domain={domain}"), "")
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created.Data.ID

	// 调用者 IP 与 Agent IP 不同：测试必须使用 Agent IP。
	w = ddnsDo(t, e, http.MethodPost, fmt.Sprintf("/ddns/%d/test", id), tok, "", "203.0.113.9:1234")
	if w.Code != http.StatusOK {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	mu.Lock()
	ips := make([]string, 0, len(seenIPs))
	for ip := range seenIPs {
		ips = append(ips, ip)
	}
	mu.Unlock()
	if !seenIPs["192.0.2.77"] || !seenIPs["2001:db8::77"] {
		t.Fatalf("webhook received ips=%v, want agent 192.0.2.77 + 2001:db8::77 (not caller)", ips)
	}
	if seenIPs["203.0.113.9"] {
		t.Fatalf("webhook received caller ip 203.0.113.9, must use agent IP")
	}
	if d, _ := seenDomain.Load().(string); d != "h.example" {
		t.Fatalf("webhook received domain=%q", d)
	}
	var resp struct {
		Data struct {
			IPv4    string                  `json:"ipv4"`
			IPv6    string                  `json:"ipv6"`
			Records []model.DDNSRecordState `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.IPv4 != "192.0.2.77" || resp.Data.IPv6 != "2001:db8::77" {
		t.Fatalf("test response ipv4=%q ipv6=%q", resp.Data.IPv4, resp.Data.IPv6)
	}
	if len(resp.Data.Records) != 2 {
		t.Fatalf("records=%d, want 2 (A+AAAA)", len(resp.Data.Records))
	}
	for _, r := range resp.Data.Records {
		if r.Status != "success" {
			t.Fatalf("%s %s: %+v", r.Domain, r.RecordType, r)
		}
		ip := "192.0.2.77"
		if r.RecordType == "AAAA" {
			ip = "2001:db8::77"
		}
		if r.LastIP != ip {
			t.Fatalf("%s: last_ip=%q want %q", r.RecordType, r.LastIP, ip)
		}
	}
}

// TestDDNSOwnerIsolationRedactionAudit 覆盖 owner 隔离、脱敏和审计。
func TestDDNSOwnerIsolationRedactionAudit(t *testing.T) {
	e := newAuthzEnv(t)
	aliceTok := e.token(t, e.alice)
	bobTok := e.token(t, e.bob)

	w := ddnsDo(t, e, http.MethodPost, "/ddns", aliceTok, fmt.Sprintf(`{"server_id":%d,"name":"p","provider":"webhook","record_type":"A","domains":"a.example","access_key":"cf-secret-token","webhook_url":"https://example.com/hook","webhook_method":"POST","webhook_headers":%q,"webhook_body":"payload {ip}"}`, e.aliceS.ID, `{"Authorization":"Bearer tok"}`), "")
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created.Data.ID

	// Bob 看不到 Alice 的 profile。
	w = ddnsDo(t, e, http.MethodGet, "/ddns", bobTok, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("bob list: %d", w.Code)
	}
	var bobList struct {
		Data struct {
			Profiles []model.DDNSProfile `json:"profiles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bobList); err != nil {
		t.Fatal(err)
	}
	if len(bobList.Data.Profiles) != 0 {
		t.Fatalf("bob sees %d profiles, want 0", len(bobList.Data.Profiles))
	}

	// Bob 不能改/删/测 Alice 的 profile。
	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, fmt.Sprintf("/ddns/%d", id)},
		{http.MethodDelete, fmt.Sprintf("/ddns/%d", id)},
		{http.MethodPost, fmt.Sprintf("/ddns/%d/test", id)},
	} {
		if w := ddnsDo(t, e, tc.method, tc.path, bobTok, "", ""); w.Code != http.StatusForbidden {
			t.Fatalf("bob %s %s: %d, want 403", tc.method, tc.path, w.Code)
		}
	}

	// 创建时必须提供 cloudflare token（webhook 无 URL 按既有契约允许创建）。
	if w := ddnsDo(t, e, http.MethodPost, "/ddns", aliceTok, fmt.Sprintf(`{"server_id":%d,"name":"p2","provider":"cloudflare","record_type":"A","domains":"b.example","access_key":"","webhook_url":"","webhook_method":"GET","webhook_headers":"{}","webhook_body":""}`, e.aliceS.ID), ""); w.Code != http.StatusBadRequest {
		t.Fatalf("create without token: %d, want 400", w.Code)
	}

	// Alice 列表：secret 字段全部脱敏。
	w = ddnsDo(t, e, http.MethodGet, "/ddns", aliceTok, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("alice list: %d", w.Code)
	}
	var aliceList struct {
		Data struct {
			Profiles []struct {
				ID             int64                   `json:"id"`
				AccessKey      string                  `json:"access_key"`
				WebhookURL     string                  `json:"webhook_url"`
				WebhookHeaders string                  `json:"webhook_headers"`
				WebhookBody    string                  `json:"webhook_body"`
				Records        []model.DDNSRecordState `json:"records"`
			} `json:"profiles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &aliceList); err != nil {
		t.Fatal(err)
	}
	if len(aliceList.Data.Profiles) != 1 {
		t.Fatalf("alice sees %d profiles, want 1", len(aliceList.Data.Profiles))
	}
	p := aliceList.Data.Profiles[0]
	if p.AccessKey != "" {
		t.Fatalf("access_key leaked: %q", p.AccessKey)
	}
	if p.WebhookURL != redactedSecret {
		t.Fatalf("webhook_url not redacted: %q", p.WebhookURL)
	}
	if p.WebhookHeaders != redactedSecret {
		t.Fatalf("webhook_headers not redacted: %q", p.WebhookHeaders)
	}
	if p.WebhookBody != redactedSecret {
		t.Fatalf("webhook_body not redacted: %q", p.WebhookBody)
	}
	if len(p.Records) != 1 {
		t.Fatalf("records=%d, want 1", len(p.Records))
	}

	// 审计日志：create 已落库且带操作者信息。
	var logs []model.AuditLog
	if err := e.srv.DB.Where("action = ?", "ddns.create").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("audit ddns.create rows=%d, want 1", len(logs))
	}
	if logs[0].UserID != e.alice.ID || !strings.Contains(logs[0].Detail, "name=p") {
		t.Fatalf("audit row: %+v", logs[0])
	}
}

// TestRetryDelayBackoffCap 验证指数退避与 1 小时上限。
func TestRetryDelayBackoffCap(t *testing.T) {
	if d := retryDelay(1); d != 30*time.Second {
		t.Fatalf("retryDelay(1)=%v", d)
	}
	if d := retryDelay(2); d != 60*time.Second {
		t.Fatalf("retryDelay(2)=%v", d)
	}
	if d := retryDelay(3); d != 120*time.Second {
		t.Fatalf("retryDelay(3)=%v", d)
	}
	if d := retryDelay(10); d != time.Hour {
		t.Fatalf("retryDelay(10)=%v, want 1h cap", d)
	}
}
