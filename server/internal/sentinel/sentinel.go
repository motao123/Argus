// Package sentinel 服务监控哨兵（借鉴 nezha ServiceSentinel）：
// 周期向 Agent 下发 HTTP/TCP/Ping 探测任务，聚合结果与可用率。
package sentinel

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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

	// 探测状态均按 serviceID/serverID 隔离。
	failCount   map[probeKey]int
	lastProbeAt map[probeKey]time.Time
	// 延迟滑动窗口：probeKey → *DelayWindow（跨分钟，仅记录成功探测的延迟）
	windows sync.Map
	// NotifyCb 发送故障/恢复/证书事件；TriggerCb 分别执行故障和恢复任务。
	NotifyCb  func(svc *model.Service, kind string, detail string)
	TriggerCb func(svc *model.Service, up bool)
}

type probeKey struct {
	serviceID int64
	serverID  int64
}

// failNotify 通知故障/恢复（防抖：连续失败计数）。
func (s *Sentinel) failNotify(svc *model.Service, serverID int64, up bool) {
	if s.NotifyCb == nil && s.TriggerCb == nil {
		return
	}
	key := probeKey{serviceID: svc.ID, serverID: serverID}
	s.mu.Lock()
	if up {
		failed := s.failCount[key] >= 3
		s.failCount[key] = 0
		s.mu.Unlock()
		if failed {
			target := serviceForProbe(svc, serverID)
			if s.NotifyCb != nil && svc.Notify {
				s.NotifyCb(target, "recovered", "")
			}
			if s.TriggerCb != nil {
				s.TriggerCb(target, true)
			}
		}
		return
	}
	s.failCount[key]++
	count := s.failCount[key]
	s.mu.Unlock()
	if count == 3 {
		target := serviceForProbe(svc, serverID)
		if s.NotifyCb != nil && svc.Notify {
			s.NotifyCb(target, "failure", "")
		}
		if s.TriggerCb != nil {
			s.TriggerCb(target, false)
		}
	}
}

func serviceForProbe(svc *model.Service, serverID int64) *model.Service {
	copy := *svc
	copy.ServerID = serverID
	return &copy
}

