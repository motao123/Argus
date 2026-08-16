// Package scheduler 调度 cron 定时任务：向目标 Agent 下发命令并持久化逐目标历史。
package scheduler

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
)

const outputLimit = 64 * 1024

const (
	TriggerScheduled     = "scheduled"
	TriggerManual        = "manual"
	TriggerAlertFailure  = "alert_failure"
	TriggerAlertRecovery = "alert_recovery"
)

// Executor 抽象 Agent 命令执行，便于可靠测试每种目标结果。
type Executor interface {
	Peers() map[int64]bool
	Exec(serverID int64, command string, timeout int) (*protocol.ExecResult, error)
}

type hubExecutor struct{ hub *agent.Hub }

func (e hubExecutor) Peers() map[int64]bool {
	out := make(map[int64]bool)
	for id := range e.hub.Peers() {
		out[id] = true
	}
	return out
}
func (e hubExecutor) Exec(id int64, command string, timeout int) (*protocol.ExecResult, error) {
	return e.hub.Exec(id, command, timeout)
}

// Scheduler cron 任务调度器。
type Scheduler struct {
	db       *gorm.DB
	executor Executor

	mu      sync.Mutex
	cron    *cron.Cron
	ids     map[int64]cron.EntryID
	running map[int64]int
}

func New(db *gorm.DB, agents *agent.Hub) *Scheduler {
	return NewWithExecutor(db, hubExecutor{hub: agents})
}

