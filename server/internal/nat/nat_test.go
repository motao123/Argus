package nat

// 里程碑 6 NAT：本地 backend/agent fake 隧道测试。
// 覆盖：普通 HTTP（含大响应背压完整性）、WebSocket 双向、长连接（无固定 60s deadline）、
// 每服务器/每用户连接配额、Agent 离线、reserved host、未知 Host、Host 规范化。

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/server/internal/model"
)

var upgrader = websocket.Upgrader{}

// ---------------------------------------------------------------------------
// fakeAgent：模拟真实 Agent 的 NAT 处理（rpc.Peer + 到后端的 TCP 管道），
// data 帧使用 Call 等待服务端应答，队列满时阻塞，与真实 Agent 背压语义一致。
// ---------------------------------------------------------------------------

type fakeAgentSession struct {
	conn   net.Conn
	writeC chan []byte
	close  chan struct{}
	once   sync.Once
}

func (s *fakeAgentSession) writeLoop(a *fakeAgent, id string) {
	for {
		select {
		case data := <-s.writeC:
			if err := writeAll(s.conn, data); err != nil {
				s.closeNAT(a, id, true)
				return
			}
		case <-s.close:
			return
		}
	}
}

func (s *fakeAgentSession) readLoop(a *fakeAgent, id string) {
	buf := make([]byte, 16*1024)
	for {
		n, err := s.conn.Read(buf)
		if n > 0 {
			resp, callErr := a.peer.Call(protocol.MethodNATData, protocol.TerminalData{
				SessionID: id,
				Data:      append([]byte(nil), buf[:n]...),
			}, 30*time.Second)
			if callErr != nil || resp.Error != nil {
				s.closeNAT(a, id, true)
				return
			}
		}
		if err != nil {
			s.closeNAT(a, id, true)
			return
		}
	}
}

func (s *fakeAgentSession) closeNAT(a *fakeAgent, id string, notify bool) {
	s.once.Do(func() {
		a.mu.Lock()
		delete(a.sess, id)
		a.mu.Unlock()
		close(s.close)
		_ = s.conn.Close()
	})
	if notify {
		_ = a.peer.Notify(protocol.MethodNATClose, protocol.TerminalData{SessionID: id})
	}
}

type fakeAgent struct {
	peer *rpc.Peer
	mu   sync.Mutex
	sess map[string]*fakeAgentSession
}

func (a *fakeAgent) Handle(method string, params json.RawMessage) (any, *protocol.RPCError) {
	switch method {
	case protocol.MethodNATConnect:
		var p protocol.NATConnectParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, protocol.NewError(protocol.ErrParams, err.Error())
		}
		conn, err := net.DialTimeout("tcp", p.Target, 5*time.Second)
		if err != nil {
			return protocol.NATConnectResult{Error: err.Error()}, nil
		}
		s := &fakeAgentSession{conn: conn, writeC: make(chan []byte, 16), close: make(chan struct{})}
		a.mu.Lock()
		if _, dup := a.sess[p.SessionID]; dup {
			a.mu.Unlock()
			_ = conn.Close()
			return protocol.NATConnectResult{Error: "session already exists"}, nil
		}
		a.sess[p.SessionID] = s
		a.mu.Unlock()
		go s.writeLoop(a, p.SessionID)
		go s.readLoop(a, p.SessionID)
		return protocol.NATConnectResult{OK: true}, nil

	case protocol.MethodNATData:
		var d protocol.TerminalData
		if err := json.Unmarshal(params, &d); err != nil {
			return nil, protocol.NewError(protocol.ErrParams, err.Error())
		}
		a.mu.Lock()
		s := a.sess[d.SessionID]
		a.mu.Unlock()
		if s == nil {
			return nil, protocol.NewError(protocol.ErrNotFound, "tunnel session not found")
		}
		select {
		case s.writeC <- append([]byte(nil), d.Data...):
			return map[string]any{"ok": true}, nil
		case <-s.close:
			return nil, protocol.NewError(protocol.ErrInternal, "tunnel session closed")
		}

	case protocol.MethodNATClose:
		var d protocol.TerminalData
		_ = json.Unmarshal(params, &d)
		a.mu.Lock()
		s := a.sess[d.SessionID]
		a.mu.Unlock()
		if s != nil {
			s.closeNAT(a, d.SessionID, false)
		}
		return nil, nil
	}
	return nil, protocol.NewError(protocol.ErrMethod, "unknown method")
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// ---------------------------------------------------------------------------
// testEnv：内存 DB + 真实 HTTP 监听（Proxy）+ fake agent 接入端点。
// ---------------------------------------------------------------------------

