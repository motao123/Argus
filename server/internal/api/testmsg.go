package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifier"
)

// testSendMessage 测试通知渠道（借鉴 komari admin:testSendMessage）。
func (s *Server) testSendMessage(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		WebhookID int64  `json:"webhook_id"`
		Title     string `json:"title"`
		Content   string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.WebhookID <= 0 {
		fail(c, http.StatusBadRequest, "webhook_id required")
		return
	}
	var n model.Notification
	if err := s.DB.First(&n, req.WebhookID).Error; err != nil {
		fail(c, http.StatusNotFound, "notification not found")
		return
	}
	title := req.Title
	if title == "" {
		title = "[Argus] 测试通知"
	}
	content := req.Content
	if content == "" {
		content = "这是一条测试消息"
	}
	go notifier.Send(&n, title, content)
	ok(c, gin.H{"ok": true, "sent_to": n.Type})
}
