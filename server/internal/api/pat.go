package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/mcp"
	"github.com/motao123/Argus/server/internal/model"
)

// ---- PAT scope 常量（argus:{resource}:{verb}）----

const (
	ScopeServerRead   = "argus:server:read"
	ScopeServerWrite  = "argus:server:write"
	ScopeServerDelete = "argus:server:delete"
	ScopeServerExec   = "argus:server:exec"

	ScopeServiceRead   = "argus:service:read"
	ScopeServiceWrite  = "argus:service:write"
	ScopeServiceDelete = "argus:service:delete"

	ScopeAlertRead   = "argus:alert:read"
	ScopeAlertWrite  = "argus:alert:write"
	ScopeAlertDelete = "argus:alert:delete"

	ScopeCronRead   = "argus:cron:read"
	ScopeCronWrite  = "argus:cron:write"
	ScopeCronDelete = "argus:cron:delete"

	ScopeNotificationRead   = "argus:notification:read"
	ScopeNotificationWrite  = "argus:notification:write"
	ScopeNotificationDelete = "argus:notification:delete"

	ScopeAdmin = "argus:admin:*"
	ScopeAll   = "argus:*"
)

// AllScopes 创建令牌时可选的全部 scope。
var AllScopes = []string{
	ScopeServerRead, ScopeServerWrite, ScopeServerDelete, ScopeServerExec,
	ScopeServiceRead, ScopeServiceWrite, ScopeServiceDelete,
	ScopeAlertRead, ScopeAlertWrite, ScopeAlertDelete,
	ScopeCronRead, ScopeCronWrite, ScopeCronDelete,
	ScopeNotificationRead, ScopeNotificationWrite, ScopeNotificationDelete,
}

// ---- 身份上下文 ----

// principal 当前请求身份（JWT 用户或 PAT）。
type principal struct {
	UserID   int64
	Username string
	IsAdmin  bool
	// IsReadonly 只读角色（readonly）：仅可查看公开视图与自有服务器状态，
	// 禁止一切写操作（执行/文件/任务/告警/配置等）。
	IsReadonly bool

	// PAT 信息（使用 PAT 时有效）
	TokenScopes  map[string]bool
	TokenServers map[int64]bool // 空 = 全部
	TokenID      int64
	IsPAT        bool
}

// principalFromContext 从 gin 上下文取身份。
func principalFromContext(c *gin.Context) *principal {
	v, _ := c.Get("principal")
	p, _ := v.(*principal)
	return p
}

// hasScope 检查是否具备 scope。
// JWT 用户（admin/user）不受 scope 限制（受角色与 owner 过滤约束）；
// scope 仅约束 PAT（借鉴 nezha：PAT 只会收窄用户原本的权限）。
func (p *principal) hasScope(scope string) bool {
	if p == nil {
		return false
	}
	if !p.IsPAT {
		return true
	}
	if p.TokenScopes[ScopeAll] || p.TokenScopes[ScopeAdmin] {
		return true
	}
	return p.TokenScopes[scope]
}

// canAccessServer PAT 白名单校验（JWT 用户不受限）。
func (p *principal) canAccessServer(serverID int64) bool {
	if p == nil || !p.IsPAT {
		return true
	}
	if len(p.TokenServers) == 0 {
		return true
	}
	return p.TokenServers[serverID]
}

// ---- 中间件 ----

// authMiddleware 支持 JWT 或 PAT（Authorization: Bearer <token>）。
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			fail(c, http.StatusUnauthorized, "missing token")
			c.Abort()
			return
		}
		raw := strings.TrimPrefix(auth, "Bearer ")
		p, err := s.identify(raw)
		if err != nil {
			fail(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}
		c.Set("principal", p)
		s.onlineTouch(c, p.Username, authMethodOf(p))
		c.Next()
	}
}

// optionalAuthMiddleware 可选认证：有 token 识别身份，无 token 视为游客（guest）。
// 借鉴 nezha 的 optionalAuth：读接口游客可访问，写接口由 requireAuth 拦截。
func (s *Server) optionalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			if p, err := s.identify(strings.TrimPrefix(auth, "Bearer ")); err == nil {
				c.Set("principal", p)
				s.onlineTouch(c, p.Username, authMethodOf(p))
				c.Next()
				return
			}
		}
		// 临时分享密钥：私有站点模式下允许携带 ?temp_key= 的访客访问公开端点
		if s.validTempShareKey(c.Query("temp_key")) {
			c.Set("principal", &principal{Username: "guest", IsReadonly: true})
			s.onlineTouch(c, "guest", "temp_key")
			c.Next()
			return
		}
		s.onlineTouch(c, "", "guest")
		c.Next()
	}
}

