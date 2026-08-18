package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/config"
	"github.com/motao123/Argus/server/internal/model"
)

// newWAFAdminTestServer 构造含用户/会话/封禁/审计表的内存库测试服务。
func newWAFAdminTestServer(t *testing.T) (*Server, *model.User, *model.User) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.User{}, &model.Session{}, &model.WAFBan{}, &model.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test123"), bcrypt.DefaultCost)
	admin := &model.User{Username: "admin", PasswordHash: string(hash), Role: model.RoleAdmin, AgentSecret: agent.GenSecret()}
	alice := &model.User{Username: "alice", PasswordHash: string(hash), Role: model.RoleUser, AgentSecret: agent.GenSecret()}
	if err := gdb.Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(alice).Error; err != nil {
		t.Fatal(err)
	}
	return &Server{DB: gdb, Cfg: &config.Config{JWTSecret: "test-secret"}}, admin, alice
}

// serveReq 向指定路由器发起请求（可指定 RemoteAddr 模拟不同来源 IP）。
func serveReq(r http.Handler, method, path, token, body, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestManualBanBlocksAndUnbanRestores 手动封禁生效 + 解封即时恢复。
func TestManualBanBlocksAndUnbanRestores(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, admin, _ := newWAFAdminTestServer(t)
	r := New(s)
	tok, err := s.issueToken(admin)
	if err != nil {
		t.Fatal(err)
	}
	me := func() int {
		return serveReq(r, http.MethodGet, "/api/v1/auth/me", tok, "", "").Code
	}
	if code := me(); code != http.StatusOK {
		t.Fatalf("before ban: got %d want 200", code)
	}
	// 手动封禁（永久）
	s.wafMgr().ban("192.0.2.1", "abuse", model.BanSourceManual, 0)
	if code := me(); code != http.StatusTooManyRequests {
		t.Fatalf("banned: got %d want 429", code)
	}
	// 解封立即恢复
	if !s.wafMgr().unban("192.0.2.1") {
		t.Fatal("unban returned false")
	}
	if code := me(); code != http.StatusOK {
		t.Fatalf("after unban: got %d want 200", code)
	}
}

// TestRateBanPersistsAndAutoRecovers 速率超限封禁持久化 + 到期自动解封（记录清理）。
func TestRateBanPersistsAndAutoRecovers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.WAFBan{}); err != nil {
		t.Fatal(err)
	}
	clk := &fakeClock{now: time.Now()}
	clock := func() time.Time { return clk.now }
	mgr := newWAFManager(gdb, clock)
	lim := newWAF(3, time.Minute, 10*time.Minute, mgr)
	lim.clock = clock

	r := gin.New()
	r.Use(lim.middleware())
	r.GET("/t", func(c *gin.Context) { c.Status(http.StatusOK) })
	get := func() int { return serveReq(r, http.MethodGet, "/t", "", "", "").Code }

	for i := 0; i < 3; i++ {
		if code := get(); code != http.StatusOK {
			t.Fatalf("request %d: got %d want 200", i+1, code)
		}
	}
	if code := get(); code != http.StatusTooManyRequests {
		t.Fatalf("over limit: got %d want 429", code)
	}
	// 封禁记录已持久化（source=rate，Count=1）
	var ban model.WAFBan
	if err := gdb.Where("ip = ?", "192.0.2.1").First(&ban).Error; err != nil {
		t.Fatalf("rate ban row missing: %v", err)
	}
	if ban.Source != model.BanSourceRate {
		t.Fatalf("source: got %s want rate", ban.Source)
	}
	// 封禁期内仍 429
	if code := get(); code != http.StatusTooManyRequests {
		t.Fatalf("still blocked: got %d want 429", code)
	}
	// 封禁到期自动解封；持久化记录保留（Count 跨周期累计，支撑指数封禁；unban 才删除）
	clk.advance(11 * time.Minute)
	if code := get(); code != http.StatusOK {
		t.Fatalf("after expiry: got %d want 200", code)
	}
	var ban2 model.WAFBan
	if err := gdb.Where("ip = ?", "192.0.2.1").First(&ban2).Error; err != nil {
		t.Fatalf("expired ban row should be retained for count accumulation: %v", err)
	}
	if ban2.Count != 1 {
		t.Fatalf("count: got %d want 1", ban2.Count)
	}
	// 再次触发（同一 IP）：Count 递增且封禁时长指数增长（base=10m, count=2 → 40m）
	if code := get(); code != http.StatusOK {
		t.Fatalf("request after expiry: got %d want 200", code)
	}
	var got429 bool
	for i := 0; i < 3; i++ {
		code := get()
		if code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("expected second rate ban after refill")
	}
	var ban3 model.WAFBan
	if err := gdb.Where("ip = ?", "192.0.2.1").First(&ban3).Error; err != nil {
		t.Fatalf("second ban row missing: %v", err)
	}
	if ban3.Count != 2 {
		t.Fatalf("count: got %d want 2", ban3.Count)
	}
	if got, want := ban3.ExpireAt.Sub(ban3.BannedAt), 40*time.Minute; got != want {
		t.Fatalf("second ban duration: got %v want %v (escalated)", got, want)
	}
}

