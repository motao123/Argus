// Package rpc 提供 WebSocket + JSON-RPC 2.0 对等端（Peer），
// 同时用于 Agent 侧与服务端侧，支持请求/应答与流式通知。
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/motao123/Argus/protocol"
)

// Handler 处理对端主动发来的方法调用。
type Handler interface {
	Handle(method string, params json.RawMessage) (any, *protocol.RPCError)
}

// CallContextHandler may honor cancellation and deadlines for long handlers.
type CallContextHandler interface {
	HandleContext(ctx context.Context, method string, params json.RawMessage) (any, *protocol.RPCError)
}

// 心跳参数（Agent ↔ Server 双向保活）。
const (
	// DefaultPingInterval 心跳 Ping 发送间隔：服务端每 30s 向 Agent 发 Ping，
	// Agent 收到后回 Pong；Agent 也以相同间隔主动 Ping，防止单向 NAT 映射老化。
	DefaultPingInterval = 30 * time.Second
	// DefaultReadTimeout 读超时：读循环在窗口内未收到任何数据帧或 Pong
	// （每收到一帧都会刷新）即判定连接死亡，触发统一的断开清理。
	DefaultReadTimeout = 75 * time.Second
)

// Peer 维护一条 WebSocket 长连接，双向复用：
// 发送请求（Call）、发送通知（Notify）、接收请求（Handler）。
// 心跳使用 WebSocket Ping/Pong 控制帧，与业务数据帧互不干扰：
// gorilla 在读取数据帧的同一读路径上处理控制帧，Pong 不会作为消息上抛。
type Peer struct {
	conn    *websocket.Conn
	handler Handler

	sendMu        sync.Mutex
	mu            sync.Mutex
	pending       map[uint64]chan *protocol.Response
	nextSeq       uint64
	closed        chan struct{}
	readTimeout   time.Duration // 读循环静默判定窗口（<=0 禁用）
	heartbeatOnce sync.Once

	// RTT 测量：pingLoop 每次发送 Ping 时记录发送时刻（UnixNano，0 = 未发送），
	// 收到 Pong 时若在窗口内则视为对最近一次 Ping 的应答，计算往返延迟。
	lastPingNano atomic.Int64
	hookMu       sync.RWMutex
	pongHook     func(rtt time.Duration) // 可选：每次测得的往返延迟回调（可为 nil）
}

// SetPongHook 注册 Pong 回调：每次收到响应本端 Ping 的 Pong 控制帧时，
// 以毫秒级精度调用 hook(rtt)。用于 Agent 侧测量 ↔ Server 的往返延迟；
// 传 nil 可取消。与 StartHeartbeat 配合使用（无心跳则收不到 Pong）。
func (p *Peer) SetPongHook(hook func(rtt time.Duration)) {
	p.hookMu.Lock()
	p.pongHook = hook
	p.hookMu.Unlock()
}

// New 创建 Peer，并安装控制帧处理器：
//   - 收到 Ping → 立即回 Pong（对端据此确认本端存活），同时刷新读超时
//   - 收到 Pong → 刷新读超时
func New(conn *websocket.Conn, handler Handler) *Peer {
	return newPeer(conn, handler, DefaultReadTimeout)
}

// newPeer 供测试注入更短的读超时；readTimeout<=0 表示不设读超时。
func newPeer(conn *websocket.Conn, handler Handler, readTimeout time.Duration) *Peer {
	p := &Peer{
		conn:        conn,
		handler:     handler,
		pending:     make(map[uint64]chan *protocol.Response),
		closed:      make(chan struct{}),
		readTimeout: readTimeout,
	}
	conn.SetPingHandler(func(appData string) error {
		// 显式回 Pong（gorilla 默认行为相同，这里显式写出并顺带刷新读超时）
		if err := conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second)); err != nil {
			return err
		}
		p.armReadDeadline()
		return nil
	})
	conn.SetPongHandler(func(string) error {
		p.armReadDeadline()
		p.firePongHook()
		return nil
	})
	return p
}

// firePongHook 计算最近一次 Ping 的往返延迟并回调（仅当 hook 已注册且
// Pong 在合理窗口内到达——超过 2 个心跳间隔视为陈旧 Pong，忽略）。
func (p *Peer) firePongHook() {
	p.hookMu.RLock()
	hook := p.pongHook
	p.hookMu.RUnlock()
	if hook == nil {
		return
	}
	if sent := p.lastPingNano.Load(); sent > 0 {
		if rtt := time.Since(time.Unix(0, sent)); rtt > 0 && rtt <= 2*DefaultPingInterval {
			hook(rtt)
		}
	}
}

// StartHeartbeat 周期发送 Ping 控制帧（interval<=0 时用 DefaultPingInterval），
// 对端收到后自动回 Pong，配合读超时保证半开连接（NAT 静默断链）
// 在 DefaultReadTimeout 内被发现。可随时调用（幂等），与 Call/Notify 并发安全。
func (p *Peer) StartHeartbeat(interval time.Duration) {
	p.heartbeatOnce.Do(func() {
		if interval <= 0 {
			interval = DefaultPingInterval
		}
		go p.pingLoop(interval)
	})
}

