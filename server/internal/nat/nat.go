// Package nat 提供按 Host 路由的 HTTP 隧道反向代理。
// 隧道只承载 HTTP/1.x（包括 WebSocket Upgrade），不暴露通用 TCP/UDP 入口。
package nat

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/server/internal/model"
)

const (
	DefaultServerConnectionLimit = 16
	DefaultUserConnectionLimit   = 32
	tunnelWriteTimeout           = 30 * time.Second
)

// tunnel 是一条到 agent 的有背压字节隧道。
// 每个 data 帧使用 JSON-RPC call，只有接收端写入有界队列/目标连接后才应答，
// 因此队列满时发送方会停下来而不是丢包。
type tunnel struct {
	peer      *rpc.Peer
	sessionID string

	mu      sync.Mutex
	closed  bool
	readCh  chan []byte
	closeCh chan struct{}
	buf     []byte
}

func newTunnel(peer *rpc.Peer, sessionID string) *tunnel {
	return &tunnel{
		peer:      peer,
		sessionID: sessionID,
		readCh:    make(chan []byte, 16),
		closeCh:   make(chan struct{}),
	}
}

func (t *tunnel) Write(p []byte) (int, error) {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	data := append([]byte(nil), p...)
	resp, err := t.peer.Call(protocol.MethodNATData, protocol.TerminalData{
		SessionID: t.sessionID,
		Data:      data,
	}, tunnelWriteTimeout)
	if err != nil {
		return 0, err
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("agent tunnel write: %s", resp.Error.Message)
	}
	return len(p), nil
}

func (t *tunnel) Read(p []byte) (int, error) {
	for len(t.buf) == 0 {
		select {
		case data := <-t.readCh:
			t.buf = data
		case <-t.closeCh:
			// 关闭前已应答（已入队）的数据必须先交付，不能直接以 EOF 丢弃
			// （agent 只有收到 data 应答后才发送 NATClose，故入队先于关闭发生）。
			select {
			case data := <-t.readCh:
				t.buf = data
			default:
				return 0, io.EOF
			}
		}
	}
	n := copy(p, t.buf)
	t.buf = t.buf[n:]
	return n, nil
}

// PushData 注入 agent 回传数据。队列满时阻塞，形成端到端背压。
func (t *tunnel) PushData(data []byte) error {
	data = append([]byte(nil), data...)
	select {
	case t.readCh <- data:
		return nil
	case <-t.closeCh:
		return io.ErrClosedPipe
	}
}

// closeLocal 关闭本地隧道；notify 决定是否通知 agent。
func (t *tunnel) closeLocal(notify bool) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.closeCh)
	t.mu.Unlock()
	if notify {
		_ = t.peer.Notify(protocol.MethodNATClose, protocol.TerminalData{SessionID: t.sessionID})
	}
	return nil
}

func (t *tunnel) Close() error { return t.closeLocal(true) }

// Proxy NAT HTTP 反向代理。
type Proxy struct {
	DB    *gorm.DB
	Peers func() map[int64]*rpc.Peer

	server *http.Server
	seq    atomic.Uint64

	mu          sync.Mutex
	tunnels     map[string]*tunnel
	byServer    map[int64]int
	byUser      map[int64]int
	reserved    map[string]struct{}
	serverLimit int
	userLimit   int
}

// New 创建 NAT HTTP 反向代理。
func New(db *gorm.DB, peers func() map[int64]*rpc.Peer) *Proxy {
	return &Proxy{
		DB: db, Peers: peers,
		tunnels:     make(map[string]*tunnel),
		byServer:    make(map[int64]int),
		byUser:      make(map[int64]int),
		reserved:    make(map[string]struct{}),
		serverLimit: DefaultServerConnectionLimit,
		userLimit:   DefaultUserConnectionLimit,
	}
}

// Configure 设置运行时连接配额和禁止 NAT 覆盖的 dashboard 域名。
func (p *Proxy) Configure(serverLimit, userLimit int, reservedHosts []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if serverLimit > 0 {
		p.serverLimit = serverLimit
	}
	if userLimit > 0 {
		p.userLimit = userLimit
	}
	p.reserved = make(map[string]struct{}, len(reservedHosts))
	for _, host := range reservedHosts {
		if host = NormalizeHost(host); host != "" {
			p.reserved[host] = struct{}{}
		}
	}
}

func (p *Proxy) Start(listen string) error {
	if listen == "" {
		listen = ":9090"
	}
	p.server = &http.Server{Addr: listen, Handler: p, ReadHeaderTimeout: 15 * time.Second}
	log.Printf("NAT HTTP tunnel listening on %s", listen)
	return p.server.ListenAndServe()
}

func (p *Proxy) Close() error {
	if p.server != nil {
		return p.server.Close()
	}
	return nil
}

// NormalizeHost canonicalizes an HTTP Host or configured hostname.
func NormalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if strings.Count(host, ":") == 1 {
		// A hostname with a malformed/non-numeric port is not a valid route key.
		if i := strings.LastIndexByte(host, ':'); i > 0 {
			host = host[:i]
		}
	}
	return strings.TrimSuffix(strings.Trim(host, "[]"), ".")
}

