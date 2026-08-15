package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

// backupDownload 打包下载数据库（借鉴 komari 备份下载）。
func (s *Server) backupDownload(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	dbPath := s.Cfg.DBPath
	file, err := os.Open(dbPath)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer file.Close()

	ts := time.Now().Format("20060102-150405")
	c.Header("Content-Disposition", `attachment; filename="argus-backup-`+ts+`.db"`)
	c.Header("Content-Type", "application/octet-stream")
	_, _ = io.Copy(c.Writer, file)
}

// backupRestore 上传数据库覆盖（需重启生效）。
func (s *Server) backupRestore(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		fail(c, http.StatusBadRequest, "file field required")
		return
	}
	if file.Size > 512<<20 {
		fail(c, http.StatusBadRequest, "file too large (max 512MB)")
		return
	}
	src, err := file.Open()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer src.Close()

	// 先备份当前库，再覆盖
	dbPath := s.Cfg.DBPath
	backupPath := dbPath + ".pre-restore." + time.Now().Format("20060102-150405")
	if b, err := os.ReadFile(dbPath); err == nil {
		_ = os.WriteFile(backupPath, b, 0o600)
	}

	dst, err := os.Create(dbPath)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	_ = os.Remove(dbPath + "-shm")
	_ = os.Remove(dbPath + "-wal")

	ok(c, gin.H{
		"ok":           true,
		"note":         "备份已写入，重启服务生效",
		"pre_backup":   filepath.Base(backupPath),
	})
}
