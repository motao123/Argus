package api

import (

	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"


	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
)

// serverView 前端视图：持久化配置 + 实时状态。
type serverView struct {
	model.Server
	Host      *hostView `json:"host,omitempty"`
	CPU       float64   `json:"cpu"`
	MemUsed   uint64    `json:"mem_used"`
	MemTotal  uint64    `json:"mem_total"`
	DiskUsed  uint64    `json:"disk_used"`
	DiskTotal uint64    `json:"disk_total"`
	NetInSpeed   float64 `json:"net_in_speed"`
	NetOutSpeed  float64 `json:"net_out_speed"`
	Load1     float64   `json:"load1"`
	Uptime    uint64    `json:"uptime"`
	Online    bool      `json:"online"`
	LastSeen  time.Time `json:"last_seen"`
}

type hostView struct {
	Hostname        string `json:"hostname"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	CPUModel        string `json:"cpu_model"`
	CPUCores        int    `json:"cpu_cores"`
	AgentVersion    string `json:"agent_version"`
	IP              string `json:"ip"`
	CountryCode     string `json:"country_code"`
}

// listServers 返回全部服务器（配置 + 实时状态）。
// 多用户：admin 看全部，普通用户只看自己名下（owner_id 匹配）。
func (s *Server) listServers(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Order("id")
	if p != nil && !p.IsAdmin && !p.IsPAT {
		q = q.Where("owner_id = ?", p.UserID)
	}
	var servers []model.Server
	if err := q.Find(&servers).Error; err != nil {
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
		if st, ok := snap[servers[i].ID]; ok {
			v.CPU = st.Last.CPU
			v.MemUsed = st.Last.MemUsed
			v.MemTotal = st.Last.MemTotal
			v.DiskUsed = st.Last.DiskUsed
			v.DiskTotal = st.Last.DiskTotal
			v.NetInSpeed = st.Last.NetInSpeed
			v.NetOutSpeed = st.Last.NetOutSpeed
			v.Load1 = st.Last.Load1
			v.Uptime = st.Last.Uptime
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
					CPUModel:        st.Host.CPUModel,
					CPUCores:        st.Host.CPUCores,
					AgentVersion:    st.Host.AgentVersion,
					IP:              st.Host.IP,
					CountryCode:     country,
				}
			}
		}
		out = append(out, v)
	}
	ok(c, gin.H{"servers": out})
}

// createServer 手动创建服务器（返回密钥，用于 Agent 配置）。
func (s *Server) createServer(c *gin.Context) {
	var req struct {
		Name  string `json:"name"`
		Group string `json:"group"`
		Note  string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	srv := model.Server{Name: req.Name, Group: req.Group, Note: req.Note, Secret: agent.GenSecret()}
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
		Name      string `json:"name"`
		Group     string `json:"group"`
		Note      string `json:"note"`
		Price     *float64 `json:"price"`
		CycleDays *int     `json:"cycle_days"`
		ExpireAt  *string  `json:"expire_at"` // RFC3339 或空
		AutoRenew *bool    `json:"auto_renew"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var srv model.Server
	if err := s.DB.First(&srv, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	updates := map[string]any{
		"name": req.Name, "group": req.Group, "note": req.Note,
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

// serverMetrics 查询历史指标。
// period: 1h / 24h / 7d；返回聚合后的点（最多 ~120 点）。
func (s *Server) serverMetrics(c *gin.Context) {
	id := mustID(c)
	period := c.DefaultQuery("period", "1h")
	now := time.Now()

	seconds, step := 3600, int64(60)
	switch period {
	case "24h":
		seconds, step = 24*3600, 300
	case "7d":
		seconds, step = 7*24*3600, 3600
	default:
		period = "1h"
	}

	from := now.Add(-time.Duration(seconds) * time.Second).Unix()
	var rows []model.Metric
	if err := s.DB.Where("server_id = ? AND ts >= ?", id, from).
		Order("ts").Find(&rows).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 内存聚合降采样到 step
	type agg struct {
		count            int
		cpu, netIn, netOut, load1 float64
		memUsed, memTotal, diskUsed, diskTotal uint64
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
			"ts": bts,
			"cpu": round2(a.cpu / n),
			"net_in": round2(a.netIn / n),
			"net_out": round2(a.netOut / n),
			"load1": round2(a.load1 / n),
			"mem_used": a.memUsed,
			"mem_total": a.memTotal,
			"disk_used": a.diskUsed,
			"disk_total": a.diskTotal,
		})
	}
	ok(c, gin.H{"period": period, "points": out})
}

// serverExec 立即在指定服务器执行命令（管理台调试用）。
func (s *Server) serverExec(c *gin.Context) {
	id := mustID(c)
	var req struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	s.auditLog(c, "server.exec", req.Command)
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

