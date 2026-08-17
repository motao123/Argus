// Package oauth 通用 OAuth2 登录（授权码模式），借鉴 komari/nezha OAuth 设计。
// 支持 GitHub / Gitee / 通用 OIDC provider，配置存 DB。
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// ProviderConfig 通用 provider 配置。
type ProviderConfig struct {
	Name         string // github / gitee / custom
	ClientID     string
	ClientSecret string
	AuthURL      string // 授权端点
	TokenURL     string // 换 token 端点
	UserInfoURL  string // 用户信息端点
	// 用户信息字段映射
	UsernameField string // 默认 login（github）/ name
	AdminLogins   string // 逗号分隔，命中即管理员
}

// Client OAuth 登录客户端。
type Client struct {
	mu      sync.Mutex
	configs map[string]*ProviderConfig
}

func NewClient() *Client {
	return &Client{configs: make(map[string]*ProviderConfig)}
}

// SetConfig 保存/更新 provider 配置。
func (c *Client) SetConfig(cfg *ProviderConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configs[cfg.Name] = cfg
}

// GetConfig 读取 provider 配置。
func (c *Client) GetConfig(name string) (*ProviderConfig, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg, ok := c.configs[name]
	return cfg, ok
}

// BuildAuthURL 生成授权跳转链接。
func (c *Client) BuildAuthURL(name, redirectURI, state string) (string, error) {
	cfg, ok := c.GetConfig(name)
	if !ok {
		return "", fmt.Errorf("provider %s not configured", name)
	}
	oauthCfg := oauthConfig(cfg, redirectURI)
	return oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOnline), nil
}

// Exchange 用授权码换 token 并拉取用户信息。
func (c *Client) Exchange(name, redirectURI, code string) (username string, err error) {
	cfg, ok := c.GetConfig(name)
	if !ok {
		return "", fmt.Errorf("provider %s not configured", name)
	}
	oauthCfg := oauthConfig(cfg, redirectURI)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	token, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfg.UserInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("userinfo: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var info map[string]any
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("userinfo parse: %w", err)
	}
	field := cfg.UsernameField
	if field == "" {
		field = "login"
	}
	username, _ = info[field].(string)
	if username == "" {
		// 兼容 name 字段
		username, _ = info["name"].(string)
	}
	if username == "" {
		return "", fmt.Errorf("userinfo missing username field %s", field)
	}
	return username, nil
}

// oauthConfig 构造 oauth2.Config。
func oauthConfig(cfg *ProviderConfig, redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.AuthURL,
			TokenURL: cfg.TokenURL,
		},
		RedirectURL: redirectURI,
		Scopes:      []string{"read:user"},
	}
}

// IsAdminLogin 判断登录名是否命中管理员名单。
func (c *ProviderConfig) IsAdminLogin(username string) bool {
	for _, u := range strings.Split(c.AdminLogins, ",") {
		if strings.TrimSpace(u) == username {
			return true
		}
	}
	return false
}

// BuildCustomURL 通用 OIDC provider 校验。
func BuildCustomURL(name, authURL, tokenURL, userInfoURL string) error {
	if name == "" || authURL == "" || tokenURL == "" || userInfoURL == "" {
		return fmt.Errorf("all fields required")
	}
	for _, u := range []string{authURL, tokenURL, userInfoURL} {
		parsed, err := url.Parse(u)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("invalid url: %s", u)
		}
	}
	return nil
}
