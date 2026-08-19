package api

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func newAuditTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return &Server{DB: db}
}

func auditAdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("principal", &principal{UserID: 7, Username: "admin", IsAdmin: true})
		c.Next()
	}
}

func TestAuditLogResultCapturesStructuredRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := newAuditTestServer(t)
	router := gin.New()
	router.Use(auditContextMiddleware(), auditAdminMiddleware())
	router.POST("/operation", func(c *gin.Context) {
		time.Sleep(time.Millisecond)
		srv.auditLogResult(c, "alert.update", "alert_id=42 name=cpu", "failure", "alert.invalid_threshold")
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/operation", nil)
	req.Header.Set("X-Request-ID", "request-123")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Request-ID"); got != "request-123" {
		t.Fatalf("response request id=%q", got)
	}

	var entry model.AuditLog
	if err := srv.DB.First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.ResourceType != "alert" || entry.ResourceID != "42" {
		t.Fatalf("resource fields=%q/%q", entry.ResourceType, entry.ResourceID)
	}
	if entry.Outcome != "failure" || entry.ErrorCode != "alert.invalid_threshold" {
		t.Fatalf("result fields=%q/%q", entry.Outcome, entry.ErrorCode)
	}
	if entry.RequestID != "request-123" || entry.DurationMS < 1 {
		t.Fatalf("request fields=%q duration=%d", entry.RequestID, entry.DurationMS)
	}
}

func TestAuditListAndCSVExportApplyStructuredFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := newAuditTestServer(t)
	entries := []model.AuditLog{
		{Username: "admin", Action: "server.update", ResourceType: "server", ResourceID: "9", Outcome: "success", RequestID: "success-request", CreatedAt: time.Now()},
		{Username: "admin", Action: "backup_schedule.restore", ResourceType: "backup_schedule", ResourceID: "3", Outcome: "failure", ErrorCode: "backup.key_mismatch", RequestID: "failure-request", CreatedAt: time.Now()},
	}
	if err := srv.DB.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(auditAdminMiddleware())
	router.GET("/logs", srv.listAuditLogs)
	router.GET("/export", srv.exportAuditLogs)

	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/logs?resource_type=backup_schedule&outcome=failure", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var response struct {
		Data struct {
			Logs []model.AuditLog `json:"logs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Logs) != 1 || response.Data.Logs[0].ErrorCode != "backup.key_mismatch" {
		t.Fatalf("filtered logs=%+v", response.Data.Logs)
	}

	exportRecorder := httptest.NewRecorder()
	router.ServeHTTP(exportRecorder, httptest.NewRequest(http.MethodGet, "/export?format=csv&days=30&resource_type=backup_schedule&outcome=failure", nil))
	if exportRecorder.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exportRecorder.Code, exportRecorder.Body.String())
	}
	rows, err := csv.NewReader(strings.NewReader(exportRecorder.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("csv rows=%d body=%s", len(rows), exportRecorder.Body.String())
	}
	if strings.Join(rows[0], ",") != "id,time,username,action,resource_type,resource_id,outcome,error_code,duration_ms,request_id,detail,ip" {
		t.Fatalf("csv header=%v", rows[0])
	}
	if rows[1][4] != "backup_schedule" || rows[1][6] != "failure" || rows[1][7] != "backup.key_mismatch" {
		t.Fatalf("csv row=%v", rows[1])
	}
}
