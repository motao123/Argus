package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// 通知分组 CRUD（借鉴 nezha NotificationGroup）。
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
