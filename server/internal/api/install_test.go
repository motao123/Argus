package api

import (
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
	"github.com/motao123/Argus/server/internal/model"
)

// installTestEnv 用户（admin，含 agent_secret）+ 服务器测试环境。
type installTestEnv struct {
	srv    *Server
	admin  *model.User
	server model.Server
}

func newInstallEnv(t *testing.T) *installTestEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(
		&model.User{}, &model.Server{}, &model.Setting{},
	); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test123"), bcrypt.DefaultCost)
	admin := &model.User{Username: "admin", PasswordHash: string(hash), Role: model.RoleAdmin, AgentSecret: "admin-agent-secret"}
	if err := gdb.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	srvRow := model.Server{Name: "srv", Secret: agent.GenSecret(), OwnerID: admin.ID}
	if err := gdb.Create(&srvRow).Error; err != nil {
		t.Fatal(err)
	}
	return &installTestEnv{srv: &Server{DB: gdb}, admin: admin, server: srvRow}
}

// asUser 注入 JWT 用户 principal 的中间件。
func asUser(userID int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("principal", &principal{UserID: userID, IsAdmin: false})
	}
}

func TestInstallCommandUserMode(t *testing.T) {
	env := newInstallEnv(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api/v1", asUser(env.admin.ID))
	authed.GET("/install/command", env.srv.installCommand)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/install/command", nil)
	req.Host = "example.com"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Command   string `json:"command"`
			ScriptURL string `json:"script_url"`
			WSUrl     string `json:"ws_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Data.Command, "curl -fsSL http://example.com/install.sh | sh -s -- -s ") {
		t.Fatalf("unexpected command: %s", out.Data.Command)
	}
	if !strings.Contains(out.Data.Command, "ws://example.com/ws/agent") {
		t.Fatalf("ws url missing: %s", out.Data.Command)
	}
	if !strings.Contains(out.Data.Command, "admin-agent-secret") {
		t.Fatalf("user agent secret missing: %s", out.Data.Command)
	}
}

func TestServerInstallCommandUsesServerSecret(t *testing.T) {
	env := newInstallEnv(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api/v1", asUser(env.admin.ID))
	authed.GET("/servers/:id/install-command", env.srv.serverInstallCommand)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers/1/install-command", nil)
	req.Host = "example.com"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Command  string `json:"command"`
			ServerID int64  `json:"server_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.ServerID != env.server.ID || !strings.Contains(out.Data.Command, env.server.Secret) {
		t.Fatalf("unexpected: %+v command=%s", out.Data, out.Data.Command)
	}
}

func TestInstallCommandOtherUsersServerDenied(t *testing.T) {
	env := newInstallEnv(t)
	// 第二个用户
	hash, _ := bcrypt.GenerateFromPassword([]byte("test123"), bcrypt.DefaultCost)
	other := &model.User{Username: "bob", PasswordHash: string(hash), Role: model.RoleUser, AgentSecret: agent.GenSecret()}
	if err := env.srv.DB.Create(other).Error; err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api/v1", asUser(other.ID))
	authed.GET("/servers/:id/install-command", env.srv.serverInstallCommand)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers/1/install-command", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("other user should be denied, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInstallBaseURLOverride(t *testing.T) {
	env := newInstallEnv(t)
	env.srv.DB.Create(&model.Setting{Key: SettingInstallBaseURL, Value: "https://monitor.example.com"})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api/v1", asUser(env.admin.ID))
	authed.GET("/install/command", env.srv.installCommand)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/install/command", nil)
	req.Host = "example.com"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			Command string `json:"command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Data.Command, "https://monitor.example.com/install.sh") {
		t.Fatalf("base url override missing: %s", out.Data.Command)
	}
	if !strings.Contains(out.Data.Command, "wss://monitor.example.com/ws/agent") {
		t.Fatalf("wss derivation missing: %s", out.Data.Command)
	}
}
