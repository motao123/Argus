package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
)

func newMCPServer(t *testing.T) (*Server, int64, int64) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.Server{}); err != nil {
		t.Fatal(err)
	}
	a := model.Server{Name: "a", Secret: agent.GenSecret(), OwnerID: 1}
	b := model.Server{Name: "b", Secret: agent.GenSecret(), OwnerID: 2}
	if err := gdb.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: gdb, Peers: func() map[int64]*rpc.Peer { return map[int64]*rpc.Peer{} }}
	return s, a.ID, b.ID
}

func principal(isAdmin bool, scopes []string, servers []int64) *Principal {
	m := map[string]bool{}
	for _, s := range scopes {
		m[s] = true
	}
	sm := map[int64]bool{}
	for _, id := range servers {
		sm[id] = true
	}
	return &Principal{UserID: 1, IsAdmin: isAdmin, Scopes: m, ServerIDs: sm}
}

func call(t *testing.T, s *Server, method string, params any, p *Principal) *rpcErr {
	t.Helper()
	raw, _ := json.Marshal(params)
	_, e := s.dispatch(method, raw, p)
	return e
}

// dispatchM 与 call 相同但返回结果，便于断言返回体。
func dispatchM(t *testing.T, s *Server, method string, params any, p *Principal) (any, *rpcErr) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return s.dispatch(method, raw, p)
}

func TestMCPRequiresScope(t *testing.T) {
	s, aID, _ := newMCPServer(t)
	// 无任何 scope 的 PAT：server.get 被拒
	e := call(t, s, "server.get", map[string]any{"id": aID}, principal(false, nil, nil))
	if e == nil || e.Message != "insufficient scope: argus:server:read" {
		t.Fatalf("no scope get: got %+v want insufficient scope", e)
	}
	// 有 read scope 但无 exec scope：exec 被拒
	e = call(t, s, "server.exec", map[string]any{"id": aID, "command": "id"}, principal(false, []string{"argus:server:read"}, nil))
	if e == nil || e.Message != "insufficient scope: argus:server:exec" {
		t.Fatalf("read-only exec: got %+v want insufficient scope", e)
	}
	// fs.write 需要 write scope
	e = call(t, s, "fs.write", map[string]any{"id": aID, "path": "/tmp/x", "data": ""}, principal(false, []string{"argus:server:read"}, nil))
	if e == nil || e.Message != "insufficient scope: argus:server:write" {
		t.Fatalf("read-only fs.write: got %+v want insufficient scope", e)
	}
}

func TestMCPOwnerAndWhitelist(t *testing.T) {
	s, aID, bID := newMCPServer(t)
	// 用户 1（ownerID=1）访问服务器 a（owner 1）→ 通过（peer 离线报 offline）
	e := call(t, s, "server.get", map[string]any{"id": aID}, principal(false, []string{"argus:server:read"}, nil))
	if e != nil {
		t.Fatalf("owner get: unexpected error %+v", e)
	}
	// 用户 1 访问服务器 b（owner 2）→ 拒绝
	e = call(t, s, "server.get", map[string]any{"id": bID}, principal(false, []string{"argus:server:read"}, nil))
	if e == nil || e.Message != "server access denied" {
		t.Fatalf("cross owner get: got %+v want denied", e)
	}
	// 白名单只含 a：访问 b 被拒
	e = call(t, s, "server.get", map[string]any{"id": bID}, principal(false, []string{"argus:server:read"}, []int64{aID}))
	if e == nil || e.Message != "server access denied" {
		t.Fatalf("whitelist miss get: got %+v want denied", e)
	}
	// admin 可访问任意服务器
	e = call(t, s, "server.get", map[string]any{"id": bID}, principal(true, nil, nil))
	if e != nil {
		t.Fatalf("admin get: unexpected error %+v", e)
	}
	// exec 授权通过后因离线报 offline（不泄露 access denied 的时机差异）
	e = call(t, s, "server.exec", map[string]any{"id": aID, "command": "id"}, principal(false, []string{"argus:server:exec"}, nil))
	if e == nil || e.Message != "server offline" {
		t.Fatalf("owner exec offline: got %+v want offline", e)
	}
}

