package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// 服务器分组 CRUD（借鉴 nezha server-group）。
func (s *Server) listGroups(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Model(&model.ServerGroup{}).Order("id")
	if p != nil && !p.IsAdmin {
		q = q.Where("owner_id = ?", p.UserID)
	}
	var groups []model.ServerGroup
	if err := q.Find(&groups).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"groups": groups})
}

func (s *Server) createGroup(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		fail(c, http.StatusBadRequest, "name required")
		return
	}
	g := model.ServerGroup{OwnerID: p.UserID, Name: req.Name}
	if err := s.DB.Create(&g).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, g)
}

func (s *Server) deleteGroup(c *gin.Context) {
	p := principalFromContext(c)
	id := mustID(c)
	var g model.ServerGroup
	if err := s.DB.First(&g, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if g.OwnerID != p.UserID && !p.IsAdmin {
		fail(c, http.StatusForbidden, "not yours")
		return
	}
	// 组内服务器移出分组（map 更新避免空字符串零值被忽略）
	s.DB.Model(&model.Server{}).Where("group_name = ?", g.Name).
		Updates(map[string]any{"group": ""})
	s.DB.Delete(&model.ServerGroup{}, id)
	ok(c, gin.H{"ok": true})
}

// ---- 通知分组 CRUD（借鉴 nezha NotificationGroup）----

func (s *Server) listNotificationGroups(c *gin.Context) {
	var groups []model.NotificationGroup
	if err := s.DB.Order("id").Find(&groups).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"groups": groups})
}

func (s *Server) saveNotificationGroup(c *gin.Context) {
	var g model.NotificationGroup
	if err := c.ShouldBindJSON(&g); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if g.Name == "" {
		fail(c, http.StatusBadRequest, "name required")
		return
	}
	if g.ID > 0 {
		var existing model.NotificationGroup
		if err := s.DB.First(&existing, g.ID).Error; err != nil {
			fail(c, http.StatusNotFound, "not found")
			return
		}
		if err := s.DB.Save(&g).Error; err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if err := s.DB.Create(&g).Error; err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ok(c, g)
}

func (s *Server) deleteNotificationGroup(c *gin.Context) {
	if err := s.DB.Delete(&model.NotificationGroup{}, mustID(c)).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
