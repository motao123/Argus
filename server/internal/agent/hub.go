// Package agent 管理 Agent WebSocket 连接：
// 注册/鉴权、上报写入状态区、任务下发与终端中继。
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/store"
)

// Hub 持有全部 Agent 连接。
type Hub struct {
	db      *gorm.DB
	store   *store.Hub
	batcher *store.MetricBatcher

	// TermDataCb Agent 终端输出回调（由 API 层注册，转发给浏览器）。
	TermDataCb func(serverID int64, data protocol.TerminalData)
	// IPChangeCb 服务器公网 IP 变化回调（DDNS 触发用）。
	IPChangeCb func(serverID int64, newIP string)

	mu      sync.RWMutex
	conns   map[int64]*rpc.Peer // serverID → 连接
	ipCache map[int64]string    // serverID → 最近 IP
}

func NewHub(db *gorm.DB, st *store.Hub, batcher *store.MetricBatcher) *Hub {
	return &Hub{
		db:      db,
		store:   st,
		batcher: batcher,
		conns:   make(map[int64]*rpc.Peer),
	}
}

// ipCache 最近上报 IP（serverID → IP），用于 DDNS 变更检测。
func (h *Hub) lastIP(serverID int64) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.ipCache == nil {
		return ""
	}
	return h.ipCache[serverID]
}

func (h *Hub) setLastIP(serverID int64, ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ipCache == nil {
		h.ipCache = make(map[int64]string)
	}
	h.ipCache[serverID] = ip
}

// Peers 返回全部在线连接（供调度器/报警器下发）。
func (h *Hub) Peers() map[int64]*rpc.Peer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[int64]*rpc.Peer, len(h.conns))
	for id, p := range h.conns {
		out[id] = p
	}
	return out
}

// Peer 取单台服务器连接，不存在返回 nil。
func (h *Hub) Peer(id int64) *rpc.Peer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[id]
}

// Serve 处理一条 Agent 连接（阻塞直到断开）。
func (h *Hub) Serve(conn *websocket.Conn) {
	h2 := &connHandler{hub: h}
	peer := rpc.New(conn, h2)
	h2.peer = peer
	peer.ReadLoop()

	if id, ok := h.idOf(peer); ok {
		h.detach(id, peer)
		log.Printf("agent #%d disconnected", id)
	}
}

// agentHandler 处理来自 Agent 的调用/通知。
type connHandler struct {
	hub  *Hub
	peer *rpc.Peer
}

func (ch *connHandler) Handle(method string, params json.RawMessage) (any, *protocol.RPCError) {
	switch method {
	case protocol.MethodRegister:
		return ch.handleRegister(params)
	case protocol.MethodReport:
		return ch.handleReport(params)
	case protocol.MethodTermData:
		// Agent 终端输出 → 回调给 API 层转发浏览器
		var d protocol.TerminalData
		if err := json.Unmarshal(params, &d); err != nil {
			return nil, protocol.NewError(protocol.ErrParams, err.Error())
		}
		if id, ok := ch.hub.idOf(ch.peer); ok && ch.hub.TermDataCb != nil {
			ch.hub.TermDataCb(id, d)
		}
		return nil, nil
	case protocol.MethodTermClose:
		// Agent 会话结束通知（无需处理，浏览器侧由连接关闭感知）
		return nil, nil
	default:
		return nil, protocol.NewError(protocol.ErrMethod, "unknown method: "+method)
	}
}

// handleRegister 首次注册（无密钥）或带密钥连接。
func (ch *connHandler) handleRegister(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.RegisterParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}

	var srv model.Server
	if p.Secret == "" {
		// 首次注册：创建服务器并生成密钥
		srv = model.Server{Name: "Server", Secret: GenSecret()}
		if err := ch.hub.db.Create(&srv).Error; err != nil {
			return nil, protocol.NewError(protocol.ErrInternal, err.Error())
		}
	} else {
		if err := ch.hub.db.Where("secret = ?", p.Secret).First(&srv).Error; err != nil {
			return nil, protocol.NewError(protocol.ErrUnauthorized, "invalid secret")
		}
	}

	ch.hub.attach(srv.ID, ch.peer)
	ch.hub.store.Upsert(&srv)
	ch.hub.store.SetOnline(srv.ID)
	log.Printf("agent %s (#%d) registered", srv.Name, srv.ID)
	return protocol.RegisterResult{ServerID: srv.ID, Secret: srv.Secret}, nil
}