type testEnv struct {
	t        *testing.T
	db       *gorm.DB
	proxy    *Proxy
	addr     string
	peers    map[int64]*rpc.Peer
	agentEP  *httptest.Server
	accepted chan *websocket.Conn
}

func newTestEnv(t *testing.T, serverLimit, userLimit int, reserved []string) *testEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.NAT{}); err != nil {
		t.Fatal(err)
	}
	e := &testEnv{
		t:        t,
		db:       gdb,
		peers:    make(map[int64]*rpc.Peer),
		accepted: make(chan *websocket.Conn, 8),
	}
	e.proxy = New(gdb, func() map[int64]*rpc.Peer { return e.peers })
	e.proxy.Configure(serverLimit, userLimit, reserved)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	e.addr = ln.Addr().String()
	e.proxy.server = &http.Server{Handler: e.proxy, ReadHeaderTimeout: 15 * time.Second}
	go func() { _ = e.proxy.server.Serve(ln) }()
	t.Cleanup(func() {
		_ = e.proxy.Close()
		e.agentEP.Close()
	})

	e.agentEP = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		select {
		case e.accepted <- conn:
		case <-time.After(5 * time.Second):
		}
	}))
	return e
}

func (e *testEnv) addRoute(serverID, ownerID int64, domain, target string) {
	e.t.Helper()
	r := model.NAT{OwnerID: ownerID, ServerID: serverID, Domain: domain, TargetAddr: target, Enabled: true}
	if err := e.db.Create(&r).Error; err != nil {
		e.t.Fatal(err)
	}
}

// hubHandler 等价于生产 Hub 的 connHandler（NAT 数据总线入口）。
type hubHandler struct{ proxy *Proxy }

func (h *hubHandler) Handle(method string, params json.RawMessage) (any, *protocol.RPCError) {
	switch method {
	case protocol.MethodNATData:
		var d protocol.TerminalData
		if err := json.Unmarshal(params, &d); err != nil {
			return nil, protocol.NewError(protocol.ErrParams, err.Error())
		}
		if err := h.proxy.PushData(d.SessionID, d.Data); err != nil {
			return nil, protocol.NewError(protocol.ErrNotFound, err.Error())
		}
		return map[string]any{"ok": true}, nil
	case protocol.MethodNATClose:
		var d protocol.TerminalData
		_ = json.Unmarshal(params, &d)
		h.proxy.CloseTunnel(d.SessionID)
		return nil, nil
	}
	return nil, protocol.NewError(protocol.ErrMethod, "unknown method")
}

// startAgentWithHandler 建立一条 fake agent 连接并登记到 peers[serverID]。
// agent 端挂 fakeAgent handler 处理 NATConnect/NATData/NATClose，
// 服务端端挂 hubHandler 等价生产 Hub 的 NAT 数据总线。
func (e *testEnv) startAgentWithHandler(serverID int64) *fakeAgent {
	e.t.Helper()
	wsURL := "ws" + strings.TrimPrefix(e.agentEP.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	serverConn := <-e.accepted
	peer := rpc.New(serverConn, &hubHandler{proxy: e.proxy})
	e.peers[serverID] = peer
	go peer.ReadLoop()

	ag := &fakeAgent{sess: make(map[string]*fakeAgentSession)}
	ag.peer = rpc.New(conn, ag)
	go ag.peer.ReadLoop()
	return ag
}

func (e *testEnv) get(domain, path string) (*http.Response, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, "http://"+e.addr+path, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Host = domain
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp, body, err
}

// dialWS 经代理建立 WebSocket（Host 指定 NAT 域名）。
func (e *testEnv) dialWS(domain, path string) (*websocket.Conn, *http.Response, error) {
	hdr := http.Header{}
	hdr.Set("Host", domain)
	return websocket.DefaultDialer.Dial("ws://"+e.addr+path, hdr)
}

// ---------------------------------------------------------------------------
// 普通 HTTP：大响应 + 慢客户端，验证端到端背压不丢包。
// ---------------------------------------------------------------------------

func TestPlainHTTPBackpressureIntegrity(t *testing.T) {
	e := newTestEnv(t, 16, 32, nil)
	payload := make([]byte, 8<<20) // 8MB，超过任意有界队列容量
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(payload)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = w.Write(payload)
	}))
	defer backend.Close()
	e.addRoute(42, 7, "app.example.com", strings.TrimPrefix(backend.URL, "http://"))
	e.startAgentWithHandler(42)

	req, err := http.NewRequest(http.MethodGet, "http://"+e.addr+"/big", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "app.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	// 慢速读取：每 4KB 停顿 2ms，迫使服务端队列打满、agent 停止读取后端，
	// 验证背压链路（而非丢弃）保证字节完整。
	slow := &slowReader{r: resp.Body, chunk: 4096, hold: 2 * time.Millisecond}
	h := sha256.New()
	if _, err := io.Copy(h, slow); err != nil {
		t.Fatal(err)
	}
	got := h.Sum(nil)
	if hex.EncodeToString(got[:]) != hex.EncodeToString(want[:]) {
		t.Fatalf("payload corrupted: got sha256 %x want %x", got, want)
	}
}

