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

	// 故障状态：serviceID → 连续失败次数
	failCount map[int64]int
	// 延迟滑动窗口：serviceID → *DelayWindow（跨分钟，仅记录成功探测的延迟）
	windows sync.Map
	// NotifyCb 发送故障/恢复/证书事件；TriggerCb 分别执行故障和恢复任务。
	NotifyCb  func(svc *model.Service, kind string, detail string)
	TriggerCb func(svc *model.Service, up bool)
}

// failNotify 通知故障/恢复（防抖：连续失败计数）。
func (s *Sentinel) failNotify(svc *model.Service, up bool) {
	if s.NotifyCb == nil && s.TriggerCb == nil {
		return
	}
	s.mu.Lock()
	if up {
		failed := s.failCount[svc.ID] >= 3
		s.failCount[svc.ID] = 0
		s.mu.Unlock()
		if failed {
			if s.NotifyCb != nil && svc.Notify {
				s.NotifyCb(svc, "recovered", "")
			}
			if s.TriggerCb != nil {
				s.TriggerCb(svc, true)
			}
		}
		return
	}
	s.failCount[svc.ID]++
	count := s.failCount[svc.ID]
	s.mu.Unlock()
	if count == 3 {
		if s.NotifyCb != nil && svc.Notify {
			s.NotifyCb(svc, "failure", "")
		}
		if s.TriggerCb != nil {
			s.TriggerCb(svc, false)
		}
	}
}

func New(db *gorm.DB) *Sentinel {
	return &Sentinel{db: db, stop: make(chan struct{}), done: make(chan struct{}), failCount: make(map[int64]int)}
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
		s.record(svc.ID, protocol.ServiceCheckResult{})
		s.failNotify(svc, false)
		return
	}
	var result protocol.ServiceCheckResult
	raw, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(raw, &result); err != nil {
		s.record(svc.ID, protocol.ServiceCheckResult{})
		s.failNotify(svc, false)
		return
	}
	s.record(svc.ID, result)
	s.failNotify(svc, result.Up)
	s.certNotify(svc, result)
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

func (s *Sentinel) certNotify(svc *model.Service, result protocol.ServiceCheckResult) {
	if !svc.CertWarn || result.CertNotAfter == 0 || s.NotifyCb == nil {
		return
	}
	identity := result.CertIssuer + ":" + time.Unix(result.CertNotAfter, 0).UTC().Format(time.RFC3339)
	if svc.LastCertIdentity != "" && svc.LastCertIdentity != identity {
		s.NotifyCb(svc, "certificate_changed", identity)
	}
	warn := 0
	for _, days := range []int{1, 7, 30} {
		if result.CertDaysRemaining <= days {
			warn = days
			break
		}
	}
	if warn > 0 && svc.LastCertWarnDays != warn {
		s.NotifyCb(svc, "certificate_expiring", fmt.Sprintf("%d", result.CertDaysRemaining))
	}
	s.db.Model(svc).Updates(map[string]any{"last_cert_identity": identity, "last_cert_warn_days": warn})
}

// window 获取（必要时创建）服务的延迟滑动窗口。
func (s *Sentinel) window(serviceID int64) *DelayWindow {
	v, _ := s.windows.LoadOrStore(serviceID, &DelayWindow{})
	return v.(*DelayWindow)
}

// record writes and merges one result into its minute bucket. Zero-valued added
// fields keep results from pre-v2 agents valid (one sent/received probe).
func (s *Sentinel) record(serviceID int64, r protocol.ServiceCheckResult) {
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
		w := s.window(serviceID)
		w.Add(r.DelayMs)
		samples = w.Len()
		if samples >= DelayMinSamples {
			p50, p95, p99, stddev, jitter, _ = w.Snapshot()
		}
	}
	h := model.ServiceHistory{ServiceID: serviceID, Ts: time.Now().Unix() / 60 * 60,
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
	var existing model.ServiceHistory
	if s.db.Where("service_id = ? AND ts = ?", serviceID, h.Ts).First(&existing).Error == nil {
		minDelay := existing.DelayMin
		if existing.Total == 0 || r.DelayMs < minDelay {
			minDelay = r.DelayMs
		}
		maxDelay := existing.DelayMax
		if r.DelayMs > maxDelay {
			maxDelay = r.DelayMs
		}
		certDays := existing.CertDays
		if h.CertDays != nil && (certDays == nil || *h.CertDays < *certDays) {
			certDays = h.CertDays
		}
		// 分位数写入当前窗口快照（最新一次探测为准）。
		updates := map[string]any{"up_count": existing.UpCount + h.UpCount, "total": existing.Total + 1,
			"delay_sum": existing.DelaySum + h.DelaySum, "delay_min": minDelay, "delay_max": maxDelay,
			"delay_p50": p50, "delay_p95": p95, "delay_p99": p99,
			"delay_stddev_ms": stddev, "delay_jitter_ms": jitter, "delay_samples": samples,
			"sent": existing.Sent + sent, "received": existing.Received + received,
			"loss_sum": existing.LossSum + r.LossPercent, "status_code": r.StatusCode,
			"dns_ms": r.DNSMs, "connect_ms": r.ConnectMs, "tls_ms": r.TLSMs, "ttfb_ms": r.TTFBMs}
		if certDays != nil {
			updates["cert_days"] = *certDays
		}
		if r.CertIssuer != "" {
			updates["cert_issuer"] = r.CertIssuer
			updates["cert_expiry"] = r.CertNotAfter
		}
		s.db.Model(&existing).Updates(updates)
		return
	}
	s.db.Create(&h)
}
