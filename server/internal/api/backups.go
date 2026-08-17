package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"

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
