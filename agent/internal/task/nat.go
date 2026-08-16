package task

import (
	"encoding/json"
	"io"
	"net"
	"sync"
	"time"

	"github.com/motao123/Argus/protocol"
)

// natSession is an HTTP backend socket. It is only reachable through a server
// side Host-routed HTTP request; the agent does not expose a generic listener.
type natSession struct {
	conn   net.Conn
	writeC chan []byte
	close  chan struct{}
	once   sync.Once
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
		return protocol.NATConnectResult{Error: err.Error()}, nil
	}
	ts := &natSession{conn: conn, writeC: make(chan []byte, 16), close: make(chan struct{})}
	natMu.Lock()
	if old := natSessions[p.SessionID]; old != nil {
		natMu.Unlock()
		_ = conn.Close()
		return protocol.NATConnectResult{Error: "session already exists"}, nil
	}
	natSessions[p.SessionID] = ts
	natMu.Unlock()

	// Server -> backend. The bounded queue and RPC response provide backpressure;
	// writes never fall through a default case and never silently truncate.
	go func() {
		for {
			select {
			case data := <-ts.writeC:
				if err := writeFull(ts.conn, data); err != nil {
					h.closeNAT(p.SessionID, true)
					return
				}
			case <-ts.close:
				return
			}
		}
	}()

	// Backend -> server. Call waits until the server tunnel has accepted each
	// frame, so a slow HTTP client naturally stops reads from the backend.
	go func() {
		buf := make([]byte, 16*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				resp, callErr := h.peer.Call(protocol.MethodNATData, protocol.TerminalData{
					SessionID: p.SessionID,
					Data:      append([]byte(nil), buf[:n]...),
				}, 30*time.Second)
				if callErr != nil || resp.Error != nil {
					h.closeNAT(p.SessionID, true)
					return
				}
			}
			if err != nil {
				h.closeNAT(p.SessionID, true)
				return
			}
		}
	}()
	return protocol.NATConnectResult{OK: true}, nil
}

func (h *Handler) handleNATData(params json.RawMessage) *protocol.RPCError {
	var p protocol.TerminalData
	if err := json.Unmarshal(params, &p); err != nil {
		return protocol.NewError(protocol.ErrParams, err.Error())
	}
	natMu.Lock()
	ts := natSessions[p.SessionID]
	natMu.Unlock()
	if ts == nil {
		return protocol.NewError(protocol.ErrNotFound, "tunnel session not found")
	}
	data := append([]byte(nil), p.Data...)
	select {
	case ts.writeC <- data:
		return nil
	case <-ts.close:
		return protocol.NewError(protocol.ErrInternal, "tunnel session closed")
	}
}

func (h *Handler) handleNATClose(params json.RawMessage) {
	var p protocol.TerminalData
	_ = json.Unmarshal(params, &p)
	h.closeNAT(p.SessionID, false)
}

func (h *Handler) closeNAT(id string, notify bool) {
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
	if notify {
		_ = h.peer.Notify(protocol.MethodNATClose, protocol.TerminalData{SessionID: id})
	}
}

func writeFull(w io.Writer, data []byte) error {
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
