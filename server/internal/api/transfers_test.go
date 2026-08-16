package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

func TestTransferLifecycle(t *testing.T) {
	e := newAuthzEnv(t) // admin/alice/bob + aliceS/bobS
	gin.SetMode(gin.TestMode)

	// admin 发起过户 aliceS → bob
	req := httptest.NewRequest(http.MethodPost, "/server-transfers", strings.NewReader(`{"server_id":`+itoa(e.aliceS.ID)+`,"to_user_id":`+itoa(e.bob.ID)+`}`))
	req.Header.Set("Authorization", "Bearer "+e.token(t, e.admin))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.POST("/server-transfers", e.srv.createTransfer)
	authed.GET("/server-transfers", e.srv.listTransfers)
	authed.POST("/server-transfers/:id/cancel", e.srv.cancelTransfer)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create transfer: got %d body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Transfer  model.ServerTransfer `json:"transfer"`
			NewSecret string               `json:"new_secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Transfer.Status != "pending" || resp.Data.NewSecret == "" {
		t.Fatalf("transfer not pending: %+v", resp.Data)
	}
	// 密钥已轮换：旧密钥失效、新密钥可注册（模拟 bob 的 Agent 用新密钥重连）
	var srv model.Server
	e.srv.DB.First(&srv, e.aliceS.ID)
	if srv.Secret == e.aliceS.Secret {
		t.Fatal("server secret should be rotated")
	}
	// 新密钥注册 → TransferCb 触发验证
	e.srv.VerifyTransfer(e.aliceS.ID)
	var tr model.ServerTransfer
	e.srv.DB.First(&tr, resp.Data.Transfer.ID)
	if tr.Status != "verified" {
		t.Fatalf("transfer should be verified, got %s", tr.Status)
	}
	e.srv.DB.First(&srv, e.aliceS.ID)
	if srv.OwnerID != e.bob.ID {
		t.Fatalf("owner should be bob, got %d", srv.OwnerID)
	}

	// 取消路径：再发起一次并取消
	req2 := httptest.NewRequest(http.MethodPost, "/server-transfers", strings.NewReader(`{"server_id":`+itoa(e.bobS.ID)+`,"to_user_id":`+itoa(e.alice.ID)+`}`))
	req2.Header.Set("Authorization", "Bearer "+e.token(t, e.admin))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var resp2 struct {
		Data struct {
			Transfer model.ServerTransfer `json:"transfer"`
		} `json:"data"`
	}
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	oldSecret := e.bobS.Secret
	var srvFresh model.Server
	if err := e.srv.DB.First(&srvFresh, e.bobS.ID).Error; err != nil {
		t.Fatalf("bobS load: %v", err)
	}
	srv = srvFresh
	if srv.Secret == oldSecret {
		t.Fatal("bobS secret should be rotated on transfer create")
	}
	req3 := httptest.NewRequest(http.MethodPost, "/server-transfers/"+itoa(resp2.Data.Transfer.ID)+"/cancel", nil)
	req3.Header.Set("Authorization", "Bearer "+e.token(t, e.admin))
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("cancel: got %d", w3.Code)
	}
	// 取消应回滚密钥（RollbackSecret = 原密钥）
	var tr2 model.ServerTransfer
	e.srv.DB.First(&tr2, resp2.Data.Transfer.ID)
	e.srv.DB.First(&srv, e.bobS.ID)
	if srv.Secret != oldSecret {
		t.Fatalf("cancel should rollback to original secret, got %s want %s", srv.Secret, oldSecret)
	}
	if srv.Secret != tr2.RollbackSecret {
		t.Fatalf("rollback secret mismatch, got %s want %s", srv.Secret, tr2.RollbackSecret)
	}
	if tr2.Status != "cancelled" {
		t.Fatalf("transfer should be cancelled, got %s", tr2.Status)
	}
}

func TestTransferRequiresAdmin(t *testing.T) {
	e := newAuthzEnv(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.POST("/server-transfers", e.srv.createTransfer)
	// 普通用户 alice 不能发起过户（admin only）
	req := httptest.NewRequest(http.MethodPost, "/server-transfers", strings.NewReader(`{"server_id":1,"to_user_id":2}`))
	req.Header.Set("Authorization", "Bearer "+e.token(t, e.alice))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin transfer: got %d want 403", w.Code)
	}
}

func TestAuditLogsAdminOnly(t *testing.T) {
	e := newAuthzEnv(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.GET("/admin/logs", e.srv.listAuditLogs)
	// admin 可读
	req := httptest.NewRequest(http.MethodGet, "/admin/logs?limit=5", nil)
	req.Header.Set("Authorization", "Bearer "+e.token(t, e.admin))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin audit: got %d", w.Code)
	}
	// 普通用户 403
	req2 := httptest.NewRequest(http.MethodGet, "/admin/logs?limit=5", nil)
	req2.Header.Set("Authorization", "Bearer "+e.token(t, e.alice))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("non-admin audit: got %d want 403", w2.Code)
	}
}

func TestNotificationRedactionAndPartialUpdate(t *testing.T) {
	e := newAuthzEnv(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.GET("/notifications", e.srv.listNotifications)
	authed.PUT("/notifications/:id", e.srv.updateNotification)
	authed.POST("/notifications", e.srv.createNotification)

	// 创建含凭据的通知
	create := httptest.NewRequest(http.MethodPost, "/notifications", strings.NewReader(`{"name":"tg","type":"telegram","url":"https://api.telegram.org/bot123456:SECRET/sendMessage","headers":"{\"X\":\"abc\"}","body":"{\"chat_id\":\"42\"}"}`))
	create.Header.Set("Authorization", "Bearer "+e.token(t, e.admin))
	create.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, create)
	var created struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)
	id := created.Data.ID

	// 列表读取：URL 脱敏、headers/body 不回显
	listReq := httptest.NewRequest(http.MethodGet, "/notifications", nil)
	listReq.Header.Set("Authorization", "Bearer "+e.token(t, e.admin))
	wl := httptest.NewRecorder()
	r.ServeHTTP(wl, listReq)
	var listResp struct {
		Data struct {
			Notifications []notificationView `json:"notifications"`
		} `json:"data"`
	}
	json.Unmarshal(wl.Body.Bytes(), &listResp)
	var found *notificationView
	for i := range listResp.Data.Notifications {
		if listResp.Data.Notifications[i].ID == id {
			found = &listResp.Data.Notifications[i]
		}
	}
	if found == nil {
		t.Fatal("notification not in list")
	}
	if strings.Contains(found.URL, "SECRET") || found.Headers != "" || found.Body != "" {
		t.Fatalf("secrets leaked in view: url=%q headers=%q body=%q", found.URL, found.Headers, found.Body)
	}
	if !strings.Contains(found.URL, "api.telegram.org") {
		t.Fatalf("host should remain visible in masked url: %q", found.URL)
	}

	// 部分更新：只改名，不触碰 URL（URL 省略保留原值）
	upd := httptest.NewRequest(http.MethodPut, "/notifications/"+itoa(id), strings.NewReader(`{"name":"tg-renamed"}`))
	upd.Header.Set("Authorization", "Bearer "+e.token(t, e.admin))
	upd.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, upd)
	if w2.Code != http.StatusOK {
		t.Fatalf("partial update: got %d", w2.Code)
	}
	var n model.Notification
	if err := e.srv.DB.First(&n, id).Error; err != nil {
		t.Fatal(err)
	}
	if n.Name != "tg-renamed" || !strings.Contains(n.URL, "SECRET") {
		t.Fatalf("partial update overwrote secrets: name=%q url=%q", n.Name, n.URL)
	}
}
