package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/backup"
	"github.com/motao123/Argus/server/internal/model"
)

// ---- 定时加密备份（里程碑9：加密备份 + 保留策略 + 恢复演练）----
// 全部接口仅 admin 可访问：备份包含整库数据与密钥派生信息，禁止普通/只读用户触碰。

// validateBackupSchedule 校验并规整计划字段。
func (s *Server) validateBackupSchedule(sch *model.BackupSchedule) error {
	sch.Name = strings.TrimSpace(sch.Name)
	if sch.Name == "" {
		return errors.New("name required")
	}
	sch.Cron = strings.TrimSpace(sch.Cron)
	if _, err := cron.ParseStandard(sch.Cron); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	sch.Target = strings.TrimSpace(sch.Target)
	if sch.Target == "" {
		return errors.New("target required")
	}
	if !isHTTPTarget(sch.Target) {
		// 本地目标：绝对路径
		if !filepath.IsAbs(sch.Target) {
			return errors.New("local target must be an absolute path")
		}
	}
	if sch.KeepCount < 1 {
		sch.KeepCount = 1
	}
	if sch.KeepCount > 365 {
		sch.KeepCount = 365
	}
	return nil
}

func isHTTPTarget(target string) bool {
	t := strings.ToLower(strings.TrimSpace(target))
	return strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://")
}

// listBackupSchedules 备份计划列表（admin）。
func (s *Server) listBackupSchedules(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var schedules []model.BackupSchedule
	if err := s.DB.Order("id").Find(&schedules).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"schedules": schedules})
}

// createBackupSchedule 创建备份计划（admin）。
func (s *Server) createBackupSchedule(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req model.BackupSchedule
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.validateBackupSchedule(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	salt, err := backup.NewSalt()
	if err != nil {
		fail(c, http.StatusInternalServerError, "generate key salt")
		return
	}
	req.KeySalt = salt
	req.KeySource = ""
	req.KeyID = ""
	if err := s.DB.Create(&req).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Backups != nil {
		s.Backups.Upsert(&req)
	}
	s.auditLog(c, "backup_schedule.create", req.Name)
	ok(c, req)
}

// updateBackupSchedule 更新备份计划（admin，部分更新语义）。
func (s *Server) updateBackupSchedule(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	var sch model.BackupSchedule
	if err := s.DB.First(&sch, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Name      *string `json:"name"`
		Enabled   *bool   `json:"enabled"`
		Cron      *string `json:"cron"`
		Target    *string `json:"target"`
		KeepCount *int    `json:"keep_count"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	// 合并后整体校验（cron/target 合法性、keep 范围）
	if req.Name != nil {
		sch.Name = *req.Name
	}
	if req.Cron != nil {
		sch.Cron = *req.Cron
	}
	if req.Target != nil {
		sch.Target = *req.Target
	}
	if req.KeepCount != nil {
		sch.KeepCount = *req.KeepCount
	}
	if err := s.validateBackupSchedule(&sch); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	updates := map[string]any{"name": sch.Name, "cron": sch.Cron, "target": sch.Target, "keep_count": sch.KeepCount}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if err := s.DB.Model(&sch).Updates(updates).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Enabled != nil {
		sch.Enabled = *req.Enabled
	}
	s.DB.First(&sch, id)
	if s.Backups != nil {
		s.Backups.Upsert(&sch)
	}
	s.auditLog(c, "backup_schedule.update", sch.Name)
	ok(c, sch)
}

// deleteBackupSchedule 删除备份计划（admin；不删除已生成的备份文件）。
func (s *Server) deleteBackupSchedule(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	var sch model.BackupSchedule
	if err := s.DB.First(&sch, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if err := s.DB.Delete(&sch).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Backups != nil {
		s.Backups.Remove(id)
	}
	s.auditLog(c, "backup_schedule.delete", sch.Name)
	ok(c, gin.H{"ok": true})
}

// runBackupSchedule 立即执行一次备份（admin；同步执行，可能耗时较长）。
func (s *Server) runBackupSchedule(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	var sch model.BackupSchedule
	if err := s.DB.First(&sch, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if s.Backups == nil {
		fail(c, http.StatusInternalServerError, "backup manager not started")
		return
	}
	if err := s.Backups.RunOnce(&sch, "manual"); err != nil {
		if errors.Is(err, backup.ErrBusy) {
			fail(c, http.StatusConflict, "backup already running", "backup.busy")
			return
		}
		fail(c, http.StatusBadGateway, "backup failed: "+err.Error(), "backup.failed")
		return
	}
	s.auditLog(c, "backup_schedule.run", sch.Name)
	ok(c, gin.H{"ok": true, "schedule_id": sch.ID})
}

// listBackupRuns 执行历史（admin）。
// downloadInstanceBackup creates a complete encrypted instance archive for a schedule.
// It is a separate format from the legacy single-database .argusenc backup.
func (s *Server) downloadInstanceBackup(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil || !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	if s.Backups == nil {
		fail(c, http.StatusInternalServerError, "backup manager not started", "backup.manager_unavailable")
		return
	}
	var sch model.BackupSchedule
	if err := s.DB.First(&sch, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found", "backup.schedule_not_found")
		return
	}
	result, err := s.Backups.CreateInstanceBackup(&sch)
	if err != nil {
		s.auditLogResult(c, "backup_schedule.instance", fmt.Sprintf("schedule_id=%d", id), "failure", "backup.instance_failed")
		fail(c, http.StatusBadGateway, "instance backup failed: "+err.Error(), "backup.instance_failed")
		return
	}
	defer os.Remove(result.Path)
	file, err := os.Open(result.Path)
	if err != nil {
		fail(c, http.StatusInternalServerError, "open archive: "+err.Error(), "backup.instance_read_failed")
		return
	}
	defer file.Close()
	if err := s.DB.Create(&model.BackupRun{
		ScheduleID: id, Trigger: "manual", Status: "success", Target: "download",
		Size: result.Size, SHA256: result.SHA256, Format: backup.InstanceArchiveFormat,
		ManifestVersion: result.ManifestVersion, ManifestSHA256: result.ManifestSHA256,
		Components: result.Components, CreatedAt: time.Now(),
	}).Error; err != nil {
		fail(c, http.StatusInternalServerError, "record archive run: "+err.Error(), "backup.audit_failed")
		return
	}
	s.auditLogResult(c, "backup_schedule.instance", fmt.Sprintf("schedule_id=%d manifest_sha256=%s", id, result.ManifestSHA256), "success", "")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="argus-instance-%d-%s.argusenc"`, id, time.Now().Format("20060102-150405")))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Argus-Key-Id", result.KeyID)
	c.Header("X-Argus-Sha256", result.SHA256)
	c.Header("X-Argus-Manifest-Version", fmt.Sprintf("%d", result.ManifestVersion))
	c.Header("X-Argus-Manifest-Sha256", result.ManifestSHA256)
	c.Header("X-Argus-Components", result.Components)
	_, _ = io.Copy(c.Writer, file)
}

