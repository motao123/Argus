// Package alert 实现报警规则状态机与 Webhook 通知。
// 设计借鉴 nezha AlertSentinel：周期快照规则 × 服务器，
// 指标持续超阈值达 duration 秒后触发，恢复时通知，单次触发避免轰炸。
package alert

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/maintenance"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifyctx"
	"github.com/motao123/Argus/server/internal/store"
	trafficquota "github.com/motao123/Argus/server/internal/traffic"
)

// Engine 报警引擎。
type Engine struct {
	db    *gorm.DB
	store *store.Hub
	// Trigger 联动任务执行器（kind 为 triggered/recovered）。
	Trigger func(cron *model.Cron, serverID int64, kind string)
	// AlertHook 报警触发/恢复事件回调（main 注入，如插件 onAlert hook）。
	AlertHook func(a *model.Alert, st store.State, value float64, kind string)
	// Notify 发送单条通知到指定渠道（main 注入 notifier.Queue.EnqueueCtx；
	// ownerID 为报警规则 owner，用于送达记录 owner/admin 隔离；
	// vars 为模板上下文变量表（notifyctx 展开），供渠道 Body 模板渲染）。
	Notify func(n *model.Notification, title, content string, ownerID int64, vars map[string]string)

	mu    sync.Mutex
	state map[string]*violation // key: alertID:serverID
	// baselines 累计流量规则（transfer_in/out/all）的计数基线（key: alertID:serverID）；
	// 首次评估时初始化为当前累计值，触发通知后重置（持久化于 model.AlertBaseline）。
	baselines map[string]transferBaseline
	stop      chan struct{}
	done      chan struct{}
	// nowFn 可注入时钟（测试用）；nil 时回退 time.Now。
	nowFn func() time.Time
}

// transferBaseline 累计流量规则的计数基线。
type transferBaseline struct {
	in  uint64
	out uint64
}

// violation 一条规则 × 一台服务器的触发状态。
type violation struct {
	triggeredAt time.Time
	notified    bool
	recovering  bool // 已恢复但通知未发（下一轮发恢复通知）
	// lastNotifyAt 上次通知时间（重复提醒间隔基准；0 = 尚未通知过）。
	lastNotifyAt time.Time
	// escalatedAt 升级时间（nil = 未升级）；升级后重复通知改发升级渠道。
	escalatedAt *time.Time
	// 达标比例采样窗口（借鉴 komari LoadNotification）
	sampleCount  int
	violateCount int
}

func NewEngine(db *gorm.DB, st *store.Hub) *Engine {
	e := &Engine{
		db:        db,
		store:     st,
		state:     make(map[string]*violation),
		baselines: make(map[string]transferBaseline),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	e.loadStates()
	return e
}

// now 返回引擎当前时间（可注入时钟，测试用）。
func (e *Engine) now() time.Time {
	if e.nowFn != nil {
		return e.nowFn()
	}
	return time.Now()
}

// Run 每 3s 检查一轮。
func (e *Engine) Run() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.stop:
			close(e.done)
			return
		case <-ticker.C:
			e.checkOnce()
			e.checkTrafficQuotas(time.Now())
		}
	}
}

// Stop 停止引擎。
func (e *Engine) Stop() {
	close(e.stop)
	<-e.done
}

