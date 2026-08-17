package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// ---- 通知送达记录（持久队列 + 重试）----

// listNotificationDeliveries 送达记录（owner/admin 隔离：非 admin 仅见自己的；
// 管理员可见全部，含系统流程与所有 owner 的记录）。
func (s *Server) listNotificationDeliveries(c *gin.Context) {
	p := principalFromContext(c)
	if s.Notifier == nil {
		fail(c, http.StatusServiceUnavailable, "notifier unavailable")
		return
	}
	offset, limit := pagination(c)
	deliveries, total, err := s.Notifier.List(p.IsAdmin, p.UserID, offset, limit)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"deliveries": deliveries}, total, offset, limit)
}

// retryNotificationDelivery 手动重试一条送达记录（仅 failed 可重试，
// 重置尝试次数并立即重新发送）。
func (s *Server) retryNotificationDelivery(c *gin.Context) {
	p := principalFromContext(c)
	if s.Notifier == nil {
		fail(c, http.StatusServiceUnavailable, "notifier unavailable")
		return
	}
	id := mustID(c)
	var d model.NotificationDelivery
	if err := s.DB.First(&d, id).Error; err != nil {
		fail(c, http.StatusNotFound, "delivery not found")
		return
	}
	// owner/admin 隔离
	if !p.IsAdmin && d.OwnerID != p.UserID {
		fail(c, http.StatusForbidden, "delivery access denied")
		return
	}
	if err := s.Notifier.Retry(id); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	s.auditLog(c, "notification.retry", fmt.Sprintf("delivery_id=%d webhook_id=%d title=%s", d.ID, d.WebhookID, d.Title))
	ok(c, gin.H{"ok": true})
}
