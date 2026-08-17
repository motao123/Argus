package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

// banForever 永久封禁哨兵（内存中以超大时间代替 nil 到期时间，保持 time.Time 判断简单）。
var banForever = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

// ---- 全局 WAF 限流（借鉴 nezha WAF；真实固定时间窗口）----

// wafState 每个 IP 的限流窗口状态；封禁判定一律以 wafBanManager（持久化封禁表）为准。
type wafState struct {
	windowStart time.Time
	count       int
}

type wafLimiter struct {
	mu       sync.Mutex
	entries  map[string]*wafState
	bans     *wafBanManager
	clock    func() time.Time
	limit    int           // 每窗口最大请求数
	window   time.Duration // 窗口长度
	blockFor time.Duration // 超限封禁时长
}

// wafMiddleware 全局中间件：每 IP 每窗口限 limit 请求，超限封禁 blockFor（写入持久化封禁表）。
func (s *Server) wafMiddleware() gin.HandlerFunc {
	return newWAF(300, time.Minute, 10*time.Minute, s.wafMgr()).middleware()
}

func newWAF(limit int, window, blockFor time.Duration, bans *wafBanManager) *wafLimiter {
	return &wafLimiter{
		entries:  make(map[string]*wafState),
		bans:     bans,
		clock:    time.Now,
		limit:    limit,
		window:   window,
		blockFor: blockFor,
	}
}

func (w *wafLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		w.mu.Lock()
		now := w.clock()
		st, ok := w.entries[ip]
		if !ok {
			st = &wafState{windowStart: now}
			w.entries[ip] = st
		}
		// 封禁判定以持久化封禁表为准（手动/速率/登录来源；到期自动解封，解封即时恢复）
		if w.bans != nil && w.bans.check(ip) {
			w.mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"success": false, "error": "ip temporarily blocked"})
			return
		}
		// 窗口重置
		if now.Sub(st.windowStart) >= w.window {
			st.windowStart = now
			st.count = 0
		}
		st.count++
		if st.count > w.limit {
			st.count = 0
			w.mu.Unlock()
			if w.bans != nil {
				w.bans.ban(ip, fmt.Sprintf("exceeded %d requests in %s", w.limit, w.window), model.BanSourceRate, w.blockFor)
			}
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"success": false, "error": "ip temporarily blocked"})
			return
		}
		// 周期性清理闲置条目，防止内存增长
		if len(w.entries) > 4096 {
			for k, e := range w.entries {
				if now.Sub(e.windowStart) > 24*w.window {
					delete(w.entries, k)
				}
			}
		}
		w.mu.Unlock()
		c.Next()
	}
}

// ---- IP 封禁管理器：内存缓存 + 持久化封禁表（WAF / 登录限流 / 手动封禁共用）----

// wafBanManager 提供 check/ban/unban 三个原子操作；
// 到期封禁在 check 时惰性清理（自动解封），管理员解封即时生效（内存与 DB 同步删除）。
type wafBanManager struct {
	mu    sync.Mutex
	bans  map[string]*model.WAFBan // ip → 最近一条记录（内存缓存）
	db    *gorm.DB
	clock func() time.Time
}

func newWAFManager(db *gorm.DB, clock func() time.Time) *wafBanManager {
	m := &wafBanManager{db: db, clock: clock, bans: make(map[string]*model.WAFBan)}
	if db == nil {
		return m
	}
	// 启动时载入未过期封禁（表缺失等错误忽略，降级为纯内存）
	var rows []model.WAFBan
	if err := db.Where("expire_at IS NULL OR expire_at > ?", clock()).Find(&rows).Error; err == nil {
		for i := range rows {
			m.bans[rows[i].IP] = &rows[i]
		}
	}
	return m
}

// wafMgr 惰性获取封禁管理器（兼容直接构造 Server 的测试与旧调用方）。
func (s *Server) wafMgr() *wafBanManager {
	if s.waf == nil {
		s.waf = newWAFManager(s.DB, time.Now)
	}
	return s.waf
}

// check 该 IP 是否处于封禁中；到期封禁自动解封并删除持久化记录。
func (m *wafBanManager) check(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.bans[ip]
	if !ok {
		return false
	}
	if b.ExpireAt != nil && !m.clock().Before(*b.ExpireAt) {
		// 到期自动解封：删除持久化记录并清除缓存（永久封禁不受影响）
		if m.db != nil {
			m.db.Delete(&model.WAFBan{}, b.ID)
		}
		delete(m.bans, ip)
		return false
	}
	return true
}

