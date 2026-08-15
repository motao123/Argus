package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// batchDeleteServers 批量删除服务器（借鉴 nezha batch-delete）。
func (s *Server) batchDeleteServers(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "ids required")
		return
	}
	for _, id := range req.IDs {
		if peer := s.Agents.Peer(id); peer != nil {
			_ = peer.Close()
		}
		s.Store.Remove(id)
		s.DB.Delete(&model.Server{}, id)
	}
	s.auditLog(c, "server.batch_delete", "")
	ok(c, gin.H{"ok": true, "deleted": len(req.IDs)})
}

// batchMoveServers 批量移动分组（借鉴 nezha batch-move）。
func (s *Server) batchMoveServers(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		IDs   []int64 `json:"ids"`
		Group string  `json:"group"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "ids required")
		return
	}
	res := s.DB.Model(&model.Server{}).Where("id IN ?", req.IDs).
		Updates(map[string]any{"group_name": req.Group})
	if res.Error != nil {
		fail(c, http.StatusInternalServerError, res.Error.Error())
		return
	}
	s.auditLog(c, "server.batch_move", req.Group)
	ok(c, gin.H{"ok": true, "moved": res.RowsAffected})
}
