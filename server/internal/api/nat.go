package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/nat"
)

// listNAT NAT 配置列表（含运行时隧道状态、并发数与配额，供 UI 展示）。
func (s *Server) listNAT(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Order("id")
	if p != nil && !p.IsAdmin {
		q = q.Where("owner_id = ?", p.UserID)
	}
	var nats []model.NAT
	if err := q.Find(&nats).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp := gin.H{"nats": nats}
	if s.NAT != nil {
		serverLimit, userLimit := s.NAT.Limits()
		resp["limits"] = gin.H{"server": serverLimit, "user": userLimit}
		resp["reserved_hosts"] = s.NAT.ReservedHosts()
		for i := range nats {
			server, user := s.NAT.Active(nats[i].ServerID, nats[i].OwnerID)
			nats[i].ActiveConnections = server
			nats[i].OwnerActiveConnections = user
			nats[i].ServerConnectionLimit = serverLimit
			nats[i].OwnerConnectionLimit = userLimit
			if s.Agents != nil && s.Agents.Peer(nats[i].ServerID) != nil {
				nats[i].Status = "online"
			} else {
				nats[i].Status = "offline"
			}
		}
	}
	ok(c, resp)
}

// validateNATDomain 规范化域名并拒绝保留域名（dashboard 等不允许被 NAT 覆盖）。
func (s *Server) validateNATDomain(domain string) (string, bool) {
	domain = nat.NormalizeHost(domain)
	if domain == "" {
		return "", false
	}
	if s.NAT != nil && s.NAT.IsReserved(domain) {
		return "", false
	}
	return domain, true
}

func (s *Server) createNAT(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		ServerID   int64  `json:"server_id"`
		Domain     string `json:"domain"`
		TargetAddr string `json:"target_addr"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if req.ServerID <= 0 || req.Domain == "" || req.TargetAddr == "" {
		fail(c, http.StatusBadRequest, "server_id/domain/target_addr required")
		return
	}
	domain, valid := s.validateNATDomain(req.Domain)
	if !valid {
		fail(c, http.StatusBadRequest, "domain is empty or reserved (dashboard) host")
		return
	}
	if _, ok := s.authorizeServer(c, req.ServerID, ScopeServerWrite); !ok {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	nat := model.NAT{
		OwnerID:    p.UserID,
		ServerID:   req.ServerID,
		Domain:     domain,
		TargetAddr: req.TargetAddr,
		Enabled:    true,
	}
	if err := s.DB.Create(&nat).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "nat.create", fmt.Sprintf("nat_id=%d domain=%s server_id=%d target=%s", nat.ID, nat.Domain, nat.ServerID, nat.TargetAddr))
	ok(c, nat)
}

func (s *Server) updateNAT(c *gin.Context) {
	id := mustID(c)
	var nat model.NAT
	if err := s.DB.First(&nat, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&nat.OwnerID, c) {
		fail(c, http.StatusForbidden, "not yours")
		return
	}
	var req struct {
		ServerID   *int64  `json:"server_id"`
		Domain     *string `json:"domain"`
		TargetAddr *string `json:"target_addr"`
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
	if req.Domain != nil {
		domain, ok := s.validateNATDomain(*req.Domain)
		if !ok {
			fail(c, http.StatusBadRequest, "domain is empty or reserved (dashboard) host")
			return
		}
		updates["domain"] = domain
	}
	if req.TargetAddr != nil {
		updates["target_addr"] = *req.TargetAddr
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if err := s.DB.Model(&nat).Updates(updates).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "nat.update", fmt.Sprintf("nat_id=%d domain=%s server_id=%d", nat.ID, nat.Domain, nat.ServerID))
	ok(c, gin.H{"ok": true})
}

func (s *Server) deleteNAT(c *gin.Context) {
	id := mustID(c)
	var nat model.NAT
	if err := s.DB.First(&nat, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&nat.OwnerID, c) {
		fail(c, http.StatusForbidden, "not yours")
		return
	}
	s.DB.Delete(&model.NAT{}, id)
	s.auditLog(c, "nat.delete", fmt.Sprintf("nat_id=%d domain=%s", nat.ID, nat.Domain))
	ok(c, gin.H{"ok": true})
}
