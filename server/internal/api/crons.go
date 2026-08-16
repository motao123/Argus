package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/scheduler"
)

func (s *Server) listCrons(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Model(&model.Cron{})
	if p == nil || !p.IsAdmin {
		q = q.Where("owner_id = ?", p.UserID)
	}
	offset, limit := pagination(c)
	var total int64
	q.Count(&total)
	var crons []model.Cron
	if err := q.Order("id").Offset(offset).Limit(limit).Find(&crons).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"crons": crons}, total, offset, limit)
}

type cronRequest struct {
	Name          string `json:"name"`
	Expression    string `json:"expression"`
	Command       string `json:"command"`
	ServerIDs     string `json:"server_ids"`
	Enabled       *bool  `json:"enabled"`
	SkipIfRunning *bool  `json:"skip_if_running"`
}

func (s *Server) authorizeCronTargets(c *gin.Context, ownerID int64, serverIDs string) bool {
	p := principalFromContext(c)
	ids, valid := parseCronServerIDs(serverIDs)
	if !valid {
		return false
	}
	for _, id := range ids {
		var srv model.Server
		if err := s.DB.First(&srv, id).Error; err != nil {
			return false
		}
		if p.IsPAT && !p.canAccessServer(id) {
			return false
		}
		if !p.IsAdmin && srv.OwnerID != p.UserID {
			return false
		}
		if !s.cronOwnerIsAdmin(ownerID) && srv.OwnerID != ownerID {
			return false
		}
	}
	return true
}

func (s *Server) cronOwnerIsAdmin(ownerID int64) bool {
	var count int64
	s.DB.Model(&model.User{}).Where("id = ? AND role = ?", ownerID, model.RoleAdmin).Count(&count)
	return count > 0
}

func (s *Server) createCron(c *gin.Context) {
	p := principalFromContext(c)
	var req cronRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" || req.Expression == "" || req.Command == "" {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if !s.authorizeCronTargets(c, p.UserID, req.ServerIDs) {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	skipIfRunning := true
	if req.SkipIfRunning != nil {
		skipIfRunning = *req.SkipIfRunning
	}
	cr := model.Cron{OwnerID: p.UserID, Name: req.Name, Expression: req.Expression, Command: req.Command, ServerIDs: req.ServerIDs, Enabled: enabled, SkipIfRunning: skipIfRunning}
	if err := s.DB.Create(&cr).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Scheduler != nil {
		s.Scheduler.Upsert(&cr)
	}
	s.auditLog(c, "cron.create", cr.Name)
	ok(c, cr)
}

func (s *Server) updateCron(c *gin.Context) {
	id := mustID(c)
	var cr model.Cron
	if err := s.DB.First(&cr, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&cr.OwnerID, c) {
		fail(c, http.StatusForbidden, "not your cron")
		return
	}
	var req cronRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if !s.authorizeCronTargets(c, cr.OwnerID, req.ServerIDs) {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	cr.Name, cr.Expression, cr.Command, cr.ServerIDs = req.Name, req.Expression, req.Command, req.ServerIDs
	if req.Enabled != nil {
		cr.Enabled = *req.Enabled
	}
	if req.SkipIfRunning != nil {
		cr.SkipIfRunning = *req.SkipIfRunning
	}
	if err := s.DB.Save(&cr).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Scheduler != nil {
		s.Scheduler.Upsert(&cr)
	}
	s.auditLog(c, "cron.update", cr.Name)
	ok(c, cr)
}

func (s *Server) deleteCron(c *gin.Context) {
	id := mustID(c)
	var cr model.Cron
	if err := s.DB.First(&cr, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&cr.OwnerID, c) {
		fail(c, http.StatusForbidden, "not your cron")
		return
	}
	if err := s.DB.Delete(&cr).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Scheduler != nil {
		s.Scheduler.Remove(id)
	}
	ok(c, gin.H{"ok": true})
}

func (s *Server) runCron(c *gin.Context) {
	id := mustID(c)
	var cr model.Cron
	if err := s.DB.First(&cr, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&cr.OwnerID, c) {
		fail(c, http.StatusForbidden, "not your cron")
		return
	}
	if s.Scheduler == nil {
		fail(c, http.StatusInternalServerError, "scheduler not started")
		return
	}
	runID, err := s.Scheduler.Enqueue(&cr, scheduler.TriggerManual, nil)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{"run_id": runID}})
}

func (s *Server) listCronRuns(c *gin.Context) {
	cronID := mustID(c)
	var cr model.Cron
	if err := s.DB.First(&cr, cronID).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&cr.OwnerID, c) {
		fail(c, http.StatusForbidden, "not your cron")
		return
	}
	offset, limit := pagination(c)
	q := s.DB.Model(&model.TaskRun{}).Where("cron_id = ? AND owner_id = ?", cr.ID, cr.OwnerID)
	var total int64
	q.Count(&total)
	var runs []model.TaskRun
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&runs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"runs": runs}, total, offset, limit)
}

func (s *Server) getCronRun(c *gin.Context) {
	cronID := mustID(c)
	runID, err := strconv.ParseInt(c.Param("runId"), 10, 64)
	if err != nil || runID <= 0 {
		fail(c, http.StatusBadRequest, "invalid run id")
		return
	}
	var cr model.Cron
	if err := s.DB.First(&cr, cronID).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&cr.OwnerID, c) {
		fail(c, http.StatusForbidden, "not your cron")
		return
	}
	var run model.TaskRun
	if err := s.DB.Preload("Results").Where("id = ? AND cron_id = ? AND owner_id = ?", runID, cr.ID, cr.OwnerID).First(&run).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	ok(c, run)
}

func parseCronServerIDs(value string) ([]int64, bool) {
	if strings.TrimSpace(value) == "" {
		return nil, true
	}
	var ids []int64
	for _, p := range strings.Split(value, ",") {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || v <= 0 {
			return nil, false
		}
		ids = append(ids, v)
	}
	return ids, true
}

func parseIDs(value string) []int64 {
	ids, _ := parseCronServerIDs(value)
	return ids
}
