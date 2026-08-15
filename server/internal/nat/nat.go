// Package nat 内网穿透：HTTP 反向代理按 Host 匹配 NAT 域名，
// 请求通过 agent 的 TCP 隧道（WS JSON-RPC 字节流）转发到内网服务。
// 借鉴 nezha NATClass + IOStream 隧道设计。
package nat

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/server/internal/model"
)

// tunnel 一条到 agent 的字节隧道（io.ReadWriteCloser）。
type tunnel struct {
	peer      *rpc.Peer
	sessionID string

	mu      sync.Mutex
	closed  bool
	buf     bytes.Buffer // agent 回传数据缓冲（由 DataSink 写入）
	readCh  chan []byte
	closeCh chan struct{}
}

func newTunnel(peer *rpc.Peer, sessionID string) *tunnel {
	t := &tunnel{
		peer:      peer,
		sessionID: sessionID,
		readCh:    make(chan []byte, 64),
		closeCh:   make(chan struct{}),
	}
	return t
}

func (t *tunnel) Write(p []byte) (int, error) {
	if t.closed {
		return 0, io.ErrClosedPipe
	}
	data := append([]byte(nil), p...)
	if err := t.peer.Notify(protocol.MethodNATData, protocol.TerminalData{
		SessionID: t.sessionID,
		Data:      data,
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *tunnel) Read(p []byte) (int, error) {
	for {
		t.mu.Lock()
		if t.buf.Len() > 0 {
			n, _ := t.buf.Read(p)
			t.mu.Unlock()
			return n, nil
		}
		closed := t.closed
		t.mu.Unlock()
		if closed {
			return 0, io.EOF
		}
		select {
		case data := <-t.readCh:
			t.mu.Lock()
			t.buf.Write(data)
			t.mu.Unlock()
		case <-t.closeCh:
			return 0, io.EOF
		case <-time.After(30 * time.Second):
			return 0, io.EOF
		}
	}
}

// PushData 注入 agent 回传数据（由 DataSink 调用）。
func (t *tunnel) PushData(data []byte) {
	select {
	case t.readCh <- data:
	default: // 缓冲满则丢弃（避免阻塞数据总线）
	}
}

// Close 关闭隧道并通知 agent。
func (t *tunnel) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	close(t.closeCh)
	_ = t.peer.Notify(protocol.MethodNATClose, protocol.TerminalData{SessionID: t.sessionID})
	return nil
}

// Proxy NAT 反向代理。
type Proxy struct {
	DB    *gorm.DB
	Peers func() map[int64]*rpc.Peer
	// DataSink 接收 agent 回传的 NAT 数据（由 server 侧注册到 Hub 回调）。
	DataSink func(sessionID string, data []byte)

	server *http.Server
	seq    atomic.Uint64

	mu       sync.Mutex
	tunnels  map[string]*tunnel
}

// New 创建 NAT 反向代理。
func New(db *gorm.DB, peers func() map[int64]*rpc.Peer) *Proxy {
	p := &Proxy{DB: db, Peers: peers, tunnels: make(map[string]*tunnel)}
	p.DataSink = p.pushData
	return p
}

// Start 启动 HTTP 监听（默认 :9090，可用 ARGUS_NAT_LISTEN 覆盖）。
func (p *Proxy) Start(listen string) error {
	if listen == "" {
		listen = ":9090"
	}
	p.server = &http.Server{
		Addr:              listen,
		Handler:           p,
		ReadHeaderTimeout: 15 * time.Second,
	}
	log.Printf("NAT proxy listening on %s (Host 匹配 NAT 域名转发到内网)", listen)
	return p.server.ListenAndServe()
}

func (p *Proxy) Close() error {
	if p.server != nil {
		return p.server.Close()
	}
	return nil
}

// pushData 数据总线入口。
func (p *Proxy) pushData(sessionID string, data []byte) {
	p.mu.Lock()
	t := p.tunnels[sessionID]
	p.mu.Unlock()
	if t != nil {
		t.PushData(data)
	}
}

// ServeHTTP 按 Host 路由到 agent 隧道。
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if i := strings.Index(host, ":"); i > 0 {
		host = host[:i]
	}
	var nat model.NAT
	if err := p.DB.Where("domain = ? AND enabled = ?", host, true).First(&nat).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	peer := p.Peers()[nat.ServerID]
	if peer == nil {
		http.Error(w, "target server offline", http.StatusBadGateway)
		return
	}

	sessionID := fmt.Sprintf("nat-%d-%d", time.Now().UnixNano(), p.seq.Add(1))
	resp, err := peer.Call(protocol.MethodNATConnect, protocol.NATConnectParams{
		SessionID: sessionID,
		Target:    nat.TargetAddr,
	}, 15*time.Second)
	if err != nil || resp.Error != nil {
		http.Error(w, "tunnel failed", http.StatusBadGateway)
		return
	}

	t := newTunnel(peer, sessionID)
	p.mu.Lock()
	p.tunnels[sessionID] = t
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.tunnels, sessionID)
		p.mu.Unlock()
		t.Close()
	}()

	// 序列化请求写入隧道
	var reqBuf bytes.Buffer
	if err := r.Write(&reqBuf); err != nil {
		return
	}
	if _, err := t.Write(reqBuf.Bytes()); err != nil {
		return
	}

	// 隧道响应 → HTTP 客户端（原始字节流）
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(60 * time.Second))

	buf := make([]byte, 16384)
	for {
		n, err := t.Read(buf)
		if n > 0 {
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
