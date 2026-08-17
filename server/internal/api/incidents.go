package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/sla"
)

// 事故与维护窗口（里程碑9 第一组：状态页事故时间线 + 维护窗口 + SLA/SLO）。
// 读取对登录用户/游客公开（状态页本身是公开资源）；管理操作按 owner 隔离，
// admin 可管理全部，普通用户仅可管理自己创建的记录，全部写入操作留审计。

// incidentServerIDs 校验非 admin 用户的目标服务器归属，返回规范化后的逗号分隔串。
func (s *Server) validateTargetServers(c *gin.Context, p *principal, raw string) (string, bool) {
	if p.IsAdmin {
		return raw, true
	}
	ids := parseIDs(raw)
	if len(ids) == 0 {
		fail(c, http.StatusForbidden, "server targets required")
		return "", false
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := s.authorizeServer(c, id, ScopeServerRead); !ok {
			fail(c, http.StatusForbidden, "server target access denied")
			return "", false
		}
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ","), true
}

// ---- 事故 ----

// listIncidents 状态页事故时间线（公开读取；强制登录模式下游客不可见）。
func (s *Server) listIncidents(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Model(&model.Incident{})
	if p == nil && s.GetSetting(SettingForceAuth, "0") == "1" {
		q = q.Where("1 = 0")
	}
	offset, limit := pagination(c)
	var total int64
	q.Count(&total)
	var incidents []model.Incident
	if err := q.Order("start_at DESC, id DESC").Offset(offset).Limit(limit).Find(&incidents).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"incidents": incidents}, total, offset, limit)
}

func (s *Server) createIncident(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		Title     string `json:"title"`
		Severity  string `json:"severity"`
		Status    string `json:"status"`
		ServerIDs string `json:"server_ids"`
		Notes     string `json:"notes"`
		StartAt   string `json:"start_at"` // RFC3339
		EndAt     string `json:"end_at"`   // RFC3339 或空
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	inc, valid := s.bindIncident(c, p, &req)
	if !valid {
		return
	}
	if err := s.DB.Create(inc).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "incident.create", incidentAuditDetail(inc))
	ok(c, inc)
}

func (s *Server) updateIncident(c *gin.Context) {
	p := principalFromContext(c)
	var inc model.Incident
	if err := s.DB.First(&inc, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&inc.OwnerID, c) {
		fail(c, http.StatusForbidden, "incident access denied")
		return
	}
	var req struct {
		Title     *string `json:"title"`
		Severity  *string `json:"severity"`
		Status    *string `json:"status"`
		ServerIDs *string `json:"server_ids"`
		Notes     *string `json:"notes"`
		StartAt   *string `json:"start_at"`
		EndAt     *string `json:"end_at"` // 空字符串 = 清除（重新开始）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			fail(c, http.StatusBadRequest, "title required")
			return
		}
		inc.Title = strings.TrimSpace(*req.Title)
	}
	if req.Severity != nil {
		if !validIncidentSeverity(*req.Severity) {
			fail(c, http.StatusBadRequest, "severity must be minor/major/critical")
			return
		}
		inc.Severity = *req.Severity
	}
	if req.Status != nil {
		if !validIncidentStatus(*req.Status) {
			fail(c, http.StatusBadRequest, "status must be ongoing/resolved")
			return
		}
		inc.Status = *req.Status
	}
	if req.ServerIDs != nil {
		ids, ok := s.validateTargetServers(c, p, *req.ServerIDs)
		if !ok {
			return
		}
		inc.ServerIDs = ids
	}
	if req.Notes != nil {
		inc.Notes = *req.Notes
	}
	if req.StartAt != nil {
		start, err := time.Parse(time.RFC3339, *req.StartAt)
		if err != nil {
			fail(c, http.StatusBadRequest, "start_at must be RFC3339")
			return
		}
		inc.StartAt = start
	}
	if req.EndAt != nil {
		if *req.EndAt == "" {
			inc.EndAt = nil
		} else {
			end, err := time.Parse(time.RFC3339, *req.EndAt)
			if err != nil {
				fail(c, http.StatusBadRequest, "end_at must be RFC3339")
				return
			}
			inc.EndAt = &end
		}
	}
	if inc.EndAt != nil && !inc.EndAt.After(inc.StartAt) {
		fail(c, http.StatusBadRequest, "end_at must be after start_at")
		return
	}
	if err := s.DB.Save(&inc).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "incident.update", incidentAuditDetail(&inc))
	ok(c, inc)
}

