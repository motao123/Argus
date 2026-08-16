package api

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	path       string
	written    int64
	ready      bool
	lastActive time.Time
	mu         sync.Mutex
}

const restoreSessionTTL = 30 * time.Minute

var restoreState = struct {
	sync.Mutex
	sessions  map[string]*restoreSession
	restoring bool
}{sessions: make(map[string]*restoreSession)}

var safeRestoreID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func newRestoreID() (string, error) {
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "restore-" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func cleanupRestoreSessions(now time.Time) {
	for id, sess := range restoreState.sessions {
		if now.Sub(sess.lastActive) <= restoreSessionTTL {
			continue
		}
		_ = os.Remove(sess.path)
		delete(restoreState.sessions, id)
	}
}

func cleanupOrphanRestoreFiles(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	active := make(map[string]struct{}, len(restoreState.sessions))
	for _, sess := range restoreState.sessions {
		active[sess.path] = struct{}{}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if _, ok := active[path]; ok {
			continue
		}
		info, err := entry.Info()
		if err == nil && now.Sub(info.ModTime()) > restoreSessionTTL {
			_ = os.Remove(path)
		}
	}
}

func parseRestoreOffset(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	offset, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid offset")
	}
	return offset, nil
}

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
	uploadID := strings.TrimSpace(c.PostForm("upload_id"))
	if uploadID != "" && !safeRestoreID.MatchString(uploadID) {
		fail(c, http.StatusBadRequest, "invalid upload_id")
		return
	}
	offset, err := parseRestoreOffset(c.PostForm("offset"))
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	final := c.PostForm("final") == "1"

	restoreState.Lock()
	cleanupRestoreSessions(time.Now())
	if restoreState.restoring {
		restoreState.Unlock()
		fail(c, http.StatusConflict, "another restore is in progress")
		return
	}
	restoreState.restoring = true
	defer func() {
		restoreState.restoring = false
		restoreState.Unlock()
	}()

	dir := filepath.Join(filepath.Dir(s.Cfg.DBPath), "restore-staging")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	cleanupOrphanRestoreFiles(dir, now)
	if uploadID == "" {
		var err error
		uploadID, err = newRestoreID()
		if err != nil {
			fail(c, http.StatusInternalServerError, "cannot generate upload_id")
			return
		}
	}
	sess, found := restoreState.sessions[uploadID]
	if !found {
		if offset != 0 {
			fail(c, http.StatusBadRequest, "unknown upload_id for resume")
			return
		}
		sess = &restoreSession{path: filepath.Join(dir, uploadID+".db"), lastActive: now}
		restoreState.sessions[uploadID] = sess
	}
	if now.Sub(sess.lastActive) > restoreSessionTTL {
		_ = os.Remove(sess.path)
		delete(restoreState.sessions, uploadID)
		fail(c, http.StatusBadRequest, "upload session expired")
		return
	}
	sess.lastActive = now
	sess.mu.Lock()
	defer sess.mu.Unlock()
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
		delete(restoreState.sessions, uploadID)
		fail(c, http.StatusBadRequest, "invalid database: "+err.Error())
		return
	}
	totalHash := c.PostForm("total_hash")
	if totalHash != "" {
		got, _, err := fileHash(sess.path)
		if err != nil || got != totalHash {
			_ = os.Remove(sess.path)
			delete(restoreState.sessions, uploadID)
			fail(c, http.StatusBadRequest, "total hash mismatch")
			return
		}
	}

	// 原子切换：先备份当前库，再替换
	if err := s.swapDatabase(sess.path); err != nil {
		_ = os.Remove(sess.path)
		delete(restoreState.sessions, uploadID)
		fail(c, http.StatusInternalServerError, "swap failed: "+err.Error())
		return
	}
	_ = os.Remove(sess.path)
	delete(restoreState.sessions, uploadID)
	ok(c, gin.H{"ok": true, "written": sess.written, "final": true, "status": "restart_required", "restart_required": true, "note": "数据库文件已切换；当前进程不会自动重启，请通过进程管理器重启服务"})
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
