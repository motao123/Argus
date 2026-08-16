// Package rpc 提供 WebSocket + JSON-RPC 2.0 对等端（Peer），
// 同时用于 Agent 侧与服务端侧，支持请求/应答与流式通知。
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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

// Peer 维护一条 WebSocket 长连接，双向复用：
// 发送请求（Call）、发送通知（Notify）、接收请求（Handler）。
type Peer struct {
	conn    *websocket.Conn
	handler Handler

	sendMu  sync.Mutex
	mu      sync.Mutex
	pending map[uint64]chan *protocol.Response
	nextSeq uint64
	closed  chan struct{}
}

// New 创建 Peer。
func New(conn *websocket.Conn, handler Handler) *Peer {
	return &Peer{
		conn:    conn,
		handler: handler,
		pending: make(map[uint64]chan *protocol.Response),
		closed:  make(chan struct{}),
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
	}()

	for {
		var msg wireMessage
		if err := p.conn.ReadJSON(&msg); err != nil {
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
