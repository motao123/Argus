package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/server/internal/ddns"
	"github.com/motao123/Argus/server/internal/model"
)

// batchDo 发起带身份的批量接口请求。
func batchDo(t *testing.T, e *authzTestEnv, method, path, token, body string) *httptest.ResponseRecorder {
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
	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.POST("/batch-config/servers", requireAdmin(), e.srv.batchConfigServers)
	authed.POST("/batch-ddns/servers", requireAdmin(), e.srv.batchDDNSServers)
	authed.GET("/clipboard", e.srv.listClipboard)
	authed.POST("/clipboard", e.srv.createClipboard)
	authed.PUT("/clipboard/:id", e.srv.updateClipboard)
	authed.DELETE("/clipboard/:id", e.srv.deleteClipboard)
	r.ServeHTTP(w, req)
	return w
}

type batchResults struct {
	Results []batchServerResult `json:"results"`
}

func decodeBatch(t *testing.T, w *httptest.ResponseRecorder) batchResults {
	t.Helper()
	var resp struct {
		Data batchResults `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data
}

func TestBatchConfigAdminOnlyAndValidation(t *testing.T) {
	e := newAuthzEnv(t)
	userTok := e.token(t, e.alice)
	adminTok := e.token(t, e.admin)

	// 非 admin → 403
	if w := batchDo(t, e, http.MethodPost, "/batch-config/servers", userTok, `{"ids":[1]}`); w.Code != http.StatusForbidden {
		t.Fatalf("user batch-config: got %d want 403", w.Code)
	}
	// 空 ids → 400
	if w := batchDo(t, e, http.MethodPost, "/batch-config/servers", adminTok, `{"ids":[]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty ids: got %d want 400", w.Code)
	}
	// 合法请求：aliceS 在库但离线 → offline；不存在的 ID → not_found
	w := batchDo(t, e, http.MethodPost, "/batch-config/servers", adminTok, `{"ids":[`+itoa(e.aliceS.ID)+`,99999],"interval":5,"interface_include":["eth0"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("batch-config: got %d %s", w.Code, w.Body.String())
	}
	res := decodeBatch(t, w)
	if len(res.Results) != 2 {
		t.Fatalf("results=%d want 2", len(res.Results))
	}
	if res.Results[0].ServerID != e.aliceS.ID || res.Results[0].Status != "offline" || res.Results[0].ServerName != "alice-srv" {
		t.Fatalf("offline result: %+v", res.Results[0])
	}
	if res.Results[1].ServerID != 99999 || res.Results[1].Status != "not_found" {
		t.Fatalf("not_found result: %+v", res.Results[1])
	}
	// 审计日志已落库
	var logs []model.AuditLog
	if err := e.srv.DB.Where("action = ?", "server.batch_config").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || !strings.Contains(logs[0].Detail, "targets=2") {
		t.Fatalf("audit logs: %+v", logs)
	}
}

// fakeAgent 通过真实 WebSocket 注册进 agent.Hub，可应答 server→agent 调用。
type fakeAgent struct {
	applyConfigs atomic.Int32
	lastCfg      atomic.Value // protocol.AgentConfig
	peer         *rpc.Peer
}

func (fa *fakeAgent) handle(method string, params json.RawMessage) (any, *protocol.RPCError) {
	switch method {
	case protocol.MethodApplyConfig:
		fa.applyConfigs.Add(1)
		var cfg protocol.AgentConfig
		_ = json.Unmarshal(params, &cfg)
		fa.lastCfg.Store(cfg)
		return map[string]any{"ok": true}, nil
	}
	return nil, protocol.NewError(protocol.ErrMethod, "unexpected method: "+method)
}

// connectFakeAgent 连接一台假 Agent（用服务器密钥注册进 Hub），返回其客户端句柄。
func connectFakeAgent(t *testing.T, e *authzTestEnv, secret string) *fakeAgent {
	t.Helper()
	up := websocket.Upgrader{}
	accepted := make(chan *websocket.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	t.Cleanup(s.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	serverConn := <-accepted
	go e.srv.Agents.Serve(serverConn)
	fa := &fakeAgent{}
	peer := rpc.New(conn, handlerFunc(fa.handle))
	go peer.ReadLoop()
	fa.peer = peer
	t.Cleanup(func() { _ = peer.Close() })
	resp, err := peer.Call(protocol.MethodRegister, protocol.RegisterParams{Secret: secret}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("register failed: %s", resp.Error.Message)
	}
	return fa
}

type handlerFunc func(method string, params json.RawMessage) (any, *protocol.RPCError)

func (f handlerFunc) Handle(method string, params json.RawMessage) (any, *protocol.RPCError) {
	return f(method, params)
}

func TestBatchConfigOnlineAppliesFields(t *testing.T) {
	e := newAuthzEnv(t)
	adminTok := e.token(t, e.admin)
	fa := connectFakeAgent(t, e, e.aliceS.Secret)

	capabilities := protocol.Capabilities{Metrics: true, Command: true}
	body := `{"ids":[` + itoa(e.aliceS.ID) + `],"server_url":"wss://example.com/ws","interval":10,"capabilities":{"metrics":true,"command":true},"interface_include":["eth0","eth1"],"interface_exclude":["lo"],"mount_include":["/data"]}`
	w := batchDo(t, e, http.MethodPost, "/batch-config/servers", adminTok, body)
	if w.Code != http.StatusOK {
		t.Fatalf("batch-config online: got %d %s", w.Code, w.Body.String())
	}
	res := decodeBatch(t, w)
	if len(res.Results) != 1 || res.Results[0].Status != "ok" || res.Results[0].ServerID != e.aliceS.ID {
		t.Fatalf("online result: %+v", res.Results)
	}
	if fa.applyConfigs.Load() != 1 {
		t.Fatalf("agent received %d apply_config calls, want 1", fa.applyConfigs.Load())
	}
	got, _ := fa.lastCfg.Load().(protocol.AgentConfig)
	if got.ServerURL != "wss://example.com/ws" || got.Interval != 10 {
		t.Fatalf("cfg server_url/interval: %+v", got)
	}
	if got.Capabilities == nil || got.Capabilities.Metrics != capabilities.Metrics || got.Capabilities.Command != capabilities.Command {
		t.Fatalf("cfg capabilities: %+v", got.Capabilities)
	}
	if len(got.InterfaceInclude) != 2 || got.InterfaceInclude[0] != "eth0" || got.InterfaceInclude[1] != "eth1" {
		t.Fatalf("cfg interface_include: %+v", got.InterfaceInclude)
	}
	if len(got.InterfaceExclude) != 1 || got.InterfaceExclude[0] != "lo" {
		t.Fatalf("cfg interface_exclude: %+v", got.InterfaceExclude)
	}
	if len(got.MountInclude) != 1 || got.MountInclude[0] != "/data" {
		t.Fatalf("cfg mount_include: %+v", got.MountInclude)
	}
	if got.Secret != "" {
		t.Fatalf("batch config must not carry secret, got %q", got.Secret)
	}
}

func TestBatchDDNSAdminOnlyValidationAndApply(t *testing.T) {
	e := newAuthzEnv(t)
	adminTok := e.token(t, e.admin)
	userTok := e.token(t, e.alice)

	// 非 admin → 403
	if w := batchDo(t, e, http.MethodPost, "/batch-ddns/servers", userTok, `{"ids":[1],"profile_id":1}`); w.Code != http.StatusForbidden {
		t.Fatalf("user batch-ddns: got %d want 403", w.Code)
	}
	// 空 ids / 缺 profile_id → 400
	if w := batchDo(t, e, http.MethodPost, "/batch-ddns/servers", adminTok, `{"ids":[],"profile_id":1}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty ids: got %d want 400", w.Code)
	}
	if w := batchDo(t, e, http.MethodPost, "/batch-ddns/servers", adminTok, `{"ids":[1]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing profile_id: got %d want 400", w.Code)
	}
	// 不存在的 profile → 404
	if w := batchDo(t, e, http.MethodPost, "/batch-ddns/servers", adminTok, `{"ids":[1],"profile_id":12345}`); w.Code != http.StatusNotFound {
		t.Fatalf("missing profile: got %d want 404", w.Code)
	}

	// 建立 webhook provider 计数，并让 Alice 的服务器上报 IP。
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	e.srv.DDNS = ddns.NewClient(ts.Client())
	e.srv.Store.Upsert(&e.aliceS).Host = protocol.HostInfo{IPv4: "192.0.2.77", IPv6: "2001:db8::77"}

	// 管理员先创建一份源 DDNS 配置（挂到 alice 的服务器上）。
	src := model.DDNSProfile{
		OwnerID: e.admin.ID, ServerID: e.aliceS.ID, Name: "batch-src", Provider: "webhook",
		RecordType: "dual", Domains: "a.example,b.example", Enabled: true,
		AccessKey: "super-secret-key", WebhookURL: ts.URL + "/u?domain={domain}&type={record_type}&ip={ip}",
		WebhookMethod: "GET", WebhookHeaders: "{}",
	}
	if err := e.srv.DB.Create(&src).Error; err != nil {
		t.Fatal(err)
	}

	// 批量应用到 alice + bob 的服务器。
	w := batchDo(t, e, http.MethodPost, "/batch-ddns/servers", adminTok, `{"ids":[`+itoa(e.aliceS.ID)+`,`+itoa(e.bobS.ID)+`],"profile_id":`+itoa(src.ID)+`}`)
	if w.Code != http.StatusOK {
		t.Fatalf("batch-ddns: got %d %s", w.Code, w.Body.String())
	}
	res := decodeBatch(t, w)
	if len(res.Results) != 2 {
		t.Fatalf("results=%d want 2", len(res.Results))
	}
	// alice 的 Agent 已上报 IP → ok；bob 未上报 → no_ip
	if res.Results[0].Status != "ok" || res.Results[0].ServerID != e.aliceS.ID || res.Results[0].ProfileID == 0 {
		t.Fatalf("alice result: %+v", res.Results[0])
	}
	if res.Results[1].Status != "no_ip" || res.Results[1].ServerID != e.bobS.ID || res.Results[1].ProfileID == 0 {
		t.Fatalf("bob result: %+v", res.Results[1])
	}
	// 响应不含任何密钥明文。
	if strings.Contains(w.Body.String(), "super-secret-key") {
		t.Fatalf("response leaked access_key: %s", w.Body.String())
	}
	// alice 的服务器应触发 webhook（dual = A + AAAA × 2 个域名 = 4 条记录）。
	if hits.Load() != 4 {
		t.Fatalf("webhook hits=%d want 4", hits.Load())
	}

	// 落库的两份新配置：owner = 操作者（admin），server 正确，密钥完整保留在 DB。
	var created []model.DDNSProfile
	if err := e.srv.DB.Where("name = ? AND id <> ?", "batch-src", src.ID).Find(&created).Error; err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 {
		t.Fatalf("created profiles=%d want 2", len(created))
	}
	byServer := map[int64]model.DDNSProfile{}
	for _, p := range created {
		byServer[p.ServerID] = p
	}
	for _, sid := range []int64{e.aliceS.ID, e.bobS.ID} {
		p := byServer[sid]
		if p.OwnerID != e.admin.ID {
			t.Fatalf("server %d owner=%d want admin", sid, p.OwnerID)
		}
		if p.AccessKey != "super-secret-key" {
			t.Fatalf("server %d access_key not persisted: %q", sid, p.AccessKey)
		}
		if p.WebhookURL == "" || p.WebhookURL == redactedSecret {
			t.Fatalf("server %d webhook_url not persisted: %q", sid, p.WebhookURL)
		}
	}
	// alice 的配置记录状态应为 success（webhook 已返回 200）。
	var states []model.DDNSRecordState
	if err := e.srv.DB.Where("profile_id = ?", byServer[e.aliceS.ID].ID).Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if st.Status != "success" {
			t.Fatalf("alice state %s/%s: %+v", st.Domain, st.RecordType, st)
		}
	}
	// 审计日志已落库且带 profile 信息。
	var logs []model.AuditLog
	if err := e.srv.DB.Where("action = ?", "server.batch_ddns").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || !strings.Contains(logs[0].Detail, "profile_id="+itoa(src.ID)) {
		t.Fatalf("audit logs: %+v", logs)
	}
}

func TestClipboardCRUD(t *testing.T) {
	e := newAuthzEnv(t)
	aliceTok := e.token(t, e.alice)
	bobTok := e.token(t, e.bob)
	adminTok := e.token(t, e.admin)

	// alice 创建
	w := batchDo(t, e, http.MethodPost, "/clipboard", aliceTok, `{"title":"t1","content":"hello world"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data model.Clipboard `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created.Data.ID
	if created.Data.UserID != e.alice.ID {
		t.Fatalf("owner=%d", created.Data.UserID)
	}

	// bob 看不到、也改不了 alice 的条目
	w = batchDo(t, e, http.MethodGet, "/clipboard", bobTok, "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "hello world") {
		t.Fatalf("bob list: %d %s", w.Code, w.Body.String())
	}
	if w := batchDo(t, e, http.MethodPut, "/clipboard/"+itoa(id), bobTok, `{"content":"hacked"}`); w.Code != http.StatusForbidden {
		t.Fatalf("bob update: got %d want 403", w.Code)
	}
	if w := batchDo(t, e, http.MethodDelete, "/clipboard/"+itoa(id), bobTok, ""); w.Code != http.StatusForbidden {
		t.Fatalf("bob delete: got %d want 403", w.Code)
	}

	// 空 content 不允许
	if w := batchDo(t, e, http.MethodPost, "/clipboard", aliceTok, `{"content":""}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty content: got %d want 400", w.Code)
	}

	// alice 编辑（仅标题）
	w = batchDo(t, e, http.MethodPut, "/clipboard/"+itoa(id), aliceTok, `{"title":"renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update title: %d %s", w.Code, w.Body.String())
	}
	var updated struct {
		Data model.Clipboard `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Data.Title != "renamed" || updated.Data.Content != "hello world" {
		t.Fatalf("updated: %+v", updated.Data)
	}

	// admin 能看到全部（含 alice 的）
	w = batchDo(t, e, http.MethodGet, "/clipboard", adminTok, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "renamed") {
		t.Fatalf("admin list: %d %s", w.Code, w.Body.String())
	}

	// alice 删除
	if w := batchDo(t, e, http.MethodDelete, "/clipboard/"+itoa(id), aliceTok, ""); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = batchDo(t, e, http.MethodGet, "/clipboard", aliceTok, "")
	if strings.Contains(w.Body.String(), "renamed") {
		t.Fatalf("item still visible after delete: %s", w.Body.String())
	}
	// 审计：create/update/delete 各一条
	for _, action := range []string{"clipboard.create", "clipboard.update", "clipboard.delete"} {
		var n int64
		e.srv.DB.Model(&model.AuditLog{}).Where("action = ?", action).Count(&n)
		if n != 1 {
			t.Fatalf("audit %s rows=%d want 1", action, n)
		}
	}
}
