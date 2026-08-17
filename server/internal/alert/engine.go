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

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifier"
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

	mu    sync.Mutex
	state map[string]*violation // key: alertID:serverID
	stop  chan struct{}
	done  chan struct{}
}

// violation 一条规则 × 一台服务器的触发状态。
type violation struct {
	triggeredAt time.Time
	notified    bool
	recovering  bool // 已恢复但通知未发（下一轮发恢复通知）
	// 达标比例采样窗口（借鉴 komari LoadNotification）
	sampleCount  int
	violateCount int
}

func NewEngine(db *gorm.DB, st *store.Hub) *Engine {
	return &Engine{
		db:    db,
		store: st,
		state: make(map[string]*violation),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
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

	for _, a := range alerts {
		allowed := alertServerIDs(a.ServerIDs)
		for serverID, st := range snap {
			if a.OwnerID != 0 && st.Server.OwnerID != a.OwnerID {
				continue
			}
			if a.OwnerID != 0 && allowed != nil && !allowed[serverID] {
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
			now := time.Now()

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
					e.notify(&a, st, value, "triggered")
				}
			} else {
				if v != nil {
					// 从触发态恢复 → 发恢复通知（如已通知过）
					if v.notified && !v.recovering {
						v.recovering = true
						e.notify(&a, st, value, "recovered")
					}
					// 恢复 60s 后清除状态
					if now.Sub(v.triggeredAt) > time.Duration(a.Duration)*time.Second+60*time.Second {
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
	case "disk":
		if st.Last.DiskTotal == 0 {
			return 0, false
		}
		return float64(st.Last.DiskUsed) / float64(st.Last.DiskTotal) * 100, true
	case "net_in_speed":
		return st.Last.NetInSpeed, true
	case "net_out_speed":
		return st.Last.NetOutSpeed, true
	case "load1":
		return st.Last.Load1, true
	case "traffic_in_cycle":
		used, _, ok := e.cycleTraffic(st.Server)
		return float64(used), ok
	case "traffic_out_cycle":
		_, used, ok := e.cycleTraffic(st.Server)
		return float64(used), ok
	default:
		return 0, false
	}
}

// cycleTraffic uses the same configured cycle as quota accounting and the usage API.
func (e *Engine) cycleTraffic(server *model.Server) (uint64, uint64, bool) {
	if e.db == nil {
		return 0, 0, false
	}
	usage, err := trafficquota.CurrentUsage(e.db, server, time.Now())
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
	title := fmt.Sprintf("[Argus] %s 流量额度 %d%%", server.Name, threshold)
	content := fmt.Sprintf("%s 当前周期 %s 至 %s，已计费 %d / %d bytes（%.2f%%）", server.Name, usage.CycleStart.Format(time.RFC3339), usage.CycleEnd.Format(time.RFC3339), usage.AccountedBytes, usage.QuotaBytes, *usage.Percentage)
	go notifier.Send(&n, title, content)
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

// notify 发送通知（多渠道）。
func (e *Engine) notify(a *model.Alert, st store.State, value float64, kind string) {
	// 插件事件 hook（异步分发，不影响通知主流程）
	if e.AlertHook != nil {
		e.AlertHook(a, st, value, kind)
	}
	// 触发任务联动与通知解耦（借鉴 nezha 报警失败/恢复触发任务）
	if a.TriggerCronID > 0 && e.Trigger != nil {
		var cron model.Cron
		if err := e.db.First(&cron, a.TriggerCronID).Error; err == nil {
			// 联动 Cron 必须与报警规则同租户，并且目标服务器归属该租户。
			if cron.OwnerID != a.OwnerID || (a.OwnerID != 0 && st.Server.OwnerID != a.OwnerID) {
				return
			}
			if targets := alertServerIDs(cron.ServerIDs); targets != nil && !targets[st.Server.ID] {
				return
			}
			go e.Trigger(&cron, st.Server.ID, kind)
		}
	}
	if !a.Notify {
		return
	}
	serverName := st.Server.Name
	title := fmt.Sprintf("[Argus] %s %s", serverName, kind)
	content := fmt.Sprintf("%s: %s = %.2f", a.Name, a.Metric, value)
	if kind == "recovered" {
		content = fmt.Sprintf("%s: %s back to normal", serverName, a.Name)
	}
	// 分组扇出（借鉴 nezha NotificationGroup）或单渠道
	targets := make([]model.Notification, 0)
	if a.GroupID > 0 {
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
	for i := range targets {
		n := targets[i]
		go notifier.Send(&n, title, content)
	}
	log.Printf("alert %s (%s): %s", a.Name, kind, content)
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
