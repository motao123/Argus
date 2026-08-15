// Package alert 实现报警规则状态机与 Webhook 通知。
// 设计借鉴 nezha AlertSentinel：周期快照规则 × 服务器，
// 指标持续超阈值达 duration 秒后触发，恢复时通知，单次触发避免轰炸。
package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/store"
)

// Engine 报警引擎。
type Engine struct {
	db    *gorm.DB
	store *store.Hub

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
		for serverID, st := range snap {
			key := fmt.Sprintf("%d:%d", a.ID, serverID)
			value, ok := e.metricValue(&a, st)
			if !ok {
				continue // 无数据（如从未上报）
			}

			e.mu.Lock()
			v := e.state[key]
			e.mu.Unlock()

			fired := e.inRange(&a, value)
			now := time.Now()

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
	default:
		return 0, false
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

// notify 发送通知（Webhook）。
func (e *Engine) notify(a *model.Alert, st store.State, value float64, kind string) {
	if !a.Notify {
		return
	}
	var n model.Notification
	if err := e.db.First(&n, a.WebhookID).Error; err != nil {
		log.Printf("alert %s: notification #%d not found", a.Name, a.WebhookID)
		return
	}
	serverName := st.Server.Name
	title := fmt.Sprintf("[Argus] %s %s", serverName, kind)
	content := fmt.Sprintf("%s: %s = %.2f", a.Name, a.Metric, value)
	if kind == "recovered" {
		content = fmt.Sprintf("%s: %s back to normal", serverName, a.Name)
	}
	go sendWebhook(&n, title, content)
	log.Printf("alert %s (%s): %s", a.Name, kind, content)
}

// sendWebhook 按通知配置发送 HTTP 请求，Body 支持 {{title}}/{{content}} 模板。
func sendWebhook(n *model.Notification, title, content string) {
	method := n.Method
	if method == "" {
		method = "POST"
	}

	// Headers 与 Body 均为 JSON 字符串
	headers := map[string]string{}
	_ = json.Unmarshal([]byte(n.Headers), &headers)

	body := n.Body
	if strings.TrimSpace(body) == "" {
		body = `{"title":"{{title}}","content":"{{content}}"}`
	}
	body = strings.ReplaceAll(body, "{{title}}", escapeJSON(title))
	body = strings.ReplaceAll(body, "{{content}}", escapeJSON(content))

	req, err := http.NewRequest(method, n.URL, bytes.NewBufferString(body))
	if err != nil {
		log.Printf("webhook build failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("webhook send failed: %v", err)
		return
	}
	_ = resp.Body.Close()
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

var _ = protocol.MethodReport
