package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/retention"
)

func TestSettingsExposeRetentionDefaultsAndRejectInvalidUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := newAuthzEnv(t)
	adminToken := e.token(t, e.admin)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.GET("/settings", e.srv.getSettings)
	authed.POST("/settings", e.srv.saveSettings)

	get := httptest.NewRequest(http.MethodGet, "/settings", nil)
	get.Header.Set("Authorization", "Bearer "+adminToken)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var response struct {
		Data struct {
			Settings map[string]string `json:"settings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Settings[retention.SettingAuditDays] != "365" || response.Data.Settings[retention.SettingAuditMaxRows] != "5000" {
		t.Fatalf("missing defaults: %v", response.Data.Settings)
	}

	body := bytes.NewBufferString(`{"settings":{"retention_metric_1m_days":"0"}}`)
	post := httptest.NewRequest(http.MethodPost, "/settings", body)
	post.Header.Set("Authorization", "Bearer "+adminToken)
	post.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	r.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusBadRequest {
		t.Fatalf("POST status=%d body=%s", postRec.Code, postRec.Body.String())
	}
	var count int64
	e.srv.DB.Model(&model.Setting{}).Where("key = ?", retention.SettingMetric1mDays).Count(&count)
	if count != 0 {
		t.Fatal("invalid setting was persisted")
	}

	oldAudit := model.AuditLog{Action: "old.audit"}
	if err := e.srv.DB.Create(&oldAudit).Error; err != nil {
		t.Fatal(err)
	}
	validBody := bytes.NewBufferString(`{"settings":{"retention_metric_1m_days":"2"}}`)
	validPost := httptest.NewRequest(http.MethodPost, "/settings", validBody)
	validPost.Header.Set("Authorization", "Bearer "+adminToken)
	validPost.Header.Set("Content-Type", "application/json")
	validRec := httptest.NewRecorder()
	r.ServeHTTP(validRec, validPost)
	if validRec.Code != http.StatusOK {
		t.Fatalf("valid POST status=%d body=%s", validRec.Code, validRec.Body.String())
	}
	if err := e.srv.DB.First(&model.AuditLog{}, oldAudit.ID).Error; err != nil {
		t.Fatalf("settings request directly cleaned data: %v", err)
	}
}
