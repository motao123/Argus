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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"crons": crons})
}

func (s *Server) createCron(c *gin.Context) {
	var cr model.Cron
	if err := c.ShouldBindJSON(&cr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}
	if err := s.DB.Create(&cr).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if s.Scheduler != nil {
		s.Scheduler.Upsert(&cr)
	}
	c.JSON(http.StatusOK, cr)
}

func (s *Server) updateCron(c *gin.Context) {
	id := mustID(c)
	var cr model.Cron
	if err := s.DB.First(&cr, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err := c.ShouldBindJSON(&cr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}
	cr.ID = id
	if err := s.DB.Save(&cr).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if s.Scheduler != nil {
		s.Scheduler.Upsert(&cr)
	}
	c.JSON(http.StatusOK, cr)
}

func (s *Server) deleteCron(c *gin.Context) {
	id := mustID(c)
	if err := s.DB.Delete(&model.Cron{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if s.Scheduler != nil {
		s.Scheduler.Remove(id)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// runCron 立即手动触发一次任务。
func (s *Server) runCron(c *gin.Context) {
	id := mustID(c)
	var cr model.Cron
	if err := s.DB.First(&cr, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if s.Scheduler == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "scheduler not started"})
		return
	}
	result := s.Scheduler.RunOnce(&cr)
	c.JSON(http.StatusOK, gin.H{"result": result})
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
