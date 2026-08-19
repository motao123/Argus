package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/backup"
	"github.com/motao123/Argus/server/internal/config"
	"github.com/motao123/Argus/server/internal/model"
)

const restoreTestMaterial = "restore-test-key-material"

func newEncryptedRestoreEnv(t *testing.T) (*Server, model.BackupSchedule, string, string) {
	t.Helper()
	dir := t.TempDir()
	livePath := filepath.Join(dir, "argus.db")
	gdb, err := gorm.Open(sqlite.Open(livePath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := gdb.AutoMigrate(&model.BackupSchedule{}, &model.BackupRun{}, &model.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec("CREATE TABLE restore_marker (value TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Exec("INSERT INTO restore_marker (value) VALUES ('live')").Error; err != nil {
		t.Fatal(err)
	}
	salt, err := backup.NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	sch := model.BackupSchedule{Name: "restore-test", Cron: "0 3 * * *", Target: dir, KeepCount: 1, KeySalt: salt}
	if err := gdb.Create(&sch).Error; err != nil {
		t.Fatal(err)
	}
	keyProvider := func() ([]byte, string, error) {
		return []byte(restoreTestMaterial), "test:restore", nil
	}
	manager := backup.NewManager(gdb, livePath, keyProvider)
	srv := &Server{DB: gdb, Cfg: &config.Config{DBPath: livePath}, Backups: manager}

	restoredPath := filepath.Join(dir, "restored.db")
	restored, err := gorm.Open(sqlite.Open(restoredPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Exec("CREATE TABLE restore_marker (value TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := restored.Exec("INSERT INTO restore_marker (value) VALUES ('restored')").Error; err != nil {
		t.Fatal(err)
	}
	restoredSQL, err := restored.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := restoredSQL.Close(); err != nil {
		t.Fatal(err)
	}

	key, _, err := backup.DeriveKey([]byte(restoreTestMaterial), salt, "")
	if err != nil {
		t.Fatal(err)
	}
	encPath := filepath.Join(dir, "restore.argusenc")
	if _, _, _, err := backup.EncryptFile(restoredPath, encPath, key); err != nil {
		t.Fatal(err)
	}
	return srv, sch, encPath, livePath
}

func encryptedRestoreRequest(t *testing.T, srv *Server, scheduleID int64, encPath, confirmation string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if confirmation != "" {
		if err := writer.WriteField("confirm", confirmation); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("file", filepath.Base(encPath))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(encPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, file); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/restore/:id", func(c *gin.Context) {
		c.Set("principal", &principal{UserID: 1, Username: "admin", IsAdmin: true})
		c.Next()
	}, srv.restoreEncryptedBackup)
	req := httptest.NewRequest(http.MethodPost, "/restore/"+itoa(scheduleID), &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func readRestoreMarker(t *testing.T, path string) string {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path+"?mode=ro"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var value string
	if err := db.Raw("SELECT value FROM restore_marker LIMIT 1").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	return value
}

func TestEncryptedRestoreRequiresExplicitConfirmation(t *testing.T) {
	srv, sch, encPath, livePath := newEncryptedRestoreEnv(t)
	recorder := encryptedRestoreRequest(t, srv, sch.ID, encPath, "RESTORE")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "backup.confirmation_required") {
		t.Fatalf("missing stable error code: %s", recorder.Body.String())
	}
	if got := readRestoreMarker(t, livePath); got != "live" {
		t.Fatalf("live database changed without confirmation: %q", got)
	}
}

func TestEncryptedRestoreRejectsWrongScheduleKey(t *testing.T) {
	srv, sch, encPath, livePath := newEncryptedRestoreEnv(t)
	srv.Backups.KeyFor = func() ([]byte, string, error) {
		return []byte("different-key-material"), "test:wrong", nil
	}
	recorder := encryptedRestoreRequest(t, srv, sch.ID, encPath, encryptedRestoreConfirmation)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "backup.key_mismatch") {
		t.Fatalf("missing stable error code: %s", recorder.Body.String())
	}
	if got := readRestoreMarker(t, livePath); got != "live" {
		t.Fatalf("live database changed after key mismatch: %q", got)
	}
}

func TestEncryptedRestoreSwitchesDatabaseAndKeepsRollback(t *testing.T) {
	srv, sch, encPath, livePath := newEncryptedRestoreEnv(t)
	restarted := make(chan struct{}, 1)
	srv.Restart = func() { restarted <- struct{}{} }
	recorder := encryptedRestoreRequest(t, srv, sch.ID, encPath, encryptedRestoreConfirmation)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data struct {
			RollbackPath    string `json:"rollback_path"`
			RestartRequired bool   `json:"restart_required"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Data.RestartRequired || response.Data.RollbackPath == "" {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
	if got := readRestoreMarker(t, livePath); got != "restored" {
		t.Fatalf("live database marker=%q want restored", got)
	}
	if got := readRestoreMarker(t, response.Data.RollbackPath); got != "live" {
		t.Fatalf("rollback database marker=%q want live", got)
	}
	restored, err := gorm.Open(sqlite.Open(livePath+"?mode=ro"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	restoredSQL, err := restored.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer restoredSQL.Close()
	var audit model.AuditLog
	if err := restored.Where("action = ? AND outcome = ?", "backup_schedule.restore", "success").First(&audit).Error; err != nil {
		t.Fatalf("restored database missing success audit: %v", err)
	}
	if audit.ResourceType != "backup_schedule" || audit.ResourceID != itoa(sch.ID) {
		t.Fatalf("restore audit resource=%q/%q", audit.ResourceType, audit.ResourceID)
	}
	health := httptest.NewRecorder()
	healthCtx, _ := gin.CreateTestContext(health)
	healthCtx.Request = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	srv.Healthz(healthCtx)
	if health.Code != http.StatusServiceUnavailable {
		t.Fatalf("health after restore = %d body=%s", health.Code, health.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("restart callback was not scheduled")
	}
}
