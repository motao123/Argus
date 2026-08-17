// 离线/上线通知哨兵（借鉴 komari notifier/offline 连接 ID 防抖）：
// 周期扫描服务器状态，离线超过阈值触发通知，恢复时通知。
package alert

import (
	"sync"
	"time"

	"github.com/motao123/Argus/server/internal/maintenance"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifyctx"
	"github.com/motao123/Argus/server/internal/store"
	"gorm.io/gorm"
)

// OfflineSentinel 离线通知哨兵。
type OfflineSentinel struct {
	db    *gorm.DB
	store *store.Hub
	// Notify 发送通知（main 注入 notifier.Queue.EnqueueCtx；ownerID 0 = 系统流程；
	// vars 为模板上下文变量表（notifyctx 展开），供渠道 Body 模板渲染）。
	Notify func(n *model.Notification, title, content string, ownerID int64, vars map[string]string)

	mu    sync.Mutex
	state map[int64]offlineState // serverID → 状态
	stop  chan struct{}
	done  chan struct{}
}

type offlineState struct {
	offlineSince time.Time // 连续离线开始时间
	notified     bool
	recovering   bool
}

func NewOfflineSentinel(db *gorm.DB, st *store.Hub) *OfflineSentinel {
	return &OfflineSentinel{db: db, store: st, state: make(map[int64]offlineState), stop: make(chan struct{}), done: make(chan struct{})}
}

func (s *OfflineSentinel) Run() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			close(s.done)
			return
		case <-ticker.C:
			s.check()
		}
	}
}

func (s *OfflineSentinel) Stop() {
	close(s.stop)
	<-s.done
}

func (s *OfflineSentinel) check() {
	var cfg model.OfflineNotify
	if err := s.db.First(&cfg).Error; err != nil || !cfg.Enabled || cfg.WebhookID <= 0 {
		return // 未配置则忽略
	}
	var n model.Notification
	if err := s.db.First(&n, cfg.WebhookID).Error; err != nil {
		return
	}

	snap := s.store.Snapshot()
	now := time.Now()
	// 维护窗口内的服务器不判离线（避免维护期误报）；窗口结束恢复原判定，
	// 维护前已离线并通知过的服务器在维护结束后正常补发恢复通知。
	inMaint, coversAll, _ := maintenance.ActiveServerIDs(s.db, now)
	for id, st := range snap {
		if coversAll || inMaint[id] {
			continue
		}
		key := id
		s.mu.Lock()
		state, seen := s.state[key]
		s.mu.Unlock()

		threshold := time.Duration(cfg.OfflineAfter) * time.Second
		if threshold <= 0 {
			threshold = 60 * time.Second
		}

		if !st.Online {
			if !seen {
				state = offlineState{offlineSince: now}
				s.mu.Lock()
				s.state[key] = state
				s.mu.Unlock()
				continue
			}
			if !state.notified && now.Sub(state.offlineSince) >= threshold {
				state.notified = true
				state.recovering = false
				s.mu.Lock()
				s.state[key] = state
				s.mu.Unlock()
				name := "未知"
				if st.Server != nil {
					name = st.Server.Name
				}
				ctx := &notifyctx.Ctx{
					Event:  "offline",
					Metric: "offline",
					Value:  cfg.OfflineAfterStr(),
					Time:   notifyctx.FormatTime(now),
				}
				ctx.FromState(&st)
				title := "[Argus] 服务器离线 " + name
				content := name + " 已离线超过 " + cfg.OfflineAfterStr()
				ctx.Title, ctx.Content = title, content
				s.sendNotify(&n, title, content, ctx)
			}
		} else if seen && state.notified {
			// 恢复
			s.mu.Lock()
			delete(s.state, key)
			s.mu.Unlock()
			name := "未知"
			if st.Server != nil {
				name = st.Server.Name
			}
			ctx := &notifyctx.Ctx{
				Event: "online",
				Time:  notifyctx.FormatTime(now),
			}
			ctx.FromState(&st)
			title := "[Argus] 服务器恢复 " + name
			content := name + " 已重新上线"
			ctx.Title, ctx.Content = title, content
			s.sendNotify(&n, title, content, ctx)
		}
	}
}

// sendNotify 发送离线/上线通知（ownerID 0 = 系统流程）。
func (s *OfflineSentinel) sendNotify(n *model.Notification, title, content string, ctx *notifyctx.Ctx) {
	if s.Notify != nil {
		s.Notify(n, title, content, 0, ctx.Flat())
	}
}
