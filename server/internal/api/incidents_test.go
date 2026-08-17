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

// incidentRouter 仅注册事故/维护窗口/SLA 路由的测试路由。
func incidentRouter(s *Server) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", s.authMiddleware())
	authed.POST("/incidents", s.createIncident)
	authed.PUT("/incidents/:id", s.updateIncident)
	authed.DELETE("/incidents/:id", s.deleteIncident)
	authed.POST("/incidents/:id/resolve", s.resolveIncident)
	authed.POST("/maintenance-windows", s.createMaintenanceWindow)
	authed.PUT("/maintenance-windows/:id", s.updateMaintenanceWindow)
	authed.DELETE("/maintenance-windows/:id", s.deleteMaintenanceWindow)
	pub := r.Group("", s.optionalAuthMiddleware())
	pub.GET("/incidents", s.listIncidents)
	pub.GET("/maintenance-windows", s.listMaintenanceWindows)
	pub.GET("/servers/:id/sla", s.serverSLA)
	return r
}

func doReq(t *testing.T, r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
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

func incidentBody(id int64, title string) string {
	return `{"title":"` + title + `","severity":"major","status":"ongoing","server_ids":"` + itoa(id) + `","start_at":"2026-08-10T10:00:00Z","notes":"n"}`
}

func TestIncidentCRUDAndIsolation(t *testing.T) {
	e := newAuthzEnv(t)
	r := incidentRouter(e.srv)
	aliceToken, bobToken, adminToken := e.token(t, e.alice), e.token(t, e.bob), e.token(t, e.admin)

	// alice 创建
	w := doReq(t, r, http.MethodPost, "/incidents", aliceToken, incidentBody(e.aliceS.ID, "alice-inc"))
	if w.Code != http.StatusOK {
		t.Fatalf("alice create: got %d: %s", w.Code, w.Body.String())
	}
	var inc model.Incident
	if err := e.srv.DB.Where("title = ?", "alice-inc").First(&inc).Error; err != nil {
		t.Fatal(err)
	}
	if inc.OwnerID != e.alice.ID {
		t.Fatalf("owner = %d want %d", inc.OwnerID, e.alice.ID)
	}

	// bob 不能改/删 alice 的事故
	w = doReq(t, r, http.MethodPut, "/incidents/"+itoa(inc.ID), bobToken, `{"title":"hacked"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob update: got %d want 403", w.Code)
	}
	w = doReq(t, r, http.MethodDelete, "/incidents/"+itoa(inc.ID), bobToken, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob delete: got %d want 403", w.Code)
	}
	w = doReq(t, r, http.MethodPost, "/incidents/"+itoa(inc.ID)+"/resolve", bobToken, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob resolve: got %d want 403", w.Code)
	}

	// 游客与登录用户都能读（状态页公开）
	w = doReq(t, r, http.MethodGet, "/incidents", "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "alice-inc") {
		t.Fatalf("guest list: got %d %s", w.Code, w.Body.String())
	}
	w = doReq(t, r, http.MethodGet, "/incidents", bobToken, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "alice-inc") {
		t.Fatalf("bob list: got %d %s", w.Code, w.Body.String())
	}

	// admin 可以改/删
	w = doReq(t, r, http.MethodPut, "/incidents/"+itoa(inc.ID), adminToken, `{"title":"admin-renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("admin update: got %d: %s", w.Code, w.Body.String())
	}
	w = doReq(t, r, http.MethodDelete, "/incidents/"+itoa(inc.ID), adminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("admin delete: got %d: %s", w.Code, w.Body.String())
	}

	// 未登录不能创建
	w = doReq(t, r, http.MethodPost, "/incidents", "", incidentBody(e.aliceS.ID, "x"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("guest create: got %d want 401", w.Code)
	}
}

func TestIncidentTargetValidation(t *testing.T) {
	e := newAuthzEnv(t)
	r := incidentRouter(e.srv)
	aliceToken, adminToken := e.token(t, e.alice), e.token(t, e.admin)

	// 普通用户不能指向他人服务器
	w := doReq(t, r, http.MethodPost, "/incidents", aliceToken, incidentBody(e.bobS.ID, "cross"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice target bob server: got %d want 403", w.Code)
	}
	// 普通用户不能留空目标
	w = doReq(t, r, http.MethodPost, "/incidents", aliceToken, `{"title":"x","server_ids":"","start_at":"2026-08-10T10:00:00Z"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("alice empty targets: got %d want 403", w.Code)
	}
	// admin 可以空目标（全部服务器）与任意服务器
	w = doReq(t, r, http.MethodPost, "/incidents", adminToken, `{"title":"global","server_ids":"","start_at":"2026-08-10T10:00:00Z"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("admin global incident: got %d: %s", w.Code, w.Body.String())
	}
	// 非法严重级别/时间
	w = doReq(t, r, http.MethodPost, "/incidents", adminToken, `{"title":"x","severity":"fatal","server_ids":"","start_at":"2026-08-10T10:00:00Z"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad severity: got %d want 400", w.Code)
	}
	w = doReq(t, r, http.MethodPost, "/incidents", adminToken, `{"title":"x","server_ids":"","start_at":"2026-08-10T10:00:00Z","end_at":"2026-08-10T09:00:00Z"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("end before start: got %d want 400", w.Code)
	}
}

func TestIncidentResolveAndAudit(t *testing.T) {
	e := newAuthzEnv(t)
	r := incidentRouter(e.srv)
	aliceToken := e.token(t, e.alice)

	w := doReq(t, r, http.MethodPost, "/incidents", aliceToken, incidentBody(e.aliceS.ID, "resolve-me"))
	if w.Code != http.StatusOK {
		t.Fatalf("create: got %d", w.Code)
	}
	var inc model.Incident
	if err := e.srv.DB.Where("title = ?", "resolve-me").First(&inc).Error; err != nil {
		t.Fatal(err)
	}
	if inc.Status != model.IncidentStatusOngoing || inc.EndAt != nil {
		t.Fatalf("initial: %s %v", inc.Status, inc.EndAt)
	}

	w = doReq(t, r, http.MethodPost, "/incidents/"+itoa(inc.ID)+"/resolve", aliceToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("resolve: got %d: %s", w.Code, w.Body.String())
	}
	if err := e.srv.DB.First(&inc, inc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if inc.Status != model.IncidentStatusResolved || inc.EndAt == nil {
		t.Fatalf("resolved: %s %v", inc.Status, inc.EndAt)
	}

	// 审计
	var logs []model.AuditLog
	e.srv.DB.Where("action IN ?", []string{"incident.create", "incident.update", "incident.resolve", "incident.delete"}).Find(&logs)
	actions := map[string]bool{}
	for _, l := range logs {
		actions[l.Action] = true
	}
	if !actions["incident.create"] || !actions["incident.resolve"] {
		t.Fatalf("audit actions missing: %v", actions)
	}
}

func TestMaintenanceWindowCRUD(t *testing.T) {
	e := newAuthzEnv(t)
	r := incidentRouter(e.srv)
	aliceToken, bobToken := e.token(t, e.alice), e.token(t, e.bob)

	body := `{"title":"weekly-mv","server_ids":"` + itoa(e.aliceS.ID) + `","start_at":"2026-08-15T22:00:00Z","end_at":"2026-08-16T02:00:00Z","recurring":true}`
	w := doReq(t, r, http.MethodPost, "/maintenance-windows", aliceToken, body)
	if w.Code != http.StatusOK {
		t.Fatalf("create: got %d: %s", w.Code, w.Body.String())
	}
	var win model.MaintenanceWindow
	if err := e.srv.DB.Where("title = ?", "weekly-mv").First(&win).Error; err != nil {
		t.Fatal(err)
	}
	if win.OwnerID != e.alice.ID || !win.Recurring {
		t.Fatalf("window: owner=%d recurring=%v", win.OwnerID, win.Recurring)
	}

	// bob 不能改
	w = doReq(t, r, http.MethodPut, "/maintenance-windows/"+itoa(win.ID), bobToken, `{"title":"hacked"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("bob update: got %d want 403", w.Code)
	}

	// 校验：end <= start、重复窗口 >= 7 天
	w = doReq(t, r, http.MethodPost, "/maintenance-windows", aliceToken, `{"title":"bad1","server_ids":"`+itoa(e.aliceS.ID)+`","start_at":"2026-08-10T10:00:00Z","end_at":"2026-08-10T10:00:00Z"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("end==start: got %d want 400", w.Code)
	}
	w = doReq(t, r, http.MethodPost, "/maintenance-windows", aliceToken, `{"title":"bad2","server_ids":"`+itoa(e.aliceS.ID)+`","start_at":"2026-08-10T10:00:00Z","end_at":"2026-08-17T10:00:00Z","recurring":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("recurring >= 7d: got %d want 400", w.Code)
	}

	// 公开读取
	w = doReq(t, r, http.MethodGet, "/maintenance-windows", "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "weekly-mv") {
		t.Fatalf("guest list: got %d %s", w.Code, w.Body.String())
	}

	// alice 删除
	w = doReq(t, r, http.MethodDelete, "/maintenance-windows/"+itoa(win.ID), aliceToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete: got %d: %s", w.Code, w.Body.String())
	}
	// 审计
	var n int64
	e.srv.DB.Model(&model.AuditLog{}).Where("action = ?", "maintenance-window.delete").Count(&n)
	if n != 1 {
		t.Fatalf("audit delete logs = %d, want 1", n)
	}
}

func TestServerSLA(t *testing.T) {
	e := newAuthzEnv(t)
	r := incidentRouter(e.srv)
	aliceToken := e.token(t, e.alice)

	// 服务器创建时间提前到 7 月，保证 8 月整月计入考核
	if err := e.srv.DB.Model(&e.aliceS).Update("created_at", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)).Error; err != nil {
		t.Fatal(err)
	}

	// 造数据：aliceS 在 8 月前 10 天全部在线；其中 8-05 维护 2 小时
	month := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	var rows []model.Metric
	for ts := month; ts.Before(end); ts = ts.Add(time.Minute) {
		rows = append(rows, model.Metric{ServerID: e.aliceS.ID, TS: ts.Unix(), Granularity: 60, CPU: 1})
	}
	if err := e.srv.DB.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.srv.DB.Create(&model.MaintenanceWindow{
		Title: "mv", ServerIDs: itoa(e.aliceS.ID),
		StartAt: time.Date(2026, 8, 5, 2, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatal(err)
	}

	w := doReq(t, r, http.MethodGet, "/servers/"+itoa(e.aliceS.ID)+"/sla?months=1", aliceToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("sla: got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			ServerID  int64   `json:"server_id"`
			SloTarget float64 `json:"slo_target"`
			Months    []struct {
				Month              string   `json:"month"`
				UptimeMinutes      int64    `json:"uptime_minutes"`
				EligibleMinutes    int64    `json:"eligible_minutes"`
				MaintenanceMinutes int64    `json:"maintenance_minutes"`
				Availability       *float64 `json:"availability"`
				SloMet             *bool    `json:"slo_met"`
			} `json:"months"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	m := resp.Data.Months[0]
	if m.Month != "2026-08" {
		t.Fatalf("month = %s", m.Month)
	}
	// 考核区间 = 本地时区 8-01 00:00 → now；数据覆盖 8-01~8-10 共 12960 分钟
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	total := int64(now.Sub(monthStart) / time.Minute)
	if m.EligibleMinutes != total-120 {
		t.Fatalf("eligible = %d want %d", m.EligibleMinutes, total-120)
	}
	if m.MaintenanceMinutes != 120 {
		t.Fatalf("maintenance = %d want 120", m.MaintenanceMinutes)
	}
	// 在线分钟 = 数据分钟 12960 - 维护期内的 120（维护期从分子扣除）
	if m.UptimeMinutes != 12840 {
		t.Fatalf("uptime = %d want 12840", m.UptimeMinutes)
	}
	wantAvail := round2(float64(12840) / float64(total-120) * 100)
	if m.Availability == nil || *m.Availability != wantAvail {
		t.Fatalf("availability = %v want %v", m.Availability, wantAvail)
	}
	// 可用率低于 99.9 → SLO 不达标（但判定字段存在）
	if resp.Data.SloTarget != 99.9 || m.SloMet == nil || *m.SloMet {
		t.Fatalf("slo: target=%v met=%v (want 99.9 / false)", resp.Data.SloTarget, m.SloMet)
	}

	// 游客对公开服务器也可读
	w = doReq(t, r, http.MethodGet, "/servers/"+itoa(e.aliceS.ID)+"/sla", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("guest sla: got %d", w.Code)
	}
	// 隐藏服务器游客不可见
	if err := e.srv.DB.Model(&e.aliceS).Update("hidden", true).Error; err != nil {
		t.Fatal(err)
	}
	w = doReq(t, r, http.MethodGet, "/servers/"+itoa(e.aliceS.ID)+"/sla", "", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("guest hidden sla: got %d want 404", w.Code)
	}
}

func TestIncidentsGuestForceAuth(t *testing.T) {
	e := newAuthzEnv(t)
	r := incidentRouter(e.srv)
	aliceToken := e.token(t, e.alice)
	if err := e.srv.DB.Create(&model.Setting{Key: SettingForceAuth, Value: "1"}).Error; err != nil {
		t.Fatal(err)
	}
	w := doReq(t, r, http.MethodPost, "/incidents", aliceToken, incidentBody(e.aliceS.ID, "priv"))
	if w.Code != http.StatusOK {
		t.Fatalf("create: got %d", w.Code)
	}
	// 强制登录模式下游客看不到
	w = doReq(t, r, http.MethodGet, "/incidents", "", "")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "priv") {
		t.Fatalf("guest with force_auth: got %d %s", w.Code, w.Body.String())
	}
	// 登录用户可见
	w = doReq(t, r, http.MethodGet, "/incidents", aliceToken, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "priv") {
		t.Fatalf("alice with force_auth: got %d %s", w.Code, w.Body.String())
	}
}
