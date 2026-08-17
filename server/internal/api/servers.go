package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
	trafficquota "github.com/motao123/Argus/server/internal/traffic"
)

// authorizeServer verifies scope, ownership and PAT server whitelist for a server resource.
func (s *Server) authorizeServer(c *gin.Context, serverID int64, scope string) (*model.Server, bool) {
	p := principalFromContext(c)
	if p == nil || !p.hasScope(scope) {
		return nil, false
	}
	var srv model.Server
	if err := s.DB.First(&srv, serverID).Error; err != nil {
		return nil, false
	}
	if p.IsAdmin {
		return &srv, true
	}
	if srv.OwnerID != p.UserID || (p.IsPAT && !p.canAccessServer(serverID)) {
		return nil, false
	}
	return &srv, true
}

// authorizePublicServer permits a visible server to guests and owner/admin users.
func (s *Server) authorizePublicServer(c *gin.Context, serverID int64) (*model.Server, bool) {
	p := principalFromContext(c)
	var srv model.Server
	if err := s.DB.First(&srv, serverID).Error; err != nil {
		return nil, false
	}
	if p == nil {
		return &srv, !srv.Hidden && s.GetSetting(SettingForceAuth, "0") != "1"
	}
	if p.IsAdmin || (srv.OwnerID == p.UserID && (!p.IsPAT || p.canAccessServer(serverID))) {
		return &srv, true
	}
	return nil, false
}

// serverView 前端视图：持久化配置 + 实时状态。
type serverView struct {
	model.Server
	Host                    *hostView             `json:"host,omitempty"`
	CPU                     float64               `json:"cpu"`
	MemUsed                 uint64                `json:"mem_used"`
	MemTotal                uint64                `json:"mem_total"`
	DiskUsed                uint64                `json:"disk_used"`
	DiskTotal               uint64                `json:"disk_total"`
	NetInSpeed              float64               `json:"net_in_speed"`
	NetOutSpeed             float64               `json:"net_out_speed"`
	Load1                   float64               `json:"load1"`
	Temperature             float64               `json:"temperature"`
	GPUUtil                 float64               `json:"gpu_util"`
	GPU                     protocol.GPUReport    `json:"gpu"`
	ProcessCount            int                   `json:"process_count"`
	TCPEstablished          int                   `json:"tcp_established"`
	TCPListen               int                   `json:"tcp_listen"`
	UDPCount                int                   `json:"udp_count"`
	DiskReadSpeed           float64               `json:"disk_read_speed"`
	DiskWriteSpeed          float64               `json:"disk_write_speed"`
	DiskReadIOPS            float64               `json:"disk_read_iops"`
	DiskWriteIOPS           float64               `json:"disk_write_iops"`
	DiskIOAvailability      protocol.Availability `json:"disk_io_availability"`
	SocketAvailability      protocol.Availability `json:"socket_availability"`
	ProcessAvailability     protocol.Availability `json:"process_availability"`
	TemperatureAvailability protocol.Availability `json:"temperature_availability"`
	// LatencyMs Agent↔Server 往返延迟（毫秒）；0 = 无测量（旧 Agent / 未上报）。
	LatencyMs    int                 `json:"latency_ms"`
	Uptime       uint64              `json:"uptime"`
	Online       bool                `json:"online"`
	LastSeen     time.Time           `json:"last_seen"`
	TrafficUsage *trafficquota.Usage `json:"traffic_usage,omitempty"`
}

