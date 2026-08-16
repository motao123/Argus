package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ---- 安全备份 / 恢复（阶段2：快照 + 分片校验 + 原子切换）----

// backupDownload 用 VACUUM INTO 生成一致性快照（WAL 安全），流式返回并附带 SHA-256。
func (s *Server) backupDownload(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	sqlDB, err := s.DB.DB()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	tmp := filepath.Join(filepath.Dir(s.Cfg.DBPath), ".backup-snapshot-"+fmt.Sprintf("%d", time.Now().UnixNano())+".db")
	defer os.Remove(tmp)
	// VACUUM INTO 生成一致性快照（SQLite >= 3.27）
	if _, err := sqlDB.Exec("VACUUM INTO '" + strings.ReplaceAll(tmp, "'", "''") + "'"); err != nil {
		fail(c, http.StatusInternalServerError, "snapshot failed: "+err.Error())
		return
	}
	hash, size, err := fileHash(tmp)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	f, err := os.Open(tmp)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	ts := time.Now().Format("20060102-150405")
	c.Header("Content-Disposition", `attachment; filename="argus-backup-`+ts+`.db"`)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Argus-Sha256", hash)
	c.Header("X-Argus-Size", fmt.Sprintf("%d", size))
	_, _ = io.Copy(c.Writer, f)
}

// fileHash 计算文件 SHA-256 与大小。
func fileHash(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// restoreSession 分片上传会话（内存态，短生命周期）。
type restoreSession struct {
	path    string
	written int64
}

var restoreSessions = map[string]*restoreSession{}

// backupRestore 分片恢复：staging 写入 + 分片顺序校验 + 最终完整性校验 + 原子切换。
// 表单字段：upload_id（可选，续传）、offset、final、file
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
	uploadID := c.PostForm("upload_id")
	offset := parseIntQuery64(c.PostForm("offset"))
	final := c.PostForm("final") == "1"

	dir := filepath.Join(filepath.Dir(s.Cfg.DBPath), "restore-staging")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	sess, found := restoreSessions[uploadID]
	if !found {
		if offset != 0 {
			fail(c, http.StatusBadRequest, "unknown upload_id for resume")
			return
		}
		sess = &restoreSession{path: filepath.Join(dir, "restore-"+fmt.Sprintf("%d", time.Now().UnixNano())+".db")}
		restoreSessions[uploadID] = sess
	}
	if sess.written != offset {
		fail(c, http.StatusConflict, fmt.Sprintf("offset mismatch: expected %d got %d", sess.written, offset))
		return
	}

	src, err := file.Open()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer src.Close()

	dst, err := os.OpenFile(sess.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	n, err := io.Copy(dst, src)
	dst.Close()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	sess.written += n

	if !final {
		ok(c, gin.H{"ok": true, "written": sess.written, "final": false})
		return
	}

	// 最终校验：SQLite 头 + integrity_check + 总哈希
	if err := validateSQLiteFile(sess.path); err != nil {
		_ = os.Remove(sess.path)
		delete(restoreSessions, uploadID)
		fail(c, http.StatusBadRequest, "invalid database: "+err.Error())
		return
	}
	totalHash := c.PostForm("total_hash")
	if totalHash != "" {
		got, _, err := fileHash(sess.path)
		if err != nil || got != totalHash {
			_ = os.Remove(sess.path)
			delete(restoreSessions, uploadID)
			fail(c, http.StatusBadRequest, "total hash mismatch")
			return
		}
	}

	// 原子切换：先备份当前库，再替换
	if err := s.swapDatabase(sess.path); err != nil {
		_ = os.Remove(sess.path)
		delete(restoreSessions, uploadID)
		fail(c, http.StatusInternalServerError, "swap failed: "+err.Error())
		return
	}
	_ = os.Remove(sess.path)
	delete(restoreSessions, uploadID)
	ok(c, gin.H{"ok": true, "written": sess.written, "final": true, "note": "恢复完成，请重启服务生效"})
}

// validateSQLiteFile 校验文件是合法 SQLite 库并可通过完整性检查。
func validateSQLiteFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	header := make([]byte, 16)
	if _, err := io.ReadFull(f, header); err != nil {
		f.Close()
		return fmt.Errorf("file too small")
	}
	f.Close()
	if string(header) != "SQLite format 3\x00" {
		return fmt.Errorf("not a sqlite database")
	}
	// 用独立连接跑 integrity_check（不触碰运行中的主库）
	dsn := "file:" + path + "?mode=ro"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("integrity check: %s", result)
	}
	return nil
}

// swapDatabase 备份当前库后原子替换（staging → live）。
func (s *Server) swapDatabase(staging string) error {
	dbPath := s.Cfg.DBPath
	// 备份当前库（保留回滚点）
	if b, err := os.ReadFile(dbPath); err == nil {
		backup := dbPath + ".pre-restore." + time.Now().Format("20060102-150405")
		if err := os.WriteFile(backup, b, 0o600); err != nil {
			return fmt.Errorf("backup current db: %w", err)
		}
	}
	// 原子替换
	if err := os.Rename(staging, dbPath); err != nil {
		return err
	}
	// 清除旧 WAL/SHM（与新库不匹配）
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	return nil
}
