// Package rtt 测量 Agent ↔ Server 的 WebSocket Ping→Pong 往返延迟。
//
// 复用 protocol/rpc.Peer 的心跳测量点：Peer.StartHeartbeat 每次发送
// Ping 控制帧时记录发送时刻，收到响应 Pong 后回调往返延迟；本包只负责
// 保存最近一次测量值并转为毫秒，供周期上报填充 ReportParams.LatencyMs。
package rtt

import (
	"sync"
	"time"
)

// Pinger 抽象：能注册 Pong 回调的对象（*rpc.Peer 实现；测试注入假实现）。
type Pinger interface {
	SetPongHook(hook func(rtt time.Duration))
}

// Meter 记录最近一次 Ping→Pong 往返延迟（毫秒粒度）。
// 并发安全：Pong 回调在 Peer 读循环 goroutine 触发，
// LatencyMs 在 Agent 上报 goroutine 读取。
type Meter struct {
	mu   sync.Mutex
	last time.Duration
}

// New 注册 Pong 回调并返回测量器；peer 为 nil 时返回空测量器（始终 0）。
func New(peer Pinger) *Meter {
	m := &Meter{}
	if peer != nil {
		peer.SetPongHook(m.observe)
	}
	return m
}

// observe 由 Peer 在每次收到响应 Ping 的 Pong 时调用（读循环 goroutine）。
func (m *Meter) observe(rtt time.Duration) {
	if rtt <= 0 {
		return
	}
	m.mu.Lock()
	m.last = rtt
	m.mu.Unlock()
}

// LatencyMs 返回最近一次往返延迟（毫秒）；0 = 尚无测量（或连接未建立）。
// 有测量但不足 1ms 时按 1ms 计，避免与"无数据"（0）混淆。
func (m *Meter) LatencyMs() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.last <= 0 {
		return 0
	}
	ms := int(m.last.Round(time.Millisecond) / time.Millisecond)
	if ms < 1 {
		ms = 1
	}
	return ms
}
