package scheduler

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
)

type fakeReply struct {
	result *protocol.ExecResult
	err    error
	wait   <-chan struct{}
}

type fakeExecutor struct {
	mu      sync.Mutex
	online  map[int64]bool
	replies map[int64]fakeReply
}

func (f *fakeExecutor) Peers() map[int64]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[int64]bool, len(f.online))
	for id, online := range f.online {
		out[id] = online
	}
	return out
}
func (f *fakeExecutor) Exec(id int64, _ string, _ int) (*protocol.ExecResult, error) {
	f.mu.Lock()
	reply := f.replies[id]
	f.mu.Unlock()
	if reply.wait != nil {
		<-reply.wait
	}
	return reply.result, reply.err
}

func newHistoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 每次调用独立内存库并强制单连接：
	// - 共享内存库按名字在进程内共享，-count=N 或全量并行时同名库会互相串扰；
	// - 单连接保证内部 goroutine 并发读写同一库不丢表/不锁冲突。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Server{}, &model.Cron{}, &model.TaskRun{}, &model.TaskRunResult{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedCron(t *testing.T, db *gorm.DB, serverCount int) (model.Cron, []model.Server) {
	t.Helper()
	owner := model.User{Username: "owner", Role: model.RoleUser}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	servers := make([]model.Server, serverCount)
	ids := make([]string, serverCount)
	for i := range servers {
		servers[i] = model.Server{Name: "server", Secret: "secret-" + string(rune('a'+i)), OwnerID: owner.ID}
		if err := db.Create(&servers[i]).Error; err != nil {
			t.Fatal(err)
		}
		ids[i] = formatID(servers[i].ID)
	}
	cr := model.Cron{OwnerID: owner.ID, Name: "task", Expression: "* * * * *", Command: "test", ServerIDs: strings.Join(ids, ","), Enabled: true, SkipIfRunning: true}
	if err := db.Create(&cr).Error; err != nil {
		t.Fatal(err)
	}
	return cr, servers
}

func waitRun(t *testing.T, db *gorm.DB, id int64) model.TaskRun {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var run model.TaskRun
		if db.Preload("Results").First(&run, id).Error == nil && run.Status != "queued" && run.Status != "running" {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %d did not finish", id)
	return model.TaskRun{}
}

func TestTaskRunSuccessAndOutputLimit(t *testing.T) {
	db := newHistoryDB(t)
	cr, servers := seedCron(t, db, 1)
	big := strings.Repeat("x", outputLimit+10)
	fake := &fakeExecutor{online: map[int64]bool{servers[0].ID: true}, replies: map[int64]fakeReply{servers[0].ID: {result: &protocol.ExecResult{Stdout: big, Stderr: "warning", Code: 0}}}}
	s := NewWithExecutor(db, fake)
	id, err := s.Enqueue(&cr, TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := waitRun(t, db, id)
	if run.Status != "success" || len(run.Results) != 1 || len(run.Results[0].Stdout) != outputLimit || !run.Results[0].Truncated {
		t.Fatalf("unexpected run: %#v results=%#v", run, run.Results)
	}
	var stored model.Cron
	db.First(&stored, cr.ID)
	if stored.LastResult == "" || stored.LastRunAt.IsZero() {
		t.Fatal("legacy last_result was not updated")
	}
}

func TestTaskRunPartialFailureOfflineAndTimeout(t *testing.T) {
	db := newHistoryDB(t)
	cr, servers := seedCron(t, db, 4)
	fake := &fakeExecutor{
		online: map[int64]bool{servers[0].ID: true, servers[1].ID: true, servers[2].ID: false, servers[3].ID: true},
		replies: map[int64]fakeReply{
			servers[0].ID: {result: &protocol.ExecResult{Stdout: "ok", Code: 0}},
			servers[1].ID: {result: &protocol.ExecResult{Stderr: "bad", Code: 2, Error: "exit status 2"}},
			servers[3].ID: {err: errors.New("context deadline exceeded")},
		},
	}
	s := NewWithExecutor(db, fake)
	id, _ := s.Enqueue(&cr, TriggerScheduled, nil)
	run := waitRun(t, db, id)
	if run.Status != "partial_failure" || len(run.Results) != 4 {
		t.Fatalf("status=%s results=%d", run.Status, len(run.Results))
	}
	statuses := map[string]int{}
	for _, result := range run.Results {
		statuses[result.Status]++
	}
	for _, status := range []string{"success", "failed", "offline", "timeout"} {
		if statuses[status] != 1 {
			t.Fatalf("statuses=%v", statuses)
		}
	}
}

func TestSkipIfRunningAndTriggerPersistence(t *testing.T) {
	db := newHistoryDB(t)
	cr, servers := seedCron(t, db, 1)
	gate := make(chan struct{})
	fake := &fakeExecutor{online: map[int64]bool{servers[0].ID: true}, replies: map[int64]fakeReply{servers[0].ID: {result: &protocol.ExecResult{Code: 0}, wait: gate}}}
	s := NewWithExecutor(db, fake)
	first, _ := s.Enqueue(&cr, TriggerAlertFailure, &servers[0].ID)
	second, _ := s.Enqueue(&cr, TriggerAlertRecovery, &servers[0].ID)
	var skipped model.TaskRun
	if err := db.First(&skipped, second).Error; err != nil || skipped.Status != "skipped" || skipped.Trigger != TriggerAlertRecovery {
		t.Fatalf("skipped=%#v err=%v", skipped, err)
	}
	close(gate)
	run := waitRun(t, db, first)
	if run.Trigger != TriggerAlertFailure {
		t.Fatalf("trigger=%s", run.Trigger)
	}
}

func TestRestartMarksRunningPersistentRunFailed(t *testing.T) {
	db := newHistoryDB(t)
	run := model.TaskRun{CronID: 9, OwnerID: 1, Trigger: TriggerManual, Status: "running", Command: "sleep"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	NewWithExecutor(db, &fakeExecutor{})
	if err := db.First(&run, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || !strings.Contains(run.Error, "restarted") || run.FinishedAt == nil {
		t.Fatalf("run after restart=%#v", run)
	}
}
