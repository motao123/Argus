package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/oauth"
)

// ---- OAuth2 登录 API ----

// oauthRedirect 发起 OAuth2 授权（state 存 cookie 防 CSRF）。
func (s *Server) oauthRedirect(c *gin.Context) {
	provider := c.Param("provider")
	cfg, ok := s.oauthConfig(provider)
	if !ok {
		fail(c, http.StatusNotFound, "provider not configured")
		return
	}
	state := randomHex(16)
	c.SetCookie("oauth_state", state, 600, "/", "", c.Request.TLS != nil, true)

	redirectURI := s.oauthRedirectURI(c, provider)
	authURL, err := s.OAuth.BuildAuthURL(provider, redirectURI, state)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	_ = cfg
	c.Redirect(http.StatusFound, authURL)
}

// oauthCallback OAuth2 回调：换 token → 拉用户 → 自动注册/登录。
func (s *Server) oauthCallback(c *gin.Context) {
	provider := c.Param("provider")
	code := c.Query("code")
	state := c.Query("state")

	if cookieState, err := c.Cookie("oauth_state"); err != nil || cookieState == "" || cookieState != state {
		fail(c, http.StatusBadRequest, "state mismatch")
		return
	}
	if code == "" {
		fail(c, http.StatusBadRequest, "no code")
		return
	}

	username, err := s.OAuth.Exchange(provider, s.oauthRedirectURI(c, provider), code)
	if err != nil {
		fail(c, http.StatusBadGateway, "oauth exchange failed: "+err.Error())
		return
	}

	// 按 username 找用户，不存在则自动注册（借鉴 komari SSO 绑定登录）
	var user model.User
	if err := s.DB.Where("username = ?", username).First(&user).Error; err != nil {
		hash, _ := bcrypt.GenerateFromPassword([]byte(randomHex(24)), bcrypt.DefaultCost)
		role := model.RoleUser
		if cfg, ok := s.oauthConfig(provider); ok && cfg.IsAdminLogin(username) {
			role = model.RoleAdmin
		}
		user = model.User{
			Username:     username,
			PasswordHash: string(hash), // OAuth 用户无密码
			Role:         role,
			AgentSecret:  agentGenSecret(),
		}
		if err := s.DB.Create(&user).Error; err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}

	token, err := s.issueTokenWithSession(c, &user)
	if err != nil {
		fail(c, http.StatusInternalServerError, "issue token")
		return
	}
	c.SetCookie("oauth_state", "", -1, "/", "", c.Request.TLS != nil, true)
	// 安全交换：JWT 不出现在 URL。发放一次性短期 code，由前端交换。
	oneTimeCode := issueOAuthCode(token, 60*time.Second)
	c.Redirect(http.StatusFound, "/login?oauth_code="+oneTimeCode)
}

// ---- OAuth 一次性 code 交换（避免 JWT 进入浏览器历史/日志）----

var oauthCodeMu sync.Mutex
var oauthCodes = map[string]oauthCodeEntry{}

type oauthCodeEntry struct {
	token     string
	expiresAt time.Time
}

func issueOAuthCode(token string, ttl time.Duration) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	code := hex.EncodeToString(buf)
	oauthCodeMu.Lock()
	defer oauthCodeMu.Unlock()
	oauthCodes[code] = oauthCodeEntry{token: token, expiresAt: time.Now().Add(ttl)}
	// 清理过期 code，防止内存增长
	for k, v := range oauthCodes {
		if time.Now().After(v.expiresAt) {
			delete(oauthCodes, k)
		}
	}
	return code
}

// consumeOAuthCode 单次消费 OAuth code，返回 JWT。
func (s *Server) consumeOAuthCode(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Code == "" {
		fail(c, http.StatusBadRequest, "code required")
		return
	}
	oauthCodeMu.Lock()
	entry, found := oauthCodes[req.Code]
	delete(oauthCodes, req.Code) // 单次使用
	oauthCodeMu.Unlock()
	if !found || time.Now().After(entry.expiresAt) {
		fail(c, http.StatusUnauthorized, "invalid or expired code")
		return
	}
	ok(c, gin.H{"token": entry.token})
}

