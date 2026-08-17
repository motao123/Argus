package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifier"
)

// ---- Alerts ----

func (s *Server) listAlerts(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Model(&model.Alert{})
	if p != nil && !p.IsAdmin {
		q = q.Where("owner_id = ?", p.UserID)
	}
	offset, limit := pagination(c)
	var total int64
	q.Count(&total)
	var alerts []model.Alert
	if err := q.Order("id").Offset(offset).Limit(limit).Find(&alerts).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"alerts": alerts}, total, offset, limit)
}

func (s *Server) createAlert(c *gin.Context) {
	p := principalFromContext(c)
	var a model.Alert
	if err := c.ShouldBindJSON(&a); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if !p.IsAdmin {
		a.OwnerID = p.UserID
	}
	if !s.validateAlertTargets(c, &a) {
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
	p := principalFromContext(c)
	id := mustID(c)
	var a model.Alert
	if err := s.DB.First(&a, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !p.IsAdmin && a.OwnerID != p.UserID {
		fail(c, http.StatusForbidden, "alert access denied")
		return
	}
	if err := c.ShouldBindJSON(&a); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	a.ID = id
	if !p.IsAdmin {
		a.OwnerID = p.UserID
	}
	if !s.validateAlertTargets(c, &a) {
		return
	}
	if err := s.DB.Save(&a).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, a)
}

func (s *Server) deleteAlert(c *gin.Context) {
	p := principalFromContext(c)
	var a model.Alert
	if err := s.DB.First(&a, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !p.IsAdmin && a.OwnerID != p.UserID {
		fail(c, http.StatusForbidden, "alert access denied")
		return
	}
	if err := s.DB.Delete(&a).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

// ---- 告警确认 / 静默 ----

// ackAlert 确认告警：记录确认人与时间；确认期间该规则不再发送触发通知
// （任务联动与插件 hook 不受影响），恢复时自动清除。
func (s *Server) ackAlert(c *gin.Context) {
	p := principalFromContext(c)
	var a model.Alert
	if err := s.DB.First(&a, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !p.IsAdmin && a.OwnerID != p.UserID {
		fail(c, http.StatusForbidden, "alert access denied")
		return
	}
	now := time.Now()
	if err := s.DB.Model(&a).Updates(map[string]any{"acked_at": now, "acked_by": p.Username}).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "alert.ack", fmt.Sprintf("alert_id=%d name=%s by=%s", a.ID, a.Name, p.Username))
	ok(c, gin.H{"ok": true, "acked_at": now, "acked_by": p.Username})
}

// unackAlert 取消确认。
func (s *Server) unackAlert(c *gin.Context) {
	p := principalFromContext(c)
	var a model.Alert
	if err := s.DB.First(&a, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !p.IsAdmin && a.OwnerID != p.UserID {
		fail(c, http.StatusForbidden, "alert access denied")
		return
	}
	if err := s.DB.Model(&a).Updates(map[string]any{"acked_at": nil, "acked_by": ""}).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "alert.unack", fmt.Sprintf("alert_id=%d name=%s", a.ID, a.Name))
	ok(c, gin.H{"ok": true})
}

// silenceAlert 静默：body {"until": "RFC3339", "from"?: "RFC3339"}。
// 静默起止时间内该规则不发送通知（任务联动与插件 hook 不受影响）。
func (s *Server) silenceAlert(c *gin.Context) {
	p := principalFromContext(c)
	var a model.Alert
	if err := s.DB.First(&a, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !p.IsAdmin && a.OwnerID != p.UserID {
		fail(c, http.StatusForbidden, "alert access denied")
		return
	}
	var req struct {
		From  *time.Time `json:"from"`
		Until *time.Time `json:"until"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if req.Until == nil || !req.Until.After(time.Now()) {
		fail(c, http.StatusBadRequest, "until must be a future time")
		return
	}
	from := time.Now()
	if req.From != nil && req.From.After(time.Now()) {
		from = *req.From
	}
	if err := s.DB.Model(&a).Updates(map[string]any{
		"silence_from": from, "silence_to": *req.Until,
	}).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "alert.silence", fmt.Sprintf("alert_id=%d name=%s from=%s until=%s", a.ID, a.Name, from.Format(time.RFC3339), req.Until.Format(time.RFC3339)))
	ok(c, gin.H{"ok": true, "silence_from": from, "silence_to": *req.Until})
}

// unsilenceAlert 取消静默。
func (s *Server) unsilenceAlert(c *gin.Context) {
	p := principalFromContext(c)
	var a model.Alert
	if err := s.DB.First(&a, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !p.IsAdmin && a.OwnerID != p.UserID {
		fail(c, http.StatusForbidden, "alert access denied")
		return
	}
	if err := s.DB.Model(&a).Updates(map[string]any{"silence_from": nil, "silence_to": nil}).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "alert.unsilence", fmt.Sprintf("alert_id=%d name=%s", a.ID, a.Name))
	ok(c, gin.H{"ok": true})
}

// validateAlertTargets enforces server ownership and PAT whitelist for alert rules.
func (s *Server) validateAlertTargets(c *gin.Context, a *model.Alert) bool {
	p := principalFromContext(c)
	if !s.validateEscalateChannel(c, a, p) {
		return false
	}
	if p.IsAdmin {
		return true
	}
	ids := parseIDs(a.ServerIDs)
	if len(ids) == 0 {
		fail(c, http.StatusForbidden, "alert targets required")
		return false
	}
	for _, id := range ids {
		if _, ok := s.authorizeServer(c, id, ScopeServerRead); !ok {
			fail(c, http.StatusForbidden, "alert server access denied")
			return false
		}
	}
	if a.TriggerCronID > 0 {
		var cr model.Cron
		if err := s.DB.First(&cr, a.TriggerCronID).Error; err != nil {
			fail(c, http.StatusBadRequest, "trigger cron not found")
			return false
		}
		if cr.OwnerID != a.OwnerID {
			fail(c, http.StatusForbidden, "trigger cron access denied")
			return false
		}
		cronIDs := parseIDs(cr.ServerIDs)
		if len(cronIDs) == 0 {
			fail(c, http.StatusForbidden, "trigger cron target scope required")
			return false
		}
		allowed := make(map[int64]bool, len(ids))
		for _, id := range ids {
			allowed[id] = true
		}
		for _, id := range cronIDs {
			if !allowed[id] {
				fail(c, http.StatusForbidden, "trigger cron target denied")
				return false
			}
		}
	}
	return true
}

// validateEscalateChannel 校验升级渠道：存在且归属匹配。
// 规则 owner 与渠道 owner 必须一致；admin 系统规则（owner=0）可使用操作者名下的渠道。
func (s *Server) validateEscalateChannel(c *gin.Context, a *model.Alert, p *principal) bool {
	if a.EscalateToChannelID <= 0 {
		return true
	}
	var n model.Notification
	if err := s.DB.First(&n, a.EscalateToChannelID).Error; err != nil {
		fail(c, http.StatusBadRequest, "escalate channel not found")
		return false
	}
	if a.OwnerID != 0 && n.OwnerID != a.OwnerID {
		fail(c, http.StatusForbidden, "escalate channel access denied")
		return false
	}
	if a.OwnerID == 0 && n.OwnerID != p.UserID {
		fail(c, http.StatusForbidden, "escalate channel access denied")
		return false
	}
	return true
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
		v.Extra = "" // 预设渠道专属配置（含 token/secret）不回显
		out = append(out, v)
	}
	okPage(c, gin.H{"notifications": out}, total, offset, limit)
}

func (s *Server) createNotification(c *gin.Context) {
	p := principalFromContext(c)
	var n model.Notification
	if err := c.ShouldBindJSON(&n); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if n.Type == "" {
		n.Type = "webhook"
	}
	if !notifier.IsValidType(n.Type) {
		fail(c, http.StatusBadRequest, "unsupported notification type: "+n.Type)
		return
	}
	// 渠道限流字段：0 = 不限，或 >= 1（不允许负数）
	if n.RateLimitPerMin < 0 || n.BurstLimit < 0 {
		fail(c, http.StatusBadRequest, "rate limit must be 0 or a positive integer")
		return
	}
	n.OwnerID = p.UserID
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
		Name         *string `json:"name"`
		Type         *string `json:"type"`
		URL          *string `json:"url"`
		Method       *string `json:"method"`
		Headers      *string `json:"headers"`
		Body         *string `json:"body"`
		ChatID       *string `json:"chat_id"`
		Extra        *string `json:"extra"`
		ClearURL     *bool   `json:"clear_url"`
		ClearHeaders *bool   `json:"clear_headers"`
		ClearBody    *bool   `json:"clear_body"`
		ClearExtra   *bool   `json:"clear_extra"`
		// 渠道限流：0 = 不限，或 >= 1
		RateLimitPerMin *int `json:"rate_limit_per_min"`
		BurstLimit      *int `json:"burst_limit"`
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
		if *req.Type != "" && !notifier.IsValidType(*req.Type) {
			fail(c, http.StatusBadRequest, "unsupported notification type: "+*req.Type)
			return
		}
		updates["type"] = *req.Type
	}
	if req.ClearURL != nil && *req.ClearURL {
		updates["url"] = ""
	} else if req.URL != nil && *req.URL != "" && *req.URL != maskURL(n.URL) {
		updates["url"] = *req.URL
	}
	if req.Method != nil {
		updates["method"] = *req.Method
	}
	if req.ClearHeaders != nil && *req.ClearHeaders {
		updates["headers"] = ""
	} else if req.Headers != nil && *req.Headers != "" {
		updates["headers"] = *req.Headers
	}
	if req.ClearBody != nil && *req.ClearBody {
		updates["body"] = ""
	} else if req.Body != nil && *req.Body != "" {
		updates["body"] = *req.Body
	}
	if req.ChatID != nil {
		updates["chat_id"] = *req.ChatID
	}
	if req.ClearExtra != nil && *req.ClearExtra {
		updates["extra"] = ""
	} else if req.Extra != nil && *req.Extra != "" {
		updates["extra"] = *req.Extra
	}
	if req.RateLimitPerMin != nil {
		if *req.RateLimitPerMin < 0 {
			fail(c, http.StatusBadRequest, "rate limit must be 0 or a positive integer")
			return
		}
		updates["rate_limit_per_min"] = *req.RateLimitPerMin
	}
	if req.BurstLimit != nil {
		if *req.BurstLimit < 0 {
			fail(c, http.StatusBadRequest, "rate limit must be 0 or a positive integer")
			return
		}
		updates["burst_limit"] = *req.BurstLimit
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