func TestMCPServerListScoped(t *testing.T) {
	s, aID, _ := newMCPServer(t)
	// 用户 1 只看到自己的服务器 a
	out, e := s.dispatch("server.list", nil, principal(false, []string{"argus:server:read"}, nil))
	if e != nil {
		t.Fatal(e)
	}
	m := out.(map[string]any)
	servers := m["servers"].([]map[string]any)
	if len(servers) != 1 || servers[0]["id"] != aID {
		t.Fatalf("scoped list: got %+v want only own server", servers)
	}
}

// ---- 里程碑6：开关 / PAT-only / 限流 / 生命周期 / 一次性传输 ----

func postJSON(t *testing.T, s *Server, path, body, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestMCPDisabledByDefault(t *testing.T) {
	s, _, _ := newMCPServer(t) // Enabled 默认 false
	rec := postJSON(t, s, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, "argus_anything")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled MCP: got %d want 404", rec.Code)
	}
}

func TestMCPRequiresPATOnly(t *testing.T) {
	s, _, _ := newMCPServer(t)
	s.Enabled = true
	s.IdentifyPAT = func(raw string) (*Principal, bool) { return principal(true, nil, nil), true }

	// 无 Authorization 头
	if rec := postJSON(t, s, "/mcp", `{}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing header: got %d want 401", rec.Code)
	}
	// 非 argus_ 前缀（JWT 拒绝）
	if rec := postJSON(t, s, "/mcp", `{}`, "eyJhbGciOiJIUzI1NiJ9.sig"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("jwt token: got %d want 401", rec.Code)
	}
	// PAT 无效
	s.IdentifyPAT = func(raw string) (*Principal, bool) { return nil, false }
	if rec := postJSON(t, s, "/mcp", `{}`, "argus_invalid"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid pat: got %d want 401", rec.Code)
	}
	// DELETE 也要 PAT
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("delete without pat: got %d want 401", rec.Code)
	}
}

func TestMCPRateLimitPerToken(t *testing.T) {
	s, _, _ := newMCPServer(t)
	s.Enabled = true
	s.RateLimit = 2
	s.IdentifyPAT = func(raw string) (*Principal, bool) { return principal(true, nil, nil), true }
	body := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}`
	for i := 0; i < 2; i++ {
		if rec := postJSON(t, s, "/mcp", body, "argus_same"); rec.Code != http.StatusOK {
			t.Fatalf("call %d: got %d want 200", i+1, rec.Code)
		}
	}
	rec := postJSON(t, s, "/mcp", body, "argus_same")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over limit: got %d want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
	// 不同 token 独立计数
	if rec := postJSON(t, s, "/mcp", body, "argus_other"); rec.Code != http.StatusOK {
		t.Fatalf("other token: got %d want 200", rec.Code)
	}
}

func TestMCPInitializeAndToolsList(t *testing.T) {
	s, _, _ := newMCPServer(t)
	p := principal(true, nil, nil)

	out, e := s.dispatch("initialize", nil, p)
	if e != nil {
		t.Fatal(e)
	}
	m := out.(map[string]any)
	if m["protocolVersion"] != protocolVersion {
		t.Fatalf("protocolVersion=%v want %s", m["protocolVersion"], protocolVersion)
	}
	if m["serverInfo"].(map[string]any)["name"] != "argus" {
		t.Fatalf("serverInfo=%v", m["serverInfo"])
	}
	caps := m["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Fatalf("missing tools capability: %v", caps)
	}

	out, e = s.dispatch("tools/list", nil, p)
	if e != nil {
		t.Fatal(e)
	}
	tools := out.(map[string]any)["tools"].([]map[string]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl["name"].(string)] = true
	}
	for _, want := range []string{"server.list", "server.get", "server.exec", "fs.list", "fs.read", "fs.write", "fs.delete", "fs.download_url", "fs.upload_url", "meta.whoami"} {
		if !names[want] {
			t.Fatalf("tools/list missing %s: %v", want, names)
		}
	}

	// 通知不需要应答 ID 也可接受；未知方法返回 -32601
	if _, e = s.dispatch("notifications/initialized", nil, p); e != nil {
		t.Fatalf("initialized notification: %v", e)
	}
	if _, e = s.dispatch("no.such.tool", nil, p); e == nil || e.Code != -32601 {
		t.Fatalf("unknown tool: got %+v want -32601", e)
	}
}

