package api

import (
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

// readonlyEnv readonly 权限矩阵测试环境：admin + 普通用户 alice + 只读用户 ro。
type readonlyEnv struct {
	srv    *Server
	ro     *model.User
	roS    model.Server
	alice  *model.User
	aliceS model.Server
}

func newReadonlyEnv(t *testing.T) *readonlyEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(
		&model.User{}, &model.Server{}, &model.Service{}, &model.ServiceHistory{},
		&model.APIToken{}, &model.Setting{}, &model.DDNSProfile{}, &model.DDNSRecordState{}, &model.NAT{}, &model.Metric{},
		&model.AuditLog{}, &model.ServerTransfer{}, &model.Notification{}, &model.Alert{}, &model.Cron{}, &model.TaskRun{}, &model.TaskRunResult{},
		&model.UpgradeJob{}, &model.UpgradeResult{}, &model.Clipboard{}, &model.Session{}, &model.BackupSchedule{}, &model.BackupRun{},
		&model.Transfer{}, &model.TrafficBaseline{}, &model.RevokedSession{}, &model.NotificationGroup{},
	); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test123"), bcrypt.DefaultCost)
	admin := &model.User{Username: "admin", PasswordHash: string(hash), Role: model.RoleAdmin, AgentSecret: agent.GenSecret()}
	alice := &model.User{Username: "alice", PasswordHash: string(hash), Role: model.RoleUser, AgentSecret: agent.GenSecret()}
	ro := &model.User{Username: "ro", PasswordHash: string(hash), Role: model.RoleReadonly, AgentSecret: agent.GenSecret()}
	if err := gdb.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(ro).Error; err != nil {
		t.Fatal(err)
	}
	aliceS := model.Server{Name: "alice-srv", Secret: agent.GenSecret(), OwnerID: alice.ID}
	roS := model.Server{Name: "ro-srv", Secret: agent.GenSecret(), OwnerID: ro.ID}
	if err := gdb.Create(&aliceS).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&roS).Error; err != nil {
		t.Fatal(err)
	}
	st := store.NewHub()
	srv := &Server{DB: gdb, Cfg: &config.Config{JWTSecret: "test-secret"}, Store: st, Agents: agent.NewHub(gdb, st, store.NewMetricBatcher(gdb))}
	return &readonlyEnv{srv: srv, ro: ro, roS: roS, alice: alice, aliceS: aliceS}
}

