package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifyctx"
)

// testSendMessage 测试通知渠道（借鉴 komari admin:testSendMessage）。
// 走持久队列：返回的 delivery_id 可在送达记录页查看/重试。
// 以统一上下文渲染（event=test，{{time}} 为当前时间），
// 渠道 Body 模板中的 {{event}}/{{server.*}} 等变量同样可用。
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
	ctx := &notifyctx.Ctx{
		Event:   "test",
		Title:   title,
		Content: content,
		Time:    notifyctx.FormatTime(time.Now()),
	}
	if s.Notifier == nil {
		fail(c, http.StatusServiceUnavailable, "notifier unavailable")
		return
	}
	if err := s.Notifier.EnqueueCtx(&n, title, content, 0, ctx.Flat()); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "sent_to": n.Type})
}
