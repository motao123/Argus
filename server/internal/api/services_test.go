package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
)

// svcRouter 注册服务创建/更新路由，复用 authzTestEnv 的身份体系。
func svcRouter(e *authzTestEnv) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.POST("/services", requireScope(ScopeServiceWrite), e.srv.createService)
	authed.PUT("/services/:id", requireScope(ScopeServiceWrite), e.srv.updateService)
	return r
}

func svcDo(e *authzTestEnv, t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	svcRouter(e).ServeHTTP(w, req)
	return w
}

func TestCreateServiceCustomRequestFields(t *testing.T) {
	e := newAuthzEnv(t)
	token := e.token(t, e.alice)
	w := svcDo(e, t, http.MethodPost, "/services", token,
		`{"server_id":`+itoa(e.aliceS.ID)+`,"name":"post-svc","type":"http","target":"http://127.0.0.1:8080/api",
		  "http_method":"POST","request_headers":"[{\"key\":\"X-Api-Key\",\"value\":\"secret\"},{\"key\":\"Host\",\"value\":\"api.example.com\"}]",
		  "request_body":"{\"name\":\"x\"}","assert_contains":"\"ok\":true"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var svc model.Service
	if err := e.srv.DB.First(&svc, "name = ?", "post-svc").Error; err != nil {
		t.Fatal(err)
	}
	if svc.HTTPMethod != "POST" || svc.RequestBody != `{"name":"x"}` || svc.AssertContains != `"ok":true` {
		t.Fatalf("stored service = %+v", svc)
	}
	var headers []protocol.KeyValue
	if err := json.Unmarshal([]byte(svc.RequestHeaders), &headers); err != nil {
		t.Fatalf("request_headers not valid JSON: %v (%q)", err, svc.RequestHeaders)
	}
	if len(headers) != 2 || headers[0].Key != "X-Api-Key" || headers[0].Value != "secret" || headers[1].Key != "Host" {
		t.Fatalf("stored headers = %+v", headers)
	}
}

func TestCreateServiceMethodAndBodyValidation(t *testing.T) {
	e := newAuthzEnv(t)
	token := e.token(t, e.alice)
	base := `{"server_id":` + itoa(e.aliceS.ID) + `,"name":"m","type":"http","target":"http://x"}`
	for _, tc := range []struct {
		name, body, wantErr string
	}{
		{"get-with-body", `{"http_method":"GET","request_body":"x"}`, "request_body"},
		{"head-with-body", `{"http_method":"HEAD","request_body":"x"}`, "request_body"},
		{"unsupported-method", `{"http_method":"OPTIONS"}`, "http_method"},
		{"bad-headers-json", `{"request_headers":"not-json"}`, "request_headers"},
		{"empty-header-key", `{"request_headers":"[{\"key\":\"\",\"value\":\"v\"}]"}`, "empty header key"},
		{"put-with-body-ok", `{"http_method":"PUT","request_body":"x"}`, ""},
		{"empty-headers-ok", `{"request_headers":""}`, ""},
	} {
		body := strings.TrimSuffix(base, "}") + "," + strings.TrimPrefix(tc.body, "{")
		w := svcDo(e, t, http.MethodPost, "/services", token, body)
		if tc.wantErr == "" {
			if w.Code != http.StatusOK {
				t.Errorf("%s: want 200, got %d %s", tc.name, w.Code, w.Body.String())
			}
			continue
		}
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.wantErr) {
			t.Errorf("%s: want 400 containing %q, got %d %s", tc.name, tc.wantErr, w.Code, w.Body.String())
		}
	}
}

func TestUpdateServiceCustomRequestFields(t *testing.T) {
	e := newAuthzEnv(t)
	token := e.token(t, e.alice)
	w := svcDo(e, t, http.MethodPut, "/services/"+itoa(e.svc.ID), token,
		`{"http_method":"PATCH","request_headers":"[{\"key\":\"X-Trace\",\"value\":\"1\"}]",
		  "request_body":"patch-data","assert_contains":"patched"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	var svc model.Service
	if err := e.srv.DB.First(&svc, e.svc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if svc.HTTPMethod != "PATCH" || svc.RequestBody != "patch-data" || svc.AssertContains != "patched" {
		t.Fatalf("updated service = %+v", svc)
	}
	if svc.RequestHeaders != `[{"key":"X-Trace","value":"1"}]` {
		t.Fatalf("updated headers = %q", svc.RequestHeaders)
	}
}

func TestUpdateServiceGetWithBodyRejected(t *testing.T) {
	e := newAuthzEnv(t)
	token := e.token(t, e.alice)
	w := svcDo(e, t, http.MethodPut, "/services/"+itoa(e.svc.ID), token,
		`{"request_body":"x","http_method":"GET"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "request_body") {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
}

func TestCreateServiceLegacyDefaults(t *testing.T) {
	e := newAuthzEnv(t)
	token := e.token(t, e.alice)
	w := svcDo(e, t, http.MethodPost, "/services", token,
		`{"server_id":`+itoa(e.aliceS.ID)+`,"name":"legacy","type":"http","target":"http://x"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var svc model.Service
	if err := e.srv.DB.First(&svc, "name = ?", "legacy").Error; err != nil {
		t.Fatal(err)
	}
	if svc.HTTPMethod != "GET" || svc.RequestHeaders != "" || svc.RequestBody != "" || svc.AssertContains != "" {
		t.Fatalf("legacy defaults = %+v", svc)
	}
}

// ---- P1：延迟分位数（滑动窗口）----

// TestServiceStatsDelayQuantiles stats 接口输出滑动窗口分位数；样本不足（< 30）时为 null。
func TestServiceStatsDelayQuantiles(t *testing.T) {
	e := newReadonlyEnv(t)
	svc := model.Service{OwnerID: e.alice.ID, ServerID: e.aliceS.ID, Name: "q", Type: "http", Target: "http://x", Interval: 60, Enabled: true}
	if err := e.srv.DB.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix() / 60 * 60
	seedHist(t, e, svc.ID,
		// 更早的桶样本不足（不入选）
		model.ServiceHistory{Ts: now - 3600, Total: 1, DelaySamples: 5, DelayP50: 1},
		// 最新且样本充足的桶 → 快照生效
		model.ServiceHistory{Ts: now, Total: 1, DelaySamples: 45,
			DelayP50: 50, DelayP95: 95, DelayP99: 99, DelayStdDevMs: 10, DelayJitterMs: 8},
	)

	w := e.doFull(t, http.MethodGet, "/api/v1/services/"+itoa(svc.ID)+"/stats", e.token(t, e.alice), "")
	if w.Code != http.StatusOK {
		t.Fatalf("stats status = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success {
		t.Fatalf("stats not success: %s", w.Body.String())
	}
	d := body.Data
	if d["delay_p50"] != float64(50) || d["delay_p95"] != float64(95) || d["delay_p99"] != float64(99) {
		t.Fatalf("stats percentiles = %v/%v/%v", d["delay_p50"], d["delay_p95"], d["delay_p99"])
	}
	if d["delay_stddev_ms"] != float64(10) || d["delay_jitter_ms"] != float64(8) {
		t.Fatalf("stats stddev/jitter = %v/%v", d["delay_stddev_ms"], d["delay_jitter_ms"])
	}
}

// TestServiceStatsDelayQuantilesNull 无样本充足的分钟桶时，分位数输出 null。
func TestServiceStatsDelayQuantilesNull(t *testing.T) {
	e := newReadonlyEnv(t)
	svc := model.Service{OwnerID: e.alice.ID, ServerID: e.aliceS.ID, Name: "n", Type: "http", Target: "http://x", Interval: 60, Enabled: true}
	if err := e.srv.DB.Create(&svc).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix() / 60 * 60
	seedHist(t, e, svc.ID,
		model.ServiceHistory{Ts: now, Total: 1, DelaySamples: 12, DelayP50: 7, DelayP95: 9, DelayP99: 12, DelayStdDevMs: 2, DelayJitterMs: 1},
	)

	w := e.doFull(t, http.MethodGet, "/api/v1/services/"+itoa(svc.ID)+"/stats", e.token(t, e.alice), "")
	if w.Code != http.StatusOK {
		t.Fatalf("stats status = %d", w.Code)
	}
	var body struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"delay_p50", "delay_p95", "delay_p99", "delay_stddev_ms", "delay_jitter_ms"} {
		if v, present := body.Data[k]; !present || v != nil {
			t.Fatalf("stats %s = %v, want null", k, v)
		}
	}
}

// TestListServicesDelayQuantiles 列表接口（serviceView）输出分位数；缺样本为 null。
func TestListServicesDelayQuantiles(t *testing.T) {
	e := newReadonlyEnv(t)
	okSvc := model.Service{OwnerID: e.alice.ID, ServerID: e.aliceS.ID, Name: "with-q", Type: "http", Target: "http://x", Interval: 60, Enabled: true}
	nullSvc := model.Service{OwnerID: e.alice.ID, ServerID: e.aliceS.ID, Name: "no-q", Type: "tcp", Target: "x:1", Interval: 60, Enabled: true}
	if err := e.srv.DB.Create(&okSvc).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.srv.DB.Create(&nullSvc).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix() / 60 * 60
	seedHist(t, e, okSvc.ID,
		model.ServiceHistory{Ts: now, Total: 1, DelaySamples: 40,
			DelayP50: 50, DelayP95: 95, DelayP99: 99, DelayStdDevMs: 10, DelayJitterMs: 8},
	)
	seedHist(t, e, nullSvc.ID,
		model.ServiceHistory{Ts: now, Total: 1, DelaySamples: 3, DelayP50: 3},
	)

	w := e.doFull(t, http.MethodGet, "/api/v1/services", e.token(t, e.alice), "")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Services []map[string]interface{} `json:"services"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	byName := map[string]map[string]interface{}{}
	for _, s := range body.Data.Services {
		byName[s["name"].(string)] = s
	}
	got := byName["with-q"]
	if got == nil {
		t.Fatalf("service with-q missing: %s", w.Body.String())
	}
	if got["delay_p50"] != float64(50) || got["delay_p95"] != float64(95) || got["delay_p99"] != float64(99) ||
		got["delay_stddev_ms"] != float64(10) || got["delay_jitter_ms"] != float64(8) {
		t.Fatalf("with-q quantiles = %v/%v/%v/%v/%v", got["delay_p50"], got["delay_p95"], got["delay_p99"], got["delay_stddev_ms"], got["delay_jitter_ms"])
	}
	gotNull := byName["no-q"]
	for _, k := range []string{"delay_p50", "delay_p95", "delay_p99", "delay_stddev_ms", "delay_jitter_ms"} {
		if v, present := gotNull[k]; !present || v != nil {
			t.Fatalf("no-q %s = %v, want null", k, v)
		}
	}
}

// seedHist 批量写入某服务的分钟桶历史。
func seedHist(t *testing.T, e *readonlyEnv, svcID int64, rows ...model.ServiceHistory) {
	t.Helper()
	for i := range rows {
		rows[i].ServiceID = svcID
		if err := e.srv.DB.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
}