func New(db *gorm.DB) *Sentinel {
	return &Sentinel{
		db: db, stop: make(chan struct{}), done: make(chan struct{}),
		failCount: make(map[probeKey]int), lastProbeAt: make(map[probeKey]time.Time),
	}
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

func (s *Sentinel) checkDue(peers map[int64]*rpc.Peer) {
	var services []model.Service
	if err := s.db.Where("enabled = ?", true).Find(&services).Error; err != nil || len(services) == 0 {
		return
	}
	serviceIDs := make([]int64, 0, len(services))
	for i := range services {
		serviceIDs = append(serviceIDs, services[i].ID)
	}
	var probes []model.ServiceProbe
	if err := s.db.Where("service_id IN ?", serviceIDs).Order("service_id, server_id").Find(&probes).Error; err != nil {
		return
	}
	byService := make(map[int64][]int64, len(services))
	for _, probe := range probes {
		byService[probe.ServiceID] = append(byService[probe.ServiceID], probe.ServerID)
	}
	now := time.Now()
	for i := range services {
		svc := &services[i]
		serverIDs := byService[svc.ID]
		if len(serverIDs) == 0 && svc.ServerID > 0 {
			serverIDs = []int64{svc.ServerID}
		}
		for _, serverID := range serverIDs {
			peer, ok := peers[serverID]
			if !ok {
				continue
			}
			key := probeKey{serviceID: svc.ID, serverID: serverID}
			s.mu.Lock()
			last, seen := s.lastProbeAt[key]
			interval := time.Duration(svc.Interval) * time.Second
			if interval <= 0 {
				interval = 60 * time.Second
			}
			due := !seen || now.Sub(last) >= interval
			if due {
				s.lastProbeAt[key] = now
			}
			s.mu.Unlock()
			if due {
				go s.probe(svc, serverID, peer)
			}
		}
	}
}

// probe 下发探测并记录结果（异步，避免阻塞扫描）。
func (s *Sentinel) probe(svc *model.Service, serverID int64, peer *rpc.Peer) {
	timeout := svc.Timeout
	if timeout <= 0 {
		timeout = 10
	}
	// 状态码列表（空 = 按区间判定）；写路径已校验，非法时静默忽略保持旧行为。
	statuses, _ := protocol.ParseStatuses(svc.ExpectedStatuses)
	resp, err := peer.Call(protocol.MethodServiceCheck, protocol.ServiceCheckParams{
		Type: svc.Type, Target: svc.Target, Timeout: timeout, Method: svc.HTTPMethod,
		VerifyTLS: svc.VerifyTLS, ExpectedStatusMin: svc.ExpectedStatusMin,
		ExpectedStatusMax: svc.ExpectedStatusMax, Statuses: statuses, MaxRedirects: svc.MaxRedirects,
		PingCount: svc.PingCount, Headers: parseRequestHeaders(svc.RequestHeaders),
		Body: svc.RequestBody, AssertContains: svc.AssertContains,
	}, time.Duration(timeout+5)*time.Second)
	if err != nil || resp.Error != nil {
		s.record(svc.ID, serverID, protocol.ServiceCheckResult{})
		s.failNotify(svc, serverID, false)
		return
	}
	var result protocol.ServiceCheckResult
	raw, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(raw, &result); err != nil {
		s.record(svc.ID, serverID, protocol.ServiceCheckResult{})
		s.failNotify(svc, serverID, false)
		return
	}
	s.record(svc.ID, serverID, result)
	s.failNotify(svc, serverID, result.Up)
	s.certNotify(svc, serverID, result)
}

// parseRequestHeaders 解析服务配置中的请求头 JSON（非法/空返回 nil，保持旧行为）。
func parseRequestHeaders(raw string) []protocol.KeyValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var headers []protocol.KeyValue
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return nil
	}
	out := headers[:0]
	for _, h := range headers {
		if strings.TrimSpace(h.Key) != "" {
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Sentinel) certNotify(svc *model.Service, serverID int64, result protocol.ServiceCheckResult) {
	if !svc.CertWarn || result.CertNotAfter == 0 || s.NotifyCb == nil {
		return
	}
	var probe model.ServiceProbe
	if err := s.db.FirstOrCreate(&probe, model.ServiceProbe{ServiceID: svc.ID, ServerID: serverID}).Error; err != nil {
		return
	}
	identity := result.CertIssuer + ":" + time.Unix(result.CertNotAfter, 0).UTC().Format(time.RFC3339)
	target := serviceForProbe(svc, serverID)
	if probe.LastCertIdentity != "" && probe.LastCertIdentity != identity {
		s.NotifyCb(target, "certificate_changed", identity)
	}
	warn := 0
	for _, days := range []int{1, 7, 30} {
		if result.CertDaysRemaining <= days {
			warn = days
			break
		}
	}
	if warn > 0 && probe.LastCertWarnDays != warn {
		s.NotifyCb(target, "certificate_expiring", fmt.Sprintf("%d", result.CertDaysRemaining))
	}
	s.db.Model(&probe).Updates(map[string]any{"last_cert_identity": identity, "last_cert_warn_days": warn})
}

// window 获取（必要时创建）服务的延迟滑动窗口。
func (s *Sentinel) window(key probeKey) *DelayWindow {
	v, _ := s.windows.LoadOrStore(key, &DelayWindow{})
	return v.(*DelayWindow)
}

// record writes and merges one result into its minute bucket. Zero-valued added
// fields keep results from pre-v2 agents valid (one sent/received probe).
func (s *Sentinel) record(serviceID, serverID int64, r protocol.ServiceCheckResult) {
	key := probeKey{serviceID: serviceID, serverID: serverID}
	sent, received := r.Sent, r.Received
	if sent == 0 {
		sent = 1
		if r.Up {
			received = 1
		}
	}
	// 滑动窗口：仅统计成功探测的延迟；样本不足 DelayMinSamples 时分位数落 0
	// （伴随 delay_samples < 30，API 侧输出 null）。
	p50, p95, p99, stddev, jitter, samples := 0, 0, 0, 0, 0, 0
	if r.Up {
		w := s.window(key)
		w.Add(r.DelayMs)
		samples = w.Len()
		if samples >= DelayMinSamples {
			p50, p95, p99, stddev, jitter, _ = w.Snapshot()
		}
	}
	h := model.ServiceHistory{ServiceID: serviceID, ServerID: serverID, Ts: time.Now().Unix() / 60 * 60,
		Total: 1, DelaySum: int64(r.DelayMs), DelayMin: r.DelayMs, DelayMax: r.DelayMs,
		DelayP50: p50, DelayP95: p95, DelayP99: p99, DelayStdDevMs: stddev, DelayJitterMs: jitter,
		DelaySamples: samples,
		Sent:         sent, Received: received, StatusCode: r.StatusCode, DNSMs: r.DNSMs,
		ConnectMs: r.ConnectMs, TLSMs: r.TLSMs, TTFBMs: r.TTFBMs,
		CertIssuer: r.CertIssuer, CertExpiry: r.CertNotAfter, LossSum: r.LossPercent}
	if r.Up {
		h.UpCount = 1
	}
	if r.CertNotAfter != 0 {
		days := r.CertDaysRemaining
		h.CertDays = &days
	}
	updates := map[string]any{
		"up_count":        gorm.Expr("up_count + excluded.up_count"),
		"total":           gorm.Expr("total + excluded.total"),
		"delay_sum":       gorm.Expr("delay_sum + excluded.delay_sum"),
		"delay_min":       gorm.Expr("CASE WHEN total = 0 OR excluded.delay_min < delay_min THEN excluded.delay_min ELSE delay_min END"),
		"delay_max":       gorm.Expr("CASE WHEN excluded.delay_max > delay_max THEN excluded.delay_max ELSE delay_max END"),
		"delay_p50":       gorm.Expr("excluded.delay_p50"),
		"delay_p95":       gorm.Expr("excluded.delay_p95"),
		"delay_p99":       gorm.Expr("excluded.delay_p99"),
		"delay_stddev_ms": gorm.Expr("excluded.delay_stddev_ms"),
		"delay_jitter_ms": gorm.Expr("excluded.delay_jitter_ms"),
		"delay_samples":   gorm.Expr("excluded.delay_samples"),
		"sent":            gorm.Expr("sent + excluded.sent"),
		"received":        gorm.Expr("received + excluded.received"),
		"loss_sum":        gorm.Expr("loss_sum + excluded.loss_sum"),
		"status_code":     gorm.Expr("excluded.status_code"),
		"dns_ms":          gorm.Expr("excluded.dns_ms"),
		"connect_ms":      gorm.Expr("excluded.connect_ms"),
		"tls_ms":          gorm.Expr("excluded.tls_ms"),
		"ttfb_ms":         gorm.Expr("excluded.ttfb_ms"),
		"cert_days":       gorm.Expr("CASE WHEN excluded.cert_days IS NULL THEN cert_days WHEN cert_days IS NULL OR excluded.cert_days < cert_days THEN excluded.cert_days ELSE cert_days END"),
		"cert_issuer":     gorm.Expr("CASE WHEN excluded.cert_issuer <> '' THEN excluded.cert_issuer ELSE cert_issuer END"),
		"cert_expiry":     gorm.Expr("CASE WHEN excluded.cert_expiry <> 0 THEN excluded.cert_expiry ELSE cert_expiry END"),
	}
	_ = s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "service_id"}, {Name: "server_id"}, {Name: "ts"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&h).Error
}