type hostView struct {
	Hostname        string `json:"hostname"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	KernelVersion   string `json:"kernel_version"`
	CPUModel        string `json:"cpu_model"`
	CPUCores        int    `json:"cpu_cores"`
	AgentVersion    string `json:"agent_version"`
	IP              string `json:"ip"`
	IPv4            string `json:"ipv4"`
	IPv6            string `json:"ipv6"`
	CountryCode     string `json:"country_code"`
}

// listServers 返回全部服务器（配置 + 实时状态）。
// 多用户：admin 看全部，普通用户只看自己名下（owner_id 匹配）。
func (s *Server) listServers(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Model(&model.Server{}).Order("sort_order, id")
	switch {
	case p == nil:
		// 游客只看非隐藏服务器（私有站点模式一律不可见）
		if s.GetSetting(SettingForceAuth, "0") == "1" {
			q = q.Where("1 = 0")
		} else {
			q = q.Where("hidden = ?", false)
		}
	case p.IsAdmin:
		// 全部
	case p.IsPAT:
		q = q.Where("owner_id = ?", p.UserID)
	default:
		// 普通用户看自己名下（含隐藏，隐藏仅对游客生效）
		q = q.Where("owner_id = ?", p.UserID)
	}
	offset, limit := pagination(c)
	var total int64
	q.Count(&total)
	var servers []model.Server
	if err := q.Offset(offset).Limit(limit).Find(&servers).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	snap := s.Store.Snapshot()
	out := make([]serverView, 0, len(servers))
	for i := range servers {
		if p != nil && p.IsPAT && !p.canAccessServer(servers[i].ID) {
			continue // PAT 白名单外服务器不可见
		}
		v := serverView{Server: servers[i]}
		if usage, err := trafficquota.CurrentUsage(s.DB, &servers[i], time.Now()); err == nil {
			v.TrafficUsage = &usage
		}
		if st, ok := snap[servers[i].ID]; ok {
			v.CPU = st.Last.CPU
			v.MemUsed = st.Last.MemUsed
			v.MemTotal = st.Last.MemTotal
			v.DiskUsed = st.Last.DiskUsed
			v.DiskTotal = st.Last.DiskTotal
			v.NetInSpeed = st.Last.NetInSpeed
			v.NetOutSpeed = st.Last.NetOutSpeed
			v.Load1 = st.Last.Load1
			v.Temperature = st.Last.Temperature
			v.GPUUtil = st.Last.GPUUtil
			v.GPU = st.Last.GPU
			v.ProcessCount = st.Last.ProcessCount
			v.TCPEstablished = st.Last.TCPEstablished
			v.TCPListen = st.Last.TCPListen
			v.UDPCount = st.Last.UDPCount
			v.DiskReadSpeed = st.Last.DiskReadSpeed
			v.DiskWriteSpeed = st.Last.DiskWriteSpeed
			v.DiskReadIOPS = st.Last.DiskReadIOPS
			v.DiskWriteIOPS = st.Last.DiskWriteIOPS
			v.DiskIOAvailability = st.Last.DiskIOAvailability
			v.SocketAvailability = st.Last.SocketAvailability
			v.ProcessAvailability = st.Last.ProcessAvailability
			v.TemperatureAvailability = st.Last.TemperatureAvailability
			v.Uptime = st.Last.Uptime
			v.LatencyMs = st.LatencyMs
			v.Online = st.Online
			v.LastSeen = st.LastSeen
			if st.Host.Hostname != "" {
				country := ""
				if s.GeoIP != nil && st.Host.IP != "" {
					country = s.GeoIP.CountryCode(st.Host.IP)
				}
				v.Host = &hostView{
					Hostname:        st.Host.Hostname,
					Platform:        st.Host.Platform,
					PlatformVersion: st.Host.PlatformVersion,
					OS:              st.Host.OS, Arch: st.Host.Arch, KernelVersion: st.Host.KernelVersion,
					CPUModel:     st.Host.CPUModel,
					CPUCores:     st.Host.CPUCores,
					AgentVersion: st.Host.AgentVersion,
					IP:           st.Host.IP,
					IPv4:         st.Host.IPv4, IPv6: st.Host.IPv6,
					CountryCode: country,
				}
			}
		}
		out = append(out, v)
	}
	okPage(c, gin.H{"servers": out}, total, offset, limit)
}

// createServer 手动创建服务器（返回密钥，用于 Agent 配置）。
func (s *Server) createServer(c *gin.Context) {
	var req struct {
		Name              string   `json:"name"`
		Group             string   `json:"group"`
		Note              string   `json:"note"`
		SloTarget         *float64 `json:"slo_target"` // 空 = 默认 99.9，0 = 不启用 SLO
		TrafficQuotaBytes uint64   `json:"traffic_quota_bytes"`
		TrafficCycleDay   int      `json:"traffic_cycle_day"`
		TrafficTimezone   string   `json:"traffic_timezone"`
		TrafficAccounting string   `json:"traffic_accounting"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	sloTarget := 99.9
	if req.SloTarget != nil && *req.SloTarget >= 0 {
		sloTarget = *req.SloTarget
	}
	if req.TrafficCycleDay == 0 {
		req.TrafficCycleDay = 1
	}
	if req.TrafficTimezone == "" {
		req.TrafficTimezone = "UTC"
	}
	if req.TrafficAccounting == "" {
		req.TrafficAccounting = trafficquota.AccountingSum
	}
	if err := trafficquota.ValidateConfig(req.TrafficCycleDay, req.TrafficTimezone, req.TrafficAccounting); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	srv := model.Server{Name: req.Name, Group: req.Group, Note: req.Note, Secret: agent.GenSecret(), OwnerID: principalFromContext(c).UserID, SloTarget: sloTarget,
		TrafficQuotaBytes: req.TrafficQuotaBytes, TrafficCycleDay: req.TrafficCycleDay, TrafficTimezone: req.TrafficTimezone, TrafficAccounting: req.TrafficAccounting}
	if err := s.DB.Create(&srv).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Upsert(&srv)
	s.auditLog(c, "server.create", srv.Name)
	// 密钥仅在创建时返回一次（Agent 配置用）
	ok(c, gin.H{"server": srv, "secret": srv.Secret})
}

