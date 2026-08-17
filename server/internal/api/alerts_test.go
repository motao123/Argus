package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// TestAlertEscalateChannelOwnerValidation 升级渠道必须存在且 owner 匹配：
// 非 admin 只能用自己名下的渠道；admin 系统规则（owner=0）可用操作者名下渠道；
// 渠道不存在返回 400。
func TestAlertEscalateChannelOwnerValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := newAuthzEnv(t)
	aliceCh := model.Notification{Name: "alice-ch", Type: "webhook", URL: "http://127.0.0.1:1/a", OwnerID: e.alice.ID}
	bobCh := model.Notification{Name: "bob-ch", Type: "webhook", URL: "http://127.0.0.1:1/b", OwnerID: e.bob.ID}
	adminCh := model.Notification{Name: "admin-ch", Type: "webhook", URL: "http://127.0.0.1:1/c", OwnerID: e.admin.ID}
	for _, ch := range []*model.Notification{&aliceCh, &bobCh, &adminCh} {
		if err := e.srv.DB.Create(ch).Error; err != nil {
			t.Fatal(err)
		}
	}

	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.POST("/alerts", e.srv.createAlert)

	post := func(token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/alerts", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	aliceToken := e.token(t, e.alice)
	adminToken := e.token(t, e.admin)
	body := func(channelID int64) string {
		return fmt.Sprintf(`{"name":"r","metric":"cpu","server_ids":"%d","escalate_to_channel_id":%d}`,
			e.aliceS.ID, channelID)
	}

	// alice 用 bob 的渠道 → 403
	if rec := post(aliceToken, body(bobCh.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("alice->bob channel status=%d body=%s", rec.Code, rec.Body.String())
	}
	// alice 用自己的渠道 → 200
	if rec := post(aliceToken, body(aliceCh.ID)); rec.Code != http.StatusOK {
		t.Fatalf("alice->own channel status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 渠道不存在 → 400
	if rec := post(aliceToken, body(99999)); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing channel status=%d body=%s", rec.Code, rec.Body.String())
	}
	// admin 系统规则（owner=0）用操作者名下渠道 → 200
	if rec := post(adminToken, body(adminCh.ID)); rec.Code != http.StatusOK {
		t.Fatalf("admin->own channel status=%d body=%s", rec.Code, rec.Body.String())
	}
}
