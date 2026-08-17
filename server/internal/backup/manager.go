package backup

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

// ErrBusy 同一计划已有备份在执行（并发保护）。
var ErrBusy = errors.New("backup already running for this schedule")

// 本地备份文件命名：argus-backup-<scheduleID>-<YYYYMMDD-HHMMSS>.argusenc
const (
	localPrefix = "argus-backup-"
	localSuffix = ".argusenc"
)

// Manager 定时加密备份管理器：cron 调度 + 快照/加密/分发 + 保留清理 + 历史记录。
type Manager struct {
	db     *gorm.DB
	dbPath string
	// KeyFor 返回密钥材料与来源标签（不落盘明文，仅存来源标签与指纹）。
	KeyFor KeyProvider
	// Client HTTP PUT 上传客户端（测试可注入）。
	Client *http.Client

	mu      sync.Mutex
	cron    *cron.Cron
	ids     map[int64]cron.EntryID
	running map[int64]bool
	workDir string
}

func NewManager(db *gorm.DB, dbPath string, keyFor KeyProvider) *Manager {
	if keyFor == nil {
		keyFor = DefaultKeyProvider(dbPath)
	}
	return &Manager{
		db: db, dbPath: dbPath, KeyFor: keyFor,
		Client: &http.Client{Timeout: 10 * time.Minute},
		cron:   cron.New(), ids: make(map[int64]cron.EntryID), running: make(map[int64]bool),
		workDir: filepath.Join(filepath.Dir(dbPath), ".backup-work"),
	}
}

// Start 加载全部启用计划并启动调度。
func (m *Manager) Start() {
	// 上次进程退出时未完成的备份不能永久保持 running。
	m.db.Model(&model.BackupSchedule{}).Where("last_status = ?", "running").Updates(map[string]any{
		"last_status": "failed", "last_error": "server restarted while backup was running",
	})
	var scheds []model.BackupSchedule
	if err := m.db.Where("enabled = ?", true).Find(&scheds).Error; err == nil {
		for i := range scheds {
			m.Upsert(&scheds[i])
		}
	}
	m.cron.Start()
	log.Printf("backup manager started with %d schedules", len(m.ids))
}

// Stop 停止调度（不中断进行中的备份）。
func (m *Manager) Stop() { m.cron.Stop() }

// Upsert 注册/更新计划调度（停用或表达式非法时移除）。
func (m *Manager) Upsert(sch *model.BackupSchedule) {
	m.remove(sch.ID)
	if !sch.Enabled {
		return
	}
	spec := strings.TrimSpace(sch.Cron)
	if spec == "" {
		return
	}
	id := sch.ID
	eid, err := m.cron.AddFunc(spec, func() {
		var cur model.BackupSchedule
		if m.db.First(&cur, id).Error == nil && cur.Enabled {
			m.RunAsync(&cur, "cron")
		}
	})
	if err != nil {
		log.Printf("backup schedule %s: bad cron %q: %v", sch.Name, spec, err)
		return
	}
	m.mu.Lock()
	m.ids[sch.ID] = eid
	m.mu.Unlock()
}

// Remove 移除计划调度。
func (m *Manager) Remove(id int64) { m.remove(id) }

func (m *Manager) remove(id int64) {
	m.mu.Lock()
	eid, ok := m.ids[id]
	delete(m.ids, id)
	m.mu.Unlock()
	if ok {
		m.cron.Remove(eid)
	}
}

// RunAsync 异步执行一次备份（cron / run-now 共用）。
func (m *Manager) RunAsync(sch *model.BackupSchedule, trigger string) {
	go func() { _ = m.RunOnce(sch, trigger) }()
}

// RunOnce 同步执行一次备份：VACUUM INTO 快照 → AES-GCM 加密 → PUT/本地写入 → 保留清理。
// 任何一步失败都记录失败历史并回写计划状态，不产生半成品密文文件。
func (m *Manager) RunOnce(sch *model.BackupSchedule, trigger string) error {
	m.mu.Lock()
	if m.running[sch.ID] {
		m.mu.Unlock()
		return ErrBusy
	}
	m.running[sch.ID] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.running, sch.ID)
		m.mu.Unlock()
	}()

	started := time.Now()
	m.db.Model(&model.BackupSchedule{}).Where("id = ?", sch.ID).Updates(map[string]any{
		"last_status": "running", "last_error": "", "last_run_at": started,
	})

	var size int64
	var sha, target string
	runErr := m.executeBackup(sch, &size, &sha, &target)

	status, errMsg := "success", ""
	if runErr != nil {
		status, errMsg = "failed", runErr.Error()
	}
	run := model.BackupRun{
		ScheduleID: sch.ID, Trigger: trigger, Status: status,
		Target: target, Size: size, SHA256: sha, Error: errMsg,
		DurationMS: time.Since(started).Milliseconds(), CreatedAt: time.Now(),
	}
	m.db.Create(&run)
	m.db.Model(&model.BackupSchedule{}).Where("id = ?", sch.ID).Updates(map[string]any{
		"last_status": status, "last_error": errMsg, "last_size": size,
	})
	m.enforceRetention(sch)
	m.pruneHistory(sch)
	return runErr
}

