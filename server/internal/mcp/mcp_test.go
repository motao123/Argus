package mcp

import (
	"encoding/json"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

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
