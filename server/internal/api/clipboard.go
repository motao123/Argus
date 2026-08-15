package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// ---- 剪贴板（借鉴 komari CloudClipboard）----

func (s *Server) listClipboard(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Order("id DESC").Limit(100)
	if !p.IsAdmin {
		q = q.Where("user_id = ?", p.UserID)
	}
	var items []model.Clipboard
	if err := q.Find(&items).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"items": items})
}

func (s *Server) createClipboard(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Content == "" {
		fail(c, http.StatusBadRequest, "content required")
		return
	}
	item := model.Clipboard{UserID: p.UserID, Title: req.Title, Content: req.Content}
	if err := s.DB.Create(&item).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, item)
}

func (s *Server) deleteClipboard(c *gin.Context) {
	p := principalFromContext(c)
	id := mustID(c)
	var item model.Clipboard
	if err := s.DB.First(&item, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if item.UserID != p.UserID && !p.IsAdmin {
		fail(c, http.StatusForbidden, "not yours")
		return
	}
	s.DB.Delete(&model.Clipboard{}, id)
	ok(c, gin.H{"ok": true})
}