func (e *Engine) checkOnce() {
	var alerts []model.Alert
	if err := e.db.Where("enabled = ?", true).Find(&alerts).Error; err != nil || len(alerts) == 0 {
		return
	}
	snap := e.store.Snapshot()

	// 仅当存在 offline 规则时查询维护窗口（避免每轮无谓查询）。
	var inMaint map[int64]bool
	var maintAll bool
	for i := range alerts {
		if alerts[i].Metric == "offline" {
			inMaint, maintAll, _ = maintenance.ActiveServerIDs(e.db, time.Now())
			break
		}
	}

	for _, a := range alerts {
		allowed := alertServerIDs(a.ServerIDs)
		for serverID, st := range snap {
			if a.OwnerID != 0 && st.Server.OwnerID != a.OwnerID {
				continue
			}
			if a.OwnerID != 0 && allowed != nil && !allowed[serverID] {
				continue
			}
			// 维护窗口内不判定 offline 告警（避免维护期误报）
			if a.Metric == "offline" && (maintAll || inMaint[serverID]) {
				continue
			}
			key := fmt.Sprintf("%d:%d", a.ID, serverID)
			value, ok := e.metricValue(&a, st)
			if !ok {
				continue // 无数据（如从未上报）
			}

			e.mu.Lock()
			v := e.state[key]
			e.mu.Unlock()

			// 达标比例采样（借鉴 komari LoadNotification：N% 采样超限才触发）
			ratio := 100
			if a.TriggerRatio != nil && *a.TriggerRatio > 0 && *a.TriggerRatio <= 100 {
				ratio = *a.TriggerRatio
			}
			fired := e.inRange(&a, value)
			now := e.now()

			if ratio < 100 {
				if v == nil {
					v = &violation{triggeredAt: now}
					e.mu.Lock()
					e.state[key] = v
					e.mu.Unlock()
				}
				v.sampleCount++
				if fired {
					v.violateCount++
				}
				// 每 10 次采样判定一次（约 30s 窗口）
				if v.sampleCount >= 10 {
					passRatio := v.violateCount * 100 / v.sampleCount
					fired = passRatio >= ratio
					v.sampleCount, v.violateCount = 0, 0
					if !fired {
						// 比例不达标视为未触发，重置开始时间
						v.triggeredAt = now
					}
				} else {
					fired = false // 采样窗口内不判定
				}
			}

			if fired {
				if v == nil {
					e.mu.Lock()
					e.state[key] = &violation{triggeredAt: now}
					e.mu.Unlock()
					continue
				}
				// 持续达 duration 秒 → 触发通知（仅一次）
				if !v.notified && now.Sub(v.triggeredAt) >= time.Duration(a.Duration)*time.Second {
					v.notified = true
					v.recovering = false
					v.lastNotifyAt = now
					e.persistState(&a, serverID, v)
					e.notify(&a, st, value, "triggered", true, false)
					// 累计流量规则：触发后重置基线（衡量自上次告警以来的流量）
					e.resetBaseline(&a, st)
				}
				// 升级：告警持续超过升级延迟后，发一条 event=escalated 并切换渠道
				// （实际投递后才记录升级，静默/确认/渠道缺失时不升级）。
				if v.notified && a.EscalateToChannelID > 0 && v.escalatedAt == nil &&
					now.Sub(v.triggeredAt) >= time.Duration(a.EscalateAfterMinutes)*time.Minute {
					if e.notify(&a, st, value, "escalated", false, true) {
						t := now
						v.escalatedAt = &t
						e.persistState(&a, serverID, v)
					}
				}
				// 重复提醒：持续期间每 N 分钟重发一次（升级后发往升级渠道）。
				if v.notified && a.RepeatMinutes > 0 && now.Sub(v.lastNotifyAt) >= time.Duration(a.RepeatMinutes)*time.Minute {
					if e.notify(&a, st, value, "repeat", false, v.escalatedAt != nil) {
						v.lastNotifyAt = now
						e.persistState(&a, serverID, v)
					}
				}
			} else {
				if v != nil {
					// 从触发态恢复 → 发恢复通知（如已通知过）；恢复同时清除确认状态
					if v.notified && !v.recovering {
						v.recovering = true
						e.clearAck(&a)
						e.notify(&a, st, value, "recovered", true, false)
					}
					// 恢复 60s 后清除状态（内存 + 持久化）
					if now.Sub(v.triggeredAt) > time.Duration(a.Duration)*time.Second+60*time.Second {
						e.clearState(&a, serverID)
						e.mu.Lock()
						delete(e.state, key)
						e.mu.Unlock()
					}
				}
			}
		}
	}
}