func TestMCPToolsCallWrapsError(t *testing.T) {
	s, _, _ := newMCPServer(t)
	out, e := s.dispatch("tools/call", json.RawMessage(`{"name":"server.get","arguments":{"id":999999}}`), principal(false, []string{"argus:server:read"}, nil))
	if e != nil {
		t.Fatal(e)
	}
	m := out.(map[string]any)
	if m["isError"] != true {
		t.Fatalf("expected isError wrapper, got %v", m)
	}
}

func TestMCPTransferURLScopes(t *testing.T) {
	s, aID, _ := newMCPServer(t)
	// 下载需要 read scope
	e := call(t, s, "fs.download_url", map[string]any{"id": aID, "path": "/tmp/f"}, principal(false, nil, nil))
	if e == nil || e.Message != "insufficient scope: argus:server:read" {
		t.Fatalf("download_url no scope: got %+v", e)
	}
	// 上传需要 write scope
	e = call(t, s, "fs.upload_url", map[string]any{"id": aID, "path": "/tmp/f", "sha256": strings.Repeat("ab", 32), "if_match": "*"}, principal(false, []string{"argus:server:read"}, nil))
	if e == nil || e.Message != "insufficient scope: argus:server:write" {
		t.Fatalf("upload_url read-only: got %+v", e)
	}
	// 上传缺少 sha256 或 if_match
	e = call(t, s, "fs.upload_url", map[string]any{"id": aID, "path": "/tmp/f"}, principal(false, []string{"argus:server:write"}, nil))
	if e == nil || e.Message != "sha256 and if_match required for upload" {
		t.Fatalf("upload_url no sha: got %+v", e)
	}
	e = call(t, s, "fs.upload_url", map[string]any{"id": aID, "path": "/tmp/f", "sha256": strings.Repeat("z", 64), "if_match": "*"}, principal(false, []string{"argus:server:write"}, nil))
	if e == nil || e.Message != "invalid sha256" {
		t.Fatalf("upload_url bad sha: got %+v", e)
	}
}

func TestTransferURLOneTimeConsumeAndExpiry(t *testing.T) {
	s, aID, _ := newMCPServer(t)
	s.TransferTTL = time.Minute
	out, e := dispatchM(t, s, "fs.download_url", map[string]any{"id": aID, "path": "/tmp/f"}, principal(false, []string{"argus:server:read"}, nil))
	if e != nil {
		t.Fatal(e)
	}
	token := strings.TrimPrefix(out.(map[string]any)["url"].(string), "/mcp/transfer/")
	if token == "" {
		t.Fatal("no transfer token")
	}
	// 方法错误：下载 URL 用 PUT → 405
	if _, code, msg := s.takeTransfer(token, http.MethodPut); code != http.StatusMethodNotAllowed || msg == "" {
		t.Fatalf("wrong method: got %d %q", code, msg)
	}
	// 正确消费一次
	if _, code, msg := s.takeTransfer(token, http.MethodGet); code != 0 || msg != "" {
		t.Fatalf("consume: got %d %q", code, msg)
	}
	// 一次性：不可重放
	if _, code, _ := s.takeTransfer(token, http.MethodGet); code != http.StatusGone {
		t.Fatalf("replay: got %d want 410", code)
	}
	// 过期
	s.TransferTTL = time.Minute
	out, e = dispatchM(t, s, "fs.download_url", map[string]any{"id": aID, "path": "/tmp/f"}, principal(false, []string{"argus:server:read"}, nil))
	if e != nil {
		t.Fatal(e)
	}
	token = strings.TrimPrefix(out.(map[string]any)["url"].(string), "/mcp/transfer/")
	s.mu.Lock()
	s.transfers[token].Expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, code, _ := s.takeTransfer(token, http.MethodGet); code != http.StatusGone {
		t.Fatalf("expired: got %d want 410", code)
	}
}