// listPublicOAuthProviders 登录页展示已启用的 OAuth provider（仅名称，不泄露凭据）。
func (s *Server) listPublicOAuthProviders(c *gin.Context) {
	var cfgs []model.OAuthConfig
	if err := s.DB.Where("enabled = ?", true).Order("id").Find(&cfgs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	names := make([]string, 0, len(cfgs))
	for _, cfg := range cfgs {
		names = append(names, cfg.Name)
	}
	ok(c, gin.H{"providers": names})
}

// oauthConfigs 管理 API：列出/保存/删除 provider 配置。
func (s *Server) listOAuthConfigs(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var cfgs []model.OAuthConfig
	if err := s.DB.Order("id").Find(&cfgs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	type oauthView struct {
		ID                     int64  `json:"id"`
		Name                   string `json:"name"`
		ClientID               string `json:"client_id"`
		AuthURL                string `json:"auth_url"`
		TokenURL               string `json:"token_url"`
		UserInfoURL            string `json:"user_info_url"`
		UsernameField          string `json:"username_field"`
		AdminLogins            string `json:"admin_logins"`
		Enabled                bool   `json:"enabled"`
		ClientSecretConfigured bool   `json:"client_secret_configured"`
	}
	out := make([]oauthView, 0, len(cfgs))
	for _, cfg := range cfgs {
		out = append(out, oauthView{ID: cfg.ID, Name: cfg.Name, ClientID: cfg.ClientID, AuthURL: cfg.AuthURL, TokenURL: cfg.TokenURL, UserInfoURL: cfg.UserInfoURL, UsernameField: cfg.UsernameField, AdminLogins: cfg.AdminLogins, Enabled: cfg.Enabled, ClientSecretConfigured: cfg.ClientSecret != ""})
	}
	ok(c, gin.H{"providers": out})
}

func (s *Server) saveOAuthConfig(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	// 独立请求结构（模型字段带 json:"-" 脱敏，不能直接绑定）
	var req struct {
		Name              string `json:"name"`
		ClientID          string `json:"client_id"`
		ClientSecret      string `json:"client_secret"`
		AuthURL           string `json:"auth_url"`
		TokenURL          string `json:"token_url"`
		UserInfoURL       string `json:"user_info_url"`
		UsernameField     string `json:"username_field"`
		AdminLogins       string `json:"admin_logins"`
		Enabled           bool   `json:"enabled"`
		ClearClientSecret bool   `json:"clear_client_secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var existing model.OAuthConfig
	existingErr := s.DB.Where("name = ?", req.Name).First(&existing).Error
	if existingErr == nil {
		if req.ClientSecret == "" && !req.ClearClientSecret {
			req.ClientSecret = existing.ClientSecret
		}
		if req.ClientID == "" {
			req.ClientID = existing.ClientID
		}
		if req.AuthURL == "" {
			req.AuthURL = existing.AuthURL
		}
		if req.TokenURL == "" {
			req.TokenURL = existing.TokenURL
		}
		if req.UserInfoURL == "" {
			req.UserInfoURL = existing.UserInfoURL
		}
		if req.UsernameField == "" {
			req.UsernameField = existing.UsernameField
		}
		if req.AdminLogins == "" {
			req.AdminLogins = existing.AdminLogins
		}
	}
	if req.ClearClientSecret {
		req.ClientSecret = ""
	}
	if err := oauth.BuildCustomURL(req.Name, req.AuthURL, req.TokenURL, req.UserInfoURL); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.UsernameField == "" {
		req.UsernameField = "login"
	}
	record := model.OAuthConfig{
		Name:          req.Name,
		ClientID:      req.ClientID,
		ClientSecret:  req.ClientSecret,
		AuthURL:       req.AuthURL,
		TokenURL:      req.TokenURL,
		UserInfoURL:   req.UserInfoURL,
		UsernameField: req.UsernameField,
		AdminLogins:   req.AdminLogins,
		Enabled:       req.Enabled,
	}
	// upsert
	if existingErr == nil {
		record.ID = existing.ID
		s.DB.Save(&record)
	} else {
		s.DB.Create(&record)
	}
	s.reloadOAuth()
	ok(c, gin.H{"ok": true})
}

func (s *Server) deleteOAuthConfig(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	name := c.Param("name")
	s.DB.Where("name = ?", name).Delete(&model.OAuthConfig{})
	s.reloadOAuth()
	ok(c, gin.H{"ok": true})
}

// oauthConfig 从 DB 读 provider 配置并同步到内存客户端。
func (s *Server) oauthConfig(name string) (*oauth.ProviderConfig, bool) {
	var dbCfg model.OAuthConfig
	if err := s.DB.Where("name = ? AND enabled = ?", name, true).First(&dbCfg).Error; err != nil {
		return nil, false
	}
	return &oauth.ProviderConfig{
		Name:          dbCfg.Name,
		ClientID:      dbCfg.ClientID,
		ClientSecret:  dbCfg.ClientSecret,
		AuthURL:       dbCfg.AuthURL,
		TokenURL:      dbCfg.TokenURL,
		UserInfoURL:   dbCfg.UserInfoURL,
		UsernameField: dbCfg.UsernameField,
		AdminLogins:   dbCfg.AdminLogins,
	}, true
}

// ReloadOAuthConfigs 全量重载 provider 配置（启动时调用）。
func (s *Server) ReloadOAuthConfigs() { s.reloadOAuth() }

// reloadOAuth 全量重载 provider 配置到内存客户端。
func (s *Server) reloadOAuth() {
	var cfgs []model.OAuthConfig
	s.DB.Where("enabled = ?", true).Find(&cfgs)
	for i := range cfgs {
		cfg := cfgs[i]
		s.OAuth.SetConfig(&oauth.ProviderConfig{
			Name:          cfg.Name,
			ClientID:      cfg.ClientID,
			ClientSecret:  cfg.ClientSecret,
			AuthURL:       cfg.AuthURL,
			TokenURL:      cfg.TokenURL,
			UserInfoURL:   cfg.UserInfoURL,
			UsernameField: cfg.UsernameField,
			AdminLogins:   cfg.AdminLogins,
		})
	}
}

// oauthRedirectURI 回调地址（同源 /api/v1/auth/oauth/:provider/callback）。
func (s *Server) oauthRedirectURI(c *gin.Context, provider string) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + "/api/v1/auth/oauth/" + provider + "/callback"
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func agentGenSecret() string { return agent.GenSecret() }