type slowReader struct {
	r     io.Reader
	chunk int
	hold  time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n >= s.chunk && s.hold > 0 {
		time.Sleep(s.hold)
	}
	return n, err
}

// 流式响应跨间隔存活（长连接快速冒烟）。
func TestStreamingResponseSurvivesGaps(t *testing.T) {
	e := newTestEnv(t, 16, 32, nil)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		for i := 1; i <= 3; i++ {
			_, _ = fmt.Fprintf(w, "chunk-%d;", i)
			fl.Flush()
			time.Sleep(400 * time.Millisecond)
		}
	}))
	defer backend.Close()
	e.addRoute(42, 7, "stream.example.com", strings.TrimPrefix(backend.URL, "http://"))
	e.startAgentWithHandler(42)

	req, _ := http.NewRequest(http.MethodGet, "http://"+e.addr+"/", nil)
	req.Host = "stream.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != "chunk-1;chunk-2;chunk-3;" {
		t.Fatalf("streamed body = %q", got)
	}
}

// ---------------------------------------------------------------------------
// WebSocket 双向：升级经隧道转发，双向消息都能到达。
// ---------------------------------------------------------------------------

func TestWebSocketBidirectional(t *testing.T) {
	e := newTestEnv(t, 16, 32, nil)
	backend := newWSEchoBackend(t)
	defer backend.Close()
	e.addRoute(42, 7, "ws.example.com", strings.TrimPrefix(backend.URL, "http://"))
	e.startAgentWithHandler(42)

	conn, resp, err := e.dialWS("ws.example.com", "/echo")
	if err != nil {
		if resp != nil {
			t.Fatalf("dial err=%v status=%d", err, resp.StatusCode)
		}
		t.Fatal(err)
	}
	defer conn.Close()

	msgs := []struct {
		mt  int
		msg string
	}{
		{websocket.TextMessage, "hello 1"},
		{websocket.TextMessage, "hello 2"},
		{websocket.BinaryMessage, "\x00\x01\x02binary"},
		{websocket.TextMessage, "hello 3"},
	}
	for _, m := range msgs {
		if err := conn.WriteMessage(m.mt, []byte(m.msg)); err != nil {
			t.Fatal(err)
		}
		mt, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if mt != m.mt || string(data) != m.msg {
			t.Fatalf("echo mismatch: mt=%d data=%q want mt=%d %q", mt, data, m.mt, m.msg)
		}
	}
}

func newWSEchoBackend(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
}

// ---------------------------------------------------------------------------
// 长连接：空闲超过旧实现的 60s 绝对 deadline 后仍可双向传输。
// ---------------------------------------------------------------------------

func TestLongConnectionIdleBeyondOldDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 62s idle test in -short mode")
	}
	const idle = 62 * time.Second // 旧实现 conn.SetDeadline(60s) 会在此时断开
	e := newTestEnv(t, 16, 32, nil)
	backend := newWSEchoBackend(t)
	defer backend.Close()
	e.addRoute(42, 7, "long.example.com", strings.TrimPrefix(backend.URL, "http://"))
	e.startAgentWithHandler(42)

	conn, _, err := e.dialWS("long.example.com", "/")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	t.Logf("connection open, idling %s (no fixed 60s deadline)", idle)
	deadline := time.NewTimer(idle)
	select {
	case <-deadline.C:
	case <-time.After(idle + 30*time.Second):
		t.Fatal("test itself timed out")
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("still-alive")); err != nil {
		t.Fatalf("write after idle: %v", err)
	}
	mt, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read after idle: %v", err)
	}
	if mt != websocket.TextMessage || string(data) != "still-alive" {
		t.Fatalf("echo after idle = %d %q", mt, data)
	}
}

// ---------------------------------------------------------------------------
// 连接配额：每服务器、每用户。
// ---------------------------------------------------------------------------