func TestHandleTransferOfflineAndSHA256Mismatch(t *testing.T) {
	s, aID, _ := newMCPServer(t)
	s.Enabled = true
	s.TransferMax = 1 << 20
	s.TransferTTL = time.Minute

	// 离线：下载 → 503
	out, _ := dispatchM(t, s, "fs.download_url", map[string]any{"id": aID, "path": "/tmp/f"}, principal(false, []string{"argus:server:read"}, nil))
	token := strings.TrimPrefix(out.(map[string]any)["url"].(string), "/mcp/transfer/")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/transfer/"+token, nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline download: got %d want 503", rec.Code)
	}

	// 上传 body 与声明 SHA-256 不符 → 422
	attachPeer(t, s, aID, &fakeAgent{files: map[string][]byte{"/tmp/f": []byte("existing")}})
	bad := "tampered content"
	hash := sha256Hex([]byte("real content"))
	out, _ = dispatchM(t, s, "fs.upload_url", map[string]any{"id": aID, "path": "/tmp/f", "sha256": hash, "if_match": "*"}, principal(false, []string{"argus:server:write"}, nil))
	token = strings.TrimPrefix(out.(map[string]any)["url"].(string), "/mcp/transfer/")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/mcp/transfer/"+token, bytes.NewReader([]byte(bad))))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("sha mismatch: got %d want 422", rec.Code)
	}
}

// fakeAgent 是一个内存文件系统上的 RPC 对端，模拟 Agent 的 fs.read/fs.write。
type fakeAgent struct {
	files map[string][]byte
}

func (f *fakeAgent) Handle(method string, params json.RawMessage) (any, *protocol.RPCError) {
	switch method {
	case protocol.MethodFsRead:
		var p protocol.FsReadParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, protocol.NewError(protocol.ErrParams, err.Error())
		}
		data, ok := f.files[p.Path]
		if !ok {
			return nil, protocol.NewError(protocol.ErrNotFound, "no such file")
		}
		return protocol.FsReadResult{Data: data, EOF: true, Size: int64(len(data))}, nil
	case protocol.MethodFsWrite:
		var p protocol.FsWriteParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, protocol.NewError(protocol.ErrParams, err.Error())
		}
		if p.Append {
			f.files[p.Path] = append(f.files[p.Path], p.Data...)
		} else {
			f.files[p.Path] = p.Data
		}
		return protocol.FsWriteResult{Bytes: len(p.Data)}, nil
	}
	return nil, protocol.NewError(protocol.ErrMethod, "unknown method")
}

// attachPeer 将真实 agent 挂到 MCP Server 的 Peers 表，返回清理函数。
func attachPeer(t *testing.T, s *Server, serverID int64, agentImpl *fakeAgent) {
	t.Helper()
	up := websocket.Upgrader{}
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- c
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// MCP 端持有 accepted 侧的 peer 并主动 Call；agent 处理器挂在 dialed 侧。
	agentPeer := rpc.New(conn, agentImpl)
	go agentPeer.ReadLoop()
	peer := rpc.New(<-accepted, nil)
	go peer.ReadLoop()
	s.Peers = func() map[int64]*rpc.Peer { return map[int64]*rpc.Peer{serverID: peer} }
	t.Cleanup(func() { _ = conn.Close() })
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestHandleTransferDownloadServesFileWithETag(t *testing.T) {
	s, aID, _ := newMCPServer(t)
	s.Enabled = true
	s.TransferMax = 1 << 20
	s.TransferTTL = time.Minute
	agentImpl := &fakeAgent{files: map[string][]byte{"/etc/hostname": []byte("hello")}}
	attachPeer(t, s, aID, agentImpl)

	out, _ := dispatchM(t, s, "fs.download_url", map[string]any{"id": aID, "path": "/etc/hostname"}, principal(false, []string{"argus:server:read"}, nil))
	token := strings.TrimPrefix(out.(map[string]any)["url"].(string), "/mcp/transfer/")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/transfer/"+token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("download: got %d body %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello" {
		t.Fatalf("download body = %q", rec.Body.String())
	}
	want := `"sha256:` + sha256Hex([]byte("hello")) + `"`
	if rec.Header().Get("ETag") != want {
		t.Fatalf("ETag = %q want %q", rec.Header().Get("ETag"), want)
	}
	if rec.Header().Get("X-Content-SHA256") != sha256Hex([]byte("hello")) {
		t.Fatalf("X-Content-SHA256 = %q", rec.Header().Get("X-Content-SHA256"))
	}
	// 一次性：再次请求 410
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/mcp/transfer/"+token, nil))
	if rec2.Code != http.StatusGone {
		t.Fatalf("replay download: got %d want 410", rec2.Code)
	}
}