// pingLoop 定时发送 Ping；写失败通常意味着连接已死：
// 关闭底层连接让读循环退出，走统一的断开清理（关闭 pending、触发 Closed）。
func (p *Peer) pingLoop(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-p.closed:
			return
		case <-t.C:
			// 记录发送时刻：对端回 Pong 时据此计算往返延迟
			p.lastPingNano.Store(time.Now().UnixNano())
			if err := p.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				_ = p.conn.Close()
				return
			}
		}
	}
}

// armReadDeadline 刷新读超时：任何一帧数据（含 Pong、Ping）到达都会重置。
func (p *Peer) armReadDeadline() {
	if p.readTimeout > 0 {
		_ = p.conn.SetReadDeadline(time.Now().Add(p.readTimeout))
	}
}

// Closed 在连接关闭时触发。
func (p *Peer) Closed() <-chan struct{} { return p.closed }

// Conn 返回底层连接（供设置读写 deadline 等）。
func (p *Peer) Conn() *websocket.Conn { return p.conn }

// Call 发送请求并等待应答。
func (p *Peer) Call(method string, params any, timeout time.Duration) (*protocol.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return p.CallContext(ctx, method, params)
}

// CallContext sends a request and waits for its response or context cancellation.
func (p *Peer) CallContext(ctx context.Context, method string, params any) (*protocol.Response, error) {
	id := p.allocID()
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	req := protocol.Request{ID: json.RawMessage(fmt.Sprintf("%d", id)), Method: method, Params: raw}

	ch := make(chan *protocol.Response, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()

	if err := p.send(req); err != nil {
		p.dropPending(id)
		return nil, err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("connection closed")
		}
		return resp, nil
	case <-ctx.Done():
		p.dropPending(id)
		return nil, ctx.Err()
	case <-p.closed:
		return nil, errors.New("connection closed")
	}
}

// Notify 发送通知（无 ID，不需要应答）。
func (p *Peer) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return p.send(protocol.Request{Method: method, Params: raw})
}

// WriteRaw 直接写入任意 JSON 值（流式场景复用发送锁）。
func (p *Peer) WriteRaw(v any) error {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	if err := p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return p.conn.WriteJSON(v)
}

// wireMessage 统一的线上消息形态：请求带 method，应答不带。
type wireMessage struct {
	ID     json.RawMessage    `json:"id,omitempty"`
	Method string             `json:"method,omitempty"`
	Params json.RawMessage    `json:"params,omitempty"`
	Result json.RawMessage    `json:"result,omitempty"`
	Error  *protocol.RPCError `json:"error,omitempty"`
}

// ReadLoop 读取入站消息直到连接关闭，统一处理两种消息：
//   - 带 method：对端发来的请求 → 分发 Handler 并应答（若有 ID）
//   - 不带 method：本端发出请求的应答 → 按 ID 配对
func (p *Peer) ReadLoop() {
	defer func() {
		p.mu.Lock()
		for id, ch := range p.pending {
			delete(p.pending, id)
			close(ch)
		}
		p.mu.Unlock()
		close(p.closed)
		// 读超时判定连接死亡时 TCP 可能仍是半开状态，主动关闭底层连接，
		// 让对端与等待方（Call/外层循环）尽快感知。
		_ = p.conn.Close()
	}()

	for {
		// 每帧刷新读超时：窗口内没有任何数据帧/Pong（静默）即超时退出。
		p.armReadDeadline()
		var msg wireMessage
		if err := p.conn.ReadJSON(&msg); err != nil {
			// 读超时（心跳静默）或连接关闭：统一走上面的断开清理
			return
		}
		if msg.Method != "" {
			// Dispatch independently so a long handler cannot block reads.
			if p.handler != nil {
				go p.dispatch(msg)
			} else if len(msg.ID) > 0 {
				_ = p.WriteRaw(protocol.Response{
					ID:    msg.ID,
					Error: protocol.NewError(protocol.ErrMethod, "no handler"),
				})
			}
			continue
		}
		// 本端请求的应答
		id := idOf(msg.ID)
		p.mu.Lock()
		ch, ok := p.pending[id]
		if ok {
			delete(p.pending, id)
		}
		p.mu.Unlock()
		if ok {
			ch <- &protocol.Response{ID: msg.ID, Result: msg.Result, Error: msg.Error}
		}
	}
}

func (p *Peer) dispatch(msg wireMessage) {
	var result any
	var rpcErr *protocol.RPCError
	if h, ok := p.handler.(CallContextHandler); ok {
		result, rpcErr = h.HandleContext(context.Background(), msg.Method, msg.Params)
	} else {
		result, rpcErr = p.handler.Handle(msg.Method, msg.Params)
	}
	if len(msg.ID) > 0 {
		_ = p.WriteRaw(protocol.Response{ID: msg.ID, Result: result, Error: rpcErr})
	}
}

func (p *Peer) allocID() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextSeq++
	return p.nextSeq
}

func (p *Peer) dropPending(id uint64) {
	p.mu.Lock()
	if ch, ok := p.pending[id]; ok {
		delete(p.pending, id)
		close(ch)
	}
	p.mu.Unlock()
}

func (p *Peer) send(req protocol.Request) error {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	if err := p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return p.conn.WriteJSON(req)
}

// Close 关闭底层连接。
func (p *Peer) Close() error { return p.conn.Close() }

func idOf(raw json.RawMessage) uint64 {
	var id uint64
	_ = json.Unmarshal(raw, &id)
	return id
}
