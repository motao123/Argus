package api

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
)

// transferTTL 待验证过户超时（30 分钟自动回滚）。
const transferTTL = 30 * time.Minute

// listTransfers 过户记录（admin）。
func (s *Server) listTransfers(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	s.sweepTransfers()
	var ts []model.ServerTransfer
	if err := s.DB.Order("id DESC").Limit(100).Find(&ts).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"transfers": ts})
}

// createTransfer 发起过户：轮换服务器密钥为一次性握手密钥，pending。
func (s *Server) createTransfer(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		ServerID int64 `json:"server_id"`
		ToUserID int64 `json:"to_user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ServerID <= 0 || req.ToUserID <= 0 {
		fail(c, http.StatusBadRequest, "server_id/to_user_id required")
		return
	}
	var srv model.Server
	if err := s.DB.First(&srv, req.ServerID).Error; err != nil {
		fail(c, http.StatusNotFound, "server not found")
		return
	}
	var to model.User
	if err := s.DB.First(&to, req.ToUserID).Error; err != nil {
		fail(c, http.StatusNotFound, "target user not found")
		return
	}
	if srv.OwnerID == req.ToUserID {
		fail(c, http.StatusBadRequest, "server already owned by target user")
		return
	}
	// 单服务器只能有一个活跃过户
	var active int64
	s.DB.Model(&model.ServerTransfer{}).
		Where("server_id = ? AND status = 'pending'", req.ServerID).Count(&active)
	if active > 0 {
		fail(c, http.StatusConflict, "active transfer already exists for this server")
		return
	}
	oldSecret := srv.Secret
	newSecret := agent.GenSecret()
	// 轮换密钥：旧密钥保存为回滚密钥；新 owner 用新密钥重连即验证
	if err := s.DB.Model(&srv).Update("secret", newSecret).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 现有连接使用旧密钥，立即断开
	if peer := s.Agents.Peer(srv.ID); peer != nil {
		_ = peer.Close()
	}
	s.Store.Remove(srv.ID)
	t := model.ServerTransfer{
		ServerID:       srv.ID,
		ServerName:     srv.Name,
		FromUserID:     srv.OwnerID,
		ToUserID:       to.ID,
		ToUsername:     to.Username,
		Status:         "pending",
		NewSecret:      newSecret,
		RollbackSecret: oldSecret,
	}
	if err := s.DB.Create(&t).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "transfer.create", srv.Name+" -> "+to.Username)
	ok(c, gin.H{"transfer": t, "new_secret": newSecret, "note": "将新密钥交给目标用户配置 Agent，重连即完成过户"})
}

// cancelTransfer 取消过户：回滚密钥与 owner，关闭待验证记录。
func (s *Server) cancelTransfer(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	var t model.ServerTransfer
	if err := s.DB.First(&t, id).Error; err != nil {
		fail(c, http.StatusNotFound, "transfer not found")
		return
	}
	if t.Status != "pending" {
		fail(c, http.StatusConflict, "transfer not pending")
		return
	}
	if err := s.DB.Model(&model.Server{}).Where("id = ?", t.ServerID).
		Update("secret", t.RollbackSecret).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.DB.Model(&t).Updates(map[string]any{"status": "cancelled", "updated_at": time.Now()})
	s.auditLog(c, "transfer.cancel", t.ServerName)
	ok(c, gin.H{"ok": true})
}

// verifyTransfer 由 Agent 重连回调触发：新密钥匹配 → 过户完成。
func (s *Server) VerifyTransfer(serverID int64) {
	var t model.ServerTransfer
	if err := s.DB.Where("server_id = ? AND status = 'pending'", serverID).Order("id DESC").First(&t).Error; err != nil {
		return
	}
	var srv model.Server
	if err := s.DB.First(&srv, serverID).Error; err != nil {
		return
	}
	// Agent 已用新密钥重连：过户成立
	s.DB.Model(&srv).Update("owner_id", t.ToUserID)
	s.DB.Model(&t).Updates(map[string]any{"status": "verified", "updated_at": time.Now()})
	log.Printf("transfer #%d verified: server %s -> user %s", t.ID, t.ServerName, t.ToUsername)
}

// sweepTransfers 超时未验证的 pending 过户自动回滚。
func (s *Server) sweepTransfers() {
	var pending []model.ServerTransfer
	s.DB.Where("status = 'pending'").Find(&pending)
	for i := range pending {
		t := &pending[i]
		if time.Since(t.UpdatedAt) < transferTTL {
			continue
		}
		s.DB.Model(&model.Server{}).Where("id = ?", t.ServerID).
			Update("secret", t.RollbackSecret)
		s.DB.Model(t).Updates(map[string]any{"status": "failed", "updated_at": time.Now()})
		log.Printf("transfer #%d timed out, rolled back secret", t.ID)
	}
}