// metricValue 从状态快照提取规则指标值。
func (e *Engine) metricValue(a *model.Alert, st store.State) (float64, bool) {
	if a.Metric == "offline" {
		if st.Online {
			return 0, true
		}
		return 1, true
	}
	if !st.Online {
		return 0, false // 离线服务器不参与在线指标
	}
	switch a.Metric {
	case "cpu":
		return st.Last.CPU, true
	case "mem":
		if st.Last.MemTotal == 0 {
			return 0, false
		}
		return float64(st.Last.MemUsed) / float64(st.Last.MemTotal) * 100, true
	case "swap":
		if st.Last.SwapTotal == 0 {
			return 0, false
		}
		return float64(st.Last.SwapUsed) / float64(st.Last.SwapTotal) * 100, true
	case "disk":
		if st.Last.DiskTotal == 0 {
			return 0, false
		}
		return float64(st.Last.DiskUsed) / float64(st.Last.DiskTotal) * 100, true
	case "net_in_speed":
		return st.Last.NetInSpeed, true
	case "net_out_speed":
		return st.Last.NetOutSpeed, true
	case "net_all_speed":
		return st.Last.NetInSpeed + st.Last.NetOutSpeed, true
	case "load1":
		return st.Last.Load1, true
	case "load5":
		return st.Last.Load5, true
	case "load15":
		return st.Last.Load15, true
	case "tcp_conn_count":
		if st.Last.TCPEstablished > 0 {
			return float64(st.Last.TCPEstablished), true
		}
		return float64(st.Last.TCPCount), true // 旧 Agent 仅上报总连接数
	case "udp_conn_count":
		return float64(st.Last.UDPCount), true
	case "process_count":
		return float64(st.Last.ProcessCount), true
	case "temperature", "temperature_max":
		return st.Last.Temperature, true
	case "gpu", "gpu_max":
		return gpuUtilMax(st.Last), true
	case "latency":
		// 延迟以毫秒为单位；0 = 无测量（旧 Agent 未上报），不参与判定
		if st.LatencyMs <= 0 {
			return 0, false
		}
		return float64(st.LatencyMs), true
	case "transfer_in":
		b, ok := e.transferBaseline(a, st)
		if !ok {
			return 0, false
		}
		return float64(subBaseline(st.Last.NetInTransfer, b.in)), true
	case "transfer_out":
		b, ok := e.transferBaseline(a, st)
		if !ok {
			return 0, false
		}
		return float64(subBaseline(st.Last.NetOutTransfer, b.out)), true
	case "transfer_all":
		b, ok := e.transferBaseline(a, st)
		if !ok {
			return 0, false
		}
		return float64(subBaseline(st.Last.NetInTransfer, b.in) + subBaseline(st.Last.NetOutTransfer, b.out)), true
	case "traffic_in_cycle":
		used, _, ok := e.cycleTraffic(a, st.Server)
		return float64(used), ok
	case "traffic_out_cycle":
		_, used, ok := e.cycleTraffic(a, st.Server)
		return float64(used), ok
	default:
		return 0, false
	}
}

// gpuUtilMax 返回多卡 GPU 的最大利用率（兼容旧字段：无明细时用平均利用率）。
func gpuUtilMax(last protocol.ReportParams) float64 {
	max := last.GPUUtil
	for _, d := range last.GPU.Devices {
		if d.Util > max {
			max = d.Util
		}
	}
	return max
}

// subBaseline 计算累计值与基线的差值，Agent 重启（计数器归零）时按 0 处理。
func subBaseline(current, baseline uint64) uint64 {
	if current <= baseline {
		return 0
	}
	return current - baseline
}

