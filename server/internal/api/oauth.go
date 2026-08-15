package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

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
	c.SetCookie("oauth_state", state, 600, "/", "", false, true)

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
	c.SetCookie("oauth_state", "", -1, "/", "", false, true)
	// 前端通过 URL 参数接收 token（简化：重定向到前台并带 token 片段）
	c.Redirect(http.StatusFound, "/login?oauth_token="+token)
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
	ok(c, gin.H{"providers": cfgs})
}

func (s *Server) saveOAuthConfig(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	// 独立请求结构（模型字段带 json:"-" 脱敏，不能直接绑定）
	var req struct {
		Name          string `json:"name"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		AuthURL       string `json:"auth_url"`
		TokenURL      string `json:"token_url"`
		UserInfoURL   string `json:"user_info_url"`
		UsernameField string `json:"username_field"`
		AdminLogins   string `json:"admin_logins"`
		Enabled       bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
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
	var existing model.OAuthConfig
	if err := s.DB.Where("name = ?", req.Name).First(&existing).Error; err == nil {
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

var _ = sha256.Sum256
