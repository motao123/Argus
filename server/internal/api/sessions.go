package api

import (
	"crypto/rand"
	"encoding/hex"
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

// isJTIRevoked 检查 JTI 是否被踢出。
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
	revokeJTI(sess.JTI, sess.ExpiresAt)
	s.DB.Delete(&model.Session{}, id)
	ok(c, gin.H{"ok": true})
}

// revokeAllSessions 踢出用户全部会话（保留当前）。
func (s *Server) revokeAllSessions(c *gin.Context) {
	p := principalFromContext(c)
	var sessions []model.Session
	s.DB.Where("user_id = ?", p.UserID).Find(&sessions)
	curJTI := ""
	if cl, err := s.parseToken(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")); err == nil {
		curJTI = cl.RegisteredClaims.ID
	}
	for i := range sessions {
		if sessions[i].JTI == curJTI {
			continue
		}
		revokeJTI(sessions[i].JTI, sessions[i].ExpiresAt)
		s.DB.Delete(&model.Session{}, sessions[i].ID)
	}
	ok(c, gin.H{"ok": true, "revoked": len(sessions)})
}

var _ = hex.EncodeToString
var _ = rand.Read
