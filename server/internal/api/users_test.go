package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/config"
	"github.com/motao123/Argus/server/internal/model"
)

// newUsersTestServer 构造仅含用户表的内存库测试服务。
func newUsersTestServer(t *testing.T) (*Server, *model.User, *model.User) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test123"), bcrypt.DefaultCost)
	admin := &model.User{Username: "admin", PasswordHash: string(hash), Role: model.RoleAdmin, AgentSecret: agent.GenSecret()}
	if err := gdb.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	alice := &model.User{Username: "alice", PasswordHash: string(hash), Role: model.RoleUser, AgentSecret: agent.GenSecret()}
	if err := gdb.Create(alice).Error; err != nil {
		t.Fatal(err)
	}
	return &Server{DB: gdb, Cfg: &config.Config{JWTSecret: "test-secret"}}, admin, alice
}

func TestGetUserSecret(t *testing.T) {
	s, admin, alice := newUsersTestServer(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", s.authMiddleware())
	authed.GET("/users/:id/secret", s.getUserSecret)

	adminToken, err := s.issueToken(admin)
	if err != nil {
		t.Fatal(err)
	}

	// admin 可读取任意用户密钥
	req := httptest.NewRequest(http.MethodGet, "/users/"+itoa(alice.ID)+"/secret", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin read secret: got %d want 200", w.Code)
	}
	var resp struct {
		Data struct {
			AgentSecret string `json:"agent_secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.AgentSecret != alice.AgentSecret {
		t.Fatalf("secret mismatch: got %q want %q", resp.Data.AgentSecret, alice.AgentSecret)
	}

	// 普通用户禁止读取他人密钥
	aliceToken, err := s.issueToken(alice)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/users/"+itoa(admin.ID)+"/secret", nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("user read others secret: got %d want 403", w.Code)
	}

	// 未认证 401
	req = httptest.NewRequest(http.MethodGet, "/users/"+itoa(alice.ID)+"/secret", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("guest read secret: got %d want 401", w.Code)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