// resolveIncident 结案：标记 resolved 并补上结束时间。
func (s *Server) resolveIncident(c *gin.Context) {
	var inc model.Incident
	if err := s.DB.First(&inc, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&inc.OwnerID, c) {
		fail(c, http.StatusForbidden, "incident access denied")
		return
	}
	now := time.Now()
	inc.Status = model.IncidentStatusResolved
	if inc.EndAt == nil {
		inc.EndAt = &now
	}
	if err := s.DB.Save(&inc).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "incident.resolve", incidentAuditDetail(&inc))
	ok(c, inc)
}

func (s *Server) deleteIncident(c *gin.Context) {
	var inc model.Incident
	if err := s.DB.First(&inc, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&inc.OwnerID, c) {
		fail(c, http.StatusForbidden, "incident access denied")
		return
	}
	if err := s.DB.Delete(&inc).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "incident.delete", incidentAuditDetail(&inc))
	ok(c, gin.H{"ok": true})
}

// bindIncident 解析并校验创建请求（非 admin 必须提供自有服务器目标）。
func (s *Server) bindIncident(c *gin.Context, p *principal, req *struct {
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	Status    string `json:"status"`
	ServerIDs string `json:"server_ids"`
	Notes     string `json:"notes"`
	StartAt   string `json:"start_at"`
	EndAt     string `json:"end_at"`
}) (*model.Incident, bool) {
	if strings.TrimSpace(req.Title) == "" {
		fail(c, http.StatusBadRequest, "title required")
		return nil, false
	}
	severity := req.Severity
	if severity == "" {
		severity = model.IncidentSeverityMinor
	}
	if !validIncidentSeverity(severity) {
		fail(c, http.StatusBadRequest, "severity must be minor/major/critical")
		return nil, false
	}
	status := req.Status
	if status == "" {
		status = model.IncidentStatusOngoing
	}
	if !validIncidentStatus(status) {
		fail(c, http.StatusBadRequest, "status must be ongoing/resolved")
		return nil, false
	}
	start, err := time.Parse(time.RFC3339, req.StartAt)
	if err != nil {
		fail(c, http.StatusBadRequest, "start_at must be RFC3339")
		return nil, false
	}
	ids, ok := s.validateTargetServers(c, p, req.ServerIDs)
	if !ok {
		return nil, false
	}
	inc := &model.Incident{
		OwnerID:   p.UserID,
		Title:     strings.TrimSpace(req.Title),
		Severity:  severity,
		Status:    status,
		ServerIDs: ids,
		Notes:     req.Notes,
		StartAt:   start,
	}
	if req.EndAt != "" {
		end, err := time.Parse(time.RFC3339, req.EndAt)
		if err != nil {
			fail(c, http.StatusBadRequest, "end_at must be RFC3339")
			return nil, false
		}
		if !end.After(start) {
			fail(c, http.StatusBadRequest, "end_at must be after start_at")
			return nil, false
		}
		inc.EndAt = &end
		if status == model.IncidentStatusOngoing {
			inc.Status = model.IncidentStatusResolved
		}
	}
	return inc, true
}

func validIncidentSeverity(v string) bool {
	switch v {
	case model.IncidentSeverityMinor, model.IncidentSeverityMajor, model.IncidentSeverityCritical:
		return true
	}
	return false
}

func validIncidentStatus(v string) bool {
	return v == model.IncidentStatusOngoing || v == model.IncidentStatusResolved
}

func incidentAuditDetail(inc *model.Incident) string {
	detail := inc.Title
	if inc.ServerIDs != "" {
		detail += " [servers: " + inc.ServerIDs + "]"
	}
	return detail
}

// ---- 维护窗口 ----

// listMaintenanceWindows 维护窗口列表（公开读取；强制登录模式下游客不可见）。
func (s *Server) listMaintenanceWindows(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Model(&model.MaintenanceWindow{})
	if p == nil && s.GetSetting(SettingForceAuth, "0") == "1" {
		q = q.Where("1 = 0")
	}
	offset, limit := pagination(c)
	var total int64
	q.Count(&total)
	var wins []model.MaintenanceWindow
	if err := q.Order("start_at DESC, id DESC").Offset(offset).Limit(limit).Find(&wins).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"windows": wins}, total, offset, limit)
}