func (s *Server) listBackupRuns(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	scheduleID := mustID(c)
	var sch model.BackupSchedule
	if err := s.DB.First(&sch, scheduleID).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	offset, limit := pagination(c)
	q := s.DB.Model(&model.BackupRun{}).Where("schedule_id = ?", scheduleID)
	var total int64
	q.Count(&total)
	var runs []model.BackupRun
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&runs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	okPage(c, gin.H{"runs": runs}, total, offset, limit)
}

// backupDrill 恢复演练（admin）：校验密文 → 解密到临时库 → integrity_check。
// 绝不替换当前数据库；可选 multipart file（加密备份），缺省时使用最近的本地备份。
const encryptedRestoreConfirmation = "RESTORE ENCRYPTED BACKUP"

func (s *Server) backupDrill(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	var sch model.BackupSchedule
	if err := s.DB.First(&sch, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if s.Backups == nil {
		fail(c, http.StatusInternalServerError, "backup manager not started")
		return
	}

	workDir := filepath.Join(filepath.Dir(s.Cfg.DBPath), ".backup-work")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 1. 定位密文文件：上传优先，其次最近本地备份
	var encPath string
	cleanup := func() {}
	file, err := c.FormFile("file")
	if err == nil {
		if file.Size > 512<<20 {
			fail(c, http.StatusBadRequest, "file too large (max 512MB)")
			return
		}
		encPath = filepath.Join(workDir, fmt.Sprintf("drill-%d-%d.argusenc", sch.ID, time.Now().UnixNano()))
		if err := c.SaveUploadedFile(file, encPath); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		cleanup = func() { _ = os.Remove(encPath) }
		defer cleanup()
	} else if !isHTTPTarget(sch.Target) {
		encPath, err = s.Backups.LatestLocalBackup(&sch)
		if err != nil {
			fail(c, http.StatusBadRequest, "no local backup file and no upload provided: "+err.Error())
			return
		}
	} else {
		fail(c, http.StatusBadRequest, "remote target requires uploading an encrypted backup file")
		return
	}

	// 2. 头部鉴别（不解密即可提示密钥指纹）
	embeddedKeyID, err := backup.ReadKeyID(encPath)
	if err != nil {
		fail(c, http.StatusBadRequest, "not a valid argus encrypted backup", "backup.bad_format")
		return
	}

	// 3. 派生密钥并解密到临时库
	key, keyID, err := s.Backups.ScheduleKey(&sch)
	if err != nil {
		fail(c, http.StatusInternalServerError, "derive key: "+err.Error())
		return
	}
	if embeddedKeyID != keyID {
		fail(c, http.StatusBadRequest,
			fmt.Sprintf("encryption key mismatch: backup key_id=%s, schedule key_id=%s (key rotated?)", embeddedKeyID, keyID),
			"backup.key_mismatch")
		return
	}
	dbPath := filepath.Join(workDir, fmt.Sprintf("drill-%d-%d.db", sch.ID, time.Now().UnixNano()))
	defer os.Remove(dbPath)
	if _, err := backup.DecryptFile(encPath, dbPath, key); err != nil {
		fail(c, http.StatusBadRequest, "decrypt failed: "+err.Error(), "backup.decrypt_failed")
		return
	}

	// 4. 校验 SQLite 头 + integrity_check（独立只读连接，不触碰运行中的主库）
	if err := validateSQLiteFile(dbPath); err != nil {
		fail(c, http.StatusBadRequest, "integrity check failed: "+err.Error(), "backup.integrity_failed")
		return
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "backup_schedule.drill", fmt.Sprintf("%s key_id=%s", sch.Name, keyID))
	ok(c, gin.H{
		"ok":           true,
		"key_id":       keyID,
		"source":       encPath,
		"db_size":      info.Size(),
		"integrity":    "ok",
		"restore_note": "演练成功：密文可解密且临时库完整性通过；未替换当前数据库",
	})
}

// restoreEncryptedBackup performs a controlled restore of one uploaded .argusenc file.
// The caller must explicitly confirm the destructive operation. Validation and decryption
// happen in staging before the live database is switched, and the response always requires
// an external process restart after a successful switch.
func appendAuditToSQLite(path string, entry *model.AuditLog) error {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open database pool: %w", err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(&model.AuditLog{}); err != nil {
		return fmt.Errorf("migrate audit log: %w", err)
	}
	if err := db.Create(entry).Error; err != nil {
		return fmt.Errorf("create audit log: %w", err)
	}
	return nil
}