// transferBaseline 返回规则 × 服务器的累计流量基线；
// 首次评估时以当前累计值为基线（自规则启用起计）并持久化。
func (e *Engine) transferBaseline(a *model.Alert, st store.State) (transferBaseline, bool) {
	key := fmt.Sprintf("%d:%d", a.ID, st.Server.ID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if b, ok := e.baselines[key]; ok {
		return b, true
	}
	b := transferBaseline{in: st.Last.NetInTransfer, out: st.Last.NetOutTransfer}
	e.baselines[key] = b
	if e.db != nil {
		_ = e.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "alert_id"}, {Name: "server_id"}},
			UpdateAll: true,
		}).Create(&model.AlertBaseline{
			AlertID: a.ID, ServerID: st.Server.ID, In: b.in, Out: b.out, UpdatedAt: time.Now(),
		}).Error
	}
	return b, true
}

// resetBaseline 触发通知后重置累计流量基线（衡量自上次告警以来的流量）。
func (e *Engine) resetBaseline(a *model.Alert, st store.State) {
	if !isTransferMetric(a.Metric) {
		return
	}
	key := fmt.Sprintf("%d:%d", a.ID, st.Server.ID)
	b := transferBaseline{in: st.Last.NetInTransfer, out: st.Last.NetOutTransfer}
	e.mu.Lock()
	e.baselines[key] = b
	e.mu.Unlock()
	if e.db != nil {
		_ = e.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "alert_id"}, {Name: "server_id"}},
			UpdateAll: true,
		}).Create(&model.AlertBaseline{
			AlertID: a.ID, ServerID: st.Server.ID, In: b.in, Out: b.out, UpdatedAt: time.Now(),
		}).Error
	}
}

func isTransferMetric(metric string) bool {
	switch metric {
	case "transfer_in", "transfer_out", "transfer_all":
		return true
	}
	return false
}

// cycleTraffic 计算周期流量规则的当前周期用量。
// 规则配置了 CycleStart（锚点 + 单位 + 间隔）时按锚点步进计算窗口并汇总 Transfer 表；
// 否则回退服务器配置的月度周期（与流量额度记账同口径）。
func (e *Engine) cycleTraffic(a *model.Alert, server *model.Server) (uint64, uint64, bool) {
	if e.db == nil {
		return 0, 0, false
	}
	now := e.now()
	if a.CycleStart != nil && trafficquota.ValidUnit(a.CycleUnit) {
		window, err := trafficquota.AnchorWindow(now, *a.CycleStart, a.CycleUnit, a.CycleInterval)
		if err != nil {
			return 0, 0, false
		}
		var total struct {
			In  uint64
			Out uint64
		}
		if err := e.db.Model(&model.Transfer{}).
			Select("COALESCE(SUM(`in`),0) AS `in`, COALESCE(SUM(`out`),0) AS `out`").
			Where("server_id = ? AND ts >= ? AND ts < ?", server.ID, window.Start.Unix(), window.End.Unix()).
			Scan(&total).Error; err != nil {
			return 0, 0, false
		}
		return total.In, total.Out, true
	}
	usage, err := trafficquota.CurrentUsage(e.db, server, now)
	if err != nil {
		return 0, 0, false
	}
	return usage.InBytes, usage.OutBytes, true
}

// checkTrafficQuotas persists threshold events before notifying. The unique event key
// makes each 80/90/100 threshold notification idempotent within one cycle.
func (e *Engine) checkTrafficQuotas(now time.Time) {
	if e.db == nil {
		return
	}
	var servers []model.Server
	if err := e.db.Where("traffic_quota_bytes > 0").Find(&servers).Error; err != nil {
		return
	}
	for i := range servers {
		usage, err := trafficquota.CurrentUsage(e.db, &servers[i], now)
		if err != nil || usage.Percentage == nil {
			continue
		}
		for _, threshold := range []int{80, 90, 100} {
			if *usage.Percentage < float64(threshold) {
				continue
			}
			event := model.TrafficQuotaEvent{ServerID: servers[i].ID, CycleStart: usage.CycleStart.Unix(), Threshold: threshold, UsageBytes: usage.AccountedBytes}
			result := e.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
			if result.Error != nil || result.RowsAffected == 0 {
				continue // already sent in this cycle, or persistence failed
			}
			e.notifyTrafficQuota(&servers[i], usage, threshold)
		}
	}
}

