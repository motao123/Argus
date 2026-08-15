package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// getOfflineNotify 离线通知配置。
func (s *Server) getOfflineNotify(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var cfg model.OfflineNotify
	if err := s.DB.First(&cfg).Error; err != nil {
		cfg = model.OfflineNotify{OfflineAfter: 60, Enabled: true}
	}
	ok(c, cfg)
}

func (s *Server) saveOfflineNotify(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		WebhookID    int64 `json:"webhook_id"`
		OfflineAfter int   `json:"offline_after"`
		Enabled      bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var cfg model.OfflineNotify
	if err := s.DB.First(&cfg).Error; err == nil {
		s.DB.Model(&cfg).Updates(map[string]any{
			"webhook_id":    req.WebhookID,
			"offline_after": req.OfflineAfter,
			"enabled":       req.Enabled,
		})
	} else {
		s.DB.Create(&model.OfflineNotify{
			WebhookID: req.WebhookID, OfflineAfter: req.OfflineAfter, Enabled: req.Enabled,
		})
	}
	ok(c, gin.H{"ok": true})
}