// TestLoginLockoutPersistsAndRecovers 登录限流持久化（source=login）+ 到期自动恢复。
// 通过清空内存限流计数模拟服务重启：封禁仅剩持久化记录仍生效。
func TestLoginLockoutPersistsAndRecovers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, admin, _ := newWAFAdminTestServer(t)
	clk := &fakeClock{now: time.Now()}
	s.waf = newWAFManager(s.DB, func() time.Time { return clk.now })
	r := New(s)

	const ip = "203.0.113.9"
	login := func(pass string) int {
		return serveReq(r, http.MethodPost, "/api/v1/auth/login", "",
			`{"username":"`+admin.Username+`","password":"`+pass+`"}`, ip+":1234").Code
	}
	// 5 次错误密码
	for i := 0; i < 5; i++ {
		if code := login("wrong"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d want 401", i+1, code)
		}
	}
	// 第 6 次（密码正确）被锁
	if code := login("test123"); code != http.StatusTooManyRequests {
		t.Fatalf("locked: got %d want 429", code)
	}
	// 封禁记录已持久化（source=login）
	var ban model.WAFBan
	if err := s.DB.Where("ip = ?", ip).First(&ban).Error; err != nil {
		t.Fatalf("login ban row missing: %v", err)
	}
	if ban.Source != model.BanSourceLogin {
		t.Fatalf("source: got %s want login", ban.Source)
	}
	// 模拟重启：清空内存限流计数，持久化封禁仍在 → 仍被锁
	loginGuards.Lock()
	delete(loginGuards.m, ip)
	loginGuards.Unlock()
	if code := login("test123"); code != http.StatusTooManyRequests {
		t.Fatalf("persisted lock: got %d want 429", code)
	}
	// 5 分钟后自动解封，可正常登录
	clk.advance(6 * time.Minute)
	if code := login("test123"); code != http.StatusOK {
		t.Fatalf("after expiry: got %d want 200", code)
	}
}

