package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// ---- Alerts ----

func (s *Server) listAlerts(c *gin.Context) {
	offset, limit := pagination(c)
	var total int64
	s.DB.Model(&model.Alert{}).Count(&total)
	var alerts []model.Alert
	if err := s.DB.Order("id").Offset(offset).Limit(limit).Find(&alerts).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"alerts": alerts}, total, offset, limit)
}

func (s *Server) createAlert(c *gin.Context) {
	var a model.Alert
	if err := c.ShouldBindJSON(&a); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.DB.Create(&a).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "alert.create", a.Name)
	ok(c, a)
}

func (s *Server) updateAlert(c *gin.Context) {
	id := mustID(c)
	var a model.Alert
	if err := s.DB.First(&a, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if err := c.ShouldBindJSON(&a); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	a.ID = id
	if err := s.DB.Save(&a).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, a)
}

func (s *Server) deleteAlert(c *gin.Context) {
	if err := s.DB.Delete(&model.Alert{}, mustID(c)).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

// ---- Notifications ----

func (s *Server) listNotifications(c *gin.Context) {
	offset, limit := pagination(c)
	var total int64
	s.DB.Model(&model.Notification{}).Count(&total)
	var ns []model.Notification
	if err := s.DB.Order("id").Offset(offset).Limit(limit).Find(&ns).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"notifications": ns}, total, offset, limit)
}

func (s *Server) createNotification(c *gin.Context) {
	var n model.Notification
	if err := c.ShouldBindJSON(&n); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.DB.Create(&n).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, n)
}

func (s *Server) updateNotification(c *gin.Context) {
	id := mustID(c)
	var n model.Notification
	if err := s.DB.First(&n, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if err := c.ShouldBindJSON(&n); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	n.ID = id
	if err := s.DB.Save(&n).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, n)
}

func (s *Server) deleteNotification(c *gin.Context) {
	if err := s.DB.Delete(&model.Notification{}, mustID(c)).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
