package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/config"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/store"
)

// authzTestEnv 双用户 + 双服务器 + 单服务的测试环境。
type authzTestEnv struct {
	srv    *Server
	admin  *model.User
	alice  *model.User
	bob    *model.User
	aliceS model.Server
	bobS   model.Server
	svc    model.Service
}

func newAuthzEnv(t *testing.T) *authzTestEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(
		&model.User{}, &model.Server{}, &model.Service{}, &model.ServiceHistory{},
		&model.APIToken{}, &model.Setting{}, &model.DDNSProfile{}, &model.NAT{}, &model.Metric{},
		&model.AuditLog{},
	); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test123"), bcrypt.DefaultCost)
	admin := &model.User{Username: "admin", PasswordHash: string(hash), Role: model.RoleAdmin, AgentSecret: agent.GenSecret()}
	alice := &model.User{Username: "alice", PasswordHash: string(hash), Role: model.RoleUser, AgentSecret: agent.GenSecret()}
	bob := &model.User{Username: "bob", PasswordHash: string(hash), Role: model.RoleUser, AgentSecret: agent.GenSecret()}
	if err := gdb.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(bob).Error; err != nil {
		t.Fatal(err)
	}
	aliceS := model.Server{Name: "alice-srv", Secret: agent.GenSecret(), OwnerID: alice.ID}
	bobS := model.Server{Name: "bob-srv", Secret: agent.GenSecret(), OwnerID: bob.ID}
	if err := gdb.Create(&aliceS).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&bobS).Error; err != nil {
		t.Fatal(err)
	}
	svc := model.Service{OwnerID: alice.ID, ServerID: aliceS.ID, Name: "alice-svc", Type: "http", Target: "http://127.0.0.1:1"}
	if err := gdb.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	st := store.NewHub()
	return &authzTestEnv{
		srv:   &Server{DB: gdb, Cfg: &config.Config{JWTSecret: "test-secret"}, Store: st, Agents: agent.NewHub(gdb, st, store.NewMetricBatcher(gdb))},
		admin: admin, alice: alice, bob: bob,
		aliceS: aliceS, bobS: bobS, svc: svc,
	}
}