func (e *Engine) notifyTrafficQuota(server *model.Server, usage trafficquota.Usage, threshold int) {
	var report model.TrafficReport
	if err := e.db.First(&report).Error; err != nil || report.WebhookID <= 0 {
		return
	}
	var n model.Notification
	if err := e.db.First(&n, report.WebhookID).Error; err != nil {
		return
	}
	ctx := &notifyctx.Ctx{
		Event:     "traffic_quota",
		Metric:    "traffic_quota",
		Value:     fmt.Sprintf("%.2f%%", *usage.Percentage),
		Threshold: fmt.Sprintf("%d%%", threshold),
		Time:      notifyctx.FormatTime(time.Now()),
	}
	if st := e.store.Get(server.ID); st != nil {
		ctx.FromState(st)
	}
	title := fmt.Sprintf("[Argus] %s 流量额度 %d%%", server.Name, threshold)
	content := fmt.Sprintf("%s 当前周期 %s 至 %s，已计费 %d / %d bytes（%.2f%%）", server.Name, usage.CycleStart.Format(time.RFC3339), usage.CycleEnd.Format(time.RFC3339), usage.AccountedBytes, usage.QuotaBytes, *usage.Percentage)
	ctx.Title, ctx.Content = title, content
	if e.Notify != nil {
		e.Notify(&n, title, content, server.OwnerID, ctx.Flat())
	}
}

// inRange 判定指标是否落在触发区间（低于下限或高于上限即触发）。
func (e *Engine) inRange(a *model.Alert, v float64) bool {
	if a.Metric == "offline" {
		return v >= 1 // 离线即触发
	}
	if a.Min != nil && v < *a.Min {
		return true
	}
	if a.Max != nil && v > *a.Max {
		return true
	}
	return false
}

// silencedAt 判定规则在指定时刻是否处于静默窗口（起止时间内不通知；
// SilenceFrom 为空视为从现在起）。
func (e *Engine) silencedAt(a *model.Alert, now time.Time) bool {
	if a.SilenceTo == nil || !now.Before(*a.SilenceTo) {
		return false
	}
	if a.SilenceFrom != nil && now.Before(*a.SilenceFrom) {
		return false
	}
	return true
}

// clearAck 恢复时清除确认状态（内存 + DB）。
func (e *Engine) clearAck(a *model.Alert) {
	if a.AckedAt == nil {
		return
	}
	a.AckedAt, a.AckedBy = nil, ""
	if e.db != nil {
		_ = e.db.Model(&model.Alert{}).Where("id = ?", a.ID).
			Updates(map[string]any{"acked_at": nil, "acked_by": ""}).Error
	}
}

