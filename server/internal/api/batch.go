package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
)

// batchDeleteServers 批量删除服务器（借鉴 nezha batch-delete）。
func (s *Server) batchDeleteServers(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "ids required")
		return
	}
	for _, id := range req.IDs {
		if peer := s.Agents.Peer(id); peer != nil {
			_ = peer.Close()
		}
		s.Store.Remove(id)
		s.DB.Delete(&model.Server{}, id)
	}
	s.auditLog(c, "server.batch_delete", "")
	ok(c, gin.H{"ok": true, "deleted": len(req.IDs)})
}

// batchMoveServers 批量移动分组（借鉴 nezha batch-move）。
func (s *Server) batchMoveServers(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		IDs   []int64 `json:"ids"`
		Group string  `json:"group"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "ids required")
		return
	}
	res := s.DB.Model(&model.Server{}).Where("id IN ?", req.IDs).
		Updates(map[string]any{"group_name": req.Group})
	if res.Error != nil {
		fail(c, http.StatusInternalServerError, res.Error.Error())
		return
	}
	s.auditLog(c, "server.batch_move", req.Group)
	ok(c, gin.H{"ok": true, "moved": res.RowsAffected})
}

// batchServerResult 批量操作中单台服务器的独立结果（逐机回执）。
type batchServerResult struct {
	ServerID   int64  `json:"server_id"`
	ServerName string `json:"server_name"`
	Status     string `json:"status"` // ok / offline / not_found / no_ip / error
	Error      string `json:"error,omitempty"`
	ProfileID  int64  `json:"profile_id,omitempty"` // 批量 DDNS 创建出的配置 ID
}

const maxBatchConcurrency = 16

// batchConfigServers 批量下发 Agent 配置（对标 nezha 批量设置；admin，逐机回执）。
// 仅下发运行配置，不接受密钥字段（避免把同一密钥广播到多台机器）。
func (s *Server) batchConfigServers(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		IDs              []int64                `json:"ids"`
		ServerURL        string                 `json:"server_url"`
		Interval         int                    `json:"interval"`
		Capabilities     *protocol.Capabilities `json:"capabilities"`
		InterfaceInclude []string               `json:"interface_include"`
		InterfaceExclude []string               `json:"interface_exclude"`
		MountInclude     []string               `json:"mount_include"`
		MountExclude     []string               `json:"mount_exclude"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "ids required")
		return
	}
	ids := dedupeIDs(req.IDs)
	if len(ids) == 0 {
		fail(c, http.StatusBadRequest, "valid ids required")
		return
	}
	names := s.serverNames(ids)
	cfg := protocol.AgentConfig{
		ServerURL: req.ServerURL, Interval: req.Interval, Capabilities: req.Capabilities,
		InterfaceInclude: req.InterfaceInclude, InterfaceExclude: req.InterfaceExclude,
		MountInclude: req.MountInclude, MountExclude: req.MountExclude,
	}
	results := make([]batchServerResult, len(ids))
	s.batchApply(ids, func(id int64) (string, string) {
		if names[id] == "" {
			return "not_found", ""
		}
		peer := s.Agents.Peer(id)
		if peer == nil {
			return "offline", ""
		}
		resp, err := peer.Call(protocol.MethodApplyConfig, cfg, 15*time.Second)
		if err != nil {
			return "error", err.Error()
		}
		if resp.Error != nil {
			return "error", resp.Error.Message
		}
		return "ok", ""
	}, func(i int, status, msg string) {
		results[i] = batchServerResult{ServerID: ids[i], ServerName: names[ids[i]], Status: status, Error: msg}
	})
	s.auditLog(c, "server.batch_config", fmt.Sprintf("targets=%d", len(ids)))
	ok(c, gin.H{"results": results})
}

// batchDDNSServers 把指定 DDNS 配置批量应用到多台服务器（admin，逐机回执）。
// 对每台目标服务器复制一份配置（新 owner = 操作者），并立即用该服务器 Agent 上报的 IP 尝试更新解析。
func (s *Server) batchDDNSServers(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		IDs       []int64 `json:"ids"`
		ProfileID int64   `json:"profile_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "ids required")
		return
	}
	if req.ProfileID <= 0 {
		fail(c, http.StatusBadRequest, "profile_id required")
		return
	}
	var source model.DDNSProfile
	if err := s.DB.First(&source, req.ProfileID).Error; err != nil {
		fail(c, http.StatusNotFound, "ddns profile not found")
		return
	}
	if err := validateProviderConfig(source.Provider, source.AccessKey); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ids := dedupeIDs(req.IDs)
	if len(ids) == 0 {
		fail(c, http.StatusBadRequest, "valid ids required")
		return
	}
	names := s.serverNames(ids)
	results := make([]batchServerResult, len(ids))
	for i, id := range ids {
		r := batchServerResult{ServerID: id, ServerName: names[id]}
		if names[id] == "" {
			r.Status = "not_found"
			results[i] = r
			continue
		}
		profile := model.DDNSProfile{
			OwnerID: p.UserID, ServerID: id, Name: source.Name, Provider: source.Provider,
			RecordType: source.RecordType, Domains: source.Domains, Enabled: true,
			AccessKey: source.AccessKey, WebhookURL: source.WebhookURL, WebhookMethod: source.WebhookMethod,
			WebhookHeaders: source.WebhookHeaders, WebhookBody: source.WebhookBody,
		}
		if err := s.DB.Create(&profile).Error; err != nil {
			r.Status, r.Error = "error", err.Error()
			results[i] = r
			continue
		}
		_ = s.syncDDNSRecords(&profile)
		st := s.Store.Get(id)
		if st == nil {
			// Agent 尚未上报 IP：配置已就绪，IP 变化回调（HandleServerIPChange）会自动更新。
			r.Status = "no_ip"
		} else {
			ips := agentIPs(st.Host)
			if ips["A"] == "" && ips["AAAA"] == "" {
				r.Status = "no_ip"
			} else {
				s.runDDNSProfile(&profile, ips, true)
				r.Status = "ok"
			}
		}
		r.ProfileID = profile.ID
		results[i] = r
	}
	s.auditLog(c, "server.batch_ddns", fmt.Sprintf("profile_id=%d targets=%d", req.ProfileID, len(ids)))
	ok(c, gin.H{"results": results})
}

// dedupeIDs 去重并过滤非法 ID，保持输入顺序。
func dedupeIDs(in []int64) []int64 {
	seen := make(map[int64]struct{}, len(in))
	out := make([]int64, 0, len(in))
	for _, id := range in {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// serverNames 批量查询服务器名称，返回 serverID → name（不存在的 ID 不在映射中）。
func (s *Server) serverNames(ids []int64) map[int64]string {
	names := make(map[int64]string, len(ids))
	var servers []model.Server
	if err := s.DB.Where("id IN ?", ids).Find(&servers).Error; err != nil {
		return names
	}
	for i := range servers {
		names[servers[i].ID] = servers[i].Name
	}
	return names
}

// batchApply 以有界并发执行逐机任务，并通过 set 回调按输入顺序回填结果。
func (s *Server) batchApply(ids []int64, run func(id int64) (status, msg string), set func(i int, status, msg string)) {
	workers := len(ids)
	if workers > maxBatchConcurrency {
		workers = maxBatchConcurrency
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				status, msg := run(ids[i])
				set(i, status, msg)
			}
		}()
	}
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}
