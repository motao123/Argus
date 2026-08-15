// 离线/上线通知哨兵（借鉴 komari notifier/offline 连接 ID 防抖）：
// 周期扫描服务器状态，离线超过阈值触发通知，恢复时通知。
package alert

import (
	"sync"
	"time"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifier"
	"github.com/motao123/Argus/server/internal/store"
	"gorm.io/gorm"
)

// OfflineSentinel 离线通知哨兵。
type OfflineSentinel struct {
	db    *gorm.DB
	store *store.Hub

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
	for id, st := range snap {
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
				go notifier.Send(&n, "[Argus] 服务器离线 "+name, name+" 已离线超过 "+cfg.OfflineAfterStr())
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
			go notifier.Send(&n, "[Argus] 服务器恢复 "+name, name+" 已重新上线")
		}
	}
}
