package api

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

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
	s.SweepTransfers()
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
	t, newSecret, err := s.startTransfer(&srv, &to)
	if errors.Is(err, errActiveTransfer) {
		fail(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.disconnectTransferServer(srv.ID)
	s.auditLog(c, "transfer.create", srv.Name+" -> "+to.Username)
	ok(c, gin.H{"transfer": t, "new_secret": newSecret, "note": "将新密钥交给目标用户配置 Agent，重连即完成过户"})
}

var errActiveTransfer = errors.New("active transfer already exists for this server")

func (s *Server) startTransfer(srv *model.Server, to *model.User) (model.ServerTransfer, string, error) {
	newSecret := agent.GenSecret()
	t := model.ServerTransfer{
		ServerID:       srv.ID,
		ServerName:     srv.Name,
		FromUserID:     srv.OwnerID,
		ToUserID:       to.ID,
		ToUsername:     to.Username,
		Status:         "pending",
		NewSecret:      newSecret,
		RollbackSecret: srv.Secret,
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var active int64
		if err := tx.Model(&model.ServerTransfer{}).
			Where("server_id = ? AND status = 'pending'", srv.ID).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return errActiveTransfer
		}
		if err := tx.Model(&model.Server{}).Where("id = ?", srv.ID).Update("secret", newSecret).Error; err != nil {
			return err
		}
		return tx.Create(&t).Error
	})
	return t, newSecret, err
}

func (s *Server) disconnectTransferServer(serverID int64) {
	if s.Agents != nil {
		if peer := s.Agents.Peer(serverID); peer != nil {
			_ = peer.Close()
		}
	}
	if s.Store != nil {
		s.Store.Remove(serverID)
	}
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
	if err := s.rollbackTransfer(&t, "cancelled"); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.disconnectTransferServer(t.ServerID)
	s.auditLog(c, "transfer.cancel", t.ServerName)
	ok(c, gin.H{"ok": true})
}

// retryTransfer 对已超时失败的过户创建一次新的待验证尝试。
func (s *Server) retryTransfer(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	var previous model.ServerTransfer
	if err := s.DB.First(&previous, id).Error; err != nil {
		fail(c, http.StatusNotFound, "transfer not found")
		return
	}
	if previous.Status != "failed" {
		fail(c, http.StatusConflict, "only failed transfers can be retried")
		return
	}
	var srv model.Server
	if err := s.DB.First(&srv, previous.ServerID).Error; err != nil {
		fail(c, http.StatusNotFound, "server not found")
		return
	}
	var to model.User
	if err := s.DB.First(&to, previous.ToUserID).Error; err != nil {
		fail(c, http.StatusNotFound, "target user not found")
		return
	}
	if srv.OwnerID == to.ID {
		fail(c, http.StatusConflict, "server already owned by target user")
		return
	}
	t, newSecret, err := s.startTransfer(&srv, &to)
	if errors.Is(err, errActiveTransfer) {
		fail(c, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.disconnectTransferServer(srv.ID)
	s.auditLog(c, "transfer.retry", previous.ServerName+" -> "+previous.ToUsername)
	ok(c, gin.H{"transfer": t, "new_secret": newSecret, "note": "将新密钥交给目标用户配置 Agent，重连即完成过户"})
}

func (s *Server) rollbackTransfer(t *model.ServerTransfer, status string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Server{}).
			Where("id = ? AND secret = ?", t.ServerID, t.NewSecret).
			Update("secret", t.RollbackSecret)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("server secret changed since transfer started")
		}
		result = tx.Model(&model.ServerTransfer{}).
			Where("id = ? AND status = 'pending'", t.ID).
			Updates(map[string]any{"status": status, "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("transfer is no longer pending")
		}
		return nil
	})
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

// SweepTransfers 超时未验证的 pending 过户自动回滚。由后台任务与列表读取共同触发。
func (s *Server) SweepTransfers() {
	var pending []model.ServerTransfer
	if err := s.DB.Where("status = 'pending' AND updated_at < ?", time.Now().Add(-transferTTL)).Find(&pending).Error; err != nil {
		log.Printf("transfer sweep: %v", err)
		return
	}
	for i := range pending {
		t := &pending[i]
		if err := s.rollbackTransfer(t, "failed"); err != nil {
			log.Printf("transfer #%d timeout rollback skipped: %v", t.ID, err)
			continue
		}
		s.disconnectTransferServer(t.ServerID)
		log.Printf("transfer #%d timed out, rolled back secret", t.ID)
	}
}
