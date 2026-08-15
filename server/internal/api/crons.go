package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// ---- Crons ----

func (s *Server) listCrons(c *gin.Context) {
	var crons []model.Cron
	if err := s.DB.Order("id").Find(&crons).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"crons": crons})
}

func (s *Server) createCron(c *gin.Context) {
	var cr model.Cron
	if err := c.ShouldBindJSON(&cr); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.DB.Create(&cr).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Scheduler != nil {
		s.Scheduler.Upsert(&cr)
	}
	ok(c, cr)
}

func (s *Server) updateCron(c *gin.Context) {
	id := mustID(c)
	var cr model.Cron
	if err := s.DB.First(&cr, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if err := c.ShouldBindJSON(&cr); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	cr.ID = id
	if err := s.DB.Save(&cr).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Scheduler != nil {
		s.Scheduler.Upsert(&cr)
	}
	ok(c, cr)
}

func (s *Server) deleteCron(c *gin.Context) {
	id := mustID(c)
	if err := s.DB.Delete(&model.Cron{}, id).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Scheduler != nil {
		s.Scheduler.Remove(id)
	}
	ok(c, gin.H{"ok": true})
}

// runCron 立即手动触发一次任务。
func (s *Server) runCron(c *gin.Context) {
	id := mustID(c)
	var cr model.Cron
	if err := s.DB.First(&cr, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if s.Scheduler == nil {
		fail(c, http.StatusInternalServerError, "scheduler not started")
		return
	}
	result := s.Scheduler.RunOnce(&cr)
	ok(c, gin.H{"result": result})
}

// parseIDs 解析逗号分隔的服务器 ID 列表。
func parseIDs(s string) []int64 {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var ids []int64
	for _, p := range strings.Split(s, ",") {
		if v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
			ids = append(ids, v)
		}
	}
	return ids
}