func (e *authzTestEnv) token(t *testing.T, u *model.User) string {
	t.Helper()
	tok, err := e.srv.issueToken(u)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (e *authzTestEnv) createPAT(t *testing.T, user *model.User, scopes []string, serverIDs string) string {
	t.Helper()
	raw := "argus_test_" + randomHex(12)
	hash := sha256.Sum256([]byte(raw))
	tok := model.APIToken{
		UserID:    user.ID,
		Name:      "t",
		TokenHash: hex.EncodeToString(hash[:]),
		Scopes:    joinScopes(scopes),
		ServerIDs: serverIDs,
	}
	if err := e.srv.DB.Create(&tok).Error; err != nil {
		t.Fatal(err)
	}
	return raw
}

func joinScopes(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

// do 发起带身份的请求。
func (e *authzTestEnv) do(t *testing.T, method, path string, token string, body string) *httptest.ResponseRecorder {
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
	authed.PUT("/servers/:id", requireScope(ScopeServerWrite), e.srv.updateServer)
	authed.DELETE("/servers/:id", requireScope(ScopeServerDelete), e.srv.deleteServer)
	authed.POST("/servers/:id/exec", requireScope(ScopeServerExec), e.srv.serverExec)
	pub := r.Group("", e.srv.optionalAuthMiddleware())
	pub.GET("/servers/:id/metrics", e.srv.forceAuth, e.srv.serverMetrics)
	authed.POST("/ddns", e.srv.createDDNS)
	authed.POST("/nats", e.srv.createNAT)
	authed.POST("/services", requireScope(ScopeServiceWrite), e.srv.createService)
	r.ServeHTTP(w, req)
	return w
}

func TestServerUpdateCrossTenant(t *testing.T) {
	e := newAuthzEnv(t)
	// alice 修改自己的服务器 → 200
	w := e.do(t, http.MethodPut, "/servers/"+itoa(e.aliceS.ID), e.token(t, e.alice), `{"name":"a2"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("alice update own: got %d want 200", w.Code)
	}
	// alice 修改 bob 的服务器 → 403
	w = e.do(t, http.MethodPut, "/servers/"+itoa(e.bobS.ID), e.token(t, e.alice), `{"name":"hack"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice update bob server: got %d want 403", w.Code)
	}
	// bob 删除 alice 的服务器 → 403
	w = e.do(t, http.MethodDelete, "/servers/"+itoa(e.aliceS.ID), e.token(t, e.bob), "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob delete alice server: got %d want 403", w.Code)
	}
	// admin 可删除任意服务器（执行真正的删除以验证授权放行）
	w = e.do(t, http.MethodDelete, "/servers/"+itoa(e.bobS.ID), e.token(t, e.admin), "")
	if w.Code != http.StatusOK {
		t.Fatalf("admin delete bob server: got %d want 200", w.Code)
	}
}

func TestServerExecCrossTenant(t *testing.T) {
	e := newAuthzEnv(t)
	// bob 对 alice 的服务器执行命令 → 403（在触碰 Agent 前被拦截）
	w := e.do(t, http.MethodPost, "/servers/"+itoa(e.aliceS.ID)+"/exec", e.token(t, e.bob), `{"command":"id"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob exec on alice server: got %d want 403", w.Code)
	}
	// PAT 无 exec scope → 403
	pat := e.createPAT(t, e.alice, []string{ScopeServerRead}, "")
	w = e.do(t, http.MethodPost, "/servers/"+itoa(e.aliceS.ID)+"/exec", pat, `{"command":"id"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("PAT no exec scope: got %d want 403", w.Code)
	}
}

func TestHiddenServerMetricsGuest(t *testing.T) {
	e := newAuthzEnv(t)
	// 把 alice 的服务器设为 hidden
	e.srv.DB.Model(&e.aliceS).Update("hidden", true)
	// 游客访问 metrics → 404
	w := e.do(t, http.MethodGet, "/servers/"+itoa(e.aliceS.ID)+"/metrics?period=1h", "", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("guest metrics on hidden server: got %d want 404", w.Code)
	}
	// owner 访问 → 200
	w = e.do(t, http.MethodGet, "/servers/"+itoa(e.aliceS.ID)+"/metrics?period=1h", e.token(t, e.alice), "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner metrics on hidden server: got %d want 200", w.Code)
	}
}

func TestServiceAndDDNSNATServerOwnership(t *testing.T) {
	e := newAuthzEnv(t)
	// alice 给 bob 的服务器创建服务 → 403
	w := e.do(t, http.MethodPost, "/services", e.token(t, e.alice), `{"server_id":`+itoa(e.bobS.ID)+`,"name":"x","type":"http","target":"http://127.0.0.1"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice create service on bob server: got %d want 403", w.Code)
	}
	// alice 给 bob 的服务器创建 DDNS → 403
	w = e.do(t, http.MethodPost, "/ddns", e.token(t, e.alice), `{"server_id":`+itoa(e.bobS.ID)+`,"name":"d","domains":"a.example"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice create ddns on bob server: got %d want 403", w.Code)
	}
	// alice 给 bob 的服务器创建 NAT → 403
	w = e.do(t, http.MethodPost, "/nats", e.token(t, e.alice), `{"server_id":`+itoa(e.bobS.ID)+`,"domain":"nat.example","target_addr":"127.0.0.1:80"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice create nat on bob server: got %d want 403", w.Code)
	}
	// alice 给自己服务器创建 DDNS → 200
	w = e.do(t, http.MethodPost, "/ddns", e.token(t, e.alice), `{"server_id":`+itoa(e.aliceS.ID)+`,"name":"d","domains":"a.example"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("alice create ddns on own server: got %d want 200", w.Code)
	}
}

func TestListServicesVisibility(t *testing.T) {
	e := newAuthzEnv(t)
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	w := httptest.NewRecorder()
	r := gin.New()
	pub := r.Group("", e.srv.optionalAuthMiddleware())
	pub.GET("/services", e.srv.forceAuth, e.srv.listServices)
	r.ServeHTTP(w, req)
	var resp struct {
		Data struct {
			Services []model.Service `json:"services"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// 游客能看到 alice 的公开服务（hidden=false）
	if len(resp.Data.Services) != 1 || resp.Data.Services[0].ID != e.svc.ID {
		t.Fatalf("guest should see alice public service, got %+v", resp.Data.Services)
	}
	// 隐藏后游客不可见
	e.srv.DB.Model(&e.svc).Update("hidden", true)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/services", nil))
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data.Services) != 0 {
		t.Fatalf("guest should not see hidden service, got %+v", resp.Data.Services)
	}
}
