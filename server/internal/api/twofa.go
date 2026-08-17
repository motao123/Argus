package api

import (
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
	s.DB.Model(&user).Update("two_fa_secret", key.Secret())
	ok(c, gin.H{"secret": key.Secret(), "otpauth_url": key.URL()})
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
	s.DB.Model(&user).Update("two_fa_enabled", true)
	ok(c, gin.H{"ok": true})
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
	s.DB.Model(&user).Update("two_fa_enabled", false)
	ok(c, gin.H{"ok": true})
}

// verifyTwoFA 校验登录/敏感操作提供的 TOTP 码。
func verifyTwoFA(user *model.User, code string) bool {
	if !user.TwoFAEnabled || user.TwoFASecret == "" {
		return true // 未启用 2FA 无需校验
	}
	return totp.Validate(code, user.TwoFASecret)
}

// sensitiveTwoFARequired 判断当前请求是否需要敏感操作二次验证：
// JWT 用户启用 2FA 时要求 X-2FA-Code 头（PAT 豁免，避免脚本/自动化受阻）。
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

// enforceSensitive2FA 在敏感操作入口校验 X-2FA-Code 头；
// 启用 2FA 的 JWT 用户未提供/校验失败时返回 428（前端提示输入验证码）。
// 返回 false 表示已写入响应，调用方应直接 return。
func (s *Server) enforceSensitive2FA(c *gin.Context) bool {
	p := principalFromContext(c)
	if !s.sensitiveTwoFARequired(c, p) {
		return true
	}
	var user model.User
	if err := s.DB.First(&user, p.UserID).Error; err != nil {
		return true
	}
	if verifyTwoFA(&user, c.GetHeader("X-2FA-Code")) {
		return true
	}
	fail(c, http.StatusPreconditionRequired, "2fa code required", "auth.2fa_required")
	c.Abort()
	return false
}