func NewWithExecutor(db *gorm.DB, executor Executor) *Scheduler {
	s := &Scheduler{db: db, executor: executor, cron: cron.New(), ids: make(map[int64]cron.EntryID), running: make(map[int64]int)}
	// 进程退出时未完成的记录不能永久保持 running。
	now := time.Now()
	db.Model(&model.TaskRun{}).Where("status = ?", "running").Updates(map[string]any{
		"status": "failed", "error": "server restarted while task was running", "finished_at": now,
	})
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

func (s *Scheduler) Stop() { s.cron.Stop() }

func (s *Scheduler) Upsert(cr *model.Cron) {
	s.remove(cr.ID)
	if cr.Enabled {
		s.schedule(cr)
	}
}
func (s *Scheduler) Remove(id int64) { s.remove(id) }

func (s *Scheduler) schedule(cr *model.Cron) {
	spec := strings.TrimSpace(cr.Expression)
	if spec == "" {
		return
	}
	cronID := cr.ID
	id, err := s.cron.AddFunc(spec, func() {
		var current model.Cron
		if s.db.First(&current, cronID).Error == nil {
			_, _ = s.Enqueue(&current, TriggerScheduled, nil)
		}
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

func (s *Scheduler) targetServers(cr *model.Cron, online map[int64]bool, onlyServerID *int64) []model.Server {
	requested := parseIDs(cr.ServerIDs)
	if onlyServerID != nil {
		requested = []int64{*onlyServerID}
	}
	var servers []model.Server
	q := s.db.Model(&model.Server{})
	var owner model.User
	ownerIsAdmin := s.db.First(&owner, cr.OwnerID).Error == nil && owner.Role == model.RoleAdmin
	if !ownerIsAdmin {
		q = q.Where("owner_id = ?", cr.OwnerID)
	}
	if len(requested) > 0 {
		q = q.Where("id IN ?", requested)
	} else {
		ids := make([]int64, 0, len(online))
		for id := range online {
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return nil
		}
		q = q.Where("id IN ?", ids)
	}
	if err := q.Find(&servers).Error; err != nil {
		return nil
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].ID < servers[j].ID })
	return servers
}

// targetIDs 保留用于目标隔离测试。
func (s *Scheduler) targetIDs(cr *model.Cron, online map[int64]bool) []int64 {
	servers := s.targetServers(cr, online, nil)
	ids := make([]int64, len(servers))
	for i := range servers {
		ids[i] = servers[i].ID
	}
	return ids
}

// Enqueue 建立持久化运行记录并异步执行。onlyServerID 用于告警联动的单目标运行。
func (s *Scheduler) Enqueue(cr *model.Cron, trigger string, onlyServerID *int64) (int64, error) {
	if cr == nil || cr.ID == 0 {
		return 0, errors.New("invalid cron")
	}
	s.mu.Lock()
	if cr.SkipIfRunning && s.running[cr.ID] > 0 {
		run := model.TaskRun{CronID: cr.ID, OwnerID: cr.OwnerID, Trigger: trigger, Status: "skipped", Command: cr.Command, Error: "task is already running"}
		err := s.db.Create(&run).Error
		s.mu.Unlock()
		return run.ID, err
	}
	s.running[cr.ID]++
	run := model.TaskRun{CronID: cr.ID, OwnerID: cr.OwnerID, Trigger: trigger, Status: "queued", Command: cr.Command}
	err := s.db.Create(&run).Error
	if err != nil {
		delete(s.running, cr.ID)
		s.mu.Unlock()
		return 0, err
	}
	s.mu.Unlock()
	go s.execute(cr.ID, run.ID, onlyServerID)
	return run.ID, nil
}

func (s *Scheduler) execute(cronID, runID int64, onlyServerID *int64) {
	defer func() {
		s.mu.Lock()
		if s.running[cronID] <= 1 {
			delete(s.running, cronID)
		} else {
			s.running[cronID]--
		}
		s.mu.Unlock()
	}()
	var cr model.Cron
	if err := s.db.First(&cr, cronID).Error; err != nil {
		s.finishRun(runID, "failed", 0, err.Error(), time.Now(), time.Now())
		return
	}
	started := time.Now()
	online := s.executor.Peers()
	targets := s.targetServers(&cr, online, onlyServerID)
	s.db.Model(&model.TaskRun{}).Where("id = ?", runID).Updates(map[string]any{"status": "running", "started_at": started, "target_count": len(targets)})
	if len(targets) == 0 {
		s.finishRun(runID, "failed", 0, "no target servers online", started, time.Now())
		s.recordLegacy(cr.ID, "no target servers online")
		return
	}

	results := make([]model.TaskRunResult, 0, len(targets))
	succeeded := 0
	for _, target := range targets {
		result := model.TaskRunResult{RunID: runID, ServerID: target.ID, ServerName: target.Name, ExitCode: -1}
		began := time.Now()
		if !online[target.ID] {
			result.Status, result.Error = "offline", "server offline"
		} else {
			res, err := s.executor.Exec(target.ID, cr.Command, 30)
			result.DurationMS = time.Since(began).Milliseconds()
			if err != nil {
				result.Status, result.Error = classifyExecError(err), err.Error()
			} else {
				result.ExitCode = res.Code
				stdout := res.Stdout
				stderr := res.Stderr
				if stdout == "" && stderr == "" { // 兼容旧 Agent 合并输出
					stdout = res.Output
				}
				result.Stdout, result.Truncated = truncateOutput(stdout)
				result.Stderr, result.Truncated = truncateMerge(stderr, result.Truncated)
				if res.Code == 0 && res.Error == "" {
					result.Status = "success"
					succeeded++
				} else {
					result.Status, result.Error = "failed", res.Error
				}
			}
		}
		if result.DurationMS == 0 {
			result.DurationMS = time.Since(began).Milliseconds()
		}
		results = append(results, result)
	}
	if err := s.db.Create(&results).Error; err != nil {
		log.Printf("task run #%d results: %v", runID, err)
	}
	status := "failed"
	if succeeded == len(results) {
		status = "success"
	} else if succeeded > 0 {
		status = "partial_failure"
	}
	finished := time.Now()
	s.finishRun(runID, status, len(targets), "", started, finished)
	s.recordLegacy(cr.ID, legacySummary(results))
}

func (s *Scheduler) finishRun(id int64, status string, targets int, runErr string, started, finished time.Time) {
	s.db.Model(&model.TaskRun{}).Where("id = ?", id).Updates(map[string]any{
		"status": status, "target_count": targets, "error": runErr, "started_at": started,
		"finished_at": finished, "duration_ms": finished.Sub(started).Milliseconds(),
	})
}

func (s *Scheduler) recordLegacy(id int64, result string) {
	if len(result) > 2048 {
		result = result[:2048]
	}
	s.db.Model(&model.Cron{}).Where("id = ?", id).Updates(map[string]any{"last_result": result, "last_run_at": time.Now()})
}

func classifyExecError(err error) string {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
		return "timeout"
	}
	if errors.Is(err, agent.ErrOffline) || strings.Contains(msg, "offline") {
		return "offline"
	}
	return "failed"
}

func truncateOutput(value string) (string, bool) {
	if len(value) <= outputLimit {
		return value, false
	}
	return value[:outputLimit], true
}
func truncateMerge(value string, already bool) (string, bool) {
	out, truncated := truncateOutput(value)
	return out, already || truncated
}

func legacySummary(results []model.TaskRunResult) string {
	lines := make([]string, 0, len(results))
	for _, r := range results {
		text := strings.TrimSpace(r.Stdout)
		if text == "" {
			text = r.Error
		}
		if len(text) > 400 {
			text = text[:400] + "..."
		}
		lines = append(lines, fmt.Sprintf("#%d: %s exit=%d %s", r.ServerID, r.Status, r.ExitCode, text))
	}
	return strings.Join(lines, "\n")
}

func parseIDs(value string) []int64 {
	var ids []int64
	for _, part := range strings.Split(value, ",") {
		var id int64
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &id); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}