func (e *readonlyEnv) token(t *testing.T, u *model.User) string {
	t.Helper()
	tok, err := e.srv.issueToken(u)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// doFull 用真实路由（api.New）发起请求，验证 readonlyGate 与各 handler 的组合行为。
func (e *readonlyEnv) doFull(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := New(e.srv)
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
	r.ServeHTTP(w, req)
	return w
}

// TestReadonlyPermissionMatrix 只读角色权限矩阵：
//
//	放行：公开+自有服务器与状态、自助账号（me/改密/2FA/会话/令牌/剪贴板）
//	拒绝：执行/文件/任务/告警/服务/配置等一切写操作与敏感读
func TestReadonlyPermissionMatrix(t *testing.T) {
	e := newReadonlyEnv(t)
	roToken := e.token(t, e.ro)
	aliceToken := e.token(t, e.alice)

	// ---- 放行（只读状态与自助） ----
	allowed := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/auth/me", ""},
		{http.MethodGet, "/api/v1/servers", ""},
		{http.MethodGet, "/api/v1/servers/" + itoa(e.roS.ID) + "/metrics?period=1h", ""},
		{http.MethodGet, "/api/v1/servers/" + itoa(e.roS.ID) + "/transfer", ""},
		{http.MethodGet, "/api/v1/services", ""},
		{http.MethodGet, "/api/v1/sessions", ""},
		{http.MethodGet, "/api/v1/tokens", ""},
		{http.MethodGet, "/api/v1/clipboard", ""},
		{http.MethodPut, "/api/v1/users/" + itoa(e.ro.ID), `{"password":"newpass123"}`},
		{http.MethodGet, "/api/v1/auth/2fa/setup", ""},
	}
	for _, tc := range allowed {
		w := e.doFull(t, tc.method, tc.path, roToken, tc.body)
		if w.Code != http.StatusOK {
			t.Errorf("readonly ALLOW %s %s: got %d want 200 (%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	// ---- 拒绝（写操作与敏感读） ----
	denied := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/servers", `{"name":"x"}`},
		{http.MethodPut, "/api/v1/servers/" + itoa(e.roS.ID), `{"name":"hack"}`},
		{http.MethodDelete, "/api/v1/servers/" + itoa(e.roS.ID), ""},
		{http.MethodPost, "/api/v1/servers/" + itoa(e.roS.ID) + "/exec", `{"command":"id"}`},
		{http.MethodGet, "/api/v1/files/" + itoa(e.roS.ID), ""},
		{http.MethodPost, "/api/v1/files/" + itoa(e.roS.ID) + "/read", `{"path":"/etc/shadow"}`},
		{http.MethodPost, "/api/v1/files/" + itoa(e.roS.ID) + "/write", `{"path":"/tmp/x","content":"y"}`},
		{http.MethodPost, "/api/v1/files/" + itoa(e.roS.ID) + "/delete", `{"path":"/tmp/x"}`},
		{http.MethodGet, "/api/v1/crons", ""},
		{http.MethodPost, "/api/v1/crons", `{"name":"c","expression":"* * * * *","command":"id"}`},
		{http.MethodPost, "/api/v1/crons/1/run", ""},
		{http.MethodGet, "/api/v1/alerts", ""},
		{http.MethodPost, "/api/v1/alerts", `{"name":"a","metric":"cpu"}`},
		{http.MethodPut, "/api/v1/alerts/1", `{"name":"a2"}`},
		{http.MethodDelete, "/api/v1/alerts/1", ""},
		{http.MethodPost, "/api/v1/services", `{"server_id":1,"name":"s","type":"http","target":"http://x"}`},
		{http.MethodPost, "/api/v1/ddns", `{"server_id":1,"name":"d","domains":"a.example"}`},
		{http.MethodPost, "/api/v1/nats", `{"server_id":1,"domain":"n.example","target_addr":"127.0.0.1:80"}`},
		{http.MethodPost, "/api/v1/tokens", `{"name":"t","scopes":["argus:server:read"]}`},
		{http.MethodDelete, "/api/v1/tokens/1", ""},
		{http.MethodDelete, "/api/v1/sessions/1", ""},
		{http.MethodPost, "/api/v1/clipboard", `{"title":"t","content":"c"}`},
		{http.MethodGet, "/api/v1/users", ""},
		{http.MethodPost, "/api/v1/users", `{"username":"x","password":"123456"}`},
		{http.MethodGet, "/api/v1/admin/backup-schedules", ""},
		{http.MethodPost, "/api/v1/admin/backup-schedules", `{"name":"b","cron":"0 3 * * *","target":"/tmp"}`},
		{http.MethodPost, "/api/v1/admin/backup-schedules/1/run", ""},
		{http.MethodPost, "/api/v1/admin/backup-schedules/1/drill", ""},
		{http.MethodGet, "/api/v1/admin/logs", ""},
		{http.MethodGet, "/api/v1/settings", ""},
		{http.MethodGet, "/api/v1/notifications", ""},
		{http.MethodPost, "/api/v1/groups", `{"name":"g"}`},
		{http.MethodPost, "/api/v1/notification-groups", `{"name":"g"}`},
		{http.MethodPost, "/api/v1/server-transfers", `{"server_id":1,"to_user_id":2}`},
		{http.MethodGet, "/api/v1/upgrade-jobs", ""},
		{http.MethodGet, "/api/v1/admin/backup", ""},
	}
	for _, tc := range denied {
		w := e.doFull(t, tc.method, tc.path, roToken, tc.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("readonly DENY %s %s: got %d want 403 (%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	// ---- 隔离：只读用户仅见自有服务器，看不到他人 ----
	w := e.doFull(t, http.MethodGet, "/api/v1/servers", roToken, "")
	if strings.Contains(w.Body.String(), "alice-srv") || !strings.Contains(w.Body.String(), "ro-srv") {
		t.Fatalf("readonly server list leaked/collapsed: %s", w.Body.String())
	}
	// 他人服务器指标 → 404（无权）
	w = e.doFull(t, http.MethodGet, "/api/v1/servers/"+itoa(e.aliceS.ID)+"/metrics?period=1h", roToken, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("readonly cross-owner metrics: got %d want 404", w.Code)
	}

	// ---- 终端 WS 对 readonly 拒绝（authWS 层） ----
	w = e.doFull(t, http.MethodGet, "/api/v1/terminal/"+itoa(e.roS.ID), roToken, "")
	if w.Code != http.StatusForbidden {
		t.Errorf("readonly terminal: got %d want 403", w.Code)
	}

	// ---- 角色变更仍需 admin：readonly 改他人密码被拒 ----
	w = e.doFull(t, http.MethodPut, "/api/v1/users/"+itoa(e.alice.ID), roToken, `{"password":"hacked123"}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("readonly change other password: got %d want 403", w.Code)
	}

	// ---- 普通用户（user）不受只读门限制（回归：默认角色兼容） ----
	w = e.doFull(t, http.MethodPost, "/api/v1/crons", aliceToken, `{"name":"alice-cron","expression":"* * * * *","command":"id"}`)
	if w.Code != http.StatusOK {
		t.Errorf("user create cron: got %d want 200 (%s)", w.Code, w.Body.String())
	}
	w = e.doFull(t, http.MethodGet, "/api/v1/users", aliceToken, "")
	if w.Code != http.StatusForbidden {
		t.Errorf("user list users: got %d want 403", w.Code)
	}
}

// TestReadonlyRoleUserManagement readonly 角色可持久化并被身份识别。
func TestReadonlyRoleUserManagement(t *testing.T) {
	e := newReadonlyEnv(t)

	// IsValidRole 校验
	if !model.IsValidRole(model.RoleReadonly) || model.IsValidRole("superuser") {
		t.Fatal("IsValidRole wrong")
	}
	ro := &model.User{Username: "ro2", PasswordHash: "x", Role: model.RoleReadonly, AgentSecret: agent.GenSecret()}
	if err := e.srv.DB.Create(ro).Error; err != nil {
		t.Fatal(err)
	}
	var back model.User
	e.srv.DB.First(&back, ro.ID)
	if back.Role != model.RoleReadonly {
		t.Fatalf("role = %q want readonly", back.Role)
	}
	// 只读用户登录后可识别为 readonly（identify）
	hash, _ := bcrypt.GenerateFromPassword([]byte("readonlypass"), bcrypt.DefaultCost)
	roLogin := &model.User{Username: "ro3", PasswordHash: string(hash), Role: model.RoleReadonly, AgentSecret: agent.GenSecret()}
	if err := e.srv.DB.Create(roLogin).Error; err != nil {
		t.Fatal(err)
	}
	p, err := e.srv.identify(e.token(t, roLogin))
	if err != nil {
		t.Fatal(err)
	}
	if p.IsAdmin || !p.IsReadonly {
		t.Fatalf("identify readonly = admin:%v readonly:%v", p.IsAdmin, p.IsReadonly)
	}
}
