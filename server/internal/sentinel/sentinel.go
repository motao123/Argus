// Package sentinel 服务监控哨兵（借鉴 nezha ServiceSentinel）：
// 周期向 Agent 下发 HTTP/TCP/Ping 探测任务，聚合结果与可用率。
package sentinel

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/server/internal/model"
)

// Sentinel 服务监控哨兵。
type Sentinel struct {
	db *gorm.DB

	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

func New(db *gorm.DB) *Sentinel {
	return &Sentinel{db: db, stop: make(chan struct{}), done: make(chan struct{})}
}

// Run 每 5s 扫描一次，到期（距上次探测 >= interval）的服务触发探测。
func (s *Sentinel) Run(peers func() map[int64]*rpc.Peer) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			close(s.done)
			return
		case <-ticker.C:
			s.checkDue(peers())
		}
	}
}

func (s *Sentinel) Stop() {
	close(s.stop)
	<-s.done
}

// lastProbeAt 内存记录上次探测时间，避免频繁查 DB。
var lastProbeAt = struct {
	sync.Mutex
	m map[int64]time.Time
}{m: make(map[int64]time.Time)}

func (s *Sentinel) checkDue(peers map[int64]*rpc.Peer) {
	var services []model.Service
	if err := s.db.Where("enabled = ?", true).Find(&services).Error; err != nil || len(services) == 0 {
		return
	}
	now := time.Now()
	for i := range services {
		svc := &services[i]
		peer, ok := peers[svc.ServerID]
		if !ok {
			continue // 探测 agent 不在线
		}
		lastProbeAt.Lock()
		last, seen := lastProbeAt.m[svc.ID]
		lastProbeAt.Unlock()
		interval := time.Duration(svc.Interval) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		if seen && now.Sub(last) < interval {
			continue
		}
		lastProbeAt.Lock()
		lastProbeAt.m[svc.ID] = now
		lastProbeAt.Unlock()

		go s.probe(svc, peer)
	}
}

// probe 下发探测并记录结果（异步，避免阻塞扫描）。
func (s *Sentinel) probe(svc *model.Service, peer *rpc.Peer) {
	resp, err := peer.Call(protocol.MethodServiceCheck, protocol.ServiceCheckParams{
		Type:    svc.Type,
		Target:  svc.Target,
		Timeout: 10,
	}, 15*time.Second)
	if err != nil {
		s.record(svc.ID, false, 0)
		return
	}
	if resp.Error != nil {
		s.record(svc.ID, false, 0)
		return
	}
	var result protocol.ServiceCheckResult
	raw, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(raw, &result); err != nil {
		s.record(svc.ID, false, 0)
		return
	}
	s.record(svc.ID, result.Up, result.DelayMs)
}

// record 写入探测历史（分钟级桶聚合，每 5s 一次探测直接落库一条）。
func (s *Sentinel) record(serviceID int64, up bool, delayMs int) {
	h := model.ServiceHistory{
		ServiceID: serviceID,
		Ts:        time.Now().Unix() / 60 * 60,
	}
	if up {
		h.UpCount = 1
	}
	h.Total = 1
	h.DelaySum = int64(delayMs)

	// 同分钟已存在则累加
	var existing model.ServiceHistory
	err := s.db.Where("service_id = ? AND ts = ?", serviceID, h.Ts).First(&existing).Error
	if err == nil {
		s.db.Model(&existing).Updates(map[string]any{
			"up_count":  existing.UpCount + h.UpCount,
			"total":     existing.Total + h.Total,
			"delay_sum": existing.DelaySum + h.DelaySum,
		})
		return
	}
	s.db.Create(&h)
}

// 保留 30 天
func (s *Sentinel) Cleanup() {
	s.db.Where("ts < ?", time.Now().Add(-30*24*time.Hour).Unix()).Delete(&model.ServiceHistory{})
}

var _ = log.Printf
