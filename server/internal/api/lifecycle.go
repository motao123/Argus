package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// scheduleRestart makes healthz fail as soon as a restore has committed while
// allowing the current response to finish before the process exits.
func (s *Server) scheduleRestart() {
	s.restartPending.Store(true)
	if s.Restart == nil {
		return
	}
	go func(restart func()) {
		time.Sleep(100 * time.Millisecond)
		if restart != nil {
			restart()
		}
	}(s.Restart)
}

func (s *Server) Healthz(c *gin.Context) {
	s.healthz(c)
}

func (s *Server) healthz(c *gin.Context) {
	if s.restartPending.Load() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "restart_pending"})
		return
	}
	if s.DB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "database_unavailable"})
		return
	}
	db, err := s.DB.DB()
	if err != nil || db.PingContext(c.Request.Context()) != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "database_unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