func TestHandleTransferDownloadSizeLimit(t *testing.T) {
	s, aID, _ := newMCPServer(t)
	s.Enabled = true
	s.TransferMax = 4 // 文件 5 字节，超限
	s.TransferTTL = time.Minute
	agentImpl := &fakeAgent{files: map[string][]byte{"/big": []byte("hello")}}
	attachPeer(t, s, aID, agentImpl)

	out, _ := dispatchM(t, s, "fs.download_url", map[string]any{"id": aID, "path": "/big"}, principal(false, []string{"argus:server:read"}, nil))
	token := strings.TrimPrefix(out.(map[string]any)["url"].(string), "/mcp/transfer/")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/transfer/"+token, nil))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize download: got %d want 413", rec.Code)
	}
}

func TestHandleTransferUploadIfMatch(t *testing.T) {
	s, aID, _ := newMCPServer(t)
	s.Enabled = true
	s.TransferMax = 1 << 20
	s.TransferTTL = time.Minute
	agentImpl := &fakeAgent{files: map[string][]byte{"/etc/motd": []byte("old")}}
	attachPeer(t, s, aID, agentImpl)

	createUpload := func(ifMatch, content string) string {
		out, e := dispatchM(t, s, "fs.upload_url", map[string]any{"id": aID, "path": "/etc/motd", "sha256": sha256Hex([]byte(content)), "if_match": ifMatch}, principal(false, []string{"argus:server:write"}, nil))
		if e != nil {
			t.Fatal(e)
		}
		return strings.TrimPrefix(out.(map[string]any)["url"].(string), "/mcp/transfer/")
	}
	upload := func(token, content string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/mcp/transfer/"+token, bytes.NewReader([]byte(content))))
		return rec
	}

	// if-match 指定了不存在的 etag → 412
	rec := upload(createUpload(`"sha256:0000000000000000000000000000000000000000000000000000000000000000"`, "new content"), "new content")
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale if-match: got %d want 412", rec.Code)
	}
	// if-match * → 覆盖成功
	rec = upload(createUpload("*", "new content"), "new content")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("if-match *: got %d body %s", rec.Code, rec.Body.String())
	}
	if got := string(agentImpl.files["/etc/motd"]); got != "new content" {
		t.Fatalf("file content = %q", got)
	}
	if rec.Header().Get("X-Content-SHA256") != sha256Hex([]byte("new content")) {
		t.Fatalf("response sha = %q", rec.Header().Get("X-Content-SHA256"))
	}
	// 精确 etag（当前内容 sha256）→ 成功
	// 精确 etag 指当前文件内容（new content）的 sha256；正文为新版本 v3
	rec = upload(createUpload(`"sha256:`+sha256Hex([]byte("new content"))+`"`, "v3"), "v3")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("exact if-match: got %d body %s", rec.Code, rec.Body.String())
	}
	if got := string(agentImpl.files["/etc/motd"]); got != "v3" {
		t.Fatalf("file content = %q", got)
	}
}

func TestHandleTransferUploadSizeLimit(t *testing.T) {
	s, aID, _ := newMCPServer(t)
	s.Enabled = true
	s.TransferMax = 4
	s.TransferTTL = time.Minute
	attachPeer(t, s, aID, &fakeAgent{files: map[string][]byte{}})

	content := "12345"
	out, _ := dispatchM(t, s, "fs.upload_url", map[string]any{"id": aID, "path": "/x", "sha256": sha256Hex([]byte(content)), "if_match": "*"}, principal(false, []string{"argus:server:write"}, nil))
	token := strings.TrimPrefix(out.(map[string]any)["url"].(string), "/mcp/transfer/")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/mcp/transfer/"+token, bytes.NewReader([]byte(content))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload: got %d want 413", rec.Code)
	}
}