// ban 封禁 IP 并持久化。dur <= 0 表示永久封禁。
// 已存在记录则累计触发次数并顺延到期时间；手动封禁升级/覆盖自动封禁。
func (m *wafBanManager) ban(ip, reason, source string, dur time.Duration) *model.WAFBan {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	row := model.WAFBan{IP: ip, Reason: reason, Count: 1, Source: source, BannedAt: now}
	if dur > 0 {
		e := now.Add(dur)
		row.ExpireAt = &e
	}
	if m.db != nil {
		var exist model.WAFBan
		if err := m.db.Where("ip = ?", ip).First(&exist).Error; err == nil {
			row = exist
			row.Count++
			if source == model.BanSourceManual {
				row.Source = model.BanSourceManual
			}
			if reason != "" {
				row.Reason = reason
			}
			row.BannedAt = now
			if dur > 0 {
				e := now.Add(dur)
				row.ExpireAt = &e
			} else {
				row.ExpireAt = nil
			}
			if err := m.db.Save(&row).Error; err != nil {
				row = model.WAFBan{IP: ip, Reason: reason, Count: 1, Source: source, BannedAt: now}
				if dur > 0 {
					e := now.Add(dur)
					row.ExpireAt = &e
				}
				m.db.Create(&row)
			}
		} else {
			m.db.Create(&row)
		}
	}
	m.bans[ip] = &row
	return &row
}

// unban 解封并删除持久化记录；该 IP 未被封禁返回 false。
func (m *wafBanManager) unban(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bans[ip]; !ok {
		return false
	}
	if m.db != nil {
		m.db.Where("ip = ?", ip).Delete(&model.WAFBan{})
	}
	delete(m.bans, ip)
	return true
}

// ---- 在线访客/用户跟踪（最近请求 IP + 活跃会话 + WS/终端长连接）----

// 在线判定：最近 10 分钟内有 HTTP 请求，或持有 WebSocket/终端等长连接。
const onlineIdleFor = 10 * time.Minute

// OnlineView 在线列表单条记录。
type OnlineView struct {
	IP           string    `json:"ip"`
	Username     string    `json:"username"`       // 游客为空
	AuthMethod   string    `json:"auth_method"`    // jwt / pat / guest
	LastActiveAt time.Time `json:"last_active_at"` // 最近活动时间
	Connections  int       `json:"connections"`    // WS/终端等长连接数
}

type onlineEntry struct {
	ip         string
	username   string
	authMethod string
	lastActive time.Time
	conns      map[string]string // connID → 连接类型（ws/term）
}

type onlineTracker struct {
	mu      sync.Mutex
	entries map[string]*onlineEntry
	idleFor time.Duration
	clock   func() time.Time
	seq     int64
}

func newOnlineTracker(idleFor time.Duration, clock func() time.Time) *onlineTracker {
	return &onlineTracker{entries: make(map[string]*onlineEntry), idleFor: idleFor, clock: clock}
}

// onlineMgr 惰性获取在线跟踪器（兼容直接构造 Server 的测试）。
func (s *Server) onlineMgr() *onlineTracker {
	if s.online == nil {
		s.online = newOnlineTracker(onlineIdleFor, time.Now)
	}
	return s.online
}

func (t *onlineTracker) entry(ip string) *onlineEntry {
	e, ok := t.entries[ip]
	if !ok {
		e = &onlineEntry{ip: ip, conns: make(map[string]string)}
		t.entries[ip] = e
	}
	return e
}

// touch 记录一次 HTTP 活动（匿名访客 username 为空）。
func (t *onlineTracker) touch(ip, username, method string) {
	if ip == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entry(ip)
	e.lastActive = t.clock()
	if username != "" {
		e.username = username
		e.authMethod = method
	} else if e.username == "" {
		e.authMethod = method
	}
}

// connOpen 注册长连接（WebSocket/终端），返回连接 ID 供 connClose 使用。
func (t *onlineTracker) connOpen(ip, username, method, kind string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entry(ip)
	t.seq++
	id := strconv.FormatInt(t.seq, 36) + "-" + randomHex(4)
	e.conns[id] = kind
	e.lastActive = t.clock()
	if username != "" {
		e.username = username
		e.authMethod = method
	} else if e.username == "" {
		e.authMethod = method
	}
	return id
}

// connClose 注销长连接；无连接且超时后移除条目。
func (t *onlineTracker) connClose(ip, id string) {
	if ip == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[ip]
	if !ok {
		return
	}
	delete(e.conns, id)
	if len(e.conns) == 0 && t.clock().Sub(e.lastActive) > t.idleFor {
		delete(t.entries, ip)
	}
}

