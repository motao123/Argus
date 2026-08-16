// Package scheduler 调度 cron 定时任务：向目标 Agent 下发命令并记录结果。
package scheduler

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
)

// Scheduler cron 任务调度器。
type Scheduler struct {
	db     *gorm.DB
	agents *agent.Hub

	mu   sync.Mutex
	cron *cron.Cron
	ids  map[int64]cron.EntryID
}

func New(db *gorm.DB, agents *agent.Hub) *Scheduler {
	s := &Scheduler{
		db:     db,
		agents: agents,
		cron:   cron.New(),
		ids:    make(map[int64]cron.EntryID),
	}
	return s
}

// Start 启动调度器并加载全部已启用任务。
func (s *Scheduler) Start() {
	var crons []model.Cron
	if err := s.db.Where("enabled = ?", true).Find(&crons).Error; err == nil {
		for i := range crons {
			s.schedule(&crons[i])
		}
	}
	s.cron.Start()
	log.Printf("scheduler started with %d jobs", len(s.ids))
}

// Stop 停止调度器。
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// Upsert 新增或更新一个任务（API 层调用）。
func (s *Scheduler) Upsert(cr *model.Cron) {
	s.remove(cr.ID)
	if !cr.Enabled {
		return
	}
	s.schedule(cr)
}

// Remove 移除任务（API 层调用）。
func (s *Scheduler) Remove(id int64) {
	s.remove(id)
}

func (s *Scheduler) schedule(cr *model.Cron) {
	spec := strings.TrimSpace(cr.Expression)
	if spec == "" {
		return
	}
	id, err := s.cron.AddFunc(spec, func() {
		s.run(cr)
	})
	if err != nil {
		log.Printf("cron %s: bad expression %q: %v", cr.Name, spec, err)
		return
	}
	s.mu.Lock()
	s.ids[cr.ID] = id
	s.mu.Unlock()
}

func (s *Scheduler) remove(id int64) {
	s.mu.Lock()
	eid, ok := s.ids[id]
	delete(s.ids, id)
	s.mu.Unlock()
	if ok {
		s.cron.Remove(eid)
	}
}

// run 执行任务：向目标服务器下发命令。
func (s *Scheduler) run(cr *model.Cron) {
	ids := parseIDs(cr.ServerIDs)
	peers := s.agents.Peers()
	if len(ids) == 0 {
		// 空 = 全部在线服务器
		for id := range peers {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		s.recordResult(cr.ID, "no target servers online")
		return
	}

	var results []string
	for _, id := range ids {
		if _, ok := peers[id]; !ok {
			results = append(results, fmt.Sprintf("#%d: offline", id))
			continue
		}
		res, err := s.agents.Exec(id, cr.Command, 30)
		if err != nil {
			results = append(results, fmt.Sprintf("#%d: %v", id, err))
			continue
		}
		out := strings.TrimSpace(res.Output)
		if len(out) > 400 {
			out = out[:400] + "..."
		}
		results = append(results, fmt.Sprintf("#%d: exit=%d %s", id, res.Code, out))
	}
	s.recordResult(cr.ID, strings.Join(results, "\n"))
}

// recordResult 记录任务最近执行结果。
func (s *Scheduler) recordResult(id int64, result string) {
	s.db.Model(&model.Cron{}).Where("id = ?", id).
		Updates(map[string]any{"last_result": result, "last_run_at": time.Now()})
}

// RunOnce 手动触发一次（API 层调用，返回执行结果摘要）。
func (s *Scheduler) RunOnce(cr *model.Cron) string {
	ids := parseIDs(cr.ServerIDs)
	peers := s.agents.Peers()
	if len(ids) == 0 {
		for id := range peers {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "no target servers online"
	}
	var results []string
	for _, id := range ids {
		peer, ok := peers[id]
		if !ok {
			results = append(results, fmt.Sprintf("#%d: offline", id))
			continue
		}
		res, err := s.agents.Exec(id, cr.Command, 30)
		_ = peer
		if err != nil {
			results = append(results, fmt.Sprintf("#%d: %v", id, err))
			continue
		}
		results = append(results, fmt.Sprintf("#%d: exit=%d %s", id, res.Code, strings.TrimSpace(res.Output)))
	}
	out := strings.Join(results, "\n")
	s.recordResult(cr.ID, out)
	return out
}

func parseIDs(s string) []int64 {
	var ids []int64
	for _, p := range strings.Split(s, ",") {
		var id int64
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &id); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}
