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
	server := New(<-accepted, h)
	client := New(c, nil)
	go server.ReadLoop()
	go client.ReadLoop()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
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
