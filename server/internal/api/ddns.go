package api

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/ddns"
	"github.com/motao123/Argus/server/internal/model"
)

const redactedSecret = "********"

type ddnsInput struct {
	ServerID       *int64  `json:"server_id"`
	Name           *string `json:"name"`
	Provider       *string `json:"provider"`
	RecordType     *string `json:"record_type"`
	AccessKey      *string `json:"access_key"`
	Domains        *string `json:"domains"`
	WebhookURL     *string `json:"webhook_url"`
	WebhookMethod  *string `json:"webhook_method"`
	WebhookHeaders *string `json:"webhook_headers"`
	WebhookBody    *string `json:"webhook_body"`
	Enabled        *bool   `json:"enabled"`
}

func currentIP(c *gin.Context) string { return c.ClientIP() }

func (s *Server) ddnsClient() *ddns.Client {
	if s.DDNS != nil {
		return s.DDNS
	}
	return ddns.NewClient(nil)
}

func redactDDNS(p *model.DDNSProfile) {
	p.AccessKey = ""
	if p.WebhookURL != "" {
		p.WebhookURL = redactedSecret
	}
	if p.WebhookHeaders != "" && p.WebhookHeaders != "{}" {
		p.WebhookHeaders = redactedSecret
	}
	if p.WebhookBody != "" {
		p.WebhookBody = redactedSecret
	}
}

func (s *Server) listDDNS(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Order("id").Preload("Records")
	if p != nil && !p.IsAdmin {
		q = q.Where("owner_id = ?", p.UserID)
	}
	var profiles []model.DDNSProfile
	if err := q.Find(&profiles).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	for i := range profiles {
		redactDDNS(&profiles[i])
	}
	ok(c, gin.H{"profiles": profiles})
}

func validateDDNSInput(req ddnsInput, creating bool) (int64, string, string, string, string, error) {
	var serverID int64
	if req.ServerID != nil {
		serverID = *req.ServerID
	}
	name := ""
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	provider := "webhook"
	if req.Provider != nil {
		provider = strings.ToLower(*req.Provider)
	}
	recordType := "A"
	if req.RecordType != nil {
		recordType = *req.RecordType
	}
	domainsRaw := ""
	if req.Domains != nil {
		domainsRaw = *req.Domains
	}
	if creating && (serverID <= 0 || name == "" || domainsRaw == "") {
		return 0, "", "", "", "", fmt.Errorf("server_id/name/domains required")
	}
	if provider != "cloudflare" && provider != "webhook" {
		return 0, "", "", "", "", fmt.Errorf("invalid provider")
	}
	if _, err := ddns.RecordTypes(recordType); err != nil {
		return 0, "", "", "", "", err
	}
	if domainsRaw != "" {
		domains, err := ddns.Domains(domainsRaw)
		if err != nil {
			return 0, "", "", "", "", err
		}
		domainsRaw = strings.Join(domains, ",")
	}
	return serverID, name, provider, recordType, domainsRaw, nil
}

func validateProviderConfig(provider, accessKey string) error {
	if provider == "cloudflare" && strings.TrimSpace(accessKey) == "" {
		return fmt.Errorf("cloudflare API token required")
	}
	return nil
}

func (s *Server) createDDNS(c *gin.Context) {
	p := principalFromContext(c)
	var req ddnsInput
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "bad request")
		return
	}
	serverID, name, provider, recordType, domains, err := validateDDNSInput(req, true)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	if _, allowed := s.authorizeServer(c, serverID, ScopeServerWrite); !allowed {
		fail(c, 403, "server access denied")
		return
	}
	profile := model.DDNSProfile{OwnerID: p.UserID, ServerID: serverID, Name: name, Provider: provider, RecordType: recordType, Domains: domains, Enabled: true, WebhookMethod: "GET", WebhookHeaders: "{}"}
	if req.AccessKey != nil {
		profile.AccessKey = *req.AccessKey
	}
	if req.WebhookURL != nil {
		profile.WebhookURL = *req.WebhookURL
	}
	if req.WebhookMethod != nil {
		profile.WebhookMethod = strings.ToUpper(*req.WebhookMethod)
	}
	if req.WebhookHeaders != nil {
		profile.WebhookHeaders = *req.WebhookHeaders
	}
	if req.WebhookBody != nil {
		profile.WebhookBody = *req.WebhookBody
	}
	if req.Enabled != nil {
		profile.Enabled = *req.Enabled
	}
	if err := validateProviderConfig(profile.Provider, profile.AccessKey); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if err := s.DB.Create(&profile).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	if err := s.syncDDNSRecords(&profile); err != nil {
		s.DB.Delete(&profile)
		fail(c, 400, err.Error())
		return
	}
	s.auditLog(c, "ddns.create", fmt.Sprintf("profile_id=%d name=%s", profile.ID, profile.Name))
	redactDDNS(&profile)
	ok(c, profile)
}

