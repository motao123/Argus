package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
)

// topDo 用真实路由（api.New）发起资源排行请求，覆盖 authMiddleware + readonlyGate + 路由注册。
func (e *authzTestEnv) topDo(t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := New(e.srv)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// seedReport 让服务器上线并写入一次实时上报（排行取 store 快照）。
func (e *authzTestEnv) seedReport(t *testing.T, id int64, r *protocol.ReportParams) {
	t.Helper()
	var srv model.Server
	if err := e.srv.DB.First(&srv, id).Error; err != nil {
		t.Fatal(err)
	}
	e.srv.Store.Upsert(&srv)
	e.srv.Store.SetReport(id, protocol.HostInfo{}, r)
}

// decodeTop 解析响应 data.servers。
func decodeTop(t *testing.T, w *httptest.ResponseRecorder) []topServer {
	t.Helper()
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Metric  string      `json:"metric"`
			Limit   int         `json:"limit"`
			Servers []topServer `json:"servers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v (%s)", err, w.Body.String())
	}
	if !body.Success {
		t.Fatalf("response not success: %s", w.Body.String())
	}
	return body.Data.Servers
}

func TestTopUnauthorized(t *testing.T) {
	e := newAuthzEnv(t)
	w := e.topDo(t, "/api/v1/admin/top?metric=cpu", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d want 401", w.Code)
	}
}

// TestTopOrdering 六个指标各自的排序值与降序（同值按 id 升序稳定）。
func TestTopOrdering(t *testing.T) {
	e := newAuthzEnv(t)
	// alice 高 CPU/低延迟/小流量；bob 低 CPU/高延迟/大流量
	e.seedReport(t, e.aliceS.ID, &protocol.ReportParams{
		CPU: 90, MemUsed: 3 << 30, MemTotal: 4 << 30,
		DiskUsed: 50 << 20, DiskTotal: 100 << 20,
		NetInSpeed: 1000, NetOutSpeed: 2000, LatencyMs: 12, Timestamp: 1,
	})
	e.seedReport(t, e.bobS.ID, &protocol.ReportParams{
		CPU: 30, MemUsed: 1 << 30, MemTotal: 8 << 30,
		DiskUsed: 90 << 20, DiskTotal: 100 << 20,
		NetInSpeed: 5000, NetOutSpeed: 300, LatencyMs: 45, Timestamp: 1,
	})
	admin := e.token(t, e.admin)

	cases := []struct {
		metric string
		first  int64 // 应排第一的服务器 id
		value  float64
	}{
		{"cpu", e.aliceS.ID, 90},
		{"mem", e.aliceS.ID, 75}, // 3/4 G = 75%
		{"disk", e.bobS.ID, 90},  // 90/100 = 90%
		{"net_in", e.bobS.ID, 5000},
		{"net_out", e.aliceS.ID, 2000},
		{"latency", e.bobS.ID, 45},
	}
	for _, tc := range cases {
		w := e.topDo(t, "/api/v1/admin/top?metric="+tc.metric, admin)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d want 200 (%s)", tc.metric, w.Code, w.Body.String())
		}
		list := decodeTop(t, w)
		if len(list) != 2 {
			t.Fatalf("%s: got %d entries want 2 (%s)", tc.metric, len(list), w.Body.String())
		}
		if list[0].ServerID != tc.first || list[0].Value != tc.value {
			t.Fatalf("%s: top = %+v want id=%d value=%v", tc.metric, list[0], tc.first, tc.value)
		}
	}
	// mem/disk 透传 used/total
	w := e.topDo(t, "/api/v1/admin/top?metric=mem", admin)
	mem := decodeTop(t, w)
	if mem[0].Used != 3<<30 || mem[0].Total != 4<<30 {
		t.Fatalf("mem used/total passthrough: %+v", mem[0])
	}
	// 同值（disk 都是 50%）按 id 升序稳定
	e.seedReport(t, e.aliceS.ID, &protocol.ReportParams{DiskUsed: 50 << 20, DiskTotal: 100 << 20, Timestamp: 2})
	e.seedReport(t, e.bobS.ID, &protocol.ReportParams{DiskUsed: 50 << 20, DiskTotal: 100 << 20, Timestamp: 2})
	w = e.topDo(t, "/api/v1/admin/top?metric=disk", admin)
	disk := decodeTop(t, w)
	if disk[0].ServerID != e.aliceS.ID || disk[1].ServerID != e.bobS.ID {
		t.Fatalf("tie order: %+v want alice then bob", disk)
	}
}

// TestTopSkippedNoData 无快照/离线/无容量/无延迟测量四种情况被跳过。
func TestTopSkippedNoData(t *testing.T) {
	e := newAuthzEnv(t)
	// bob：注册到 store 但从未上报（快照存在、Online=false）
	var bobS model.Server
	if err := e.srv.DB.First(&bobS, e.bobS.ID).Error; err != nil {
		t.Fatal(err)
	}
	e.srv.Store.Upsert(&bobS)
	// alice：上报但 mem_total=0（无内存数据），LatencyMs=0（无延迟测量）
	e.seedReport(t, e.aliceS.ID, &protocol.ReportParams{CPU: 88, MemUsed: 1 << 30, Timestamp: 1})
	admin := e.token(t, e.admin)

	// cpu：alice 有值（在线）→ 1 条
	w := e.topDo(t, "/api/v1/admin/top?metric=cpu", admin)
	if got := decodeTop(t, w); len(got) != 1 || got[0].ServerID != e.aliceS.ID {
		t.Fatalf("cpu list: %+v want only alice", got)
	}
	// mem：alice total=0 跳过 → 空
	w = e.topDo(t, "/api/v1/admin/top?metric=mem", admin)
	if got := decodeTop(t, w); len(got) != 0 {
		t.Fatalf("mem list: %+v want empty", got)
	}
	// latency：alice 无测量 → 空
	w = e.topDo(t, "/api/v1/admin/top?metric=latency", admin)
	if got := decodeTop(t, w); len(got) != 0 {
		t.Fatalf("latency list: %+v want empty", got)
	}

	// alice 离线后不再参与排行
	e.srv.Store.MarkOffline(e.aliceS.ID)
	w = e.topDo(t, "/api/v1/admin/top?metric=cpu", admin)
	if got := decodeTop(t, w); len(got) != 0 {
		t.Fatalf("cpu after offline: %+v want empty", got)
	}
}

func TestTopLimit(t *testing.T) {
	e := newAuthzEnv(t)
	// 三台：alice/bob + 额外一台属于 admin
	extra := model.Server{Name: "extra-srv", Secret: agent.GenSecret(), OwnerID: e.admin.ID}
	if err := e.srv.DB.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	e.seedReport(t, e.aliceS.ID, &protocol.ReportParams{CPU: 10, Timestamp: 1})
	e.seedReport(t, e.bobS.ID, &protocol.ReportParams{CPU: 20, Timestamp: 1})
	e.seedReport(t, extra.ID, &protocol.ReportParams{CPU: 30, Timestamp: 1})
	admin := e.token(t, e.admin)

	// 默认 limit=10 → 全部返回
	w := e.topDo(t, "/api/v1/admin/top?metric=cpu", admin)
	if got := decodeTop(t, w); len(got) != 3 {
		t.Fatalf("default limit: got %d want 3", len(got))
	}
	// limit=2 → 只取前两名（30, 20）
	w = e.topDo(t, "/api/v1/admin/top?metric=cpu&limit=2", admin)
	got := decodeTop(t, w)
	if len(got) != 2 || got[0].Value != 30 || got[1].Value != 20 {
		t.Fatalf("limit=2: %+v want [30 20]", got)
	}
	// limit=0 / 负数 → 回退默认
	for _, bad := range []string{"0", "-5"} {
		w = e.topDo(t, "/api/v1/admin/top?metric=cpu&limit="+bad, admin)
		if got := decodeTop(t, w); len(got) != 3 {
			t.Fatalf("limit=%s: got %d want 3 (default)", bad, len(got))
		}
	}
	// limit 上限钳制（构造 60 台验证不返回 60 条）
	for i := 0; i < 60; i++ {
		srv := model.Server{Name: "bulk-srv", Secret: agent.GenSecret(), OwnerID: e.admin.ID}
		if err := e.srv.DB.Create(&srv).Error; err != nil {
			t.Fatal(err)
		}
		e.seedReport(t, srv.ID, &protocol.ReportParams{CPU: 1, Timestamp: 1})
	}
	w = e.topDo(t, "/api/v1/admin/top?metric=cpu&limit=999", admin)
	if got := decodeTop(t, w); len(got) != 50 {
		t.Fatalf("limit clamp: got %d want 50", len(got))
	}
}

// TestTopOwnerIsolation JWT 只看自己名下；PAT 需 server:read scope + 白名单。
func TestTopOwnerIsolation(t *testing.T) {
	e := newAuthzEnv(t)
	e.seedReport(t, e.aliceS.ID, &protocol.ReportParams{CPU: 90, Timestamp: 1})
	e.seedReport(t, e.bobS.ID, &protocol.ReportParams{CPU: 80, Timestamp: 1})

	// alice 只见 aliceS
	w := e.topDo(t, "/api/v1/admin/top?metric=cpu", e.token(t, e.alice))
	got := decodeTop(t, w)
	if len(got) != 1 || got[0].ServerID != e.aliceS.ID {
		t.Fatalf("alice sees: %+v want only aliceS", got)
	}
	// bob 只见 bobS
	w = e.topDo(t, "/api/v1/admin/top?metric=cpu", e.token(t, e.bob))
	got = decodeTop(t, w)
	if len(got) != 1 || got[0].ServerID != e.bobS.ID {
		t.Fatalf("bob sees: %+v want only bobS", got)
	}
	// admin 见两台
	w = e.topDo(t, "/api/v1/admin/top?metric=cpu", e.token(t, e.admin))
	if got := decodeTop(t, w); len(got) != 2 {
		t.Fatalf("admin sees: %+v want both", got)
	}

	// PAT 缺 server:read scope → 403
	noScope := e.createPAT(t, e.alice, []string{ScopeServerExec}, "")
	if w := e.topDo(t, "/api/v1/admin/top?metric=cpu", noScope); w.Code != http.StatusForbidden {
		t.Fatalf("PAT no read scope: got %d want 403", w.Code)
	}
	// PAT 白名单外服务器不可见：alice 的 PAT 指向 bobS，则 aliceS 被过滤（空列表，语义同 listServers）
	patBobOnly := e.createPAT(t, e.alice, []string{ScopeServerRead}, itoa(e.bobS.ID))
	w = e.topDo(t, "/api/v1/admin/top?metric=cpu", patBobOnly)
	if w.Code != http.StatusOK {
		t.Fatalf("PAT whitelist-only-bob: got %d want 200 (%s)", w.Code, w.Body.String())
	}
	if got := decodeTop(t, w); len(got) != 0 {
		t.Fatalf("PAT whitelist-only-bob: %+v want empty", got)
	}
	// PAT 白名单含 aliceS → 只看到 aliceS
	patOwn := e.createPAT(t, e.alice, []string{ScopeServerRead}, itoa(e.aliceS.ID))
	w = e.topDo(t, "/api/v1/admin/top?metric=cpu", patOwn)
	got = decodeTop(t, w)
	if len(got) != 1 || got[0].ServerID != e.aliceS.ID {
		t.Fatalf("PAT own: %+v want only aliceS", got)
	}
}

// TestTopReadonlyRole 只读角色可查看自有服务器排行（readonlyGate 白名单）。
func TestTopReadonlyRole(t *testing.T) {
	e := newAuthzEnv(t)
	ro := &model.User{Username: "ro-top", PasswordHash: "x", Role: model.RoleReadonly, AgentSecret: "s"}
	if err := e.srv.DB.Create(ro).Error; err != nil {
		t.Fatal(err)
	}
	roS := model.Server{Name: "ro-srv", Secret: "sec", OwnerID: ro.ID}
	if err := e.srv.DB.Create(&roS).Error; err != nil {
		t.Fatal(err)
	}
	e.seedReport(t, roS.ID, &protocol.ReportParams{CPU: 77, Timestamp: 1})

	// readonly 自己 → 200 且只含自有服务器
	w := e.topDo(t, "/api/v1/admin/top?metric=cpu", e.token(t, ro))
	if w.Code != http.StatusOK {
		t.Fatalf("readonly own top: got %d want 200 (%s)", w.Code, w.Body.String())
	}
	got := decodeTop(t, w)
	if len(got) != 1 || got[0].ServerID != roS.ID || got[0].Value != 77 {
		t.Fatalf("readonly sees: %+v want roS 77", got)
	}
}

func TestTopInvalidMetric(t *testing.T) {
	e := newAuthzEnv(t)
	admin := e.token(t, e.admin)
	for _, m := range []string{"", "foo", "load", "gpu"} {
		w := e.topDo(t, "/api/v1/admin/top?metric="+m, admin)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("metric=%q: got %d want 400", m, w.Code)
		}
	}
}