func (s *Server) restoreEncryptedBackup(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil || !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	auditDetail := fmt.Sprintf("schedule_id=%d", id)
	failRestore := func(status int, message, code string) {
		s.auditLogResult(c, "backup_schedule.restore", auditDetail, "failure", code)
		fail(c, status, message, code)
	}
	if subtle.ConstantTimeCompare([]byte(c.PostForm("confirm")), []byte(encryptedRestoreConfirmation)) != 1 {
		failRestore(http.StatusBadRequest, "explicit restore confirmation required", "backup.confirmation_required")
		return
	}
	if s.Backups == nil {
		failRestore(http.StatusInternalServerError, "backup manager not started", "backup.manager_unavailable")
		return
	}

	var sch model.BackupSchedule
	if err := s.DB.First(&sch, id).Error; err != nil {
		failRestore(http.StatusNotFound, "not found", "backup.schedule_not_found")
		return
	}
	auditDetail = fmt.Sprintf("schedule_id=%d name=%s", sch.ID, sch.Name)
	file, err := c.FormFile("file")
	if err != nil {
		failRestore(http.StatusBadRequest, "encrypted backup file required", "backup.file_required")
		return
	}
	if file.Size <= 0 || file.Size > 512<<20 {
		failRestore(http.StatusBadRequest, "file must be between 1 byte and 512MB", "backup.file_size_invalid")
		return
	}

	workDir := filepath.Join(filepath.Dir(s.Cfg.DBPath), "restore-staging")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		failRestore(http.StatusInternalServerError, err.Error(), "backup.staging_failed")
		return
	}
	stamp := time.Now().UnixNano()
	encPath := filepath.Join(workDir, fmt.Sprintf("encrypted-%d-%d.argusenc", sch.ID, stamp))
	dbPath := filepath.Join(workDir, fmt.Sprintf("decrypted-%d-%d.db", sch.ID, stamp))
	defer os.Remove(encPath)
	defer os.Remove(dbPath)
	if err := c.SaveUploadedFile(file, encPath); err != nil {
		failRestore(http.StatusInternalServerError, "save upload: "+err.Error(), "backup.upload_failed")
		return
	}

	embeddedKeyID, err := backup.ReadKeyID(encPath)
	if err != nil {
		failRestore(http.StatusBadRequest, "not a valid argus encrypted backup", "backup.bad_format")
		return
	}
	key, keyID, err := s.Backups.ScheduleKey(&sch)
	if err != nil {
		failRestore(http.StatusInternalServerError, "derive key: "+err.Error(), "backup.key_derivation_failed")
		return
	}
	auditDetail = fmt.Sprintf("schedule_id=%d name=%s key_id=%s", sch.ID, sch.Name, keyID)
	if subtle.ConstantTimeCompare([]byte(embeddedKeyID), []byte(keyID)) != 1 {
		failRestore(http.StatusBadRequest,
			fmt.Sprintf("encryption key mismatch: backup key_id=%s, schedule key_id=%s", embeddedKeyID, keyID),
			"backup.key_mismatch")
		return
	}
	if _, err := backup.DecryptFile(encPath, dbPath, key); err != nil {
		failRestore(http.StatusBadRequest, "decrypt failed: "+err.Error(), "backup.decrypt_failed")
		return
	}
	if err := validateSQLiteFile(dbPath); err != nil {
		failRestore(http.StatusBadRequest, "integrity check failed: "+err.Error(), "backup.integrity_failed")
		return
	}
	entry, auditReady := newAuditEntry(c, "backup_schedule.restore", auditDetail+" validated=true", "success", "")
	if !auditReady {
		failRestore(http.StatusInternalServerError, "build restore audit record", "backup.audit_failed")
		return
	}
	if err := appendAuditToSQLite(dbPath, &entry); err != nil {
		failRestore(http.StatusInternalServerError, "write staging audit log: "+err.Error(), "backup.audit_failed")
		return
	}
	if err := validateSQLiteFile(dbPath); err != nil {
		failRestore(http.StatusInternalServerError, "post-audit integrity check failed: "+err.Error(), "backup.audit_failed")
		return
	}

	var rollbackPath string
	err = s.Backups.WithRestoreLock(func() error {
		var swapErr error
		rollbackPath, swapErr = s.swapDatabase(dbPath)
		return swapErr
	})
	if err != nil {
		failRestore(http.StatusInternalServerError, "database switch failed: "+err.Error(), "backup.restore_failed")
		return
	}

	ok(c, gin.H{
		"ok":               true,
		"key_id":           keyID,
		"rollback_path":    rollbackPath,
		"status":           "restart_required",
		"restart_required": true,
		"note":             "加密备份已校验、解密并切换；请立即通过进程管理器重启 Argus Server",
	})
}