func (s *Server) updateDDNS(c *gin.Context) {
	id := mustID(c)
	var profile model.DDNSProfile
	if err := s.DB.First(&profile, id).Error; err != nil {
		fail(c, 404, "not found")
		return
	}
	if !s.canManage(&profile.OwnerID, c) {
		fail(c, 403, "not yours")
		return
	}
	var req ddnsInput
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "bad request")
		return
	}
	if req.ServerID != nil {
		if _, allowed := s.authorizeServer(c, *req.ServerID, ScopeServerWrite); !allowed {
			fail(c, 403, "server access denied")
			return
		}
	}
	_, _, _, _, normalizedDomains, err := validateDDNSInput(req, false)
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	updates := map[string]any{}
	if req.ServerID != nil {
		updates["server_id"] = *req.ServerID
	}
	if req.Name != nil {
		updates["name"] = strings.TrimSpace(*req.Name)
	}
	if req.Provider != nil {
		updates["provider"] = strings.ToLower(*req.Provider)
	}
	if req.RecordType != nil {
		updates["record_type"] = *req.RecordType
	}
	if req.Domains != nil {
		updates["domains"] = normalizedDomains
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.AccessKey != nil && *req.AccessKey != "" && *req.AccessKey != redactedSecret {
		updates["access_key"] = *req.AccessKey
	}
	if req.WebhookURL != nil && *req.WebhookURL != "" && *req.WebhookURL != redactedSecret {
		updates["webhook_url"] = *req.WebhookURL
	}
	if req.WebhookHeaders != nil && *req.WebhookHeaders != redactedSecret {
		updates["webhook_headers"] = *req.WebhookHeaders
	}
	if req.WebhookBody != nil && *req.WebhookBody != redactedSecret {
		updates["webhook_body"] = *req.WebhookBody
	}
	if req.WebhookMethod != nil {
		updates["webhook_method"] = strings.ToUpper(*req.WebhookMethod)
	}
	finalProvider := profile.Provider
	if v, ok := updates["provider"].(string); ok {
		finalProvider = v
	}
	accessKey := profile.AccessKey
	if v, ok := updates["access_key"].(string); ok {
		accessKey = v
	}
	if err := validateProviderConfig(finalProvider, accessKey); err != nil {
		fail(c, 400, err.Error())
		return
	}
	if err := s.DB.Model(&profile).Updates(updates).First(&profile).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	if err := s.syncDDNSRecords(&profile); err != nil {
		fail(c, 400, err.Error())
		return
	}
	s.auditLog(c, "ddns.update", fmt.Sprintf("profile_id=%d name=%s", profile.ID, profile.Name))
	ok(c, gin.H{"ok": true})
}

func (s *Server) deleteDDNS(c *gin.Context) {
	id := mustID(c)
	var profile model.DDNSProfile
	if err := s.DB.First(&profile, id).Error; err != nil {
		fail(c, 404, "not found")
		return
	}
	if !s.canManage(&profile.OwnerID, c) {
		fail(c, 403, "not yours")
		return
	}
	_ = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("profile_id = ?", id).Delete(&model.DDNSRecordState{}).Error; err != nil {
			return err
		}
		return tx.Delete(&profile).Error
	})
	s.auditLog(c, "ddns.delete", fmt.Sprintf("profile_id=%d name=%s", profile.ID, profile.Name))
	ok(c, gin.H{"ok": true})
}

func (s *Server) syncDDNSRecords(profile *model.DDNSProfile) error {
	domains, err := ddns.Domains(profile.Domains)
	if err != nil {
		return err
	}
	types, err := ddns.RecordTypes(profile.RecordType)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, domain := range domains {
		for _, rt := range types {
			wanted[domain+"\x00"+rt] = true
			state := model.DDNSRecordState{ProfileID: profile.ID, OwnerID: profile.OwnerID, Domain: domain, RecordType: rt, Status: "pending"}
			s.DB.Where("profile_id=? AND domain=? AND record_type=?", profile.ID, domain, rt).FirstOrCreate(&state)
		}
	}
	var existing []model.DDNSRecordState
	s.DB.Where("profile_id=?", profile.ID).Find(&existing)
	for i := range existing {
		if !wanted[existing[i].Domain+"\x00"+existing[i].RecordType] {
			s.DB.Delete(&existing[i])
		}
	}
	// 配置变更或 Agent IP 变化后，重置 stopped（401）记录为 pending 以便自愈重试。
	s.DB.Model(&model.DDNSRecordState{}).
		Where("profile_id = ? AND status = ?", profile.ID, "stopped").
		Updates(map[string]any{"status": "pending", "retry_count": 0, "next_retry": nil})
	return nil
}