func (s *Server) updateServer(c *gin.Context) {
	id := mustID(c)
	var req struct {
		Name              *string  `json:"name"`
		Group             *string  `json:"group"`
		Note              *string  `json:"note"`
		Price             *float64 `json:"price"`
		CycleDays         *int     `json:"cycle_days"`
		ExpireAt          *string  `json:"expire_at"` // RFC3339 或空
		AutoRenew         *bool    `json:"auto_renew"`
		Tags              *string  `json:"tags"`
		SortOrder         *int     `json:"sort_order"`
		Hidden            *bool    `json:"hidden"`
		SloTarget         *float64 `json:"slo_target"`
		TrafficQuotaBytes *uint64  `json:"traffic_quota_bytes"`
		TrafficCycleDay   *int     `json:"traffic_cycle_day"`
		TrafficTimezone   *string  `json:"traffic_timezone"`
		TrafficAccounting *string  `json:"traffic_accounting"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var srv model.Server
	if _, ok := s.authorizeServer(c, id, ScopeServerWrite); !ok {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	if err := s.DB.First(&srv, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	// 部分更新语义：未提交字段保留原值（防止单字段更新清空 name/group/note）
	updates := map[string]any{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Group != nil {
		updates["group_name"] = *req.Group
	}
	if req.Note != nil {
		updates["note"] = *req.Note
	}
	if req.Price != nil {
		updates["price"] = *req.Price
	}
	if req.CycleDays != nil {
		updates["cycle_days"] = *req.CycleDays
	}
	if req.AutoRenew != nil {
		updates["auto_renew"] = *req.AutoRenew
	}
	if req.Tags != nil {
		updates["tags"] = *req.Tags
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	if req.Hidden != nil {
		updates["hidden"] = *req.Hidden
	}
	if req.SloTarget != nil {
		if *req.SloTarget < 0 {
			fail(c, http.StatusBadRequest, "slo_target must be >= 0")
			return
		}
		updates["slo_target"] = *req.SloTarget
	}
	day, timezone, accounting := srv.TrafficCycleDay, srv.TrafficTimezone, srv.TrafficAccounting
	if day == 0 {
		day = 1
	}
	if timezone == "" {
		timezone = "UTC"
	}
	if accounting == "" {
		accounting = trafficquota.AccountingSum
	}
	if req.TrafficCycleDay != nil {
		day = *req.TrafficCycleDay
	}
	if req.TrafficTimezone != nil {
		timezone = *req.TrafficTimezone
	}
	if req.TrafficAccounting != nil {
		accounting = *req.TrafficAccounting
	}
	if err := trafficquota.ValidateConfig(day, timezone, accounting); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.TrafficQuotaBytes != nil {
		updates["traffic_quota_bytes"] = *req.TrafficQuotaBytes
	}
	if req.TrafficCycleDay != nil {
		updates["traffic_cycle_day"] = day
	}
	if req.TrafficTimezone != nil {
		updates["traffic_timezone"] = timezone
	}
	if req.TrafficAccounting != nil {
		updates["traffic_accounting"] = accounting
	}
	if req.ExpireAt != nil {
		if *req.ExpireAt == "" {
			updates["expire_at"] = nil
		} else if t, err := time.Parse(time.RFC3339, *req.ExpireAt); err == nil {
			updates["expire_at"] = t
		}
	}
	if err := s.DB.Model(&srv).Updates(updates).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.DB.First(&srv, id)
	s.Store.Upsert(&srv)
	s.auditLog(c, "server.update", srv.Name)
	ok(c, srv)
}

func (s *Server) deleteServer(c *gin.Context) {
	id := mustID(c)
	if _, ok := s.authorizeServer(c, id, ScopeServerDelete); !ok {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	if err := s.DB.Delete(&model.Server{}, id).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if peer := s.Agents.Peer(id); peer != nil {
		_ = peer.Close()
	}
	s.Store.Remove(id)
	s.auditLog(c, "server.delete", "")
	ok(c, gin.H{"ok": true})
}

// maxCompareServers 指标对比单次最多服务器数（前端选择上限与之一致）。
const maxCompareServers = 10

// metricPeriodConfig 解析 period 参数（1h / 24h / 7d，非法值回退 1h），
// 返回时间窗秒数、降采样步长（秒）与存储粒度（秒）。
func metricPeriodConfig(period string) (seconds, step int64, gran int) {
	switch period {
	case "24h":
		return 24 * 3600, 300, 300
	case "7d":
		return 7 * 24 * 3600, 3600, 3600
	default:
		return 3600, 60, 60
	}
}

// aggregateMetrics 把原始指标行按 step 秒降采样为聚合点
// （与单机 serverMetrics 口径完全一致，多机对比可复用）。
func aggregateMetrics(rows []model.Metric, step int64) []gin.H {
	type agg struct {
		count                                                                         int
		cpu, netIn, netOut, load1, temp, gpu, process, tcpEstablished, tcpListen, udp float64
		diskReadSpeed, diskWriteSpeed, diskReadIOPS, diskWriteIOPS                    float64
		memUsed, memTotal, diskUsed, diskTotal                                        uint64
	}
	buckets := map[int64]*agg{}
	var order []int64
	for _, r := range rows {
		bts := r.TS / step * step
		a, ok := buckets[bts]
		if !ok {
			a = &agg{}
			buckets[bts] = a
			order = append(order, bts)
		}
		a.count++
		a.cpu += r.CPU
		a.netIn += r.NetInSpeed
		a.netOut += r.NetOutSpeed
		a.load1 += r.Load1
		a.temp += r.Temperature
		a.gpu += r.GPUUtil
		a.process += r.ProcessCount
		a.tcpEstablished += r.TCPEstablished
		a.tcpListen += r.TCPListen
		a.udp += r.UDPCount
		a.diskReadSpeed += r.DiskReadSpeed
		a.diskWriteSpeed += r.DiskWriteSpeed
		a.diskReadIOPS += r.DiskReadIOPS
		a.diskWriteIOPS += r.DiskWriteIOPS
		a.memUsed = r.MemUsed
		a.memTotal = r.MemTotal
		a.diskUsed = r.DiskUsed
		a.diskTotal = r.DiskTotal
	}

	out := make([]gin.H, 0, len(order))
	for _, bts := range order {
		a := buckets[bts]
		n := float64(a.count)
		out = append(out, gin.H{
			"ts":            bts,
			"cpu":           round2(a.cpu / n),
			"net_in":        round2(a.netIn / n),
			"net_out":       round2(a.netOut / n),
			"load1":         round2(a.load1 / n),
			"temperature":   round2(a.temp / n),
			"gpu_util":      round2(a.gpu / n),
			"process_count": round2(a.process / n), "tcp_established": round2(a.tcpEstablished / n), "tcp_listen": round2(a.tcpListen / n), "udp_count": round2(a.udp / n),
			"disk_read_speed": round2(a.diskReadSpeed / n), "disk_write_speed": round2(a.diskWriteSpeed / n), "disk_read_iops": round2(a.diskReadIOPS / n), "disk_write_iops": round2(a.diskWriteIOPS / n),
			"mem_used":   a.memUsed,
			"mem_total":  a.memTotal,
			"disk_used":  a.diskUsed,
			"disk_total": a.diskTotal,
		})
	}
	return out
}

// serverMetrics 查询单台服务器历史指标。
// period: 1h / 24h / 7d；返回聚合后的点（最多 ~120 点）。
func (s *Server) serverMetrics(c *gin.Context) {
	id := mustID(c)
	if _, ok := s.authorizePublicServer(c, id); !ok {
		fail(c, http.StatusNotFound, "server not found")
		return
	}
	period := c.DefaultQuery("period", "1h")
	seconds, step, gran := metricPeriodConfig(period)
	from := time.Now().Add(-time.Duration(seconds) * time.Second).Unix()

	var rows []model.Metric
	if err := s.DB.Where("server_id = ? AND ts >= ? AND granularity = ?", id, from, gran).
		Order("ts").Find(&rows).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"period": period, "points": aggregateMetrics(rows, step)})
}

// compareMetrics 批量对比多台服务器历史指标（admin/owner 逐 id 校验）。
// GET /api/v1/metrics/compare?ids=1,2,3&period=24h
// 单次 IN 查询拉取全部原始行、Go 内按 server_id 分桶聚合，避免 N+1；最多 10 台。
func (s *Server) compareMetrics(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil {
		fail(c, http.StatusUnauthorized, "login required")
		return
	}
	// 解析 ids（逗号分隔，去重、忽略非法项，保持请求顺序）
	ids := make([]int64, 0, 10)
	seen := make(map[int64]bool)
	for _, part := range strings.Split(c.Query("ids"), ",") {
		id := parseIntQuery64(strings.TrimSpace(part))
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		fail(c, http.StatusBadRequest, "ids required")
		return
	}
	if len(ids) > maxCompareServers {
		fail(c, http.StatusBadRequest, "at most 10 servers per compare")
		return
	}
	// 逐 id 校验：admin 或 owner（PAT 需 scope + 白名单）
	servers := make([]model.Server, 0, len(ids))
	for _, id := range ids {
		srv, ok := s.authorizeServer(c, id, ScopeServerRead)
		if !ok {
			fail(c, http.StatusForbidden, "server access denied")
			return
		}
		servers = append(servers, *srv)
	}

	period := c.DefaultQuery("period", "1h")
	seconds, step, gran := metricPeriodConfig(period)
	from := time.Now().Add(-time.Duration(seconds) * time.Second).Unix()

	var rows []model.Metric
	if err := s.DB.Where("server_id IN ? AND ts >= ? AND granularity = ?", ids, from, gran).
		Order("ts").Find(&rows).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	byServer := make(map[int64][]model.Metric)
	for _, r := range rows {
		byServer[r.ServerID] = append(byServer[r.ServerID], r)
	}
	out := make([]gin.H, 0, len(servers))
	for _, srv := range servers {
		out = append(out, gin.H{
			"server_id":   srv.ID,
			"server_name": srv.Name,
			"points":      aggregateMetrics(byServer[srv.ID], step),
		})
	}
	ok(c, gin.H{"period": period, "series": out})
}

// serverApplyConfig 下发 Agent 配置（借鉴 nezha ApplyConfig）。
func (s *Server) serverApplyConfig(c *gin.Context) {
	id := mustID(c)
	if _, ok := s.authorizeServer(c, id, ScopeServerWrite); !ok {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	var req struct {
		ServerURL        string          `json:"server_url"`
		Interval         int             `json:"interval"`
		Secret           string          `json:"secret"`
		Capabilities     json.RawMessage `json:"capabilities"`
		InterfaceInclude []string        `json:"interface_include"`
		InterfaceExclude []string        `json:"interface_exclude"`
		MountInclude     []string        `json:"mount_include"`
		MountExclude     []string        `json:"mount_exclude"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	// 校验能力名合法并规范化（未知名报 400；空/缺省表示不修改）
	caps, err := protocol.ParseCapabilities(req.Capabilities)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	peer := s.Agents.Peer(id)
	if peer == nil {
		fail(c, http.StatusConflict, "server offline", "server.offline")
		return
	}
	resp, err := peer.Call(protocol.MethodApplyConfig, protocol.AgentConfig{
		ServerURL: req.ServerURL, Interval: req.Interval, Secret: req.Secret, Capabilities: caps,
		InterfaceInclude: req.InterfaceInclude, InterfaceExclude: req.InterfaceExclude, MountInclude: req.MountInclude, MountExclude: req.MountExclude,
	}, 15*time.Second)
	if err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	if resp.Error != nil {
		status, msg, apiCode := applyConfigError(resp.Error)
		fail(c, status, msg, apiCode)
		return
	}
	s.auditLog(c, "server.apply_config", "")
	ok(c, gin.H{"ok": true})
}

// applyConfigError 把 Agent 返回的 RPC 错误映射为 HTTP 响应参数（含稳定 code，供前端 i18n 翻译）。
// 被禁能力导致的 "capability disabled" 返回 capability.disabled 稳定码，其余错误回退原始消息。
func applyConfigError(rpcErr *protocol.RPCError) (status int, msg, apiCode string) {
	if rpcErr.Code == protocol.ErrCapabilityDisabled {
		return http.StatusBadGateway, "capability disabled", "capability.disabled"
	}
	return http.StatusBadGateway, rpcErr.Message, ""
}

// serverExec 立即在指定服务器执行命令（管理台调试用）。
func (s *Server) serverExec(c *gin.Context) {
	id := mustID(c)
	if _, ok := s.authorizeServer(c, id, ScopeServerExec); !ok {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	var req struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	cmdHash := sha256.Sum256([]byte(req.Command))
	s.auditLog(c, "server.exec", fmt.Sprintf("sha256=%s length=%d", hex.EncodeToString(cmdHash[:]), len(req.Command)))
	result, err := s.Agents.Exec(id, req.Command, req.Timeout)
	if err != nil {
		code := http.StatusBadGateway
		if err == agent.ErrOffline {
			code = http.StatusConflict
		}
		fail(c, code, err.Error())
		return
	}
	ok(c, result)
}

func mustID(c *gin.Context) int64 {
	return mustIDParam(c, "id")
}

func mustIDParam(c *gin.Context, name string) int64 {
	id, _ := strconv.ParseInt(c.Param(name), 10, 64)
	return id
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
