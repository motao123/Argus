package api

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// topMetrics 资源排行支持的指标（值越大越靠前）。
// latency 依赖并行工作新增的 Agent↔Server RTT 测量（store.State.LatencyMs）。
var topMetrics = map[string]bool{
	"cpu": true, "mem": true, "disk": true,
	"net_in": true, "net_out": true, "latency": true,
}

// topServer 资源排行单行。
// Value 为排序值：cpu/mem/disk = 百分比，net_in/net_out = B/s，latency = 毫秒；
// Used/Total 仅 mem/disk 透传（前端展示用量），其余指标省略。
type topServer struct {
	ServerID   int64   `json:"server_id"`
	ServerName string  `json:"server_name"`
	Value      float64 `json:"value"`
	Used       uint64  `json:"used,omitempty"`
	Total      uint64  `json:"total,omitempty"`
}

// maxTopLimit 排行单次返回上限（防呆，避免超大 limit 拖垮响应）。
const maxTopLimit = 50

// serverTop 管理端资源排行：CPU/内存/磁盘/上下行速率/延迟 TOP N。
// GET /api/v1/admin/top?metric=cpu|mem|disk|net_in|net_out|latency&limit=10
// 数据直接取自 store 实时快照（无历史聚合）：
//   - 仅在线服务器参与排行；无快照（未注册/未上报）或离线一律跳过
//   - mem/disk 需 total > 0，否则跳过（无容量数据无从计算占比）
//   - latency 需已有 RTT 测量（旧 Agent / 未上报时 LatencyMs=0，跳过）
//
// 权限：admin 看全部服务器，普通用户/PAT 只看自己名下（owner_id 过滤 + PAT 白名单）。
func (s *Server) serverTop(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil {
		fail(c, http.StatusUnauthorized, "login required")
		return
	}
	metric := c.Query("metric")
	if !topMetrics[metric] {
		fail(c, http.StatusBadRequest, "invalid metric (cpu|mem|disk|net_in|net_out|latency)")
		return
	}
	limit := parseIntQuery(c, "limit", 10)
	if limit < 1 {
		limit = 10
	}
	if limit > maxTopLimit {
		limit = maxTopLimit
	}

	q := s.DB.Model(&model.Server{}).Order("sort_order, id")
	if !p.IsAdmin {
		q = q.Where("owner_id = ?", p.UserID)
	}
	var servers []model.Server
	if err := q.Find(&servers).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	snap := s.Store.Snapshot()
	type entry struct {
		id    int64
		name  string
		value float64
		used  uint64
		total uint64
	}
	entries := make([]entry, 0, len(servers))
	for i := range servers {
		if p.IsPAT && !p.canAccessServer(servers[i].ID) {
			continue // PAT 白名单外服务器不可见
		}
		st, ok := snap[servers[i].ID]
		if !ok || !st.Online {
			continue // 无快照或离线：不参与实时排行
		}
		var value float64
		var used, total uint64
		switch metric {
		case "cpu":
			value = st.Last.CPU
		case "mem":
			if st.Last.MemTotal == 0 {
				continue
			}
			value = float64(st.Last.MemUsed) / float64(st.Last.MemTotal) * 100
			used, total = st.Last.MemUsed, st.Last.MemTotal
		case "disk":
			if st.Last.DiskTotal == 0 {
				continue
			}
			value = float64(st.Last.DiskUsed) / float64(st.Last.DiskTotal) * 100
			used, total = st.Last.DiskUsed, st.Last.DiskTotal
		case "net_in":
			value = st.Last.NetInSpeed
		case "net_out":
			value = st.Last.NetOutSpeed
		case "latency":
			if st.LatencyMs <= 0 {
				continue // 尚无 RTT 测量（旧 Agent / 未上报）
			}
			value = float64(st.LatencyMs)
		}
		entries = append(entries, entry{id: servers[i].ID, name: servers[i].Name, value: value, used: used, total: total})
	}
	// 降序排列；同值按 id 升序，保证输出稳定
	sort.Slice(entries, func(a, b int) bool {
		if entries[a].value != entries[b].value {
			return entries[a].value > entries[b].value
		}
		return entries[a].id < entries[b].id
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]topServer, 0, len(entries))
	for _, e := range entries {
		out = append(out, topServer{ServerID: e.id, ServerName: e.name, Value: round2(e.value), Used: e.used, Total: e.total})
	}
	ok(c, gin.H{"metric": metric, "limit": limit, "servers": out})
}
