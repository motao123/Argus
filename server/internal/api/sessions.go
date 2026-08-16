package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// ---- 会话管理（登录时记录，支持踢出）----

// 内存 JWT 黑名单（jti → 过期时间），服务重启后由 DB 会话恢复。
var (
	tokenBlacklistMu sync.Mutex
	tokenBlacklist   = map[string]time.Time{}
)

// revokeJTI 将 JTI 加入黑名单。
func revokeJTI(jti string, until time.Time) {
	tokenBlacklistMu.Lock()
	tokenBlacklist[jti] = until
	tokenBlacklistMu.Unlock()
}

// persistRevokedJTI keeps a revoked token invalid after a process restart.
func (s *Server) persistRevokedJTI(jti string, until time.Time) {
	if jti == "" || until.IsZero() || !until.After(time.Now()) {
		return
	}
	revokeJTI(jti, until)
	var revoked model.RevokedSession
	if err := s.DB.Where("jti = ?", jti).First(&revoked).Error; err != nil {
		s.DB.Create(&model.RevokedSession{JTI: jti, ExpiresAt: until})
	} else if revoked.ExpiresAt.Before(until) {
		s.DB.Model(&revoked).Update("expires_at", until)
	}
}

// isJTIRevoked checks the in-memory blacklist.
func isJTIRevoked(jti string) bool {
	tokenBlacklistMu.Lock()
	defer tokenBlacklistMu.Unlock()
	until, ok := tokenBlacklist[jti]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(tokenBlacklist, jti)
		return false
	}
	return true
}

// isJTIRevoked checks memory first, then the persistent revocation table.
func (s *Server) isJTIRevoked(jti string) bool {
	if isJTIRevoked(jti) {
		return true
	}
	var revoked model.RevokedSession
	if err := s.DB.Where("jti = ?", jti).First(&revoked).Error; err != nil {
		return false
	}
	if time.Now().After(revoked.ExpiresAt) {
		s.DB.Delete(&revoked)
		return false
	}
	revokeJTI(jti, revoked.ExpiresAt)
	return true
}

// issueTokenWithSession 签发 JWT 并记录会话（复用 issueToken + 新增 jti）。
func (s *Server) issueTokenWithSession(c *gin.Context, u *model.User) (string, error) {
	jti := randomHex(16)
	token, err := s.issueTokenWithJTI(u, jti)
	if err != nil {
		return "", err
	}
	sess := model.Session{
		UserID:    u.ID,
		JTI:       jti,
		UserAgent: c.GetHeader("User-Agent"),
		IP:        currentIP(c),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}
	s.DB.Create(&sess)
	return token, nil
}

// listSessions 当前用户会话列表（admin 可查他人）。
func (s *Server) listSessions(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Order("id DESC").Limit(50)
	if !p.IsAdmin {
		q = q.Where("user_id = ?", p.UserID)
	}
	var sessions []model.Session
	if err := q.Find(&sessions).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"sessions": sessions})
}

// revokeSession 踢出会话（本人或 admin）。
func (s *Server) revokeSession(c *gin.Context) {
	p := principalFromContext(c)
	id := mustID(c)
	var sess model.Session
	if err := s.DB.First(&sess, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if sess.UserID != p.UserID && !p.IsAdmin {
		fail(c, http.StatusForbidden, "not your session")
		return
	}
	s.persistRevokedJTI(sess.JTI, sess.ExpiresAt)
	s.DB.Delete(&model.Session{}, id)
	s.auditLog(c, "session.revoke", fmt.Sprintf("session_id=%d", id))
	ok(c, gin.H{"ok": true})
}

// revokeAllSessions 踢出当前用户全部会话（保留当前）。管理员可通过 user_id 指定用户。
func (s *Server) revokeAllSessions(c *gin.Context) {
	p := principalFromContext(c)
	userID := p.UserID
	if p.IsAdmin && c.Query("user_id") != "" {
		userID = parseIntQuery64(c.Query("user_id"))
		if userID <= 0 {
			fail(c, http.StatusBadRequest, "invalid user_id")
			return
		}
	}
	var sessions []model.Session
	s.DB.Where("user_id = ?", userID).Find(&sessions)
	curJTI := ""
	if cl, err := s.parseToken(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")); err == nil {
		curJTI = cl.RegisteredClaims.ID
	}
	revoked := 0
	for i := range sessions {
		if sessions[i].JTI == curJTI {
			continue
		}
		s.persistRevokedJTI(sessions[i].JTI, sessions[i].ExpiresAt)
		s.DB.Delete(&model.Session{}, sessions[i].ID)
		revoked++
	}
	s.auditLog(c, "session.revoke_all", fmt.Sprintf("user_id=%d revoked=%d", userID, revoked))
	ok(c, gin.H{"ok": true, "revoked": revoked})
}

var _ = hex.EncodeToString
var _ = rand.Read
