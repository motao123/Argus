package backup

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

const testMaterial = "test-backup-key-material-0123456789"

func testKeyFor() KeyProvider {
	return func() ([]byte, string, error) { return []byte(testMaterial), "test:", nil }
}

func newTestDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := gdb.AutoMigrate(&model.BackupSchedule{}, &model.BackupRun{}); err != nil {
		t.Fatal(err)
	}
	return gdb
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.db")
	if err := os.WriteFile(plain, []byte("SQLite format 3\x00"+strings.Repeat("payload-", 1000)), 0o600); err != nil {
		t.Fatal(err)
	}
	enc := filepath.Join(dir, "out.argusenc")
	keyID, sha, size, err := EncryptFile(plain, enc, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if size == 0 || sha == "" || len(keyID) != 16 {
		t.Fatalf("bad metadata keyID=%q sha=%q size=%d", keyID, sha, size)
	}
	// 头部指纹一致
	headID, err := ReadKeyID(enc)
	if err != nil || headID != keyID {
		t.Fatalf("ReadKeyID = %q, %v; want %q", headID, err, keyID)
	}
	// 解密还原
	dec := filepath.Join(dir, "dec.db")
	gotKeyID, err := DecryptFile(enc, dec, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if gotKeyID != keyID {
		t.Fatalf("decrypt keyID = %q want %q", gotKeyID, keyID)
	}
	a, _ := os.ReadFile(plain)
	b, _ := os.ReadFile(dec)
	if string(a) != string(b) {
		t.Fatal("decrypted content mismatch")
	}
	// 密文不含明文（应被 GCM 完全混淆）
	raw, _ := os.ReadFile(enc)
	if strings.Contains(string(raw), "payload-") {
		t.Fatal("ciphertext leaks plaintext")
	}
	// 篡改检测
	raw[len(raw)-1] ^= 0xFF
	if err := os.WriteFile(enc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptFile(enc, dec, []byte("0123456789abcdef0123456789abcdef")); err == nil {
		t.Fatal("tampered ciphertext decrypted without error")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.db")
	os.WriteFile(plain, []byte("hello"), 0o600)
	enc := filepath.Join(dir, "out.argusenc")
	if _, _, _, err := EncryptFile(plain, enc, []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")); err != nil {
		t.Fatal(err)
	}
	// 同一长度不同密钥 → 指纹不匹配（ErrKeyMismatch）
	_, err := DecryptFile(enc, filepath.Join(dir, "dec.db"), []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	if err != ErrKeyMismatch {
		t.Fatalf("want ErrKeyMismatch, got %v", err)
	}
}

func TestKeyDerivationDeterministic(t *testing.T) {
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	k1, id1, err := DeriveKey([]byte("material"), salt, "")
	if err != nil {
		t.Fatal(err)
	}
	k2, id2, err := DeriveKey([]byte("material"), salt, "")
	if err != nil || string(k1) != string(k2) || id1 != id2 {
		t.Fatal("derivation not deterministic for same salt")
	}
	// 不同盐 → 不同密钥与指纹
	salt2, _ := NewSalt()
	k3, id3, _ := DeriveKey([]byte("material"), salt2, "")
	if string(k1) == string(k3) || id1 == id3 {
		t.Fatal("different salts must yield different keys")
	}
	// 不同材料 → 不同密钥
	k4, _, _ := DeriveKey([]byte("other"), salt, "")
	if string(k1) == string(k4) {
		t.Fatal("different materials must yield different keys")
	}
}

// TestRunOnceLocal 全链路：VACUUM INTO → 加密 → 写本地 → 解密校验 → 状态与历史落库。
func TestRunOnceLocal(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "argus.db")
	gdb := newTestDB(t, dbPath)
	target := filepath.Join(dir, "backups")
	sch := &model.BackupSchedule{Name: "nightly", Enabled: true, Cron: "0 3 * * *", Target: target, KeepCount: 2}
	if err := gdb.Create(sch).Error; err != nil {
		t.Fatal(err)
	}
	m := NewManager(gdb, dbPath, testKeyFor())
	if err := m.RunOnce(sch, "manual"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// 本地文件存在且可解密为合法 SQLite
	files, err := m.listLocalBackups(sch)
	if err != nil || len(files) != 1 {
		t.Fatalf("backup files = %v, err=%v", files, err)
	}
	key, _, err := m.ScheduleKey(sch)
	if err != nil {
		t.Fatal(err)
	}
	dec := filepath.Join(dir, "dec.db")
	if _, err := DecryptFile(files[0], dec, key); err != nil {
		t.Fatalf("decrypt backup: %v", err)
	}
	// integrity_check 通过（独立只读连接）
	dsn := "file:" + dec + "?mode=ro"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil || result != "ok" {
		t.Fatalf("integrity_check = %q err=%v", result, err)
	}
	// 计划状态回写
	var fresh model.BackupSchedule
	gdb.First(&fresh, sch.ID)
	if fresh.LastStatus != "success" || fresh.LastSize == 0 || fresh.LastRunAt == nil {
		t.Fatalf("schedule status not updated: %+v", fresh)
	}
	if fresh.KeyID == "" || fresh.KeySource != "test:" || fresh.KeySalt == "" {
		t.Fatalf("key metadata not persisted: %+v", fresh)
	}
	// 密钥明文不可见于数据库行
	if strings.Contains(fresh.KeySource, testMaterial) || strings.Contains(fresh.KeyID, testMaterial) {
		t.Fatal("key material leaked into db")
	}
	// 历史记录
	var runs []model.BackupRun
	gdb.Where("schedule_id = ?", sch.ID).Find(&runs)
	if len(runs) != 1 || runs[0].Status != "success" || runs[0].Size == 0 || runs[0].SHA256 == "" {
		t.Fatalf("run history wrong: %+v", runs)
	}
}

// TestRetention 保留策略：超出 KeepCount 的旧文件被清理，历史记录同步裁剪。
func TestRetention(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "argus.db")
	gdb := newTestDB(t, dbPath)
	target := filepath.Join(dir, "backups")
	os.MkdirAll(target, 0o700)
	sch := &model.BackupSchedule{Name: "keep", Target: target, KeepCount: 3}
	gdb.Create(sch)
	m := NewManager(gdb, dbPath, testKeyFor())

	// 造 6 份“备份”（名称带时间戳，字典序=时间序）
	for i := 0; i < 6; i++ {
		name := filepath.Join(target, localPrefix+itoa(sch.ID)+"-"+time.Date(2026, 1, 1+i, 3, 0, 0, 0, time.UTC).Format("20060102-150405")+localSuffix)
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		// 同步造历史
		gdb.Create(&model.BackupRun{ScheduleID: sch.ID, Status: "success", CreatedAt: time.Now()})
	}
	// 干扰文件不应被清理
	os.WriteFile(filepath.Join(target, "other.txt"), []byte("y"), 0o600)
	os.WriteFile(filepath.Join(target, localPrefix+"999-20260102-030405"+localSuffix), []byte("z"), 0o600)

	m.enforceRetention(sch)
	m.pruneHistory(sch)

	remain, _ := m.listLocalBackups(sch)
	if len(remain) != 3 {
		t.Fatalf("retention left %d files, want 3: %v", len(remain), remain)
	}
	if _, err := os.Stat(filepath.Join(target, "other.txt")); err != nil {
		t.Fatal("unrelated file removed")
	}
	if _, err := os.Stat(filepath.Join(target, localPrefix+"999-20260102-030405"+localSuffix)); err != nil {
		t.Fatal("other schedule file removed")
	}
	var runs int64
	gdb.Model(&model.BackupRun{}).Where("schedule_id = ?", sch.ID).Count(&runs)
	if runs != 3 {
		t.Fatalf("history pruned to %d, want 3", runs)
	}
}

// TestRunOnceHTTP PUT 上传目标：服务端收到密文并解密校验。
func TestCreateInstanceBackupIncludesManifestAndAssets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ARGUS_DATA_DIR", dir)
	dbPath := filepath.Join(dir, "argus.db")
	gdb := newTestDB(t, dbPath)
	for _, item := range []struct{ rel, body string }{
		{"themes/night/theme.css", "body{}"},
		{"plugins/demo/plugin.js", "console.log('ok')"},
		{"scripts/notify-1.js", "send()"},
	} {
		path := filepath.Join(dir, filepath.FromSlash(item.rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(item.body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sch := &model.BackupSchedule{Name: "instance", Target: filepath.Join(dir, "backups"), KeepCount: 1}
	if err := gdb.Create(sch).Error; err != nil {
		t.Fatal(err)
	}
	m := NewManager(gdb, dbPath, testKeyFor())
	result, err := m.CreateInstanceBackup(sch)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(result.Path)
	if result.ManifestVersion != InstanceArchiveVersion || result.ManifestSHA256 == "" || result.Components == "" {
		t.Fatalf("bad result: %+v", result)
	}
	key, _, err := m.ScheduleKey(sch)
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "instance.zip")
	if _, err := DecryptFile(result.Path, zipPath, key); err != nil {
		t.Fatal(err)
	}
	manifest, err := InspectInstanceArchive(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 4 {
		t.Fatalf("entries=%d want 4", len(manifest.Entries))
	}
}

func TestRunOnceHTTP(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "argus.db")
	gdb := newTestDB(t, dbPath)

	var gotBody []byte
	var gotHeaders http.Header
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeaders = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sch := &model.BackupSchedule{Name: "remote", Enabled: true, Cron: "0 3 * * *", Target: srv.URL + "/backups", KeepCount: 7}
	gdb.Create(sch)
	m := NewManager(gdb, dbPath, testKeyFor())
	if err := m.RunOnce(sch, "cron"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method = %s want PUT", gotMethod)
	}
	if gotHeaders.Get("X-Argus-Key-Id") == "" || gotHeaders.Get("X-Argus-Sha256") == "" {
		t.Fatalf("missing argus headers: %v", gotHeaders)
	}
	key, _, err := m.ScheduleKey(sch)
	if err != nil {
		t.Fatal(err)
	}
	enc := filepath.Join(dir, "remote.argusenc")
	if err := os.WriteFile(enc, gotBody, 0o600); err != nil {
		t.Fatal(err)
	}
	dec := filepath.Join(dir, "dec.db")
	if _, err := DecryptFile(enc, dec, key); err != nil {
		t.Fatalf("decrypt uploaded backup: %v", err)
	}
	var fresh model.BackupSchedule
	gdb.First(&fresh, sch.ID)
	if fresh.LastStatus != "success" {
		t.Fatalf("schedule status wrong: %+v", fresh)
	}
	var run model.BackupRun
	if err := gdb.Where("schedule_id = ?", sch.ID).First(&run).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.Target, "PUT") {
		t.Fatalf("run target missing PUT marker: %+v", run)
	}
}

// TestRunFailure 上游 500 时：计划标记 failed、历史记录错误、无残留半成品。
func TestRunFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "argus.db")
	gdb := newTestDB(t, dbPath)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	sch := &model.BackupSchedule{Name: "fail", Target: srv.URL + "/x", KeepCount: 3}
	gdb.Create(sch)
	m := NewManager(gdb, dbPath, testKeyFor())
	if err := m.RunOnce(sch, "manual"); err == nil {
		t.Fatal("expected failure")
	}
	var fresh model.BackupSchedule
	gdb.First(&fresh, sch.ID)
	if fresh.LastStatus != "failed" || fresh.LastError == "" {
		t.Fatalf("schedule failure not recorded: %+v", fresh)
	}
	var runs []model.BackupRun
	gdb.Where("schedule_id = ?", sch.ID).Find(&runs)
	if len(runs) != 1 || runs[0].Status != "failed" || runs[0].Error == "" {
		t.Fatalf("run history wrong: %+v", runs)
	}
	// 工作目录无残留
	entries, err := os.ReadDir(m.workDir)
	if err == nil && len(entries) != 0 {
		t.Fatalf("work dir not cleaned: %v", entries)
	}
}

// TestConcurrentGuard 同一计划并发执行被拒绝。
func TestConcurrentGuard(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "argus.db")
	gdb := newTestDB(t, dbPath)
	target := filepath.Join(dir, "backups")
	sch := &model.BackupSchedule{Name: "c", Target: target, KeepCount: 2}
	gdb.Create(sch)
	m := NewManager(gdb, dbPath, testKeyFor())

	m.mu.Lock()
	m.running[sch.ID] = true
	m.mu.Unlock()
	if err := m.RunOnce(sch, "manual"); err != ErrBusy {
		t.Fatalf("want ErrBusy, got %v", err)
	}
}

// TestScheduling 调度注册：启用计划注册 cron，停用/移除后注销。
func TestScheduling(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "argus.db")
	gdb := newTestDB(t, dbPath)
	m := NewManager(gdb, dbPath, testKeyFor())

	on := &model.BackupSchedule{Name: "on", Enabled: true, Cron: "0 4 * * *", Target: dir, KeepCount: 2}
	gdb.Create(on)
	m.Upsert(on)
	if len(m.ids) != 1 {
		t.Fatalf("enabled schedule not registered: %v", m.ids)
	}
	off := &model.BackupSchedule{Name: "off", Enabled: false, Cron: "0 4 * * *", Target: dir, KeepCount: 2}
	gdb.Create(off)
	m.Upsert(off)
	if len(m.ids) != 1 {
		t.Fatalf("disabled schedule registered: %v", m.ids)
	}
	// 非法表达式不注册
	bad := &model.BackupSchedule{Name: "bad", Enabled: true, Cron: "not-a-cron", Target: dir, KeepCount: 2}
	m.Upsert(bad)
	if len(m.ids) != 1 {
		t.Fatalf("bad cron registered: %v", m.ids)
	}
	m.Remove(on.ID)
	if len(m.ids) != 0 {
		t.Fatalf("removed schedule still registered: %v", m.ids)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
