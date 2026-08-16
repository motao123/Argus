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

const testUpgradeSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func upgradeRouter(e *authzTestEnv) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.GET("/upgrade-jobs", requireAdmin(), e.srv.listUpgradeJobs)
	authed.POST("/upgrade-jobs", requireAdmin(), e.srv.createUpgradeJob)
	return r
}

func postUpgrade(t *testing.T, e *authzTestEnv, r *gin.Engine, body string, asAdmin bool) *httptest.ResponseRecorder {
	t.Helper()
	u := e.admin
	if !asAdmin {
		u = e.alice
	}
	req := httptest.NewRequest(http.MethodPost, "/upgrade-jobs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.token(t, u))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func waitUpgradeJob(t *testing.T, e *authzTestEnv, id int64, terminal ...string) model.UpgradeJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var job model.UpgradeJob
		if err := e.srv.DB.Preload("Results").First(&job, id).Error; err != nil {
			t.Fatal(err)
		}
		for _, s := range terminal {
			if job.Status == s {
				return job
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	var job model.UpgradeJob
	e.srv.DB.Preload("Results").First(&job, id)
	t.Fatalf("upgrade job %d did not reach terminal state %v: %+v", id, terminal, job)
	return job
}

func TestUpgradeJobValidation(t *testing.T) {
	e := newAuthzEnv(t)
	r := upgradeRouter(e)

	// 非 admin 拒绝
	if w := postUpgrade(t, e, r, `{"server_ids":[1],"url":"http://x/a","sha256":"`+testUpgradeSHA+`"}`, false); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin create: got %d want 403", w.Code)
	}
	// 缺 sha256
	if w := postUpgrade(t, e, r, `{"server_ids":[`+itoa(e.aliceS.ID)+`],"url":"http://x/a"}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("missing sha256: got %d want 400", w.Code)
	}
	// sha256 非 64 位
	if w := postUpgrade(t, e, r, `{"server_ids":[`+itoa(e.aliceS.ID)+`],"url":"http://x/a","sha256":"abcd"}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("short sha256: got %d want 400", w.Code)
	}
	// 非十六进制 sha256
	if w := postUpgrade(t, e, r, `{"server_ids":[`+itoa(e.aliceS.ID)+`],"url":"http://x/a","sha256":"`+strings.Repeat("z", 64)+`"}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("non-hex sha256: got %d want 400", w.Code)
	}
	// 非 http(s) URL
	if w := postUpgrade(t, e, r, `{"server_ids":[`+itoa(e.aliceS.ID)+`],"url":"ftp://x/a","sha256":"`+testUpgradeSHA+`"}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("bad scheme: got %d want 400", w.Code)
	}
	// 空 server_ids
	if w := postUpgrade(t, e, r, `{"server_ids":[],"url":"http://x/a","sha256":"`+testUpgradeSHA+`"}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("empty servers: got %d want 400", w.Code)
	}
	// 并发超上限
	if w := postUpgrade(t, e, r, `{"server_ids":[`+itoa(e.aliceS.ID)+`],"url":"http://x/a","sha256":"`+testUpgradeSHA+`","concurrency":99}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("huge concurrency: got %d want 400", w.Code)
	}
}

func TestUpgradeJobLifecycleOfflineTargets(t *testing.T) {
	e := newAuthzEnv(t)
	e.srv.upgradeResumeDelay = time.Nanosecond
	r := upgradeRouter(e)

	// 两个离线目标 + 一个不存在的服务器；并发 1
	body := `{"server_ids":[` + itoa(e.aliceS.ID) + `,` + itoa(e.bobS.ID) + `,999999],` +
		`"url":"http://artifacts.example/argus-agent","sha256":"` + testUpgradeSHA + `","version":"9.9.9","concurrency":1}`
	w := postUpgrade(t, e, r, body, true)
	if w.Code != http.StatusAccepted {
		t.Fatalf("create: got %d body %s", w.Code, w.Body.String())
	}
	var created model.UpgradeJob
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "pending" || created.TargetCount != 3 || created.Concurrency != 1 {
		t.Fatalf("created job: %+v", created)
	}

	job := waitUpgradeJob(t, e, created.ID, "completed")
	byServer := map[int64]string{}
	for _, res := range job.Results {
		byServer[res.ServerID] = res.Status
	}
	if byServer[e.aliceS.ID] != "offline" || byServer[e.bobS.ID] != "offline" {
		t.Fatalf("offline targets: %+v", byServer)
	}
	if byServer[999999] != "failure" {
		t.Fatalf("missing server should be failure: %+v", byServer)
	}
	for _, res := range job.Results {
		if res.FinishedAt == nil {
			t.Fatalf("result %d missing finished_at", res.ID)
		}
	}
	// 审计日志
	var count int64
	e.srv.DB.Model(&model.AuditLog{}).Where("action = ?", "server.upgrade.create").Count(&count)
	if count != 1 {
		t.Fatalf("create audit count = %d", count)
	}
	// 列表接口
	req := httptest.NewRequest(http.MethodGet, "/upgrade-jobs", nil)
	req.Header.Set("Authorization", "Bearer "+e.token(t, e.admin))
	wl := httptest.NewRecorder()
	r.ServeHTTP(wl, req)
	if wl.Code != http.StatusOK {
		t.Fatalf("list: got %d", wl.Code)
	}
	var listed struct {
		Data struct {
			Jobs []model.UpgradeJob `json:"jobs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(wl.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data.Jobs) != 1 || len(listed.Data.Jobs[0].Results) != 3 {
		t.Fatalf("listed jobs: %+v", listed.Data.Jobs)
	}
}

func TestUpgradeJobRestartRecovery(t *testing.T) {
	e := newAuthzEnv(t)
	e.srv.upgradeResumeDelay = time.Nanosecond

	// 模拟崩溃现场：job running，一台机器 running，另一台 pending
	now := time.Now()
	job := model.UpgradeJob{URL: "http://artifacts.example/argus-agent", SHA256: testUpgradeSHA, Version: "2.0.0", Status: "running", Concurrency: 2, TargetCount: 2, CreatedBy: e.admin.ID, StartedAt: &now}
	if err := e.srv.DB.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	running := model.UpgradeResult{JobID: job.ID, ServerID: e.aliceS.ID, ServerName: e.aliceS.Name, Status: "running", StartedAt: &now}
	pending := model.UpgradeResult{JobID: job.ID, ServerID: e.bobS.ID, ServerName: e.bobS.Name, Status: "pending"}
	if err := e.srv.DB.Create(&running).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.srv.DB.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}

	if err := e.srv.InitializeUpgradeJobs(); err != nil {
		t.Fatal(err)
	}

	// 恢复后的 job 应完成剩余目标（bobS 离线），aliceS 标记 interrupted
	recovered := waitUpgradeJob(t, e, job.ID, "completed")
	byServer := map[int64]*model.UpgradeResult{}
	for i := range recovered.Results {
		byServer[recovered.Results[i].ServerID] = &recovered.Results[i]
	}
	if byServer[e.aliceS.ID].Status != "interrupted" {
		t.Fatalf("aliceS should be interrupted: %+v", byServer[e.aliceS.ID])
	}
	if byServer[e.aliceS.ID].Error != "server restarted during upgrade" {
		t.Fatalf("interrupted error: %q", byServer[e.aliceS.ID].Error)
	}
	if byServer[e.bobS.ID].Status != "offline" {
		t.Fatalf("bobS should be re-attempted and offline: %+v", byServer[e.bobS.ID])
	}
}

func TestUpgradeJobPendingResumesOnStartup(t *testing.T) {
	e := newAuthzEnv(t)
	e.srv.upgradeResumeDelay = time.Nanosecond

	// 重启前已创建但从未开始的 pending job
	job := model.UpgradeJob{URL: "http://artifacts.example/argus-agent", SHA256: testUpgradeSHA, Version: "1.0.0", Status: "pending", Concurrency: 2, TargetCount: 1, CreatedBy: e.admin.ID}
	if err := e.srv.DB.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	res := model.UpgradeResult{JobID: job.ID, ServerID: e.aliceS.ID, ServerName: e.aliceS.Name, Status: "pending"}
	if err := e.srv.DB.Create(&res).Error; err != nil {
		t.Fatal(err)
	}

	if err := e.srv.InitializeUpgradeJobs(); err != nil {
		t.Fatal(err)
	}
	recovered := waitUpgradeJob(t, e, job.ID, "completed")
	if recovered.Results[0].Status != "offline" {
		t.Fatalf("pending target result: %+v", recovered.Results[0])
	}
}

func TestUpgradeJobConcurrencyPersistedAndBounded(t *testing.T) {
	e := newAuthzEnv(t)
	r := upgradeRouter(e)
	// 未指定 concurrency → 默认 2
	w := postUpgrade(t, e, r, `{"server_ids":[`+itoa(e.aliceS.ID)+`],"url":"http://x/a","sha256":"`+testUpgradeSHA+`"}`, true)
	var created model.UpgradeJob
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Concurrency != defaultUpgradeConcurrency {
		t.Fatalf("default concurrency = %d", created.Concurrency)
	}
	// 显式上限内合法
	w = postUpgrade(t, e, r, `{"server_ids":[`+itoa(e.bobS.ID)+`],"url":"http://x/a","sha256":"`+testUpgradeSHA+`","concurrency":`+itoa(maxUpgradeConcurrency)+`}`, true)
	if w.Code != http.StatusAccepted {
		t.Fatalf("max concurrency: got %d", w.Code)
	}
}
