package api

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

const (
	auditRequestIDKey = "audit_request_id"
	auditStartedAtKey = "audit_started_at"
)

var auditResourceIDPattern = regexp.MustCompile(`(?:^|\s)(?:[a-z_]+_id|item|job|targets?)=([^\s]+)`)

// auditContextMiddleware attaches stable request metadata used by structured audit records.
func auditContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" || len(requestID) > 64 {
			requestID = randomHex(16)
		}
		c.Set(auditRequestIDKey, requestID)
		c.Set(auditStartedAtKey, time.Now())
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// auditLog records a successful management operation while retaining the legacy call shape.
func (s *Server) auditLog(c *gin.Context, action, detail string) {
	s.auditLogResult(c, action, detail, "success", "")
}

func (s *Server) auditLogResult(c *gin.Context, action, detail, outcome, errorCode string) {
	entry, ok := newAuditEntry(c, action, detail, outcome, errorCode)
	if !ok {
		return
	}
	_ = s.DB.Create(&entry).Error
}

func newAuditEntry(c *gin.Context, action, detail, outcome, errorCode string) (model.AuditLog, bool) {
	p := principalFromContext(c)
	if p == nil {
		return model.AuditLog{}, false
	}
	resourceType := action
	if i := strings.IndexByte(action, '.'); i > 0 {
		resourceType = action[:i]
	}
	resourceID := ""
	if matches := auditResourceIDPattern.FindStringSubmatch(detail); len(matches) == 2 {
		resourceID = strings.Trim(matches[1], `"',`)
	}
	requestID, _ := c.Get(auditRequestIDKey)
	requestIDString, _ := requestID.(string)
	startedAt, _ := c.Get(auditStartedAtKey)
	var durationMS int64
	if started, ok := startedAt.(time.Time); ok {
		durationMS = time.Since(started).Milliseconds()
	}
	if outcome == "" {
		outcome = "success"
	}
	return model.AuditLog{
		UserID:       p.UserID,
		Username:     p.Username,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
		DurationMS:   durationMS,
		RequestID:    requestIDString,
		Detail:       detail,
		IP:           currentIP(c),
	}, true
}

// listAuditLogs 审计日志（分页，admin）。
func (s *Server) listAuditLogs(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	offset, limit := pagination(c)
	q := s.DB.Model(&model.AuditLog{})
	if resourceType := strings.TrimSpace(c.Query("resource_type")); resourceType != "" {
		q = q.Where("resource_type = ?", resourceType)
	}
	if outcome := strings.TrimSpace(c.Query("outcome")); outcome != "" {
		q = q.Where("outcome = ?", outcome)
	}
	var total int64
	q.Count(&total)
	var logs []model.AuditLog
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"logs": logs}, total, offset, limit)
}

// exportAuditLogs 导出审计日志（CSV/JSON，admin）。参数：
// format=csv|json（默认 csv）、days=30（回看窗口，默认 30，上限 365）、action=（可选精确过滤）。
func (s *Server) exportAuditLogs(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	format := c.DefaultQuery("format", "csv")
	if format != "csv" && format != "json" {
		fail(c, http.StatusBadRequest, "format must be csv or json")
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days <= 0 || days > 365 {
		days = 30
	}
	q := s.DB.Model(&model.AuditLog{})
	if action := strings.TrimSpace(c.Query("action")); action != "" {
		q = q.Where("action = ?", action)
	}
	if resourceType := strings.TrimSpace(c.Query("resource_type")); resourceType != "" {
		q = q.Where("resource_type = ?", resourceType)
	}
	if outcome := strings.TrimSpace(c.Query("outcome")); outcome != "" {
		q = q.Where("outcome = ?", outcome)
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	var logs []model.AuditLog
	// 导出上限 10 万行，防止超大表打爆内存
	if err := q.Where("created_at >= ?", cutoff).Order("id ASC").Limit(100000).Find(&logs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []model.AuditLog{}
	}
	ts := time.Now().Unix()
	if format == "json" {
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="argus-audit-`+strconv.FormatInt(ts, 10)+`.json"`)
		_ = json.NewEncoder(c.Writer).Encode(logs)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="argus-audit-`+strconv.FormatInt(ts, 10)+`.csv"`)
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"id", "time", "username", "action", "resource_type", "resource_id", "outcome", "error_code", "duration_ms", "request_id", "detail", "ip"})
	for _, l := range logs {
		_ = w.Write([]string{
			strconv.FormatInt(l.ID, 10),
			l.CreatedAt.Format(time.RFC3339),
			l.Username,
			l.Action,
			l.ResourceType,
			l.ResourceID,
			l.Outcome,
			l.ErrorCode,
			strconv.FormatInt(l.DurationMS, 10),
			l.RequestID,
			l.Detail,
			l.IP,
		})
	}
	w.Flush()
}

// listMCPAudits MCP 调用审计分页查询（admin，对齐 nezha mcp_audit_logs 展示）。
func (s *Server) listMCPAudits(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	offset, limit := pagination(c)
	q := s.DB.Model(&model.MCPAuditLog{})
	if tool := c.Query("tool"); tool != "" {
		q = q.Where("tool = ?", tool)
	}
	if outcome := c.Query("outcome"); outcome != "" {
		q = q.Where("outcome = ?", outcome)
	}
	var total int64
	q.Count(&total)
	var logs []model.MCPAuditLog
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []model.MCPAuditLog{}
	}
	okPage(c, gin.H{"logs": logs}, total, offset, limit)
}
