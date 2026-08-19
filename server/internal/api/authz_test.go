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
	// :memory: 库每个连接都是独立数据库，必须单连接才能跨 goroutine 可见。
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(
		&model.User{}, &model.Server{}, &model.Service{}, &model.ServiceProbe{}, &model.ServiceHistory{},
		&model.APIToken{}, &model.Setting{}, &model.DDNSProfile{}, &model.DDNSRecordState{}, &model.NAT{}, &model.Metric{},
		&model.AuditLog{}, &model.ServerTransfer{}, &model.Notification{}, &model.Alert{}, &model.Cron{}, &model.TaskRun{}, &model.TaskRunResult{},
		&model.UpgradeJob{}, &model.UpgradeResult{},
		&model.Incident{}, &model.MaintenanceWindow{}, &model.Clipboard{},
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
	if err := gdb.Create(&model.ServiceProbe{ServiceID: svc.ID, ServerID: aliceS.ID}).Error; err != nil {
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
	authed.GET("/files/:serverId", requireScope(ScopeServerRead), e.srv.listFiles)
	authed.POST("/files/:serverId/read", requireScope(ScopeServerRead), e.srv.readFile)
	authed.POST("/files/:serverId/write", requireScope(ScopeServerWrite), e.srv.writeFile)
	authed.POST("/files/:serverId/delete", requireScope(ScopeServerWrite), e.srv.deleteFile)
	pub := r.Group("", e.srv.optionalAuthMiddleware())
	pub.GET("/servers/:id/metrics", e.srv.forceAuth, e.srv.serverMetrics)
	authed.POST("/ddns", e.srv.createDDNS)
	authed.POST("/nats", e.srv.createNAT)
	authed.POST("/services", requireScope(ScopeServiceWrite), e.srv.createService)
	authed.GET("/alerts", requireScope(ScopeAlertRead), e.srv.listAlerts)
	authed.POST("/alerts", requireScope(ScopeAlertWrite), e.srv.createAlert)
	authed.PUT("/alerts/:id", requireScope(ScopeAlertWrite), e.srv.updateAlert)
	authed.DELETE("/alerts/:id", requireScope(ScopeAlertDelete), e.srv.deleteAlert)
	authed.GET("/crons", requireScope(ScopeCronRead), e.srv.listCrons)
	authed.POST("/crons", requireScope(ScopeCronWrite), e.srv.createCron)
	authed.PUT("/crons/:id", requireScope(ScopeCronWrite), e.srv.updateCron)
	authed.DELETE("/crons/:id", requireScope(ScopeCronDelete), e.srv.deleteCron)
	authed.POST("/crons/:id/run", requireScope(ScopeCronWrite), e.srv.runCron)
	authed.GET("/crons/:id/runs", requireScope(ScopeCronRead), e.srv.listCronRuns)
	authed.GET("/crons/:id/runs/:runId", requireScope(ScopeCronRead), e.srv.getCronRun)

	r.ServeHTTP(w, req)
	return w
}

func TestFileAccessOwnerIsolation(t *testing.T) {
	e := newAuthzEnv(t)
	aliceJWT := e.token(t, e.alice)
	bobJWT := e.token(t, e.bob)
	alicePAT := e.createPAT(t, e.alice, []string{ScopeServerRead}, itoa(e.aliceS.ID)+","+itoa(e.bobS.ID))
	adminPAT := e.createPAT(t, e.admin, []string{ScopeServerRead}, itoa(e.bobS.ID))

	// Offline owner access reaches the agent call; cross-owner access is denied first.
	for _, tc := range []struct {
		name, token string
		serverID    int64
		want        int
	}{
		{"JWT owner", aliceJWT, e.aliceS.ID, http.StatusConflict},
		{"JWT cross-owner", aliceJWT, e.bobS.ID, http.StatusForbidden},
		{"second JWT cross-owner", bobJWT, e.aliceS.ID, http.StatusForbidden},
		{"PAT owner", alicePAT, e.aliceS.ID, http.StatusConflict},
		{"PAT cross-owner", alicePAT, e.bobS.ID, http.StatusForbidden},
		{"admin PAT", adminPAT, e.bobS.ID, http.StatusConflict},
	} {
		w := e.do(t, http.MethodGet, "/files/"+itoa(tc.serverID), tc.token, "")
		if w.Code != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, w.Code, tc.want)
		}
	}
}