func TestConnectionQuotas(t *testing.T) {
	e := newTestEnv(t, 1, 1, nil) // 每服务器 1、每用户 1
	backend := newWSEchoBackend(t)
	defer backend.Close()
	addr := strings.TrimPrefix(backend.URL, "http://")
	e.addRoute(42, 7, "a.example.com", addr)
	e.addRoute(43, 7, "b.example.com", addr)
	e.startAgentWithHandler(42)
	e.startAgentWithHandler(43)

	// 第一条占用 server42 与 owner7 的唯一配额
	conn1, resp, err := e.dialWS("a.example.com", "/")
	if err != nil {
		if resp != nil {
			t.Fatalf("first dial err=%v status=%d", err, resp.StatusCode)
		}
		t.Fatal(err)
	}
	defer conn1.Close()

	// 同服务器第二条 → 429（服务器配额）
	if _, resp, err := e.dialWS("a.example.com", "/"); err == nil {
		t.Fatal("expected second connection on same server to be rejected")
	} else if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("server quota: got %v want 429", resp)
	}

	// 同用户、不同服务器 → 429（用户配额）
	if _, resp, err := e.dialWS("b.example.com", "/"); err == nil {
		t.Fatal("expected second user connection to be rejected")
	} else if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("user quota: got %v want 429", resp)
	}

	// 释放后恢复
	_ = conn1.Close()
	waitSlot := time.Now().Add(2 * time.Second)
	for {
		if _, resp, err := e.dialWS("a.example.com", "/"); err == nil {
			break // 配额释放成功
		} else if resp != nil && resp.StatusCode == http.StatusTooManyRequests && time.Now().Before(waitSlot) {
			time.Sleep(20 * time.Millisecond)
			continue
		} else if time.Now().After(waitSlot) {
			t.Fatal("quota slot never released after close")
		} else {
			t.Fatalf("unexpected dial error after close: %v status=%v", err, resp)
		}
	}
}

// ---------------------------------------------------------------------------
// 离线、reserved host、未知 Host、Host 规范化。
// ---------------------------------------------------------------------------

func TestOfflineServer(t *testing.T) {
	e := newTestEnv(t, 16, 32, nil)
	e.addRoute(44, 7, "off.example.com", "127.0.0.1:1") // 无 agent 在线
	resp, body, err := e.get("off.example.com", "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "offline") {
		t.Fatalf("body = %q, want offline hint", body)
	}
}

func TestReservedHostRejected(t *testing.T) {
	e := newTestEnv(t, 16, 32, []string{"dashboard.example.com", "localhost"})
	e.addRoute(42, 7, "dashboard.example.com", "127.0.0.1:1") // 即使配置了也不允许路由
	e.addRoute(42, 7, "ok.example.com", "127.0.0.1:1")

	for _, host := range []string{"dashboard.example.com", "dashboard.example.com:9090", "localhost", "localhost:9090"} {
		resp, body, err := e.get(host, "/")
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusMisdirectedRequest {
			t.Errorf("host %q: status = %d (%s), want 421", host, resp.StatusCode, body)
		}
	}
	// 保留域名不会被路由到后端（421 而非 502/404 说明命中保留检查）
	got := e.proxy.IsReserved("DASHBOARD.example.com.")
	if !got {
		t.Error("IsReserved should normalize and match case/trailing dot")
	}
	if hosts := e.proxy.ReservedHosts(); len(hosts) != 2 {
		t.Errorf("ReservedHosts = %v, want 2 entries", hosts)
	}
}

func TestUnknownHostNotFound(t *testing.T) {
	e := newTestEnv(t, 16, 32, nil)
	e.addRoute(42, 7, "known.example.com", "127.0.0.1:1")
	resp, _, err := e.get("nope.example.com", "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"Nat.Example.COM":    "nat.example.com",
		"example.com:8080":   "example.com",
		"example.com:abc":    "example.com",
		"[::1]:9090":         "::1",
		"::1":                "::1",
		"example.com.":       "example.com",
		"  sub.example.com ": "sub.example.com",
	}
	for in, want := range cases {
		if got := NormalizeHost(in); got != want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTunnelReadBlocksUntilData(t *testing.T) {
	// 无固定 30s 空闲 EOF：Read 在没有数据且未关闭时保持阻塞（1s 窗口内验证）。
	tun := newTunnel(nil, "t1")
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 16)
		_, err := tun.Read(buf)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("Read returned early with %v (fixed idle EOF regression)", err)
	case <-time.After(time.Second):
	}
	select {
	case tun.readCh <- []byte("x"):
	default:
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Read after data = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after data pushed")
	}
}
