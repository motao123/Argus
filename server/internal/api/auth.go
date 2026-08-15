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
type loginGuard struct {
	failCount int
	lockUntil time.Time
}

var loginGuards = struct {
	sync.Mutex
	m map[string]*loginGuard
}{m: make(map[string]*loginGuard)}

func loginAllowed(ip string) (bool, int) {
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

func loginFail(ip string) {
	loginGuards.Lock()
	defer loginGuards.Unlock()
	g, ok := loginGuards.m[ip]
	if !ok {
		g = &loginGuard{}
		loginGuards.m[ip] = g
	}
	g.failCount++
	if g.failCount >= 5 {
		g.lockUntil = time.Now().Add(5 * time.Minute)
		g.failCount = 0
	}
}

func loginSuccess(ip string) {
	loginGuards.Lock()
	defer loginGuards.Unlock()
	delete(loginGuards.m, ip)
}

// login 管理员登录，返回 JWT。
func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TwoFACode string `json:"two_fa_code"` // 启用 2FA 后必填
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	ip := currentIP(c)
	allowed, _ := loginAllowed(ip)
	if !allowed {
		fail(c, http.StatusTooManyRequests, "too many attempts, locked 5 minutes")
		return
	}
	var user model.User
	if err := s.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		loginFail(ip)
		fail(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		loginFail(ip)
		fail(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !verifyTwoFA(&user, req.TwoFACode) {
		loginFail(ip)
		fail(c, http.StatusUnauthorized, "invalid 2fa code")
		return
	}
	loginSuccess(ip)
	token, err := s.issueTokenWithSession(c, &user)
	if err != nil {
		fail(c, http.StatusInternalServerError, "issue token")
		return
	}
	ok(c, gin.H{"token": token, "username": user.Username})
}
