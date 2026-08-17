package rtt

import (
	"testing"
	"time"
)

// fakePeer 可注入的假 Peer：记录注册的 Pong 回调，测试中手动触发模拟 Pong。
type fakePeer struct {
	hook func(rtt time.Duration)
}

func (f *fakePeer) SetPongHook(hook func(rtt time.Duration)) { f.hook = hook }

func TestMeterLatencyMs(t *testing.T) {
	f := &fakePeer{}
	m := New(f)
	if f.hook == nil {
		t.Fatal("New 应注册 Pong 回调")
	}
	// 尚无测量 → 0
	if got := m.LatencyMs(); got != 0 {
		t.Fatalf("初始 LatencyMs() = %d, want 0", got)
	}
	// 一次 Pong → 毫秒值
	f.hook(150 * time.Millisecond)
	if got := m.LatencyMs(); got != 150 {
		t.Fatalf("LatencyMs() = %d, want 150", got)
	}
	// 多次更新取最近一次
	f.hook(42 * time.Millisecond)
	if got := m.LatencyMs(); got != 42 {
		t.Fatalf("更新后 LatencyMs() = %d, want 42", got)
	}
	// 不足 1ms 按 1ms 计（避免与「无数据」0 混淆）
	f.hook(300 * time.Microsecond)
	if got := m.LatencyMs(); got != 1 {
		t.Fatalf("亚毫秒 LatencyMs() = %d, want 1", got)
	}
	// 非法（<=0）测量忽略，保留上次有效值
	f.hook(-5 * time.Millisecond)
	if got := m.LatencyMs(); got != 1 {
		t.Fatalf("负值后 LatencyMs() = %d, want 1", got)
	}
}

func TestNewNilPeer(t *testing.T) {
	m := New(nil) // 不应 panic
	if got := m.LatencyMs(); got != 0 {
		t.Fatalf("nil peer LatencyMs() = %d, want 0", got)
	}
}

func TestMeterConcurrentSafe(t *testing.T) {
	f := &fakePeer{}
	m := New(f)
	done := make(chan struct{})
	go func() { // 模拟 Peer 读循环 goroutine 高频回调
		for i := 0; i < 1000; i++ {
			f.hook(time.Duration(i) * time.Millisecond)
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ { // 模拟上报 goroutine 并发读取
		_ = m.LatencyMs()
	}
	<-done
	if got := m.LatencyMs(); got != 999 {
		t.Fatalf("并发后 LatencyMs() = %d, want 999", got)
	}
}
