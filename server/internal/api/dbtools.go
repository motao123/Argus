package api

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// dbSize 数据库体积。
func (s *Server) dbSize(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	info, err := os.Stat(s.Cfg.DBPath)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 含 WAL
	walSize := int64(0)
	if w, err := os.Stat(s.Cfg.DBPath + "-wal"); err == nil {
		walSize = w.Size()
	}
	ok(c, gin.H{"db_size": info.Size(), "wal_size": walSize, "total": info.Size() + walSize})
}

// dbVacuum 执行 VACUUM 压缩。
func (s *Server) dbVacuum(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	if err := s.DB.Exec("VACUUM").Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}
