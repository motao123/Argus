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
	// IPChangeCb 服务器 Agent 上报的公网 IPv4/IPv6 变化回调（DDNS 触发用）。
	IPChangeCb func(serverID int64, host protocol.HostInfo)
	// NATDataCb Agent 回传 NAT 隧道数据。回调返回后才向 agent 应答，用于背压。
	NATDataCb func(sessionID string, data []byte) error
	// NATCloseCb Agent 后端连接关闭通知。
	NATCloseCb func(sessionID string)
	// TransferCb 服务器按密钥注册成功回调（过户验证用）。
	TransferCb func(serverID int64)

	mu      sync.RWMutex
	conns   map[int64]*rpc.Peer // serverID → 连接
	ipCache map[int64]string    // serverID → 最近 IPv4/IPv6 签名
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
	// 心跳：服务端每 30s 发 Ping，Agent 回 Pong；
	// 读超时（默认 75s）内无任何数据帧/Pong 即判定连接死亡，走下方断开清理
	peer.StartHeartbeat(rpc.DefaultPingInterval)
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
	case protocol.MethodNATData:
		var d protocol.TerminalData
		if err := json.Unmarshal(params, &d); err != nil {
			return nil, protocol.NewError(protocol.ErrParams, err.Error())
		}
		if ch.hub.NATDataCb == nil {
			return nil, protocol.NewError(protocol.ErrInternal, "NAT proxy unavailable")
		}
		if err := ch.hub.NATDataCb(d.SessionID, d.Data); err != nil {
			return nil, protocol.NewError(protocol.ErrNotFound, err.Error())
		}
		return map[string]any{"ok": true}, nil
	case protocol.MethodNATClose:
		var d protocol.TerminalData
		if err := json.Unmarshal(params, &d); err != nil {
			return nil, protocol.NewError(protocol.ErrParams, err.Error())
		}
		if ch.hub.NATCloseCb != nil {
			ch.hub.NATCloseCb(d.SessionID)
		}
		return nil, nil
	default:
		return nil, protocol.NewError(protocol.ErrMethod, "unknown method: "+method)
	}
}

// handleRegister 注册：必须携带密钥。
// 密钥优先匹配既有 Server.Secret（重连）；否则匹配 User.AgentSecret（按用户创建 owner server）。
func (ch *connHandler) handleRegister(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.RegisterParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	if p.Secret == "" {
		// 禁止空密钥注册：防止公网实例被任意新增节点占用
		return nil, protocol.NewError(protocol.ErrUnauthorized, "secret required")
	}

	var srv model.Server
	if err := ch.hub.db.Where("secret = ?", p.Secret).First(&srv).Error; err == nil {
		// 已有服务器：重连
	} else {
		// 尝试按用户注册密钥创建 owner server
		var user model.User
		if err := ch.hub.db.Where("agent_secret = ?", p.Secret).First(&user).Error; err != nil {
			return nil, protocol.NewError(protocol.ErrUnauthorized, "invalid secret")
		}
		srv = model.Server{Name: "Server", Secret: GenSecret(), OwnerID: user.ID}
		if err := ch.hub.db.Create(&srv).Error; err != nil {
			return nil, protocol.NewError(protocol.ErrInternal, err.Error())
		}
	}

	ch.hub.attach(srv.ID, ch.peer)
	// Persist optional registration metadata; omitted fields preserve legacy agents.
	updates := make(map[string]any)
	if p.Protocol != "" {
		srv.Protocol, updates["protocol"] = p.Protocol, p.Protocol
	}
	if p.Version != "" {
		srv.Version, updates["version"] = p.Version, p.Version
	}
	if p.OS != "" {
		srv.OS, updates["os"] = p.OS, p.OS
	}
	if p.Arch != "" {
		srv.Arch, updates["arch"] = p.Arch, p.Arch
	}
	if p.Capabilities != nil {
		if raw, err := json.Marshal(p.Capabilities); err == nil {
			srv.Capabilities = string(raw)
			updates["capabilities"] = srv.Capabilities
		}
	}
	if len(updates) > 0 {
		ch.hub.db.Model(&srv).Updates(updates)
	}
	ch.hub.store.Upsert(&srv)
	ch.hub.store.SetOnline(srv.ID)
	log.Printf("agent %s (#%d) registered", srv.Name, srv.ID)
	if ch.hub.TransferCb != nil {
		ch.hub.TransferCb(srv.ID)
	}
	return protocol.RegisterResult{ServerID: srv.ID, Secret: srv.Secret, Capabilities: p.Capabilities}, nil
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
		// 主机名变更则同步到 DB，并回写 Store 快照（否则 WS 实时视图与 REST 名称不一致）
		var srv model.Server
		if err := ch.hub.db.First(&srv, id).Error; err == nil && srv.Name == "Server" {
			srv.Name = p.Host.Hostname
			ch.hub.db.Model(&srv).Update("name", srv.Name)
			ch.hub.store.Upsert(&srv)
		}
		// IPv4 或 IPv6 变化均触发 DDNS，传递 Agent HostInfo 而不是请求来源 IP。
		if ch.hub.IPChangeCb != nil {
			signature := p.Host.IPv4 + "\x00" + p.Host.IPv6
			if p.Host.IPv4 == "" {
				signature = p.Host.IP + "\x00" + p.Host.IPv6
			}
			if signature != "\x00" && ch.hub.lastIP(id) != signature {
				ch.hub.setLastIP(id, signature)
				go ch.hub.IPChangeCb(id, p.Host)
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
func (h *Hub) OpenTerminal(serverID int64, sessionID string, cols, rows int) error {
	peer := h.Peer(serverID)
	if peer == nil {
		return ErrOffline
	}
	resp, err := peer.Call(protocol.MethodTerminal, protocol.TerminalParams{SessionID: sessionID, Cols: cols, Rows: rows}, 10*time.Second)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return &AgentError{resp.Error.Message}
	}
	return nil
}

// ResizeTerm 通知 Agent 调整终端窗口。
func (h *Hub) ResizeTerm(serverID int64, resize protocol.TerminalResize) error {
	peer := h.Peer(serverID)
	if peer == nil {
		return ErrOffline
	}
	return peer.Notify(protocol.MethodTermResize, resize)
}

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
