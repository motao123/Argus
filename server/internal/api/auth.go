package api

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifyctx"
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
	s.notifyLogin(&user, ip, c.GetHeader("User-Agent"))
	ok(c, gin.H{"token": token, "username": user.Username})
}

// notifyLogin 登录成功通知（借鉴 komari Login 事件；需配置 login_notify_webhook_id）。
func (s *Server) notifyLogin(user *model.User, ip, userAgent string) {
	if s.Notifier == nil {
		return
	}
	idStr := s.GetSetting(SettingLoginNotifyWebhook, "0")
	webhookID, _ := strconv.ParseInt(idStr, 10, 64)
	if webhookID <= 0 {
		return
	}
	var n model.Notification
	if err := s.DB.First(&n, webhookID).Error; err != nil {
		return
	}
	title := fmt.Sprintf("[Argus] 新登录 %s", user.Username)
	content := fmt.Sprintf("用户 %s 于 %s 登录成功\nIP: %s\nUser-Agent: %s", user.Username, time.Now().Format(time.RFC3339), ip, userAgent)
	ctx := &notifyctx.Ctx{
		Event:   "login",
		Title:   title,
		Content: content,
		Time:    notifyctx.FormatTime(time.Now()),
		Extras: map[string]string{
			"user":     user.Username,
			"login_ip": ip,
			"ua":       userAgent,
		},
	}
	_ = s.Notifier.EnqueueCtx(&n, title, content, user.ID, ctx.Flat())
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
