package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// listNAT NAT 配置列表。
func (s *Server) listNAT(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Order("id")
	if p != nil && !p.IsAdmin {
		q = q.Where("owner_id = ?", p.UserID)
	}
	var nats []model.NAT
	if err := q.Find(&nats).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"nats": nats})
}

func (s *Server) createNAT(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		ServerID   int64  `json:"server_id"`
		Domain     string `json:"domain"`
		TargetAddr string `json:"target_addr"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if req.ServerID <= 0 || req.Domain == "" || req.TargetAddr == "" {
		fail(c, http.StatusBadRequest, "server_id/domain/target_addr required")
		return
	}
	nat := model.NAT{
		OwnerID:    p.UserID,
		ServerID:   req.ServerID,
		Domain:     req.Domain,
		TargetAddr: req.TargetAddr,
		Enabled:    true,
	}
	if err := s.DB.Create(&nat).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, nat)
}

func (s *Server) updateNAT(c *gin.Context) {
	id := mustID(c)
	var nat model.NAT
	if err := s.DB.First(&nat, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&nat.OwnerID, c) {
		fail(c, http.StatusForbidden, "not yours")
		return
	}
	var req struct {
		ServerID   *int64  `json:"server_id"`
		Domain     *string `json:"domain"`
		TargetAddr *string `json:"target_addr"`
		Enabled    *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	updates := map[string]any{}
	if req.ServerID != nil {
		updates["server_id"] = *req.ServerID
	}
	if req.Domain != nil {
		updates["domain"] = *req.Domain
	}
	if req.TargetAddr != nil {
		updates["target_addr"] = *req.TargetAddr
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if err := s.DB.Model(&nat).Updates(updates).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

func (s *Server) deleteNAT(c *gin.Context) {
	id := mustID(c)
	var nat model.NAT
	if err := s.DB.First(&nat, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&nat.OwnerID, c) {
		fail(c, http.StatusForbidden, "not yours")
		return
	}
	s.DB.Delete(&model.NAT{}, id)
	ok(c, gin.H{"ok": true})
}
