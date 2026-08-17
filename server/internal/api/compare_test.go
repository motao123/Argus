package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// compareDo 用真实路由（api.New）发起指标对比请求，覆盖 authMiddleware + readonlyGate + 路由注册。
func (e *authzTestEnv) compareDo(t *testing.T, path, token string) *httptest.ResponseRecorder {
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

// seedMetrics 为指定服务器写入 count 条 granularity 粒度、step 秒间隔的指标行。
func seedMetrics(t *testing.T, e *authzTestEnv, serverID int64, tsBase int64, gran, step, count int, cpu float64) {
	t.Helper()
	rows := make([]model.Metric, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, model.Metric{
			ServerID:    serverID,
			TS:          tsBase + int64(i*step),
			Granularity: gran,
			CPU:         cpu + float64(i),
			NetInSpeed:  float64(i) * 1000,
			Load1:       float64(i) / 2,
			MemUsed:     uint64(1024 * i),
			MemTotal:    4096,
			DiskUsed:    uint64(512 * i),
			DiskTotal:   10240,
		})
	}
	if err := e.srv.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
}

// decodeSeries 解析响应 data.series。
func decodeSeries(t *testing.T, w *httptest.ResponseRecorder) []struct {
	ServerID   int64  `json:"server_id"`
	ServerName string `json:"server_name"`
	Points     []struct {
		TS    int64   `json:"ts"`
		CPU   float64 `json:"cpu"`
		Mem   uint64  `json:"mem_used"`
		Total uint64  `json:"mem_total"`
	} `json:"points"`
} {
	t.Helper()
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Period string `json:"period"`
			Series []struct {
				ServerID   int64  `json:"server_id"`
				ServerName string `json:"server_name"`
				Points     []struct {
					TS    int64   `json:"ts"`
					CPU   float64 `json:"cpu"`
					Mem   uint64  `json:"mem_used"`
					Total uint64  `json:"mem_total"`
				} `json:"points"`
			} `json:"series"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v (%s)", err, w.Body.String())
	}
	if !body.Success {
		t.Fatalf("response not success: %s", w.Body.String())
	}
	return body.Data.Series
}

func TestCompareMetricsAuth(t *testing.T) {
	e := newAuthzEnv(t)
	// 未登录 → 401（authed 组强制认证）
	w := e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID), "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d want 401", w.Code)
	}
	// admin → 200
	w = e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID)+","+itoa(e.bobS.ID), e.token(t, e.admin))
	if w.Code != http.StatusOK {
		t.Fatalf("admin compare: got %d want 200 (%s)", w.Code, w.Body.String())
	}
}

func TestCompareMetricsMultiServerAndAggregation(t *testing.T) {
	e := newAuthzEnv(t)
	now := time.Now().Unix()
	// 24h 窗（gran=300, step=300）：桶边界对齐，行内以 100s 间隔塞 3 行验证求平均
	base := now/300*300 - 1800
	seedMetrics(t, e, e.aliceS.ID, base, 300, 100, 3, 10) // cpu 10/11/12 同桶 → 11
	seedMetrics(t, e, e.bobS.ID, base, 300, 100, 3, 50)   // cpu 50/51/52 同桶 → 51
	// 错误粒度（60s）的行不应混入 24h 聚合
	seedMetrics(t, e, e.aliceS.ID, base+50, 60, 60, 1, 99)

	w := e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID)+","+itoa(e.bobS.ID)+"&period=24h", e.token(t, e.admin))
	if w.Code != http.StatusOK {
		t.Fatalf("admin multi compare: got %d want 200 (%s)", w.Code, w.Body.String())
	}
	series := decodeSeries(t, w)
	if len(series) != 2 {
		t.Fatalf("series count: got %d want 2 (%s)", len(series), w.Body.String())
	}
	if series[0].ServerID != e.aliceS.ID || series[0].ServerName != "alice-srv" {
		t.Fatalf("series[0] = %+v want aliceS", series[0])
	}
	if series[1].ServerID != e.bobS.ID || series[1].ServerName != "bob-srv" {
		t.Fatalf("series[1] = %+v want bobS", series[1])
	}
	// 三行 cpu=10,11,12 同桶 → 均值 11；ts 对齐到桶起点；60s 粒度行被过滤
	if len(series[0].Points) != 1 || series[0].Points[0].CPU != 11 || series[0].Points[0].TS != base {
		t.Fatalf("alice points = %+v want single bucket cpu=11 ts=%d", series[0].Points, base)
	}
	if len(series[1].Points) != 1 || series[1].Points[0].CPU != 51 {
		t.Fatalf("bob points = %+v want cpu=51", series[1].Points)
	}
	// 透传字段（mem/disk 取桶内最后一行）
	if series[0].Points[0].Mem != 1024*2 || series[0].Points[0].Total != 4096 {
		t.Fatalf("mem passthrough = %+v", series[0].Points[0])
	}
}

func TestCompareMetricsCrossOwner(t *testing.T) {
	e := newAuthzEnv(t)
	aliceToken := e.token(t, e.alice)

	// alice 只看自己的 → 200
	w := e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID), aliceToken)
	if w.Code != http.StatusOK {
		t.Fatalf("alice own compare: got %d want 200 (%s)", w.Code, w.Body.String())
	}
	// alice 看 bob 的 → 403
	w = e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.bobS.ID), aliceToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice bob-only compare: got %d want 403", w.Code)
	}
	// 混搭（一台越权）→ 整体 403，不泄露任何数据
	w = e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID)+","+itoa(e.bobS.ID), aliceToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice mixed compare: got %d want 403", w.Code)
	}
	// bob 的 PAT 白名单指向 bobS：查 aliceS → 403
	pat := e.createPAT(t, e.bob, []string{ScopeServerRead}, itoa(e.bobS.ID))
	w = e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID), pat)
	if w.Code != http.StatusForbidden {
		t.Fatalf("PAT whitelist cross-server compare: got %d want 403", w.Code)
	}
	// 缺 server read scope → 403
	noScope := e.createPAT(t, e.alice, []string{ScopeServerExec}, "")
	w = e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID), noScope)
	if w.Code != http.StatusForbidden {
		t.Fatalf("PAT no read scope: got %d want 403", w.Code)
	}
	// PAT 有 scope + 白名单含目标 → 200
	okPAT := e.createPAT(t, e.alice, []string{ScopeServerRead}, itoa(e.aliceS.ID))
	w = e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID), okPAT)
	if w.Code != http.StatusOK {
		t.Fatalf("PAT ok compare: got %d want 200 (%s)", w.Code, w.Body.String())
	}
}

func TestCompareMetricsLimits(t *testing.T) {
	e := newAuthzEnv(t)
	adminToken := e.token(t, e.admin)

	// 缺 ids → 400
	w := e.compareDo(t, "/api/v1/metrics/compare?period=1h", adminToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing ids: got %d want 400", w.Code)
	}
	// 非法 ids → 400
	w = e.compareDo(t, "/api/v1/metrics/compare?ids=abc,xyz", adminToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid ids: got %d want 400", w.Code)
	}
	// 重复 id 去重后放行
	w = e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID)+","+itoa(e.aliceS.ID), adminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("dedup ids: got %d want 200 (%s)", w.Code, w.Body.String())
	}
	// 超过 10 台 → 400
	ids := make([]string, 0, 11)
	for i := int64(1); i <= 11; i++ {
		ids = append(ids, itoa(i))
	}
	w = e.compareDo(t, "/api/v1/metrics/compare?ids="+strings.Join(ids, ","), adminToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("11 ids: got %d want 400", w.Code)
	}
}

func TestCompareMetricsReadonlyRole(t *testing.T) {
	e := newAuthzEnv(t)
	ro := &model.User{Username: "ro-cmp", PasswordHash: "x", Role: model.RoleReadonly, AgentSecret: "s"}
	if err := e.srv.DB.Create(ro).Error; err != nil {
		t.Fatal(err)
	}
	roS := model.Server{Name: "ro-srv", Secret: "sec", OwnerID: ro.ID}
	if err := e.srv.DB.Create(&roS).Error; err != nil {
		t.Fatal(err)
	}
	// readonly 可对比自有服务器（readonlyGate 白名单）
	w := e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(roS.ID), e.token(t, ro))
	if w.Code != http.StatusOK {
		t.Fatalf("readonly own compare: got %d want 200 (%s)", w.Code, w.Body.String())
	}
	// readonly 对比他人服务器 → 403（owner 校验优先于只读放行）
	w = e.compareDo(t, "/api/v1/metrics/compare?ids="+itoa(e.aliceS.ID), e.token(t, ro))
	if w.Code != http.StatusForbidden {
		t.Fatalf("readonly cross-owner compare: got %d want 403", w.Code)
	}
}