// notify 发送通知（多渠道）。
// sideEffects=true（triggered/recovered）附带插件 hook 与任务联动；
// repeat/escalated 仅为通知（不重复触发任务/插件）。
// escalated=true 时通知发往升级渠道（校验存在且 owner 匹配）。
// 返回是否实际投递（静默/确认/无目标渠道时返回 false，调用方据此推进重复/升级状态）。
func (e *Engine) notify(a *model.Alert, st store.State, value float64, kind string, sideEffects, escalated bool) bool {
	// 插件事件 hook（异步分发，不影响通知主流程）
	if sideEffects && e.AlertHook != nil {
		e.AlertHook(a, st, value, kind)
	}
	// 触发任务联动与通知解耦（借鉴 nezha 报警失败/恢复触发任务）
	if sideEffects && a.TriggerCronID > 0 && e.Trigger != nil {
		var cron model.Cron
		if err := e.db.First(&cron, a.TriggerCronID).Error; err == nil {
			// 联动 Cron 必须与报警规则同租户，并且目标服务器归属该租户。
			if cron.OwnerID != a.OwnerID || (a.OwnerID != 0 && st.Server.OwnerID != a.OwnerID) {
				return false
			}
			if targets := alertServerIDs(cron.ServerIDs); targets != nil && !targets[st.Server.ID] {
				return false
			}
			go e.Trigger(&cron, st.Server.ID, kind)
		}
	}
	if !a.Notify {
		return false
	}
	// 静默窗口内 / 已确认的告警不发送通知（任务联动与插件 hook 不受影响）
	now := e.now()
	if e.silencedAt(a, now) {
		log.Printf("alert %s (%s): silenced until %s, notification skipped", a.Name, kind, a.SilenceTo.Format(time.RFC3339))
		return false
	}
	if a.AckedAt != nil {
		log.Printf("alert %s (%s): acknowledged by %s, notification skipped", a.Name, kind, a.AckedBy)
		return false
	}
	serverName := st.Server.Name
	// 统一通知上下文：事件/服务器/规则/指标/阈值/时间，默认格式标题正文。
	ctx := &notifyctx.Ctx{
		Event:     kind,
		Rule:      a.Name,
		Metric:    a.Metric,
		Value:     fmt.Sprintf("%.2f", value),
		Threshold: alertThreshold(a, value),
		Time:      notifyctx.FormatTime(now),
	}
	ctx.FromState(&st)
	title := fmt.Sprintf("[Argus] %s %s", serverName, kind)
	content := fmt.Sprintf("%s: %s = %.2f", a.Name, a.Metric, value)
	if kind == "recovered" {
		content = fmt.Sprintf("%s: %s back to normal", serverName, a.Name)
	}
	// 规则自定义模板（可空）：首行为标题、其余为正文；空则用默认格式。
	if strings.TrimSpace(a.Template) != "" {
		title, content = renderAlertTemplate(a.Template, ctx, title)
	}
	ctx.Title, ctx.Content = title, content
	// 分组扇出（借鉴 nezha NotificationGroup）或单渠道；升级后仅发升级渠道
	targets := make([]model.Notification, 0)
	if escalated && a.EscalateToChannelID > 0 {
		var n model.Notification
		if err := e.db.First(&n, a.EscalateToChannelID).Error; err != nil {
			log.Printf("alert %s (%s): escalate channel #%d not found, notification dropped", a.Name, kind, a.EscalateToChannelID)
			return false
		}
		if a.OwnerID != 0 && n.OwnerID != a.OwnerID {
			log.Printf("alert %s (%s): escalate channel #%d owner mismatch, notification dropped", a.Name, kind, a.EscalateToChannelID)
			return false
		}
		targets = append(targets, n)
	} else if a.GroupID > 0 {
		var group model.NotificationGroup
		if err := e.db.First(&group, a.GroupID).Error; err == nil {
			for _, idStr := range strings.Split(group.MemberIDs, ",") {
				var n model.Notification
				if err := e.db.First(&n, parseInt64(strings.TrimSpace(idStr))).Error; err == nil {
					targets = append(targets, n)
				}
			}
		}
	} else if a.WebhookID > 0 {
		var n model.Notification
		if err := e.db.First(&n, a.WebhookID).Error; err == nil {
			targets = append(targets, n)
		}
	}
	vars := ctx.Flat()
	delivered := false
	for i := range targets {
		n := targets[i]
		if e.Notify != nil {
			e.Notify(&n, title, content, a.OwnerID, vars)
			delivered = true
		} else {
			log.Printf("alert notify: no queue wired, drop delivery to channel #%d", n.ID)
		}
	}
	log.Printf("alert %s (%s): %s", a.Name, kind, content)
	return delivered
}