// handleReport 周期状态上报。首次携带 Host 信息时同步更新。
func (ch *connHandler) handleReport(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.ReportParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	id, ok := ch.hub.idOf(ch.peer)
	if !ok {
		return nil, protocol.NewError(protocol.ErrUnauthorized, "not registered")
	}
	if p.Host.Hostname != "" {
		// 主机名变更则同步到 DB
		var srv model.Server
		if err := ch.hub.db.First(&srv, id).Error; err == nil && srv.Name == "Server" {
			srv.Name = p.Host.Hostname
			ch.hub.db.Model(&srv).Update("name", srv.Name)
		}
		// IP 变化触发 DDNS 回调（每 30s 至多一次，避免频繁）
		if ch.hub.IPChangeCb != nil && p.Host.IP != "" {
			if lastIP := ch.hub.lastIP(id); lastIP != p.Host.IP {
				ch.hub.setLastIP(id, p.Host.IP)
				go ch.hub.IPChangeCb(id, p.Host.IP)
			}
		}
	}
	ch.hub.store.SetReport(id, p.Host, &p)
	ch.hub.batcher.Feed(id, &p)
	return nil, nil
}

// attach 记录连接与服务器绑定。
func (h *Hub) attach(serverID int64, peer *rpc.Peer) {
	h.mu.Lock()
	if old, ok := h.conns[serverID]; ok {
		_ = old.Close() // 旧连接踢下线（防重连竞态：后连者胜）
	}
	h.conns[serverID] = peer
	h.mu.Unlock()
}

// detach 连接断开时移除并标记离线。
func (h *Hub) detach(serverID int64, peer *rpc.Peer) {
	h.mu.Lock()
	if h.conns[serverID] == peer {
		delete(h.conns, serverID)
	}
	h.mu.Unlock()
	h.store.MarkOffline(serverID)
}

func (h *Hub) idOf(peer *rpc.Peer) (int64, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for id, p := range h.conns {
		if p == peer {
			return id, true
		}
	}
	return 0, false
}

// Call 向服务器下发任意 JSON-RPC 调用并等待结果。
func (h *Hub) Call(serverID int64, method string, params any) (*protocol.Response, error) {
	peer := h.Peer(serverID)
	if peer == nil {
		return nil, ErrOffline
	}
	return peer.Call(method, params, 35*time.Second)
}

// Exec 向服务器下发远程命令并等待结果（最长 35s）。
func (h *Hub) Exec(serverID int64, command string, timeout int) (*protocol.ExecResult, error) {
	peer := h.Peer(serverID)
	if peer == nil {
		return nil, ErrOffline
	}
	resp, err := peer.Call(protocol.MethodExec, protocol.ExecParams{Command: command, Timeout: timeout}, 35*time.Second)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, &AgentError{resp.Error.Message}
	}
	var result protocol.ExecResult
	raw, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// OpenTerminal 向 Agent 打开终端会话，返回 agent 侧 Peer 供中继。
func (h *Hub) OpenTerminal(serverID int64, sessionID string) error {
	peer := h.Peer(serverID)
	if peer == nil {
		return ErrOffline
	}
	resp, err := peer.Call(protocol.MethodTerminal, protocol.TerminalParams{SessionID: sessionID}, 10*time.Second)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return &AgentError{resp.Error.Message}
	}
	return nil
}

// SendTermData 向 Agent 转发终端输入。
func (h *Hub) SendTermData(serverID int64, data protocol.TerminalData) error {
	peer := h.Peer(serverID)
	if peer == nil {
		return ErrOffline
	}
	return peer.Notify(protocol.MethodTermData, data)
}

// CloseTerm 通知 Agent 关闭终端会话。
func (h *Hub) CloseTerm(serverID int64, sessionID string) {
	if peer := h.Peer(serverID); peer != nil {
		_ = peer.Notify(protocol.MethodTermClose, protocol.TerminalData{SessionID: sessionID})
	}
}

func GenSecret() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// ErrOffline 目标服务器不在线。
var ErrOffline = &AgentError{"server offline"}

// AgentError Agent 侧返回的错误。
type AgentError struct{ Msg string }

func (e *AgentError) Error() string { return e.Msg }
