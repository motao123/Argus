// Package store 提供内存优先的服务器状态区与分钟级指标降采样批处理。
// 设计借鉴 nezha：实时状态全在内存，DB 只做持久化；指标先缓冲再落库。
package store

import (
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
)

// State 单台服务器的运行时状态（内存态）。
type State struct {
	Server   *model.Server
	Host     protocol.HostInfo
	Last     protocol.ReportParams
	Online   bool
	LastSeen time.Time
}

// Hub 服务器运行时状态中心。
type Hub struct {
	mu      sync.RWMutex
	servers map[int64]*State
}

func NewHub() *Hub {
	return &Hub{servers: make(map[int64]*State)}
}

// Upsert 注册或更新服务器配置（Agent 注册时调用）。
func (h *Hub) Upsert(s *model.Server) *State {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.servers[s.ID]
	if !ok {
		st = &State{}
		h.servers[s.ID] = st
	}
	st.Server = s
	return st
}

// Remove 删除服务器状态（服务器被删除时调用）。
func (h *Hub) Remove(id int64) {
	h.mu.Lock()
	delete(h.servers, id)
	h.mu.Unlock()
}

// Get 按 ID 取状态，不存在返回 nil。
func (h *Hub) Get(id int64) *State {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.servers[id]
}

// SetReport 写入一次 Agent 上报，更新在线状态。
func (h *Hub) SetReport(id int64, host protocol.HostInfo, r *protocol.ReportParams) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.servers[id]
	if !ok {
		return
	}
	if host.Hostname != "" {
		st.Host = host
	}
	st.Last = *r
	st.Online = true
	st.LastSeen = time.Now()
}

// MarkOffline 标记服务器离线（检测到连接断开时调用）。
func (h *Hub) MarkOffline(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st, ok := h.servers[id]; ok {
		st.Online = false
	}
}

// SetOnline 标记上线（连接建立、鉴权通过时调用）。
func (h *Hub) SetOnline(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st, ok := h.servers[id]; ok {
		st.Online = true
		st.LastSeen = time.Now()
	}
}

// Snapshot 深拷贝全部服务器状态（供 WS 推送与前端查询）。
func (h *Hub) Snapshot() map[int64]State {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[int64]State, len(h.servers))
	for id, st := range h.servers {
		cp := *st
		if st.Server != nil {
			s := *st.Server
			cp.Server = &s
		}
		out[id] = cp
	}
	return out
}

// ---- 指标降采样 ----

// bucket 单服务器一分钟内的聚合缓冲。
type bucket struct {
	serverID   int64
	ts         int64 // 整分钟
	count      int
	cpuSum     float64
	memUsed    uint64
	memTotal   uint64
	diskUsed   uint64
	diskTotal  uint64
	netInSum   float64
	netOutSum  float64
	load1Sum   float64
}

// MetricBatcher 聚合 Agent 上报为分钟级指标并批量落库。
type MetricBatcher struct {
	db *gorm.DB

	mu      sync.Mutex
	buckets map[int64]*bucket // key: serverID
	stop    chan struct{}
	done    chan struct{}
}

func NewMetricBatcher(db *gorm.DB) *MetricBatcher {
	return &MetricBatcher{
		db:      db,
		buckets: make(map[int64]*bucket),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Feed 接收一条上报。
func (m *MetricBatcher) Feed(serverID int64, r *protocol.ReportParams) {
	minute := r.Timestamp / 60 * 60
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.buckets[serverID]
	if !ok || b.ts != minute {
		b = &bucket{serverID: serverID, ts: minute}
		m.buckets[serverID] = b
	}
	b.count++
	b.cpuSum += r.CPU
	b.memUsed = r.MemUsed
	b.memTotal = r.MemTotal
	b.diskUsed = r.DiskUsed
	b.diskTotal = r.DiskTotal
	b.netInSum += r.NetInSpeed
	b.netOutSum += r.NetOutSpeed
	b.load1Sum += r.Load1
}

// Run 每 60s flush 一次上一分钟数据。
func (m *MetricBatcher) Run() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			m.Flush()
			close(m.done)
			return
		case <-ticker.C:
			m.Flush()
		}
	}
}

// Flush 将完成的分钟桶写入 DB。
func (m *MetricBatcher) Flush() {
	nowMinute := time.Now().Unix() / 60 * 60
	m.mu.Lock()
	rows := make([]*model.Metric, 0, len(m.buckets))
	for id, b := range m.buckets {
		if b.ts >= nowMinute {
			continue // 当前分钟未结束
		}
		n := float64(b.count)
		rows = append(rows, &model.Metric{
			ServerID:   id,
			TS:         b.ts,
			CPU:        b.cpuSum / n,
			MemUsed:    b.memUsed,
			MemTotal:   b.memTotal,
			DiskUsed:   b.diskUsed,
			DiskTotal:  b.diskTotal,
			NetInSpeed: b.netInSum / n,
			NetOutSpeed: b.netOutSum / n,
			Load1:      b.load1Sum / n,
		})
		delete(m.buckets, id)
	}
	m.mu.Unlock()
	if len(rows) > 0 {
		m.db.CreateInBatches(rows, 200)
	}
}

// Stop 停止批处理。
func (m *MetricBatcher) Stop() {
	close(m.stop)
	<-m.done
}
