package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/ddns"
	"github.com/motao123/Argus/server/internal/model"
)

// ---- DDNS API ----

// listDDNS 当前用户的 DDNS 配置列表。
func (s *Server) listDDNS(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Order("id")
	if p != nil && !p.IsAdmin {
		q = q.Where("owner_id = ?", p.UserID)
	}
	var profiles []model.DDNSProfile
	if err := q.Find(&profiles).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"profiles": profiles})
}

// createDDNS 创建 DDNS 配置。
func (s *Server) createDDNS(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		ServerID   int64  `json:"server_id"`
		Name       string `json:"name"`
		Provider   string `json:"provider"`
		AccessKey  string `json:"access_key"`
		Domains    string `json:"domains"`
		WebhookURL string `json:"webhook_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if req.ServerID <= 0 || req.Name == "" || strings.TrimSpace(req.Domains) == "" {
		fail(c, http.StatusBadRequest, "server_id/name/domains required")
		return
	}
	if _, ok := s.authorizeServer(c, req.ServerID, ScopeServerWrite); !ok {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	if req.Provider != "cloudflare" {
		req.Provider = "webhook"
	}
	profile := model.DDNSProfile{
		OwnerID:    p.UserID,
		ServerID:   req.ServerID,
		Name:       req.Name,
		Provider:   req.Provider,
		AccessKey:  req.AccessKey,
		Domains:    req.Domains,
		WebhookURL: req.WebhookURL,
		Enabled:    true,
	}
	if err := s.DB.Create(&profile).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, profile)
}

func (s *Server) updateDDNS(c *gin.Context) {
	id := mustID(c)
	var profile model.DDNSProfile
	if err := s.DB.First(&profile, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&profile.OwnerID, c) {
		fail(c, http.StatusForbidden, "not yours")
		return
	}
	var req struct {
		ServerID   *int64  `json:"server_id"`
		Name       *string `json:"name"`
		Provider   *string `json:"provider"`
		AccessKey  *string `json:"access_key"`
		Domains    *string `json:"domains"`
		WebhookURL *string `json:"webhook_url"`
		Enabled    *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	updates := map[string]any{}
	if req.ServerID != nil {
		if _, ok := s.authorizeServer(c, *req.ServerID, ScopeServerWrite); !ok {
			fail(c, http.StatusForbidden, "server access denied")
			return
		}
		updates["server_id"] = *req.ServerID
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Provider != nil {
		updates["provider"] = *req.Provider
	}
	if req.AccessKey != nil {
		updates["access_key"] = *req.AccessKey
	}
	if req.Domains != nil {
		updates["domains"] = *req.Domains
	}
	if req.WebhookURL != nil {
		updates["webhook_url"] = *req.WebhookURL
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if err := s.DB.Model(&profile).Updates(updates).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

func (s *Server) deleteDDNS(c *gin.Context) {
	id := mustID(c)
	var profile model.DDNSProfile
	if err := s.DB.First(&profile, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&profile.OwnerID, c) {
		fail(c, http.StatusForbidden, "not yours")
		return
	}
	s.DB.Delete(&model.DDNSProfile{}, id)
	ok(c, gin.H{"ok": true})
}

// testDDNS 立即用当前 IP 测试更新。
func (s *Server) testDDNS(c *gin.Context) {
	id := mustID(c)
	var profile model.DDNSProfile
	if err := s.DB.First(&profile, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&profile.OwnerID, c) {
		fail(c, http.StatusForbidden, "not yours")
		return
	}
	if _, ok := s.authorizeServer(c, profile.ServerID, ScopeServerWrite); !ok {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	ip := currentIP(c)
	results := make(map[string]string)
	provider := ddns.NewProvider(profile.Provider)
	cred := profile.AccessKey
	if profile.Provider == "webhook" {
		cred = profile.WebhookURL
	}
	for _, domain := range strings.Split(profile.Domains, ",") {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if err := provider.Update(domain, ip, cred); err != nil {
			results[domain] = "失败: " + err.Error()
		} else {
			results[domain] = "成功 → " + ip
		}
	}
	ok(c, gin.H{"ip": ip, "results": results})
}

// currentIP 获取当前请求 IP（仅信任配置的可信代理，见 main.go SetTrustedProxies）。
func currentIP(c *gin.Context) string {
	return c.ClientIP()
}

// HandleServerIPChange 服务器 IP 变化回调：更新匹配的 DDNS 配置（异步）。
func (s *Server) HandleServerIPChange(serverID int64, newIP string) {
	var profiles []model.DDNSProfile
	if err := s.DB.Where("server_id = ? AND enabled = ?", serverID, true).Find(&profiles).Error; err != nil || len(profiles) == 0 {
		return
	}
	for i := range profiles {
		profile := &profiles[i]
		if profile.LastIP == newIP {
			continue
		}
		provider := ddns.NewProvider(profile.Provider)
		cred := profile.AccessKey
		if profile.Provider == "webhook" {
			cred = profile.WebhookURL
		}
		allOK := true
		for _, domain := range strings.Split(profile.Domains, ",") {
			domain = strings.TrimSpace(domain)
			if domain == "" {
				continue
			}
			if err := provider.Update(domain, newIP, cred); err != nil {
				allOK = false
			}
		}
		if allOK {
			profile.LastIP = newIP
			s.DB.Model(profile).Updates(map[string]any{"last_ip": newIP, "last_updated": time.Now()})
		}
	}
}
