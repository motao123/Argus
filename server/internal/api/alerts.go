package api

import (
	"net/http"
	"net/url"
	"strings"

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

// notificationView 脱敏视图：URL 打码、headers/body 不回显（借鉴 nezha 脱敏规范）。
type notificationView struct {
	model.Notification
	Headers string `json:"headers"`
	Body    string `json:"body"`
}

// maskURL 保留协议与主机，隐藏路径/查询中的凭据（如 token/secret）。
func maskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		if raw == "" {
			return ""
		}
		return raw[:min(4, len(raw))] + "***"
	}
	host := u.Host
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	return u.Scheme + "://" + host + "/***"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Server) listNotifications(c *gin.Context) {
	offset, limit := pagination(c)
	var total int64
	s.DB.Model(&model.Notification{}).Count(&total)
	var ns []model.Notification
	if err := s.DB.Order("id").Offset(offset).Limit(limit).Find(&ns).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]notificationView, 0, len(ns))
	for _, n := range ns {
		v := notificationView{Notification: n}
		v.URL = maskURL(n.URL)
		v.Headers = ""
		v.Body = ""
		out = append(out, v)
	}
	okPage(c, gin.H{"notifications": out}, total, offset, limit)
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
	// 部分更新：未提交字段保留原值（读取已脱敏，避免空值覆盖凭据）
	var req struct {
		Name    *string `json:"name"`
		Type    *string `json:"type"`
		URL     *string `json:"url"`
		Method  *string `json:"method"`
		Headers *string `json:"headers"`
		Body    *string `json:"body"`
		ChatID  *string `json:"chat_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Type != nil {
		updates["type"] = *req.Type
	}
	if req.URL != nil {
		updates["url"] = *req.URL
	}
	if req.Method != nil {
		updates["method"] = *req.Method
	}
	if req.Headers != nil {
		updates["headers"] = *req.Headers
	}
	if req.Body != nil {
		updates["body"] = *req.Body
	}
	if req.ChatID != nil {
		updates["chat_id"] = *req.ChatID
	}
	if len(updates) > 0 {
		if err := s.DB.Model(&n).Updates(updates).Error; err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ok(c, gin.H{"ok": true})
}

func (s *Server) deleteNotification(c *gin.Context) {
	if err := s.DB.Delete(&model.Notification{}, mustID(c)).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