// IsReserved 报告该 host 是否被禁止 NAT 路由（dashboard 等保留域名）。
func (p *Proxy) IsReserved(host string) bool {
	host = NormalizeHost(host)
	p.mu.Lock()
	_, ok := p.reserved[host]
	p.mu.Unlock()
	return ok
}

// ReservedHosts 返回当前保留域名（排序），供 API/UI 展示。
func (p *Proxy) ReservedHosts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.reserved))
	for h := range p.reserved {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// PushData is the agent data-bus entry point. Returning only after enqueueing
// lets the RPC response act as flow-control acknowledgement.
func (p *Proxy) PushData(sessionID string, data []byte) error {
	p.mu.Lock()
	t := p.tunnels[sessionID]
	p.mu.Unlock()
	if t == nil {
		return io.ErrClosedPipe
	}
	return t.PushData(data)
}

// CloseTunnel handles an agent-side EOF/error notification.
func (p *Proxy) CloseTunnel(sessionID string) {
	p.mu.Lock()
	t := p.tunnels[sessionID]
	p.mu.Unlock()
	if t != nil {
		_ = t.closeLocal(false)
	}
}

// Limits returns current runtime limits for API/UI display.
func (p *Proxy) Limits() (server, user int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.serverLimit, p.userLimit
}

// Active returns active tunnel counts for a server and owner.
func (p *Proxy) Active(serverID, ownerID int64) (server, user int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byServer[serverID], p.byUser[ownerID]
}

func (p *Proxy) reserve(t *tunnel, nat *model.NAT) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byServer[nat.ServerID] >= p.serverLimit || p.byUser[nat.OwnerID] >= p.userLimit {
		return false
	}
	p.tunnels[t.sessionID] = t
	p.byServer[nat.ServerID]++
	p.byUser[nat.OwnerID]++
	return true
}

func (p *Proxy) release(sessionID string, nat *model.NAT) {
	p.mu.Lock()
	if _, ok := p.tunnels[sessionID]; ok {
		delete(p.tunnels, sessionID)
		if p.byServer[nat.ServerID] > 0 {
			p.byServer[nat.ServerID]--
		}
		if p.byUser[nat.OwnerID] > 0 {
			p.byUser[nat.OwnerID]--
		}
	}
	p.mu.Unlock()
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, token := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
			return true
		}
	}
	return false
}

// ServeHTTP routes one HTTP request (or one upgraded WebSocket) through an agent.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := NormalizeHost(r.Host)
	if host == "" || p.IsReserved(host) {
		http.Error(w, "reserved host", http.StatusMisdirectedRequest)
		return
	}
	var route model.NAT
	if err := p.DB.Where("domain = ? AND enabled = ?", host, true).First(&route).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	peer := p.Peers()[route.ServerID]
	if peer == nil {
		http.Error(w, "target server offline", http.StatusBadGateway)
		return
	}

	sessionID := fmt.Sprintf("nat-%d-%d", time.Now().UnixNano(), p.seq.Add(1))
	t := newTunnel(peer, sessionID)
	if !p.reserve(t, &route) {
		http.Error(w, "tunnel connection quota exceeded", http.StatusTooManyRequests)
		return
	}
	defer func() {
		p.release(sessionID, &route)
		_ = t.Close()
	}()

	resp, err := peer.Call(protocol.MethodNATConnect, protocol.NATConnectParams{
		SessionID: sessionID,
		Target:    route.TargetAddr,
	}, 15*time.Second)
	if err != nil || resp.Error != nil {
		http.Error(w, "tunnel failed", http.StatusBadGateway)
		return
	}
	var connected protocol.NATConnectResult
	if err := decodeResult(resp.Result, &connected); err != nil || !connected.OK {
		http.Error(w, "tunnel failed", http.StatusBadGateway)
		return
	}

	upgrade := isWebSocketUpgrade(r)
	r.Close = !upgrade // regular requests get an unambiguous backend EOF; upgrades stay bidirectional.
	if err := r.Write(t); err != nil {
		http.Error(w, "tunnel request failed", http.StatusBadGateway)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()

	// No absolute connection deadline: WebSocket and streaming HTTP may remain
	// idle/active indefinitely. Individual tunnel writes still have bounded stalls.
	clientReader := io.Reader(conn)
	if rw != nil {
		clientReader = rw
	}
	errCh := make(chan error, 2)
	go func() { _, err := io.Copy(conn, t); errCh <- err }()
	go func() { _, err := io.Copy(t, clientReader); errCh <- err }()
	<-errCh
	_ = t.Close()
	_ = conn.Close()
	<-errCh
}

func decodeResult(v any, out any) error {
	var b []byte
	var err error
	if raw, ok := v.(json.RawMessage); ok {
		b = raw
	} else {
		b, err = json.Marshal(v)
		if err != nil {
			return err
		}
	}
	return json.Unmarshal(b, out)
}
