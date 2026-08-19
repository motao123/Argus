package api

import (
	"fmt"
	"image/png"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"

	"github.com/motao123/Argus/server/internal/model"
)

// ---- 2FA（TOTP，借鉴 komari 2fa 设计）----

// twoFASetup 生成 TOTP secret 与二维码（未启用前可反复生成）。
func (s *Server) twoFASetup(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil {
		fail(c, http.StatusUnauthorized, "login required")
		return
	}
	var user model.User
	if err := s.DB.First(&user, p.UserID).Error; err != nil {
		fail(c, http.StatusNotFound, "user not found")
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Argus",
		AccountName: user.Username,
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 保存 secret（无论是否启用，setup 即写入；enable 时校验 code）
	if err := s.DB.Model(&user).Update("two_fa_secret", key.Secret()).Error; err != nil {
		s.auditLogResult(c, "auth.2fa_setup", fmt.Sprintf("user_id=%d", user.ID), "failure", "auth.2fa_setup_failed")
		fail(c, http.StatusInternalServerError, "save 2fa setup", "auth.2fa_setup_failed")
		return
	}
	ok(c, gin.H{"secret": key.Secret(), "otpauth_url": key.URL()})
	s.auditLogResult(c, "auth.2fa_setup", fmt.Sprintf("user_id=%d", user.ID), "success", "")
}

// twoFAQRCode 输出二维码 PNG（otpauth URL 渲染）。
func (s *Server) twoFAQRCode(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil {
		fail(c, http.StatusUnauthorized, "login required")
		return
	}
	var user model.User
	if err := s.DB.First(&user, p.UserID).Error; err != nil || user.TwoFASecret == "" {
		fail(c, http.StatusNotFound, "2fa not setup", "auth.2fa_not_setup")
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Argus",
		AccountName: user.Username,
		Secret:      []byte(user.TwoFASecret),
	})
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	img, err := key.Image(220, 220)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.Header("Content-Type", "image/png")
	_ = png.Encode(c.Writer, img)
}

// twoFAEnable 校验验证码并启用 2FA。
func (s *Server) twoFAEnable(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil {
		fail(c, http.StatusUnauthorized, "login required")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var user model.User
	if err := s.DB.First(&user, p.UserID).Error; err != nil {
		fail(c, http.StatusNotFound, "user not found")
		return
	}
	if user.TwoFASecret == "" {
		fail(c, http.StatusBadRequest, "run setup first", "auth.2fa_not_setup")
		return
	}
	if !totp.Validate(req.Code, user.TwoFASecret) {
		fail(c, http.StatusBadRequest, "invalid code", "auth.2fa_invalid")
		return
	}
	if err := s.DB.Model(&user).Update("two_fa_enabled", true).Error; err != nil {
		s.auditLogResult(c, "auth.2fa_enable", fmt.Sprintf("user_id=%d", user.ID), "failure", "auth.2fa_enable_failed")
		fail(c, http.StatusInternalServerError, "enable 2fa", "auth.2fa_enable_failed")
		return
	}
	ok(c, gin.H{"ok": true})
	s.auditLogResult(c, "auth.2fa_enable", fmt.Sprintf("user_id=%d", user.ID), "success", "")
}

// twoFADisable 校验验证码并关闭 2FA。
func (s *Server) twoFADisable(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil {
		fail(c, http.StatusUnauthorized, "login required")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var user model.User
	if err := s.DB.First(&user, p.UserID).Error; err != nil {
		fail(c, http.StatusNotFound, "user not found")
		return
	}
	if !totp.Validate(req.Code, user.TwoFASecret) {
		fail(c, http.StatusBadRequest, "invalid code", "auth.2fa_invalid")
		return
	}
	if err := s.DB.Model(&user).Update("two_fa_enabled", false).Error; err != nil {
		s.auditLogResult(c, "auth.2fa_disable", fmt.Sprintf("user_id=%d", user.ID), "failure", "auth.2fa_disable_failed")
		fail(c, http.StatusInternalServerError, "disable 2fa", "auth.2fa_disable_failed")
		return
	}
	ok(c, gin.H{"ok": true})
	s.auditLogResult(c, "auth.2fa_disable", fmt.Sprintf("user_id=%d", user.ID), "success", "")
}

// verifyTwoFA 校验登录/敏感操作提供的 TOTP 码。
func verifyTwoFA(user *model.User, code string) bool {
	if !user.TwoFAEnabled || user.TwoFASecret == "" {
		return true // 未启用 2FA 无需校验
	}
	return totp.Validate(code, user.TwoFASecret)
}

// sensitiveTwoFARequired is retained for WebSocket paths that cannot use the
// middleware wrapper; the WS handler performs its own code validation.
func (s *Server) sensitiveTwoFARequired(c *gin.Context, p *principal) bool {
	if p == nil || p.IsPAT || p.IsReadonly {
		return false
	}
	var user model.User
	if err := s.DB.First(&user, p.UserID).Error; err != nil {
		return false
	}
	return user.TwoFAEnabled && user.TwoFASecret != ""
}

func sensitiveAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := principalFromContext(c)
		if p == nil || !p.IsAdmin {
			fail(c, http.StatusForbidden, "admin only")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) sensitiveAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := principalFromContext(c)
		if p == nil || !p.IsAdmin {
			fail(c, http.StatusForbidden, "admin only")
			c.Abort()
			return
		}
		if !s.enforceSensitive2FA(c) {
			return
		}
		c.Next()
	}
}

// enforceSensitive2FA 在敏感操作入口校验 X-2FA-Code 头；
// 启用 2FA 的 JWT 用户未提供/校验失败时返回 428（前端提示输入验证码）。
// 返回 false 表示已写入响应，调用方应直接 return。
func (s *Server) enforceSensitive2FA(c *gin.Context) bool {
	p := principalFromContext(c)
	if p != nil && p.IsPAT {
		fail(c, http.StatusForbidden, "PAT is not allowed for sensitive operations", "auth.sensitive_pat_denied")
		c.Abort()
		return false
	}
	if p == nil || p.IsReadonly {
		fail(c, http.StatusForbidden, "sensitive operation requires an authenticated administrator", "auth.sensitive_denied")
		c.Abort()
		return false
	}
	var user model.User
	if err := s.DB.First(&user, p.UserID).Error; err != nil || !user.TwoFAEnabled || user.TwoFASecret == "" {
		fail(c, http.StatusPreconditionRequired, "2fa setup and enablement required", "auth.2fa_setup_required")
		c.Abort()
		return false
	}
	if verifyTwoFA(&user, c.GetHeader("X-2FA-Code")) {
		return true
	}
	fail(c, http.StatusPreconditionRequired, "2fa code required", "auth.2fa_required")
	c.Abort()
	return false
}