// validTempShareKey 校验临时分享密钥是否有效（存储 SHA-256；未过期或未设过期时间）。
func (s *Server) validTempShareKey(key string) bool {
	if key == "" {
		return false
	}
	stored := s.GetSetting(SettingTempShareKey, "")
	if stored == "" {
		return false
	}
	sum := sha256.Sum256([]byte(key))
	hashed := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(hashed), []byte(stored)) != 1 {
		return false
	}
	expires := s.GetSetting(SettingTempShareExpiresAt, "")
	if expires == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return false
	}
	return time.Now().Before(t)
}

// requireAuth 写操作必须登录（游客 401）。
// forceAuth 私有站点模式：开启时游客读接口 401（借鉴 komari 私有站点 + nezha force_auth）。
func (s *Server) forceAuth(c *gin.Context) {
	if s.GetSetting(SettingForceAuth, "0") == "1" {
		p := principalFromContext(c)
		if p == nil {
			fail(c, http.StatusUnauthorized, "login required (private site)")
			c.Abort()
			return
		}
	}
	c.Next()
}

func requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := principalFromContext(c)
		if p == nil {
			fail(c, http.StatusUnauthorized, "login required")
			c.Abort()
			return
		}
		c.Next()
	}
}

// authWS WebSocket 端点 token 校验（Query 或 Header）。
func (s *Server) authWS(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		token = strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	}
	p, err := s.identify(token)
	if err != nil {
		fail(c, http.StatusUnauthorized, "invalid token")
		c.Abort()
		return
	}
	if p.IsReadonly {
		fail(c, http.StatusForbidden, "readonly role cannot open a terminal", "auth.readonly_denied")
		c.Abort()
		return
	}
	c.Set("principal", p)
	s.onlineTouch(c, p.Username, authMethodOf(p))
	c.Next()
}

// requireScope 校验 scope，不足返回 403。
func requireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := principalFromContext(c)
		if p == nil || !p.hasScope(scope) {
			fail(c, http.StatusForbidden, "insufficient scope: "+scope)
			c.Abort()
			return
		}
		c.Next()
	}
}

// readonlyGate 只读角色权限门（挂载到 authed 组）：
// readonly 仅放行账号自助与状态查看白名单，其余一律 403。
// 白名单按 gin 路由模式（method + FullPath）匹配，写接口天然被拒。
// 注：/auth/me、2FA 自助与公开读接口（servers/services 等）注册在 authed 组外，天然放行。
func (s *Server) readonlyGate() gin.HandlerFunc {
	allowed := map[string]bool{
		"PUT /api/v1/users/:id": true, // 仅自助改密（角色变更仍要求 admin）
		"GET /api/v1/sessions":  true,
		"GET /api/v1/clipboard": true,
		"GET /api/v1/tokens":    true,
		// 多节点指标对比（只读状态查看，与单机 metrics 口径一致，逐 id owner/admin 校验）
		"GET /api/v1/metrics/compare": true,
		// 资源排行（只读状态查看，owner 仅见自有服务器的实时快照排行）
		"GET /api/v1/admin/top": true,
	}
	return func(c *gin.Context) {
		p := principalFromContext(c)
		if p == nil || !p.IsReadonly {
			c.Next()
			return
		}
		if allowed[c.Request.Method+" "+c.FullPath()] {
			c.Next()
			return
		}
		fail(c, http.StatusForbidden, "readonly role: read-only access only", "auth.readonly_denied")
		c.Abort()
	}
}

// ---- 全局爆破防护（借鉴 nezha WAF）----
// wafLimiter / wafBanManager / wafMiddleware 见 waf.go：限流状态在内存，
// 封禁记录持久化到封禁表（rate/login/manual 三来源共用，到期自动解封）。

// identify 识别 Bearer token：先试 JWT，再试 PAT。
func (s *Server) identify(raw string) (*principal, error) {
	if strings.HasPrefix(raw, "argus_") {
		return s.identifyPAT(raw)
	}
	cl, err := s.parseToken(raw)
	if err != nil {
		return nil, errInvalidToken
	}
	if s.isJTIRevoked(cl.RegisteredClaims.ID) {
		return nil, errInvalidToken
	}
	var user model.User
	if err := s.DB.First(&user, cl.UserID).Error; err != nil {
		return nil, errInvalidToken
	}
	return &principal{UserID: user.ID, Username: user.Username, IsAdmin: user.Role == model.RoleAdmin, IsReadonly: user.Role == model.RoleReadonly}, nil
}