func TestPProfAdminOnly(t *testing.T) {
	e := newAuthzEnv(t)
	adminJWT := e.token(t, e.admin)
	userJWT := e.token(t, e.alice)
	ordinaryPAT := e.createPAT(t, e.alice, []string{ScopeServerRead}, "")
	adminPAT := e.createPAT(t, e.alice, []string{ScopeAdmin}, "")
	allPAT := e.createPAT(t, e.alice, []string{ScopeAll}, "")

	request := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.GET("/debug/pprof/*pprof", e.srv.AuthMiddlewareForPProf(), e.srv.PProfHandler)
		r.ServeHTTP(w, req)
		return w.Code
	}

	for _, tc := range []struct {
		name, token string
		want        int
	}{
		{"ordinary JWT", userJWT, http.StatusForbidden},
		{"ordinary PAT", ordinaryPAT, http.StatusForbidden},
		{"admin JWT", adminJWT, http.StatusOK},
		{"admin scope PAT", adminPAT, http.StatusOK},
		{"all scope PAT", allPAT, http.StatusOK},
	} {
		if got := request(tc.token); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

func TestCronCrossTenant(t *testing.T) {
	e := newAuthzEnv(t)
	aliceToken := e.token(t, e.alice)
	bobToken := e.token(t, e.bob)
	adminToken := e.token(t, e.admin)

	ownBody := `{"name":"alice-cron","expression":"* * * * *","command":"id","server_ids":"` + itoa(e.aliceS.ID) + `"}`
	w := e.do(t, http.MethodPost, "/crons", aliceToken, ownBody)
	if w.Code != http.StatusOK {
		t.Fatalf("alice create own cron: got %d: %s", w.Code, w.Body.String())
	}
	var cr model.Cron
	if err := e.srv.DB.Where("name = ?", "alice-cron").First(&cr).Error; err != nil {
		t.Fatal(err)
	}
	if cr.OwnerID != e.alice.ID {
		t.Fatalf("cron owner = %d want %d", cr.OwnerID, e.alice.ID)
	}

	w = e.do(t, http.MethodPost, "/crons", aliceToken, `{"name":"cross","expression":"* * * * *","command":"id","server_ids":"`+itoa(e.bobS.ID)+`"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice target bob server: got %d want 403", w.Code)
	}
	w = e.do(t, http.MethodGet, "/crons", bobToken, "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "alice-cron") {
		t.Fatalf("bob can see alice cron: %d %s", w.Code, w.Body.String())
	}
	w = e.do(t, http.MethodPut, "/crons/"+itoa(cr.ID), bobToken, ownBody)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob update alice cron: got %d want 403", w.Code)
	}
	w = e.do(t, http.MethodDelete, "/crons/"+itoa(cr.ID), bobToken, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob delete alice cron: got %d want 403", w.Code)
	}
	w = e.do(t, http.MethodPost, "/crons/"+itoa(cr.ID)+"/run", bobToken, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob run alice cron: got %d want 403", w.Code)
	}
	aliceRun := model.TaskRun{CronID: cr.ID, OwnerID: e.alice.ID, Trigger: "manual", Status: "success", Command: cr.Command}
	if err := e.srv.DB.Create(&aliceRun).Error; err != nil {
		t.Fatal(err)
	}
	w = e.do(t, http.MethodGet, "/crons/"+itoa(cr.ID)+"/runs", bobToken, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob list alice runs: got %d want 403", w.Code)
	}
	w = e.do(t, http.MethodGet, "/crons/"+itoa(cr.ID)+"/runs/"+itoa(aliceRun.ID), bobToken, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob detail alice run: got %d want 403", w.Code)
	}
	w = e.do(t, http.MethodGet, "/crons/"+itoa(cr.ID)+"/runs", aliceToken, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"trigger":"manual"`) {
		t.Fatalf("alice cannot list own runs: %d %s", w.Code, w.Body.String())
	}

	adminBody := `{"name":"admin-cron","expression":"* * * * *","command":"id","server_ids":"` + itoa(e.bobS.ID) + `"}`
	w = e.do(t, http.MethodPost, "/crons", adminToken, adminBody)
	if w.Code != http.StatusOK {
		t.Fatalf("admin create cron on bob server: got %d: %s", w.Code, w.Body.String())
	}
}

func TestAlertCrossTenant(t *testing.T) {
	e := newAuthzEnv(t)
	aliceToken := e.token(t, e.alice)
	bobToken := e.token(t, e.bob)
	adminToken := e.token(t, e.admin)
	cron := model.Cron{Name: "alice-cron", Expression: "* * * * *", Command: "id", ServerIDs: itoa(e.aliceS.ID), OwnerID: e.alice.ID}
	if err := e.srv.DB.Create(&cron).Error; err != nil {
		t.Fatal(err)
	}
	body := `{"name":"alice-alert","metric":"cpu","duration":1,"server_ids":"` + itoa(e.aliceS.ID) + `","trigger_cron_id":` + itoa(cron.ID) + `}`
	w := e.do(t, http.MethodPost, "/alerts", aliceToken, body)
	if w.Code != http.StatusOK {
		t.Fatalf("alice create own alert: got %d", w.Code)
	}
	var a model.Alert
	if err := e.srv.DB.First(&a).Error; err != nil {
		t.Fatal(err)
	}
	if a.OwnerID != e.alice.ID {
		t.Fatalf("alert owner = %d", a.OwnerID)
	}
	w = e.do(t, http.MethodGet, "/alerts", bobToken, "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "alice-alert") {
		t.Fatalf("bob can see alice alert: %d %s", w.Code, w.Body.String())
	}
	w = e.do(t, http.MethodPut, "/alerts/"+itoa(a.ID), bobToken, `{"name":"hacked"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob update alert: got %d want 403", w.Code)
	}
	w = e.do(t, http.MethodDelete, "/alerts/"+itoa(a.ID), bobToken, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob delete alert: got %d want 403", w.Code)
	}
	w = e.do(t, http.MethodPost, "/alerts", aliceToken, `{"name":"cross","metric":"cpu","server_ids":"`+itoa(e.bobS.ID)+`"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice target bob server: got %d want 403", w.Code)
	}
	w = e.do(t, http.MethodPost, "/alerts", adminToken, `{"name":"global","metric":"cpu"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("admin global alert: got %d", w.Code)
	}
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
