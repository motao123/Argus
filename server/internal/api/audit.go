package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// auditLog 记录管理操作（由各 handler 调用）。
func (s *Server) auditLog(c *gin.Context, action, detail string) {
	p := principalFromContext(c)
	if p == nil {
		return
	}
	entry := model.AuditLog{
		UserID:   p.UserID,
		Username: p.Username,
		Action:   action,
		Detail:   detail,
		IP:       currentIP(c),
	}
	s.DB.Create(&entry)
}

// listAuditLogs 审计日志（分页，admin）。
func (s *Server) listAuditLogs(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	offset, limit := pagination(c)
	var total int64
	s.DB.Model(&model.AuditLog{}).Count(&total)
	var logs []model.AuditLog
	if err := s.DB.Order("id DESC").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"logs": logs}, total, offset, limit)
}