// loadStates 启动时从 DB 恢复进行中的告警状态（重启后重复提醒/升级进度不丢失）。
// 恢复的条目视为已通知（避免重启后重复发送 triggered），lastNotifyAt/escalatedAt
// 决定后续重复/升级节奏。同时恢复累计流量规则的计数基线。
func (e *Engine) loadStates() {
	if e.db == nil {
		return
	}
	var rows []model.AlertState
	if err := e.db.Find(&rows).Error; err != nil {
		log.Printf("alert: load persisted states: %v", err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range rows {
		r := &rows[i]
		e.state[fmt.Sprintf("%d:%d", r.AlertID, r.ServerID)] = &violation{
			triggeredAt:  r.TriggeredAt,
			notified:     true,
			lastNotifyAt: r.LastNotifyAt,
			escalatedAt:  r.EscalatedAt,
		}
	}
	var baselines []model.AlertBaseline
	if err := e.db.Find(&baselines).Error; err == nil {
		for i := range baselines {
			b := &baselines[i]
			e.baselines[fmt.Sprintf("%d:%d", b.AlertID, b.ServerID)] = transferBaseline{in: b.In, out: b.Out}
		}
	}
}

// persistState 幂等持久化告警持续状态（重复/升级进度，重启恢复用）。
func (e *Engine) persistState(a *model.Alert, serverID int64, v *violation) {
	if e.db == nil {
		return
	}
	row := model.AlertState{
		AlertID:      a.ID,
		ServerID:     serverID,
		TriggeredAt:  v.triggeredAt,
		LastNotifyAt: v.lastNotifyAt,
		EscalatedAt:  v.escalatedAt,
	}
	_ = e.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "alert_id"}, {Name: "server_id"}},
		UpdateAll: true,
	}).Create(&row).Error
}

// clearState 恢复后清除持久化告警状态。
func (e *Engine) clearState(a *model.Alert, serverID int64) {
	if e.db == nil {
		return
	}
	_ = e.db.Where("alert_id = ? AND server_id = ?", a.ID, serverID).Delete(&model.AlertState{}).Error
	_ = e.db.Where("alert_id = ? AND server_id = ?", a.ID, serverID).Delete(&model.AlertBaseline{}).Error
}

// alertThreshold 触发时对应的阈值文本：低于下限取 min，高于上限取 max；
// offline 规则无阈值（返回空串）。
func alertThreshold(a *model.Alert, value float64) string {
	if a.Metric == "offline" {
		return ""
	}
	if a.Min != nil && value < *a.Min {
		return fmt.Sprintf("%.2f", *a.Min)
	}
	if a.Max != nil && value > *a.Max {
		return fmt.Sprintf("%.2f", *a.Max)
	}
	return ""
}

// renderAlertTemplate 规则自定义模板渲染：用上下文替换占位符后，
// 首行为标题、其余为正文；单行模板作为正文（标题保留默认）；
// 空行/空白标题回退默认标题。
func renderAlertTemplate(tmpl string, ctx *notifyctx.Ctx, defaultTitle string) (title, content string) {
	rendered := strings.TrimSpace(ctx.Render(tmpl))
	if rendered == "" {
		return defaultTitle, ""
	}
	if i := strings.IndexByte(rendered, '\n'); i >= 0 {
		title = strings.TrimSpace(rendered[:i])
		if title == "" {
			title = defaultTitle
		}
		return title, strings.TrimSpace(rendered[i+1:])
	}
	return defaultTitle, rendered
}

func alertServerIDs(raw string) map[int64]bool {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	ids := make(map[int64]bool)
	for _, part := range strings.Split(raw, ",") {
		if id := parseInt64(strings.TrimSpace(part)); id > 0 {
			ids[id] = true
		}
	}
	return ids
}

func parseInt64(str string) int64 {
	var n int64
	for _, ch := range str {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int64(ch-'0')
	}
	return n
}
