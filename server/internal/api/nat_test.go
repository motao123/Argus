package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/nat"
)

// natTestRouter 只挂 NAT 相关路由，复用 authzTestEnv。
func natTestRouter(e *authzTestEnv) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.GET("/nats", e.srv.listNAT)
	authed.POST("/nats", e.srv.createNAT)
	authed.PUT("/nats/:id", e.srv.updateNAT)
	authed.DELETE("/nats/:id", e.srv.deleteNAT)
	return r
}

func natReq(t *testing.T, r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("{}")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestNATOwnerIsolationAuditAndReserved：owner 隔离、reserved 域名拒绝、
// 域名规范化、审计记录、运行时状态/限额回填。
func TestNATOwnerIsolationAuditAndReserved(t *testing.T) {
	e := newAuthzEnv(t)
	e.srv.NAT = nat.New(e.srv.DB, func() map[int64]*rpc.Peer { return nil })
	e.srv.NAT.Configure(3, 7, []string{"dashboard.example.com"})
	r := natTestRouter(e)
	aliceJWT := e.token(t, e.alice)
	bobJWT := e.token(t, e.bob)

	// alice 创建（域名大小写/端口规范化，owner 为 alice）
	w := natReq(t, r, http.MethodPost, "/nats", aliceJWT,
		`{"server_id":`+itoa(e.aliceS.ID)+`,"domain":"APP.Example.com:8080","target_addr":"127.0.0.1:3000"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create = %d, body %s", w.Code, w.Body.String())
	}
	var createdResp struct {
		Data model.NAT `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createdResp); err != nil {
		t.Fatal(err)
	}
	created := createdResp.Data
	if created.Domain != "app.example.com" {
		t.Fatalf("domain not normalized: %q", created.Domain)
	}
	if created.OwnerID != e.alice.ID {
		t.Fatalf("owner = %d, want %d", created.OwnerID, e.alice.ID)
	}

	// alice 创建 reserved 域名 → 400
	w = natReq(t, r, http.MethodPost, "/nats", aliceJWT,
		`{"server_id":`+itoa(e.aliceS.ID)+`,"domain":"dashboard.example.com","target_addr":"127.0.0.1:3000"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("reserved create = %d, want 400", w.Code)
	}

	// bob 更新/删除 alice 的 NAT → 403
	w = natReq(t, r, http.MethodPut, "/nats/"+itoa(created.ID), bobJWT, `{"enabled":false}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob update = %d, want 403", w.Code)
	}
	w = natReq(t, r, http.MethodDelete, "/nats/"+itoa(created.ID), bobJWT, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob delete = %d, want 403", w.Code)
	}

	// 列表隔离：bob 看不到，alice 看得到；响应带 limits/reserved_hosts/status
	w = natReq(t, r, http.MethodGet, "/nats", bobJWT, "")
	var bobList struct {
		Data struct {
			Nats []model.NAT `json:"nats"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bobList); err != nil {
		t.Fatal(err)
	}
	if len(bobList.Data.Nats) != 0 {
		t.Fatalf("bob sees %d nats, want 0", len(bobList.Data.Nats))
	}
	w = natReq(t, r, http.MethodGet, "/nats", aliceJWT, "")
	var aliceList struct {
		Data struct {
			Nats   []model.NAT `json:"nats"`
			Limits struct {
				Server int `json:"server"`
				User   int `json:"user"`
			} `json:"limits"`
			ReservedHosts []string `json:"reserved_hosts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &aliceList); err != nil {
		t.Fatal(err)
	}
	if len(aliceList.Data.Nats) != 1 {
		t.Fatalf("alice sees %d nats, want 1", len(aliceList.Data.Nats))
	}
	n := aliceList.Data.Nats[0]
	if n.Status != "offline" || n.ActiveConnections != 0 || n.ServerConnectionLimit != 3 || n.OwnerConnectionLimit != 7 {
		t.Fatalf("runtime fields = %+v", n)
	}
	if aliceList.Data.Limits.Server != 3 || aliceList.Data.Limits.User != 7 {
		t.Fatalf("limits = %+v", aliceList.Data.Limits)
	}
	if len(aliceList.Data.ReservedHosts) != 1 || aliceList.Data.ReservedHosts[0] != "dashboard.example.com" {
		t.Fatalf("reserved_hosts = %v", aliceList.Data.ReservedHosts)
	}

	// 审计：create/update/delete 均记录
	var logs []model.AuditLog
	if err := e.srv.DB.Order("id").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Action != "nat.create" || logs[0].UserID != e.alice.ID {
		t.Fatalf("audit logs = %+v", logs)
	}

	// alice 正常更新
	w = natReq(t, r, http.MethodPut, "/nats/"+itoa(created.ID), aliceJWT,
		`{"target_addr":"127.0.0.1:8080","enabled":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("alice update = %d, body %s", w.Code, w.Body.String())
	}
	if err := e.srv.DB.First(&created, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if created.TargetAddr != "127.0.0.1:8080" || created.Enabled {
		t.Fatalf("updated nat = %+v", created)
	}

	// alice 删除成功 + 审计
	w = natReq(t, r, http.MethodDelete, "/nats/"+itoa(created.ID), aliceJWT, "")
	if w.Code != http.StatusOK {
		t.Fatalf("alice delete = %d", w.Code)
	}
	if err := e.srv.DB.First(&created, created.ID).Error; err == nil {
		t.Fatal("nat still exists after delete")
	}
	if err := e.srv.DB.Order("id").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	actions := make([]string, 0, len(logs))
	for _, l := range logs {
		actions = append(actions, l.Action)
	}
	if strings.Join(actions, ",") != "nat.create,nat.update,nat.delete" {
		t.Fatalf("audit actions = %v", actions)
	}
}
