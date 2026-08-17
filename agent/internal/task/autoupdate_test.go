package task

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/motao123/Argus/protocol"
)

// fakeCaller 记录调用并返回可编程的应答。
type fakeCaller struct {
	mu       sync.Mutex
	calls    int
	failures int // 前 N 次调用返回错误
	updates  []protocol.CheckUpdateResult
}

func (f *fakeCaller) call(method string, params any, timeout time.Duration) (*protocol.Response, error) {
	f.mu.Lock()
	i := f.calls
	f.calls++
	f.mu.Unlock()
	if i < f.failures {
		return nil, errors.New("rpc error")
	}
	// 失败次数耗尽后重复最后一个应答（模拟服务端持续返回同一更新）。
	u := protocol.CheckUpdateResult{}
	if len(f.updates) > 0 {
		idx := i - f.failures
		if idx >= len(f.updates) {
			idx = len(f.updates) - 1
		}
		u = f.updates[idx]
	}
	return &protocol.Response{Result: mustRaw(u)}, nil
}

func (f *fakeCaller) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func mustRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// newTestUpdater 构造周期极小、随机数固定的检查器，便于快速触发。
func newTestUpdater(caller *fakeCaller, current string) (*AutoUpdater, *fakeUpgrader) {
	up := &fakeUpgrader{}
	u := &AutoUpdater{
		Call:           caller.call,
		CurrentVersion: current,
		Enabled:        true,
		MinInterval:    1 * time.Millisecond,
		MaxInterval:    2 * time.Millisecond,
		RandFloat:      func() float64 { return 0.5 },
		Upgrade:        up.upgrade,
		Logf:           func(string, ...any) {},
	}
	return u, up
}

type fakeUpgrader struct {
	mu     sync.Mutex
	calls  []*protocol.UpgradeParams
	errors int // 前 N 次升级返回错误
	done   chan struct{}
}

func (f *fakeUpgrader) upgrade(p *protocol.UpgradeParams) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p)
	// 仅在升级成功时通知（失败只记日志，进入下一周期重试）
	if len(f.calls) > f.errors {
		if f.done != nil {
			select {
			case f.done <- struct{}{}:
			default:
			}
		}
		return "upgraded, restarting", nil
	}
	return "", errors.New("download failed")
}

func (f *fakeUpgrader) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeUpgrader) last() *protocol.UpgradeParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

const testSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestAutoUpdaterTriggersUpgrade(t *testing.T) {
	caller := &fakeCaller{updates: []protocol.CheckUpdateResult{{Version: "0.2.0", URL: "https://example.com/agent", SHA256: testSHA}}}
	u, up := newTestUpdater(caller, "0.1.0")
	up.done = make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go u.Run(ctx)

	select {
	case <-up.done:
	case <-time.After(2 * time.Second):
		t.Fatal("upgrade was not triggered")
	}
	cancel()

	if got := up.callCount(); got != 1 {
		t.Fatalf("upgrade calls = %d, want 1", got)
	}
	if p := up.last(); p == nil || p.Version != "0.2.0" || p.URL != "https://example.com/agent" || p.SHA256 != testSHA {
		t.Fatalf("unexpected upgrade params: %+v", p)
	}
	if caller.count() < 1 {
		t.Fatal("no check_update calls made")
	}
}

func TestAutoUpdaterSkipsSameVersion(t *testing.T) {
	caller := &fakeCaller{updates: []protocol.CheckUpdateResult{{Version: "0.1.0", URL: "https://example.com/agent", SHA256: testSHA}}}
	u, up := newTestUpdater(caller, "0.1.0")
	ctx, cancel := context.WithCancel(context.Background())
	go u.Run(ctx)
	// 运行一段时间：检查周期 1-2ms，50ms 内应有多次检查
	time.Sleep(50 * time.Millisecond)
	cancel()

	if caller.count() == 0 {
		t.Fatal("expected periodic checks to happen")
	}
	if up.callCount() != 0 {
		t.Fatalf("upgrade calls = %d, want 0 (same version)", up.callCount())
	}
}

func TestAutoUpdaterDisabledDoesNotCheck(t *testing.T) {
	caller := &fakeCaller{}
	u, _ := newTestUpdater(caller, "0.1.0")
	u.Enabled = false
	u.Run(context.Background())
	if caller.count() != 0 {
		t.Fatalf("check calls = %d, want 0 (disabled)", caller.count())
	}
}

func TestAutoUpdaterNilRunDoesNotPanic(t *testing.T) {
	var u *AutoUpdater
	u.Run(context.Background()) // 不 panic
}

func TestAutoUpdaterUpgradeFailureKeepsRunning(t *testing.T) {
	caller := &fakeCaller{updates: []protocol.CheckUpdateResult{{Version: "0.2.0", URL: "https://example.com/agent", SHA256: testSHA}}}
	u, up := newTestUpdater(caller, "0.1.0")
	up.errors = 1 // 第一次升级失败（如下载/校验失败）
	up.done = make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go u.Run(ctx)

	select {
	case <-up.done:
	case <-time.After(2 * time.Second):
		t.Fatal("upgrade did not recover after failure")
	}
	cancel()

	if up.callCount() < 2 {
		t.Fatalf("upgrade calls = %d, want >= 2 (retried next cycle)", up.callCount())
	}
}

func TestAutoUpdaterCheckFailureKeepsRunning(t *testing.T) {
	caller := &fakeCaller{failures: 3, updates: []protocol.CheckUpdateResult{{Version: "0.2.0", URL: "https://example.com/agent", SHA256: testSHA}}}
	u, up := newTestUpdater(caller, "0.1.0")
	up.done = make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go u.Run(ctx)

	select {
	case <-up.done:
	case <-time.After(2 * time.Second):
		t.Fatal("upgrade did not run after RPC failures")
	}
	cancel()

	if caller.count() < 4 {
		t.Fatalf("check calls = %d, want >= 4 (3 failures + success)", caller.count())
	}
}

func TestAutoUpdaterEmptyResultSkips(t *testing.T) {
	caller := &fakeCaller{updates: []protocol.CheckUpdateResult{{}}} // 服务端未配置 = 无更新
	u, up := newTestUpdater(caller, "0.1.0")
	ctx, cancel := context.WithCancel(context.Background())
	go u.Run(ctx)
	time.Sleep(30 * time.Millisecond)
	cancel()

	if up.callCount() != 0 {
		t.Fatalf("upgrade calls = %d, want 0 (empty result)", up.callCount())
	}
}

func TestAutoUpdaterNextIntervalRange(t *testing.T) {
	u, _ := newTestUpdater(&fakeCaller{}, "0.1.0")
	u.MinInterval, u.MaxInterval = 30*time.Minute, 90*time.Minute
	for _, r := range []float64{0, 0.25, 0.5, 0.999} {
		u.RandFloat = func() float64 { return r }
		d := u.nextInterval()
		if d < u.MinInterval || d >= u.MaxInterval {
			t.Fatalf("nextInterval(%v) = %v, out of [30m, 90m)", r, d)
		}
	}
	// NaN / 越界兜底
	for _, bad := range []float64{-1, 1, 1.5, math.NaN()} {
		u.RandFloat = func() float64 { return bad }
		if d := u.nextInterval(); d < u.MinInterval || d >= u.MaxInterval {
			t.Fatalf("nextInterval(%v) = %v, out of range", bad, d)
		}
	}
}
