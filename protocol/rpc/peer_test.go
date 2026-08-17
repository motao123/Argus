package rpc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/motao123/Argus/protocol"
)

type testHandler struct{ calls atomic.Int32 }

func (h *testHandler) Handle(method string, _ json.RawMessage) (any, *protocol.RPCError) {
	h.calls.Add(1)
	if method == "slow" {
		time.Sleep(200 * time.Millisecond)
	}
	return method, nil
}

func peerPair(t *testing.T, h Handler) (*Peer, *Peer) {
	return peerPairOpts(t, h, DefaultReadTimeout)
}

// peerPairOpts 与 peerPair 相同，但允许注入更短的读超时（心跳测试用）。
func peerPairOpts(t *testing.T, h Handler, readTimeout time.Duration) (*Peer, *Peer) {
	t.Helper()
	up := websocket.Upgrader{}
	accepted := make(chan *websocket.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- c
	}))
	t.Cleanup(s.Close)
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	server := newPeer(<-accepted, h, readTimeout)
	client := newPeer(c, nil, readTimeout)
	go server.ReadLoop()
	go client.ReadLoop()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
}

// rawPeerPair 建立一条真实 WebSocket 连接：raw 端为裸连接（不包装 Peer、不读帧），
// peer 端为被测 Peer（指定读超时）。
func rawPeerPair(t *testing.T, readTimeout time.Duration) (*Peer, *websocket.Conn) {
	t.Helper()
	up := websocket.Upgrader{}
	accepted := make(chan *websocket.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- c
	}))
	t.Cleanup(s.Close)
	raw, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	peer := newPeer(<-accepted, nil, readTimeout)
	go peer.ReadLoop()
	t.Cleanup(func() { _ = raw.Close(); _ = peer.Close() })
	return peer, raw
}

// TestHeartbeatKeepsIdleConnectionAlive 验证双向心跳：无任何业务流量时，
// 仅靠 Ping/Pong 控制帧保活，远超读超时窗口也不断开，且之后仍可承载业务 RPC。
func TestHeartbeatKeepsIdleConnectionAlive(t *testing.T) {
	client, server := peerPairOpts(t, &testHandler{}, 300*time.Millisecond)
	client.StartHeartbeat(50 * time.Millisecond)
	server.StartHeartbeat(50 * time.Millisecond)

	// 静默期 > 2 个读超时窗口：若 Pong 未刷新读超时，连接早已被判定死亡
	time.Sleep(1200 * time.Millisecond)
	select {
	case <-client.Closed():
		t.Fatal("client died during idle keepalive")
	default:
	}
	select {
	case <-server.Closed():
		t.Fatal("server died during idle keepalive")
	default:
	}

	// 心跳与业务帧共存：连接依然健康，RPC 正常往返
	resp, err := client.Call("fast", nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
}

// TestSilentPeerTimesOut 验证静默对端（不回 Pong、不发数据，模拟 NAT 静默断链）
// 在受控的读超时内被判定死亡，并触发统一的断开清理（Closed + 底层连接关闭）。
func TestSilentPeerTimesOut(t *testing.T) {
	peer, raw := rawPeerPair(t, 200*time.Millisecond)
	peer.StartHeartbeat(50 * time.Millisecond)

	select {
	case <-peer.Closed():
	case <-time.After(3 * time.Second):
		t.Fatal("peer did not detect silent connection within read timeout")
	}
	// 读超时判定死亡后应关闭底层连接：裸对端应观察到 EOF/关闭而非继续通信
	_ = raw.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := raw.ReadMessage(); err == nil {
		t.Fatal("raw peer still receives data after timeout disconnect")
	}
}

// TestHeartbeatDoesNotInterfereWithRPC 验证高频心跳（控制帧）与并发业务 RPC
// （数据帧）在同一连接上共存互不干扰。混入慢调用使在途 RPC 跨越多个 Ping 周期，
// 强制控制帧与数据帧交叠。
func TestHeartbeatDoesNotInterfereWithRPC(t *testing.T) {
	client, server := peerPairOpts(t, &testHandler{}, 500*time.Millisecond)
	client.StartHeartbeat(20 * time.Millisecond)
	server.StartHeartbeat(20 * time.Millisecond)

	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		method := "fast"
		if i%8 == 0 { // 200ms 慢调用，横跨约 10 个 Ping 周期
			method = "slow"
		}
		go func() {
			_, err := client.Call(method, nil, 2*time.Second)
			errs <- err
		}()
	}
	for i := 0; i < 64; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-client.Closed():
		t.Fatal("client died during heartbeat+rpc")
	case <-server.Closed():
		t.Fatal("server died during heartbeat+rpc")
	default:
	}
}

// TestPeerRespondsToPing 验证 Peer 收到 Ping 显式回 Pong（裸端计数），
// 且 Peer 自身周期性 Ping 由裸端默认处理器回 Pong 维持存活。
func TestPeerRespondsToPing(t *testing.T) {
	peer, raw := rawPeerPair(t, 250*time.Millisecond)
	peer.StartHeartbeat(50 * time.Millisecond)

	var pongs atomic.Int32
	raw.SetPongHandler(func(string) error { pongs.Add(1); return nil })
	go func() {
		for {
			if _, _, err := raw.ReadMessage(); err != nil {
				return
			}
		}
	}()
	stop := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(stop) {
		if err := raw.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n := pongs.Load(); n < 3 {
		t.Fatalf("peer responded with %d pongs, want >= 3", n)
	}
	select {
	case <-peer.Closed():
		t.Fatal("peer died although ping/pong exchanged")
	default:
	}
}

func TestLongHandlerDoesNotBlockReadLoop(t *testing.T) {
	client, _ := peerPair(t, &testHandler{})
	done := make(chan error, 1)
	go func() { _, err := client.Call("slow", nil, time.Second); done <- err }()
	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	resp, err := client.Call("fast", nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 150*time.Millisecond {
		t.Fatal("fast call blocked behind slow handler")
	}
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCallsAndClose(t *testing.T) {
	client, server := peerPair(t, &testHandler{})
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() { _, err := client.Call("fast", nil, time.Second); errs <- err }()
	}
	for i := 0; i < 32; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	_ = server.Close()
	select {
	case <-client.Closed():
	case <-time.After(time.Second):
		t.Fatal("peer did not close")
	}
	resp, err := client.Call("fast", nil, 50*time.Millisecond)
	if err == nil || resp != nil {
		t.Fatal("call on closed peer should fail")
	}
}

// TestPongHookMeasuresRTT 验证往返延迟测量：client 注册 hook 并启动心跳，
// 对端（server）收到 Ping 控制帧自动回 Pong，hook 应收到 >0 的往返延迟
// （复用心跳 Ping 测量点，与 Agent 侧 RTT 上报同一条路径）。
func TestPongHookMeasuresRTT(t *testing.T) {
	client, server := peerPair(t, nil)
	_ = server // server 的 PingHandler 由 newPeer 安装：收到 Ping 自动回 Pong

	rtts := make(chan time.Duration, 4)
	client.SetPongHook(func(rtt time.Duration) { rtts <- rtt })
	client.StartHeartbeat(10 * time.Millisecond)

	select {
	case rtt := <-rtts:
		if rtt <= 0 {
			t.Fatalf("rtt = %v, want > 0", rtt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for pong RTT callback")
	}

	// 取消 hook：连接继续心跳保活，但不再有回调
	client.SetPongHook(nil)
	time.Sleep(50 * time.Millisecond)
	select {
	case rtt := <-rtts:
		t.Fatalf("unexpected callback after SetPongHook(nil): rtt=%v", rtt)
	default:
	}
}