// restoreInstanceBackup validates and installs a complete encrypted instance archive.
// Database replacement remains last; a process restart is required after success.
func (s *Server) restoreInstanceBackup(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil || !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	id := mustID(c)
	failRestore := func(status int, msg, code string) {
		s.auditLogResult(c, "backup_schedule.instance_restore", fmt.Sprintf("schedule_id=%d", id), "failure", code)
		fail(c, status, msg, code)
	}
	if subtle.ConstantTimeCompare([]byte(c.PostForm("confirm")), []byte(encryptedRestoreConfirmation)) != 1 {
		failRestore(http.StatusBadRequest, "explicit restore confirmation required", "backup.confirmation_required")
		return
	}
	if s.Backups == nil {
		failRestore(http.StatusInternalServerError, "backup manager not started", "backup.manager_unavailable")
		return
	}
	var sch model.BackupSchedule
	if err := s.DB.First(&sch, id).Error; err != nil {
		failRestore(http.StatusNotFound, "not found", "backup.schedule_not_found")
		return
	}
	file, err := c.FormFile("file")
	if err != nil || file.Size <= 0 || file.Size > 512<<20 {
		failRestore(http.StatusBadRequest, "instance archive file required and must be <= 512MB", "backup.file_size_invalid")
		return
	}
	root := filepath.Dir(s.Cfg.DBPath)
	stagingRoot := filepath.Join(root, "restore-staging", fmt.Sprintf("instance-%d-%d", id, time.Now().UnixNano()))
	if err := os.MkdirAll(stagingRoot, 0700); err != nil {
		failRestore(http.StatusInternalServerError, err.Error(), "backup.staging_failed")
		return
	}
	defer os.RemoveAll(stagingRoot)
	encPath := filepath.Join(stagingRoot, "archive.argusenc")
	if err := c.SaveUploadedFile(file, encPath); err != nil {
		failRestore(http.StatusInternalServerError, err.Error(), "backup.upload_failed")
		return
	}
	embeddedKeyID, err := backup.ReadKeyID(encPath)
	if err != nil {
		failRestore(http.StatusBadRequest, "not a valid encrypted archive", "backup.bad_format")
		return
	}
	key, keyID, err := s.Backups.ScheduleKey(&sch)
	if err != nil {
		failRestore(http.StatusInternalServerError, err.Error(), "backup.key_derivation_failed")
		return
	}
	if subtle.ConstantTimeCompare([]byte(embeddedKeyID), []byte(keyID)) != 1 {
		failRestore(http.StatusBadRequest, "encryption key mismatch", "backup.key_mismatch")
		return
	}
	zipPath := filepath.Join(stagingRoot, "archive.zip")
	if _, err := backup.DecryptFile(encPath, zipPath, key); err != nil {
		failRestore(http.StatusBadRequest, "decrypt failed: "+err.Error(), "backup.decrypt_failed")
		return
	}
	archiveRoot := filepath.Join(stagingRoot, "unpacked")
	manifest, err := backup.ExtractInstanceArchive(zipPath, archiveRoot)
	if err != nil {
		failRestore(http.StatusBadRequest, "archive validation failed: "+err.Error(), "backup.archive_invalid")
		return
	}
	stagedDB := filepath.Join(archiveRoot, "db", "argus.db")
	if err := validateSQLiteFile(stagedDB); err != nil {
		failRestore(http.StatusBadRequest, "integrity check failed: "+err.Error(), "backup.integrity_failed")
		return
	}
	entry, auditReady := newAuditEntry(c, "backup_schedule.instance_restore", fmt.Sprintf("schedule_id=%d manifest_sha256=%s", id, manifest.ManifestSHA256), "success", "")
	if !auditReady || appendAuditToSQLite(stagedDB, &entry) != nil {
		failRestore(http.StatusInternalServerError, "write staging audit record", "backup.audit_failed")
		return
	}
	type movedDir struct {
		target, old string
		installed   bool
	}
	var rollbackDB string
	var moved []movedDir
	scriptsRoot := os.Getenv("ARGUS_DATA_DIR")
	if scriptsRoot == "" {
		wd, _ := os.Getwd()
		scriptsRoot = filepath.Join(wd, "data")
	}
	err = s.Backups.WithRestoreLock(func() error {
		for _, item := range []struct{ name, target string }{
			{name: "themes", target: filepath.Join(root, "themes")},
			{name: "plugins", target: filepath.Join(root, "plugins")},
			{name: "scripts", target: filepath.Join(scriptsRoot, "scripts")},
		} {
			name, target := item.name, item.target
			src := filepath.Join(archiveRoot, name)
			if _, statErr := os.Stat(src); os.IsNotExist(statErr) {
				continue
			}
			old := target + ".pre-restore." + fmt.Sprint(time.Now().UnixNano())
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			d := movedDir{target: target, old: old}
			if _, statErr := os.Stat(target); statErr == nil {
				if err := os.Rename(target, old); err != nil {
					return err
				}
			}
			if err := os.Rename(src, target); err != nil {
				if _, oldErr := os.Stat(old); oldErr == nil {
					_ = os.Rename(old, target)
				}
				return err
			}
			d.installed = true
			moved = append(moved, d)
		}
		var swapErr error
		rollbackDB, swapErr = s.swapDatabase(stagedDB)
		return swapErr
	})
	if err != nil {
		for i := len(moved) - 1; i >= 0; i-- {
			d := moved[i]
			if d.installed {
				_ = os.RemoveAll(d.target)
			}
			if _, oldErr := os.Stat(d.old); oldErr == nil {
				_ = os.Rename(d.old, d.target)
			}
		}
		failRestore(http.StatusInternalServerError, "instance restore failed: "+err.Error(), "backup.restore_failed")
		return
	}
	s.auditLogResult(c, "backup_schedule.instance_restore", fmt.Sprintf("schedule_id=%d manifest_sha256=%s", id, manifest.ManifestSHA256), "success", "")
	ok(c, gin.H{"ok": true, "format": manifest.Format, "manifest_version": manifest.Version, "manifest_sha256": manifest.ManifestSHA256, "rollback_path": rollbackDB, "status": "restart_required", "restart_required": true})
}
