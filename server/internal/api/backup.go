package api

import (
	"io"
	"net/http"
	"os"
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

// backupRestore 上传数据库覆盖（需重启生效）。支持分片上传（offset 续传，借鉴 komari 分片上传）。
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

	dbPath := s.Cfg.DBPath
	offset := parseIntQuery64(c.PostForm("offset"))

	// 首个分片：备份当前库
	if offset == 0 {
		backupPath := dbPath + ".pre-restore." + time.Now().Format("20060102-150405")
		if b, err := os.ReadFile(dbPath); err == nil {
			_ = os.WriteFile(backupPath, b, 0o600)
		}
	}

	// 续写模式打开文件
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	dst, err := os.OpenFile(dbPath, flags, 0o600)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if c.PostForm("final") == "1" {
		_ = os.Remove(dbPath + "-shm")
		_ = os.Remove(dbPath + "-wal")
	}
	ok(c, gin.H{
		"ok":      true,
		"written": offset + file.Size,
		"final":   c.PostForm("final") == "1",
		"note":    "写入完成，重启服务生效",
	})
}