func agentIPs(stHost protocol.HostInfo) map[string]string {
	v4 := stHost.IPv4
	if v4 == "" {
		v4 = stHost.IP
	}
	return map[string]string{"A": v4, "AAAA": stHost.IPv6}
}

func (s *Server) testDDNS(c *gin.Context) {
	id := mustID(c)
	var profile model.DDNSProfile
	if err := s.DB.First(&profile, id).Error; err != nil {
		fail(c, 404, "not found")
		return
	}
	if !s.canManage(&profile.OwnerID, c) {
		fail(c, 403, "not yours")
		return
	}
	if _, allowed := s.authorizeServer(c, profile.ServerID, ScopeServerWrite); !allowed {
		fail(c, 403, "server access denied")
		return
	}
	st := s.Store.Get(profile.ServerID)
	if st == nil {
		fail(c, 409, "agent has not reported host IP")
		return
	}
	_ = s.syncDDNSRecords(&profile)
	s.runDDNSProfile(&profile, agentIPs(st.Host), true)
	var records []model.DDNSRecordState
	s.DB.Where("profile_id=?", profile.ID).Order("domain, record_type").Find(&records)
	s.auditLog(c, "ddns.test", fmt.Sprintf("profile_id=%d", profile.ID))
	ok(c, gin.H{"ipv4": agentIPs(st.Host)["A"], "ipv6": agentIPs(st.Host)["AAAA"], "records": records})
}

func retryDelay(count int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < count && d < time.Hour; i++ {
		d *= 2
	}
	if d > time.Hour {
		d = time.Hour
	}
	return d
}

func (s *Server) runDDNSProfile(profile *model.DDNSProfile, ips map[string]string, force bool) {
	var states []model.DDNSRecordState
	s.DB.Where("profile_id=?", profile.ID).Find(&states)
	now := time.Now()
	for i := range states {
		state := &states[i]
		ip := ips[state.RecordType]
		if ip == "" {
			continue
		}
		// 401/403 后记录进入 stopped：停止自动重试，直到用户显式测试或配置/IP 变化重置。
		if !force && state.Status == "stopped" {
			continue
		}
		if !force && state.LastIP == ip && state.Status == "success" {
			continue
		}
		if !force && state.NextRetry != nil && state.NextRetry.After(now) {
			continue
		}
		attempt := time.Now()
		err := s.ddnsClient().Provider(profile.Provider).Update(ddns.Request{Domain: state.Domain, RecordType: state.RecordType, IP: ip, AccessKey: profile.AccessKey, WebhookURL: profile.WebhookURL, WebhookMethod: profile.WebhookMethod, WebhookHeaders: profile.WebhookHeaders, WebhookBody: profile.WebhookBody})
		updates := map[string]any{"last_attempt": attempt}
		if err == nil {
			updates["status"] = "success"
			updates["last_ip"] = ip
			updates["last_success"] = attempt
			updates["last_error"] = ""
			updates["retry_count"] = 0
			updates["next_retry"] = nil
			s.DB.Model(profile).Updates(map[string]any{"last_ip": ip, "last_updated": attempt})
		} else if errors.Is(err, ddns.ErrUnauthorized) {
			updates["status"] = "stopped"
			updates["last_error"] = err.Error()
			updates["next_retry"] = nil
		} else {
			count := state.RetryCount + 1
			next := attempt.Add(retryDelay(count))
			updates["status"] = "retrying"
			updates["last_error"] = err.Error()
			updates["retry_count"] = count
			updates["next_retry"] = next
		}
		s.DB.Model(state).Updates(updates)
	}
}

// HandleServerIPChange receives Agent-reported addresses, never the API caller's address.
func (s *Server) HandleServerIPChange(serverID int64, host protocol.HostInfo) {
	var profiles []model.DDNSProfile
	if err := s.DB.Where("server_id=? AND enabled=?", serverID, true).Find(&profiles).Error; err != nil {
		return
	}
	for i := range profiles {
		_ = s.syncDDNSRecords(&profiles[i])
		s.runDDNSProfile(&profiles[i], agentIPs(host), false)
	}
}

// RunDDNSRetries resumes persisted due retries after restart.
func (s *Server) RunDDNSRetries() {
	var states []model.DDNSRecordState
	now := time.Now()
	s.DB.Where("next_retry IS NOT NULL AND next_retry <= ?", now).Find(&states)
	seen := map[int64]bool{}
	for _, state := range states {
		if seen[state.ProfileID] {
			continue
		}
		seen[state.ProfileID] = true
		var p model.DDNSProfile
		if s.DB.First(&p, state.ProfileID).Error != nil || !p.Enabled {
			continue
		}
		st := s.Store.Get(p.ServerID)
		if st != nil {
			s.runDDNSProfile(&p, agentIPs(st.Host), false)
		}
	}
}