// TestOnlineListAndAdminIsolation 在线列表：最近请求 + 长连接计数 + admin 隔离。
func TestOnlineListAndAdminIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, admin, alice := newWAFAdminTestServer(t)
	r := New(s)
	adminTok, err := s.issueToken(admin)
	if err != nil {
		t.Fatal(err)
	}
	aliceTok, err := s.issueToken(alice)
	if err != nil {
		t.Fatal(err)
	}

	// admin 发一次请求 → 在线条目
	if code := serveReq(r, http.MethodGet, "/api/v1/auth/me", adminTok, "", "").Code; code != http.StatusOK {
		t.Fatalf("me: got %d want 200", code)
	}
	// admin 可查在线列表
	w := serveReq(r, http.MethodGet, "/api/v1/admin/online", adminTok, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("online list: got %d want 200", w.Code)
	}
	var resp struct {
		Data struct {
			Online []OnlineView `json:"online"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range resp.Data.Online {
		if v.IP == "192.0.2.1" && v.Username == "admin" && v.AuthMethod == "jwt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("online entry missing: %+v", resp.Data.Online)
	}

	// 非 admin 访问在线列表 → 403
	if code := serveReq(r, http.MethodGet, "/api/v1/admin/online", aliceTok, "", "203.0.113.99:1234").Code; code != http.StatusForbidden {
		t.Fatalf("non-admin: got %d want 403", code)
	}

	// 长连接：connOpen 计入连接数，connClose 后随 idle 超时移除
	connID := s.online.connOpen("203.0.113.50", "alice", "jwt", "ws")
	views := s.online.snapshot()
	got := -1
	for _, v := range views {
		if v.IP == "203.0.113.50" {
			got = v.Connections
		}
	}
	if got != 1 {
		t.Fatalf("connections: got %d want 1", got)
	}
	s.online.connClose("203.0.113.50", connID)
}

// TestOnlineIdleExpiry 在线条目 idle 超时清理；长连接期间保持在线。
func TestOnlineIdleExpiry(t *testing.T) {
	clk := &fakeClock{now: time.Now()}
	clock := func() time.Time { return clk.now }
	tr := newOnlineTracker(10*time.Minute, clock)
	tr.touch("1.2.3.4", "", "guest")
	if n := len(tr.snapshot()); n != 1 {
		t.Fatalf("after touch: got %d want 1", n)
	}
	clk.advance(11 * time.Minute)
	if n := len(tr.snapshot()); n != 0 {
		t.Fatalf("after idle: got %d want 0", n)
	}

	clk2 := &fakeClock{now: time.Now()}
	tr2 := newOnlineTracker(10*time.Minute, func() time.Time { return clk2.now })
	conn := tr2.connOpen("5.6.7.8", "bob", "jwt", "ws")
	clk2.advance(30 * time.Minute)
	if n := len(tr2.snapshot()); n != 1 {
		t.Fatalf("conn should stay online: got %d want 1", n)
	}
	tr2.connClose("5.6.7.8", conn)
	clk2.advance(11 * time.Minute)
	if n := len(tr2.snapshot()); n != 0 {
		t.Fatalf("after close+idle: got %d want 0", n)
	}
}

// TestAdminBanAPIAndAudit 封禁管理 API：封禁生效/列表分页/解封恢复/404/审计/admin 隔离。
func TestAdminBanAPIAndAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, admin, alice := newWAFAdminTestServer(t)
	r := New(s)
	adminTok, err := s.issueToken(admin)
	if err != nil {
		t.Fatal(err)
	}
	aliceTok, err := s.issueToken(alice)
	if err != nil {
		t.Fatal(err)
	}

	// 非 admin 禁止封禁
	if code := serveReq(r, http.MethodPost, "/api/v1/admin/waf/ban", aliceTok,
		`{"ip":"198.51.100.7","reason":"x","hours":1}`, "").Code; code != http.StatusForbidden {
		t.Fatalf("non-admin ban: got %d want 403", code)
	}
	// admin 手动封禁 24h
	w := serveReq(r, http.MethodPost, "/api/v1/admin/waf/ban", adminTok,
		`{"ip":"198.51.100.7","reason":"abuse","hours":24}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("ban: got %d want 200", w.Code)
	}
	var banResp struct {
		Data struct {
			Ban model.WAFBan `json:"ban"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &banResp); err != nil {
		t.Fatal(err)
	}
	if banResp.Data.Ban.Source != model.BanSourceManual || banResp.Data.Ban.ExpireAt == nil {
		t.Fatalf("ban row unexpected: %+v", banResp.Data.Ban)
	}
	// 封禁生效：该 IP 访问被 429
	if code := serveReq(r, http.MethodGet, "/api/v1/auth/me", adminTok, "", "198.51.100.7:1234").Code; code != http.StatusTooManyRequests {
		t.Fatalf("banned request: got %d want 429", code)
	}
	// 封禁记录列表（分页）
	w = serveReq(r, http.MethodGet, "/api/v1/admin/waf/bans?offset=0&limit=10", adminTok, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("bans list: got %d want 200", w.Code)
	}
	var listResp struct {
		Data struct {
			Bans []model.WAFBan `json:"bans"`
		} `json:"data"`
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if listResp.Pagination.Total < 1 {
		t.Fatalf("bans total: got %d want >= 1", listResp.Pagination.Total)
	}
	found := false
	for _, b := range listResp.Data.Bans {
		if b.IP == "198.51.100.7" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bans list missing entry: %+v", listResp.Data.Bans)
	}
	// 审计日志已记录 waf.ban
	var auditCount int64
	s.DB.Model(&model.AuditLog{}).Where("action = ?", "waf.ban").Count(&auditCount)
	if auditCount < 1 {
		t.Fatal("audit log for waf.ban missing")
	}
	// 解封 → 立即恢复 200
	if code := serveReq(r, http.MethodDelete, "/api/v1/admin/waf/ban/198.51.100.7", adminTok, "", "").Code; code != http.StatusOK {
		t.Fatalf("unban: got %d want 200", code)
	}
	if code := serveReq(r, http.MethodGet, "/api/v1/auth/me", adminTok, "", "198.51.100.7:1234").Code; code != http.StatusOK {
		t.Fatalf("after unban: got %d want 200", code)
	}
	// 重复解封 → 404
	if code := serveReq(r, http.MethodDelete, "/api/v1/admin/waf/ban/198.51.100.7", adminTok, "", "").Code; code != http.StatusNotFound {
		t.Fatalf("unban again: got %d want 404", code)
	}
	// 审计日志已记录 waf.unban
	s.DB.Model(&model.AuditLog{}).Where("action = ?", "waf.unban").Count(&auditCount)
	if auditCount < 1 {
		t.Fatal("audit log for waf.unban missing")
	}
}

func TestBanDurationEscalation(t *testing.T) {
	cases := []struct {
		base  time.Duration
		count int
		want  time.Duration
	}{
		{5 * time.Minute, 1, 5 * time.Minute},   // 首次封禁：基准时长
		{5 * time.Minute, 2, 20 * time.Minute},  // 第 2 次：×4
		{5 * time.Minute, 3, 45 * time.Minute},  // 第 3 次：×9
		{5 * time.Minute, 5, 125 * time.Minute}, // 第 5 次：×25
		{5 * time.Minute, 30, 72 * time.Hour},   // 上限截断
		{5 * time.Minute, 99, 72 * time.Hour},   // 超上限截断
	}
	for _, c := range cases {
		if got := banDuration(c.base, c.count); got != c.want {
			t.Fatalf("banDuration(%v, %d) = %v, want %v", c.base, c.count, got, c.want)
		}
	}
}

// TestRepeatedAutoBanEscalatesDuration 验证同一 IP 重复自动封禁时 ExpireAt 指数增长。
func TestRepeatedAutoBanEscalatesDuration(t *testing.T) {
	s, _, _ := newWAFAdminTestServer(t)
	ip := "203.0.113.77"
	// 第 1 次封禁：基准 5 分钟
	m := newWAFManager(s.DB, time.Now)
	row := m.ban(ip, "5 failed login attempts", model.BanSourceLogin, 5*time.Minute)
	if got := row.ExpireAt.Sub(row.BannedAt); got != 5*time.Minute {
		t.Fatalf("1st ban duration = %v, want 5m", got)
	}
	// 模拟到期后再次触发（同一 IP，Count 已递增）
	row = m.ban(ip, "5 failed login attempts", model.BanSourceLogin, 5*time.Minute)
	if row.Count != 2 {
		t.Fatalf("count = %d, want 2", row.Count)
	}
	if got := row.ExpireAt.Sub(row.BannedAt); got != 20*time.Minute {
		t.Fatalf("2nd ban duration = %v, want 20m (escalated)", got)
	}
	// 手动封禁不指数化：始终用指定时长
	row = m.ban(ip, "manual", model.BanSourceManual, 1*time.Hour)
	if got := row.ExpireAt.Sub(row.BannedAt); got != 1*time.Hour {
		t.Fatalf("manual ban duration = %v, want 1h", got)
	}
}