// ScheduleKey 派生计划当前密钥（运行与恢复演练共用）。盐缺失时生成并持久化。
func (m *Manager) ScheduleKey(sch *model.BackupSchedule) ([]byte, string, error) {
	material, source, err := m.KeyFor()
	if err != nil {
		return nil, "", err
	}
	if sch.KeySalt == "" {
		salt, err := NewSalt()
		if err != nil {
			return nil, "", err
		}
		sch.KeySalt = salt
		m.db.Model(&model.BackupSchedule{}).Where("id = ?", sch.ID).Update("key_salt", salt)
	}
	key, keyID, err := DeriveKey(material, sch.KeySalt, "")
	if err != nil {
		return nil, "", err
	}
	// 回写来源标签与指纹（密钥本身不落盘）
	m.db.Model(&model.BackupSchedule{}).Where("id = ?", sch.ID).Updates(map[string]any{
		"key_source": source, "key_id": keyID,
	})
	return key, keyID, nil
}

// LatestLocalBackup 返回计划最近的本地备份文件（恢复演练用）。
func (m *Manager) LatestLocalBackup(sch *model.BackupSchedule) (string, error) {
	files, err := m.listLocalBackups(sch)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", errors.New("no local backup files found")
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	return files[0], nil
}

func (m *Manager) executeBackup(sch *model.BackupSchedule, size *int64, sha *string, target *string) error {
	key, keyID, err := m.ScheduleKey(sch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.workDir, 0o700); err != nil {
		return err
	}
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}

	// 1. VACUUM INTO 一致性快照（WAL 安全，不触碰运行中的主库）
	snap := filepath.Join(m.workDir, fmt.Sprintf("snap-%d-%d.db", sch.ID, time.Now().UnixNano()))
	defer os.Remove(snap)
	escaped := strings.ReplaceAll(snap, "'", "''")
	if _, err := sqlDB.Exec("VACUUM INTO '" + escaped + "'"); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	// 2. AES-256-GCM 加密
	enc := filepath.Join(m.workDir, fmt.Sprintf("enc-%d-%d.argusenc", sch.ID, time.Now().UnixNano()))
	defer os.Remove(enc)
	kid, sum, sz, err := EncryptFile(snap, enc, key)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	if kid != keyID {
		return errors.New("internal key fingerprint mismatch")
	}
	*size, *sha = sz, sum

	// 3. 分发：HTTP PUT 或本地目录
	filename := fmt.Sprintf("%s%d-%s%s", localPrefix, sch.ID, time.Now().Format("20060102-150405"), localSuffix)
	if isHTTPTarget(sch.Target) {
		if err := m.uploadHTTP(sch.Target, filename, enc, kid, sch.ID, sum, sz); err != nil {
			return fmt.Errorf("upload: %w", err)
		}
		*target = sch.Target + " (PUT)"
		return nil
	}
	path, err := m.writeLocal(sch.Target, filename, enc)
	if err != nil {
		return fmt.Errorf("write local: %w", err)
	}
	*target = path
	return nil
}

// uploadHTTP 通过 PUT 上传加密备份（URL 含 userinfo 时自动附带 Basic Auth）。
func (m *Manager) uploadHTTP(url, filename, encPath, keyID string, scheduleID int64, sha string, size int64) error {
	f, err := os.Open(encPath)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequest(http.MethodPut, url, f)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Argus-Filename", filename)
	req.Header.Set("X-Argus-Key-Id", keyID)
	req.Header.Set("X-Argus-Schedule-Id", fmt.Sprintf("%d", scheduleID))
	req.Header.Set("X-Argus-Sha256", sha)
	req.Header.Set("X-Argus-Size", fmt.Sprintf("%d", size))
	resp, err := m.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// writeLocal 写入本地目录（原子：临时文件 + rename）。
func (m *Manager) writeLocal(dir, filename, encPath string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filename)
	tmp := dst + ".tmp"
	src, err := os.Open(encPath)
	if err != nil {
		return "", err
	}
	defer src.Close()
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	_, cpyErr := io.Copy(out, src)
	closeErr := out.Close()
	if cpyErr != nil {
		_ = os.Remove(tmp)
		return "", cpyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dst, nil
}

// enforceRetention 本地目标：删除超出 KeepCount 的旧备份文件。
// 远程目标由服务端策略管理，这里仅清理历史记录（pruneHistory）。
func (m *Manager) enforceRetention(sch *model.BackupSchedule) {
	if sch.KeepCount < 1 {
		sch.KeepCount = 1
	}
	if isHTTPTarget(sch.Target) {
		return
	}
	files, err := m.listLocalBackups(sch)
	if err != nil {
		return
	}
	sort.Strings(files) // 文件名含时间戳，字典序即时间序
	for i := 0; i+sch.KeepCount < len(files); i++ {
		if err := os.Remove(files[i]); err != nil {
			log.Printf("backup retention: remove %s: %v", files[i], err)
		}
	}
}

// pruneHistory 仅保留每计划最近 KeepCount 条执行记录。
func (m *Manager) pruneHistory(sch *model.BackupSchedule) {
	if sch.KeepCount < 1 {
		sch.KeepCount = 1
	}
	var ids []int64
	if err := m.db.Model(&model.BackupRun{}).Where("schedule_id = ?", sch.ID).
		Order("id DESC").Limit(sch.KeepCount).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return
	}
	m.db.Where("schedule_id = ? AND id NOT IN ?", sch.ID, ids).Delete(&model.BackupRun{})
}

func (m *Manager) listLocalBackups(sch *model.BackupSchedule) ([]string, error) {
	entries, err := os.ReadDir(sch.Target)
	if err != nil {
		return nil, err
	}
	pattern := localPrefix + fmt.Sprint(sch.ID) + "-"
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, pattern) && strings.HasSuffix(name, localSuffix) {
			files = append(files, filepath.Join(sch.Target, name))
		}
	}
	return files, nil
}

func isHTTPTarget(target string) bool {
	t := strings.ToLower(strings.TrimSpace(target))
	return strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://")
}
