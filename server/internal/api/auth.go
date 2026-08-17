package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/motao123/Argus/server/internal/model"
)

// 登录限流：单 IP 5 次失败锁定 5 分钟。
// 封禁写入持久化封禁表（source=login），服务重启仍生效；管理员解封即时恢复。
type loginGuard struct {
	failCount int
	lockUntil time.Time
}

var loginGuards = struct {
	sync.Mutex
	m map[string]*loginGuard
}{m: make(map[string]*loginGuard)}

func (s *Server) loginAllowed(ip string) (bool, int) {
	// 持久化封禁优先（手动封禁 / 登录限流 / 速率超限均阻止登录）
	if s.wafMgr().check(ip) {
		return false, 0
	}
	loginGuards.Lock()
	defer loginGuards.Unlock()
	g, ok := loginGuards.m[ip]
	if !ok {
		loginGuards.m[ip] = &loginGuard{}
		return true, 5
	}
	if time.Now().Before(g.lockUntil) {
		return false, 0
	}
	return true, 5 - g.failCount
}

func (s *Server) loginFail(ip string) {
	lock := false
	loginGuards.Lock()
	g, ok := loginGuards.m[ip]
	if !ok {
		g = &loginGuard{}
		loginGuards.m[ip] = g
	}
	g.failCount++
	if g.failCount >= 5 {
		g.lockUntil = time.Now().Add(5 * time.Minute)
		g.failCount = 0
		lock = true
	}
	loginGuards.Unlock()
	if lock {
		s.wafMgr().ban(ip, "5 failed login attempts", model.BanSourceLogin, 5*time.Minute)
	}
}

func (s *Server) loginSuccess(ip string) {
	loginGuards.Lock()
	defer loginGuards.Unlock()
	delete(loginGuards.m, ip)
}

// login 管理员登录，返回 JWT。
func (s *Server) login(c *gin.Context) {
	var req struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		TwoFACode string `json:"two_fa_code"` // 启用 2FA 后必填
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	ip := currentIP(c)
	allowed, _ := s.loginAllowed(ip)
	if !allowed {
		fail(c, http.StatusTooManyRequests, "too many attempts, locked 5 minutes", "auth.too_many_attempts")
		return
	}
	var user model.User
	if err := s.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		s.loginFail(ip)
		fail(c, http.StatusUnauthorized, "invalid credentials", "auth.invalid_credentials")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		s.loginFail(ip)
		fail(c, http.StatusUnauthorized, "invalid credentials", "auth.invalid_credentials")
		return
	}
	if !verifyTwoFA(&user, req.TwoFACode) {
		s.loginFail(ip)
		fail(c, http.StatusUnauthorized, "invalid 2fa code", "auth.2fa_invalid")
		return
	}
	s.loginSuccess(ip)
	token, err := s.issueTokenWithSession(c, &user)
	if err != nil {
		fail(c, http.StatusInternalServerError, "issue token")
		return
	}
	ok(c, gin.H{"token": token, "username": user.Username})
}

// me 当前登录用户信息（含 2FA 状态）。
func (s *Server) me(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil {
		fail(c, http.StatusUnauthorized, "login required")
		return
	}
	var user model.User
	if err := s.DB.First(&user, p.UserID).Error; err != nil {
		fail(c, http.StatusUnauthorized, "user not found")
		return
	}
	ok(c, gin.H{
		"username":       user.Username,
		"role":           user.Role,
		"two_fa_enabled": user.TwoFAEnabled,
	})
}
