// Package geoip GeoIP 归属地查询（借鉴 komari provider 化 + 缓存）。
// 支持配置在线 provider（ip-api.com / geojs.io），本地可配 mock 端点验证。
package geoip

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// Provider 查询 IP 归属地。
type Provider interface {
	// Lookup 返回国家码（如 CN/US），失败返回空串。
	Lookup(ip string) string
}

// Service 带缓存的 GeoIP 服务。
type Service struct {
	mu       sync.RWMutex
	provider Provider
	cache    map[string]cacheEntry
	ttl      time.Duration
}

type cacheEntry struct {
	country string
	expire  time.Time
}

// New 创建服务（默认空 provider，不查询）。
func New() *Service {
	return &Service{
		provider: &emptyProvider{},
		cache:    make(map[string]cacheEntry),
		ttl:      24 * time.Hour,
	}
}

// SetProvider 设置 provider（空字符串 = 禁用）。
func (s *Service) SetProvider(provider Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if provider == nil {
		s.provider = &emptyProvider{}
	} else {
		s.provider = provider
	}
}

// CountryCode 查询国家码（带缓存；miss 时同步查询）。
func (s *Service) CountryCode(ip string) string {
	if ip == "" {
		return ""
	}
	s.mu.RLock()
	e, ok := s.cache[ip]
	s.mu.RUnlock()
	if ok && time.Now().Before(e.expire) {
		return e.country
	}
	country := ""
	if p := s.provider; p != nil {
		country = p.Lookup(ip)
	}
	s.mu.Lock()
	s.cache[ip] = cacheEntry{country: country, expire: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return country
}

// ---- 空 provider ----

type emptyProvider struct{}

func (p *emptyProvider) Lookup(ip string) string { return "" }

// ---- HTTP provider（ip-api.com 风格 JSON 响应 {countryCode}）----

// HTTPProvider 通用 HTTP provider：GET {endpoint}/{ip}，响应 JSON 含 country_code 或 countryCode 字段。
type HTTPProvider struct {
	Endpoint string // 如 https://ipapi.co 或本地 mock
	Timeout  time.Duration
}

func (p *HTTPProvider) Lookup(ip string) string {
	client := &http.Client{Timeout: p.Timeout}
	if p.Timeout <= 0 {
		client.Timeout = 5 * time.Second
	}
	url := p.Endpoint + "/" + ip
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Argus/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var data struct {
		CountryCode string `json:"country_code"`
		Country     string `json:"countryCode"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	if data.CountryCode != "" {
		return data.CountryCode
	}
	return data.Country
}

var _ = log.Printf
