package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/motao123/Argus/server/internal/model"
)

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
	var user model.User
	if err := s.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		fail(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		fail(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !verifyTwoFA(&user, req.TwoFACode) {
		fail(c, http.StatusUnauthorized, "invalid 2fa code")
		return
	}
	token, err := s.issueTokenWithSession(c, &user)
	if err != nil {
		fail(c, http.StatusInternalServerError, "issue token")
		return
	}
	ok(c, gin.H{"token": token, "username": user.Username})
}
