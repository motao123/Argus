package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// ---- 服务器过户转移（借鉴 nezha ServerTransfer）----

// transferServer 将服务器过户给目标用户：改 owner + 轮换密钥。
// 仅 admin 可执行（简化版：不做 nezha 的双向确认状态机）。
func (s *Server) transferServer(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		ServerID    int64 `json:"server_id"`
		TargetUserID int64 `json:"target_user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var srv model.Server
	if err := s.DB.First(&srv, req.ServerID).Error; err != nil {
		fail(c, http.StatusNotFound, "server not found")
		return
	}
	var target model.User
	if err := s.DB.First(&target, req.TargetUserID).Error; err != nil {
		fail(c, http.StatusNotFound, "target user not found")
		return
	}

	// 轮换密钥：旧 agent 连接踢下线，新密钥需要 agent 重新注册
	newSecret := agentGenSecret()
	if err := s.DB.Model(&srv).Updates(map[string]any{
		"owner_id": target.ID,
		"secret":   newSecret,
	}).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if peer := s.Agents.Peer(srv.ID); peer != nil {
		_ = peer.Close()
	}
	s.Store.Upsert(&srv)
	s.Store.MarkOffline(srv.ID)

	ok(c, gin.H{
		"ok":           true,
		"server_id":    srv.ID,
		"new_owner":    target.Username,
		"new_secret":   newSecret, // 新密钥仅此一次返回
		"note":         "agent 需用新密钥重新注册",
	})
}