func (s *Server) createMaintenanceWindow(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		Title     string `json:"title"`
		ServerIDs string `json:"server_ids"`
		StartAt   string `json:"start_at"`
		EndAt     string `json:"end_at"`
		Recurring bool   `json:"recurring"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	win, valid := s.bindMaintenanceWindow(c, p, &req)
	if !valid {
		return
	}
	if err := s.DB.Create(win).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "maintenance-window.create", maintenanceAuditDetail(win))
	ok(c, win)
}

func (s *Server) updateMaintenanceWindow(c *gin.Context) {
	p := principalFromContext(c)
	var win model.MaintenanceWindow
	if err := s.DB.First(&win, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&win.OwnerID, c) {
		fail(c, http.StatusForbidden, "maintenance window access denied")
		return
	}
	var req struct {
		Title     *string `json:"title"`
		ServerIDs *string `json:"server_ids"`
		StartAt   *string `json:"start_at"`
		EndAt     *string `json:"end_at"`
		Recurring *bool   `json:"recurring"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			fail(c, http.StatusBadRequest, "title required")
			return
		}
		win.Title = strings.TrimSpace(*req.Title)
	}
	if req.ServerIDs != nil {
		ids, ok := s.validateTargetServers(c, p, *req.ServerIDs)
		if !ok {
			return
		}
		win.ServerIDs = ids
	}
	if req.StartAt != nil {
		start, err := time.Parse(time.RFC3339, *req.StartAt)
		if err != nil {
			fail(c, http.StatusBadRequest, "start_at must be RFC3339")
			return
		}
		win.StartAt = start
	}
	if req.EndAt != nil {
		end, err := time.Parse(time.RFC3339, *req.EndAt)
		if err != nil {
			fail(c, http.StatusBadRequest, "end_at must be RFC3339")
			return
		}
		win.EndAt = end
	}
	if req.Recurring != nil {
		win.Recurring = *req.Recurring
	}
	if !validWindowTimes(&win) {
		fail(c, http.StatusBadRequest, "end_at must be after start_at and window shorter than 7 days when recurring")
		return
	}
	if err := s.DB.Save(&win).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "maintenance-window.update", maintenanceAuditDetail(&win))
	ok(c, win)
}

func (s *Server) deleteMaintenanceWindow(c *gin.Context) {
	var win model.MaintenanceWindow
	if err := s.DB.First(&win, mustID(c)).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&win.OwnerID, c) {
		fail(c, http.StatusForbidden, "maintenance window access denied")
		return
	}
	if err := s.DB.Delete(&win).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "maintenance-window.delete", maintenanceAuditDetail(&win))
	ok(c, gin.H{"ok": true})
}

func (s *Server) bindMaintenanceWindow(c *gin.Context, p *principal, req *struct {
	Title     string `json:"title"`
	ServerIDs string `json:"server_ids"`
	StartAt   string `json:"start_at"`
	EndAt     string `json:"end_at"`
	Recurring bool   `json:"recurring"`
}) (*model.MaintenanceWindow, bool) {
	if strings.TrimSpace(req.Title) == "" {
		fail(c, http.StatusBadRequest, "title required")
		return nil, false
	}
	start, err := time.Parse(time.RFC3339, req.StartAt)
	if err != nil {
		fail(c, http.StatusBadRequest, "start_at must be RFC3339")
		return nil, false
	}
	end, err := time.Parse(time.RFC3339, req.EndAt)
	if err != nil {
		fail(c, http.StatusBadRequest, "end_at must be RFC3339")
		return nil, false
	}
	ids, ok := s.validateTargetServers(c, p, req.ServerIDs)
	if !ok {
		return nil, false
	}
	win := &model.MaintenanceWindow{
		OwnerID:   p.UserID,
		Title:     strings.TrimSpace(req.Title),
		ServerIDs: ids,
		StartAt:   start,
		EndAt:     end,
		Recurring: req.Recurring,
	}
	if !validWindowTimes(win) {
		fail(c, http.StatusBadRequest, "end_at must be after start_at and window shorter than 7 days when recurring")
		return nil, false
	}
	return win, true
}

// validWindowTimes 结束须晚于开始；重复窗口时长须小于 7 天（每周一次）。
func validWindowTimes(w *model.MaintenanceWindow) bool {
	if !w.EndAt.After(w.StartAt) {
		return false
	}
	if w.Recurring && w.EndAt.Sub(w.StartAt) >= 7*24*time.Hour {
		return false
	}
	return true
}

func maintenanceAuditDetail(w *model.MaintenanceWindow) string {
	detail := w.Title
	if w.ServerIDs != "" {
		detail += " [servers: " + w.ServerIDs + "]"
	}
	return detail
}

// ---- SLA / SLO ----

// serverSLA 服务器逐月可用性（SLO 达标判定），游客可查看公开服务器。
func (s *Server) serverSLA(c *gin.Context) {
	id := mustID(c)
	srv, valid := s.authorizePublicServer(c, id)
	if !valid {
		fail(c, http.StatusNotFound, "server not found")
		return
	}
	months := parseIntQuery(c, "months", 6)
	if months < 1 {
		months = 1
	}
	if months > 24 {
		months = 24
	}
	series := sla.Series(s.DB, srv.ID, srv.CreatedAt, time.Now(), months)
	for i := range series {
		sla.ApplySLO(&series[i], srv.SloTarget)
	}
	ok(c, gin.H{"server_id": srv.ID, "slo_target": srv.SloTarget, "months": series})
}