// snapshot 返回当前在线视图（按最近活动倒序；清理超时条目）。
func (t *onlineTracker) snapshot() []OnlineView {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock()
	out := make([]OnlineView, 0, len(t.entries))
	for ip, e := range t.entries {
		if len(e.conns) == 0 && now.Sub(e.lastActive) > t.idleFor {
			delete(t.entries, ip)
			continue
		}
		out = append(out, OnlineView{
			IP:           e.ip,
			Username:     e.username,
			AuthMethod:   e.authMethod,
			LastActiveAt: e.lastActive,
			Connections:  len(e.conns),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActiveAt.After(out[j].LastActiveAt) })
	return out
}

// ---- 身份辅助 ----

// authMethodOf 身份登录方式（jwt / pat / guest）。
func authMethodOf(p *principal) string {
	if p == nil {
		return "guest"
	}
	if p.IsPAT {
		return "pat"
	}
	return "jwt"
}

// usernameOf 身份的登录名（游客为空）。
func usernameOf(p *principal) string {
	if p == nil {
		return ""
	}
	return p.Username
}

// onlineTouch 记录一次 HTTP 活动（s.online 未初始化时静默跳过，兼容旧测试）。
func (s *Server) onlineTouch(c *gin.Context, username, method string) {
	if s.online != nil {
		s.online.touch(c.ClientIP(), username, method)
	}
}

// ---- 管理 API：在线列表 / WAF 封禁管理（admin）----

// listOnline 在线访客/用户列表：最近请求 IP + 活跃会话 + 长连接（10s 前端轮询）。
func (s *Server) listOnline(c *gin.Context) {
	t := s.onlineMgr()
	views := t.snapshot()
	// 活跃会话并入：idleFor 内创建且未过期的登录会话视为在线（登录方式 jwt），
	// 并为仅有游客活动的 IP 补全用户名。
	now := t.clock()
	var sessions []model.Session
	if s.DB != nil {
		s.DB.Where("expires_at > ? AND created_at > ?", now, now.Add(-t.idleFor)).Find(&sessions)
	}
	if len(sessions) > 0 {
		byIP := make(map[string]int, len(views))
		for i := range views {
			byIP[views[i].IP] = i
		}
		nameCache := make(map[int64]string)
		lookup := func(uid int64) string {
			if name, ok := nameCache[uid]; ok {
				return name
			}
			name := ""
			if s.DB != nil {
				var u model.User
				if err := s.DB.First(&u, uid).Error; err == nil {
					name = u.Username
				}
			}
			nameCache[uid] = name
			return name
		}
		for _, sess := range sessions {
			if i, ok := byIP[sess.IP]; ok {
				if views[i].Username == "" {
					if name := lookup(sess.UserID); name != "" {
						views[i].Username = name
						views[i].AuthMethod = "jwt"
					}
				}
				continue
			}
			v := OnlineView{IP: sess.IP, Username: lookup(sess.UserID), AuthMethod: "jwt", LastActiveAt: sess.CreatedAt}
			byIP[sess.IP] = len(views)
			views = append(views, v)
		}
		sort.Slice(views, func(i, j int) bool { return views[i].LastActiveAt.After(views[j].LastActiveAt) })
	}
	ok(c, gin.H{"online": views})
}

// listBans 封禁记录（分页；顺带清理过期记录）。
func (s *Server) listBans(c *gin.Context) {
	offset, limit := pagination(c)
	if s.DB != nil {
		s.DB.Where("expire_at IS NOT NULL AND expire_at <= ?", time.Now()).Delete(&model.WAFBan{})
	}
	var total int64
	if s.DB != nil {
		s.DB.Model(&model.WAFBan{}).Count(&total)
	}
	var bans []model.WAFBan
	if s.DB != nil {
		s.DB.Order("banned_at DESC").Offset(offset).Limit(limit).Find(&bans)
	}
	okPage(c, gin.H{"bans": bans}, total, offset, limit)
}

// banIP 手动封禁（admin）：{ip, reason?, hours}，hours=0 表示永久。
func (s *Server) banIP(c *gin.Context) {
	var req struct {
		IP     string `json:"ip"`
		Reason string `json:"reason"`
		Hours  int    `json:"hours"` // 0 = 永久
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	ip := strings.TrimSpace(req.IP)
	if ip == "" || len(ip) > 64 {
		fail(c, http.StatusBadRequest, "invalid ip")
		return
	}
	if req.Hours < 0 || req.Hours > 87600 { // 0..10 年
		fail(c, http.StatusBadRequest, "invalid hours")
		return
	}
	row := s.wafMgr().ban(ip, req.Reason, model.BanSourceManual, time.Duration(req.Hours)*time.Hour)
	s.auditLog(c, "waf.ban", fmt.Sprintf("ip=%s hours=%d reason=%q", ip, req.Hours, req.Reason))
	ok(c, gin.H{"ban": row})
}

// unbanIP 解封（admin）：即时恢复（内存与持久化记录同步删除，登录限流计数一并清除）。
func (s *Server) unbanIP(c *gin.Context) {
	ip := strings.TrimSpace(c.Param("ip"))
	if ip == "" {
		fail(c, http.StatusBadRequest, "invalid ip")
		return
	}
	if !s.wafMgr().unban(ip) {
		fail(c, http.StatusNotFound, "not banned")
		return
	}
	// 登录限流内存计数同步清除，解封后立即可登录
	loginGuards.Lock()
	delete(loginGuards.m, ip)
	loginGuards.Unlock()
	s.auditLog(c, "waf.unban", fmt.Sprintf("ip=%s", ip))
	ok(c, gin.H{"ok": true})
}
