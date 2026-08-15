package task

import (
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/motao123/Argus/protocol"
)

// natSession TCP 隧道会话（借鉴 terminal 会话模式，目标为任意 TCP 服务）。
type natSession struct {
	conn  net.Conn
	close chan struct{}
	once  sync.Once
}

var (
	natMu       sync.Mutex
	natSessions = map[string]*natSession{}
)

func (h *Handler) handleNATConnect(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.NATConnectParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	if p.SessionID == "" || p.Target == "" {
		return nil, protocol.NewError(protocol.ErrParams, "session_id/target required")
	}
	conn, err := net.DialTimeout("tcp", p.Target, 10*time.Second)
	if err != nil {
		return &protocol.FsDeleteResult{Error: err.Error()}, nil
	}
	ts := &natSession{conn: conn, close: make(chan struct{})}
	natMu.Lock()
	natSessions[p.SessionID] = ts
	natMu.Unlock()

	// 隧道 → server
	go func() {
		buf := make([]byte, 16384)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				_ = h.peer.Notify(protocol.MethodNATData, protocol.TerminalData{
					SessionID: p.SessionID,
					Data:      append([]byte(nil), buf[:n]...),
				})
			}
			if err != nil {
				h.closeNAT(p.SessionID)
				return
			}
		}
	}()
	return map[string]any{"ok": true}, nil
}

func (h *Handler) handleNATData(params json.RawMessage) {
	var p protocol.TerminalData
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	natMu.Lock()
	ts := natSessions[p.SessionID]
	natMu.Unlock()
	if ts == nil {
		return
	}
	ts.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, _ = ts.conn.Write(p.Data)
}

func (h *Handler) handleNATClose(params json.RawMessage) {
	var p protocol.TerminalData
	_ = json.Unmarshal(params, &p)
	h.closeNAT(p.SessionID)
}

func (h *Handler) closeNAT(id string) {
	natMu.Lock()
	ts, ok := natSessions[id]
	if ok {
		delete(natSessions, id)
	}
	natMu.Unlock()
	if !ok {
		return
	}
	ts.once.Do(func() {
		close(ts.close)
		_ = ts.conn.Close()
	})
	_ = h.peer.Notify(protocol.MethodNATClose, protocol.TerminalData{SessionID: id})
}
