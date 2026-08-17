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