// IdentifyPATToken 供 MCP 端点校验 PAT，返回完整授权上下文。
func (s *Server) IdentifyPATToken(raw string) (*mcp.Principal, bool) {
	p, err := s.identifyPAT(raw)
	if err != nil {
		return nil, false
	}
	mp := &mcp.Principal{
		UserID:     p.UserID,
		IsAdmin:    p.IsAdmin,
		IsReadonly: p.IsReadonly,
		Scopes:     p.TokenScopes,
		ServerIDs:  p.TokenServers,
	}
	return mp, true
}

func (s *Server) identifyPAT(raw string) (*principal, error) {
	hash := sha256.Sum256([]byte(raw))
	var tok model.APIToken
	if err := s.DB.Where("token_hash = ? AND revoked = ?", hex.EncodeToString(hash[:]), false).First(&tok).Error; err != nil {
		return nil, errInvalidToken
	}
	if tok.ExpiresAt != nil && time.Now().After(*tok.ExpiresAt) {
		return nil, errInvalidToken
	}
	var user model.User
	if err := s.DB.First(&user, tok.UserID).Error; err != nil {
		return nil, errInvalidToken
	}
	p := &principal{
		UserID:      tok.UserID,
		Username:    user.Username,
		IsAdmin:     user.Role == model.RoleAdmin,
		IsReadonly:  user.Role == model.RoleReadonly,
		IsPAT:       true,
		TokenID:     tok.ID,
		TokenScopes: make(map[string]bool),
	}
	for _, sc := range strings.Split(tok.Scopes, ",") {
		sc = strings.TrimSpace(sc)
		if sc != "" {
			p.TokenScopes[sc] = true
		}
	}
	p.TokenServers = make(map[int64]bool)
	for _, sid := range strings.Split(tok.ServerIDs, ",") {
		if v := parseIntQuery64(strings.TrimSpace(sid)); v > 0 {
			p.TokenServers[v] = true
		}
	}
	return p, nil
}

func parseIntQuery64(s string) int64 {
	var n int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int64(ch-'0')
	}
	return n
}

// ---- 令牌管理 API ----

// listTokens 当前用户的令牌列表（管理员可带 ?user_id= 查他人）。
func (s *Server) listTokens(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Model(&model.APIToken{})
	if !p.IsAdmin {
		q = q.Where("user_id = ?", p.UserID)
	} else if uid := c.Query("user_id"); uid != "" {
		q = q.Where("user_id = ?", parseIntQuery64(uid))
	}
	var tokens []model.APIToken
	if err := q.Order("id DESC").Find(&tokens).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"tokens": tokens})
}

// createToken 创建 PAT（明文仅返回一次）。
func (s *Server) createToken(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ServerIDs string   `json:"server_ids"`
		ExpiresIn *int     `json:"expires_in"` // 天数，0/空 = 永不过期
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if strings.TrimSpace(req.Name) == "" || len(req.Scopes) == 0 {
		fail(c, http.StatusBadRequest, "name and scopes required")
		return
	}
	// admin scope 仅管理员可签发
	scopes := make([]string, 0, len(req.Scopes))
	for _, sc := range req.Scopes {
		if sc == ScopeAdmin || sc == ScopeAll {
			if !p.IsAdmin {
				fail(c, http.StatusForbidden, "admin scope requires admin role")
				return
			}
		}
		scopes = append(scopes, strings.TrimSpace(sc))
	}

	// 生成明文令牌：argus_ + 32 字节随机
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	plain := "argus_" + hex.EncodeToString(buf)
	hash := sha256.Sum256([]byte(plain))

	var expiresAt *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 && *req.ExpiresIn <= 3650 {
		t := time.Now().Add(time.Duration(*req.ExpiresIn) * 24 * time.Hour)
		expiresAt = &t
	}

	tok := model.APIToken{
		UserID:    p.UserID,
		Name:      req.Name,
		TokenHash: hex.EncodeToString(hash[:]),
		Scopes:    strings.Join(scopes, ","),
		ServerIDs: req.ServerIDs,
		ExpiresAt: expiresAt,
	}
	if err := s.DB.Create(&tok).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"token": plain, "id": tok.ID, "name": tok.Name, "scopes": tok.Scopes})
	s.auditLog(c, "pat.create", tok.Name)
}

// revokeToken 吊销令牌。
func (s *Server) revokeToken(c *gin.Context) {
	p := principalFromContext(c)
	id := mustID(c)
	var tok model.APIToken
	if err := s.DB.First(&tok, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if tok.UserID != p.UserID && !p.IsAdmin {
		fail(c, http.StatusForbidden, "not your token")
		return
	}
	if err := s.DB.Model(&tok).Update("revoked", true).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
	s.auditLog(c, "pat.revoke", fmt.Sprintf("token_id=%d", id))
}
