// Package task 处理服务端下发的任务：远程命令执行与网页终端隧道。
package task

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
)

// Handler 实现 rpc.Handler 接口。
type Handler struct {
	conn *websocket.Conn
	peer *rpc.Peer
	caps protocol.Capabilities

	mu       sync.Mutex
	sessions map[string]*termSession
}

// termSession 一个终端会话：pty 进程 + 输出转发 goroutine。
type termSession struct {
	cmd    *exec.Cmd
	io     *terminalIO
	stdin  chan []byte
	close  chan struct{}
	closed sync.Once
}

func NewHandler(conn *websocket.Conn) *Handler {
	return &Handler{conn: conn, caps: DefaultCapabilities(), sessions: make(map[string]*termSession)}
}

func DefaultCapabilities() protocol.Capabilities {
	return protocol.Capabilities{Metrics: true, Probe: true, Command: true, Terminal: true, Files: true, Upgrade: true, NAT: true}
}

func (h *Handler) SetCapabilities(c protocol.Capabilities) { h.caps = c }

func disabled() (any, *protocol.RPCError) {
	return nil, protocol.NewError(protocol.ErrUnauthorized, "capability disabled")
}

// SetPeer 设置 JSON-RPC 对等端（用于流式通知）。
func (h *Handler) SetPeer(peer *rpc.Peer) { h.peer = peer }

// Handle 分发服务端调用。
func (h *Handler) Handle(method string, params json.RawMessage) (any, *protocol.RPCError) {
	switch method {
	case protocol.MethodExec:
		if !h.caps.Command {
			return disabled()
		}
		return h.handleExec(params)
	case protocol.MethodTerminal:
		if !h.caps.Terminal {
			return disabled()
		}
		return h.handleTerminalOpen(params)
	case protocol.MethodTermData:
		if !h.caps.Terminal {
			return disabled()
		}
		h.handleTermData(params)
		return nil, nil
	case protocol.MethodTermResize:
		if !h.caps.Terminal {
			return disabled()
		}
		h.handleTermResize(params)
		return nil, nil
	case protocol.MethodTermClose:
		if !h.caps.Terminal {
			return disabled()
		}
		h.handleTermClose(params)
		return nil, nil
	case protocol.MethodServiceCheck:
		if !h.caps.Probe {
			return disabled()
		}
		return h.handleServiceCheck(params)
	case protocol.MethodFsList:
		if !h.caps.Files {
			return disabled()
		}
		return h.handleFsList(params)
	case protocol.MethodFsRead:
		if !h.caps.Files {
			return disabled()
		}
		return h.handleFsRead(params)
	case protocol.MethodFsWrite:
		if !h.caps.Files {
			return disabled()
		}
		return h.handleFsWrite(params)
	case protocol.MethodFsDelete:
		if !h.caps.Files {
			return disabled()
		}
		return h.handleFsDelete(params)
	case protocol.MethodNATConnect:
		if !h.caps.NAT {
			return disabled()
		}
		return h.handleNATConnect(params)
	case protocol.MethodNATData:
		if !h.caps.NAT {
			return disabled()
		}
		h.handleNATData(params)
		return nil, nil
	case protocol.MethodNATClose:
		if !h.caps.NAT {
			return disabled()
		}
		h.handleNATClose(params)
		return nil, nil
	case protocol.MethodApplyConfig:
		return h.handleApplyConfig(params)
	case protocol.MethodUpgrade:
		if !h.caps.Upgrade {
			return disabled()
		}
		return h.handleUpgrade(params)
	default:
		return nil, protocol.NewError(protocol.ErrMethod, "unknown method: "+method)
	}
}

// handleUpgrade 自升级：下载 → SHA-256 校验 → 备份 → 原子替换 → 重启。
func (h *Handler) handleUpgrade(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.UpgradeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	if p.URL == "" || p.SHA256 == "" {
		return nil, protocol.NewError(protocol.ErrParams, "url and sha256 required")
	}
	up, err := upgradeSelf(&p)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternal, err.Error())
	}
	return map[string]any{"ok": true, "note": up}, nil
}

// ---- 服务监控探测 ----

func (h *Handler) handleServiceCheck(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.ServiceCheckParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	timeout := time.Duration(p.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	result := probeService(p.Type, p.Target, timeout)
	return result, nil
}

// ---- terminal ----

func (h *Handler) handleTerminalOpen(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.TerminalParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	if p.SessionID == "" {
		return nil, protocol.NewError(protocol.ErrParams, "session_id required")
	}

	shell := p.Command
	if shell == "" {
		shell = defaultShell()
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	io, err := startTerminal(cmd, p.Cols, p.Rows)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternal, err.Error())
	}

	ts := &termSession{cmd: cmd, io: io, stdin: make(chan []byte, 64), close: make(chan struct{})}
	h.mu.Lock()
	h.sessions[p.SessionID] = ts
	h.mu.Unlock()

	// stdout → server
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := io.Read(buf)
			if n > 0 {
				_ = h.peer.Notify(protocol.MethodTermData, protocol.TerminalData{
					SessionID: p.SessionID,
					Data:      append([]byte(nil), buf[:n]...),
				})
			}
			if err != nil {
				h.closeSession(p.SessionID)
				return
			}
		}
	}()

	// stdin 队列 → 进程
	go func() {
		for {
			select {
			case data := <-ts.stdin:
				_, _ = io.Write(data)
			case <-ts.close:
				return
			}
		}
	}()

	// 进程退出清理
	go func() {
		_ = cmd.Wait()
		h.closeSession(p.SessionID)
	}()

	return map[string]any{"ok": true}, nil
}

func (h *Handler) handleTermData(params json.RawMessage) {
	var p protocol.TerminalData
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	h.mu.Lock()
	ts := h.sessions[p.SessionID]
	h.mu.Unlock()
	if ts == nil {
		return
	}
	select {
	case ts.stdin <- p.Data:
	case <-ts.close:
	}
}

func (h *Handler) handleTermResize(params json.RawMessage) {
	var p protocol.TerminalResize
	if json.Unmarshal(params, &p) != nil {
		return
	}
	h.mu.Lock()
	ts := h.sessions[p.SessionID]
	h.mu.Unlock()
	if ts != nil {
		_ = ts.io.Resize(p.Cols, p.Rows)
	}
}

func (h *Handler) handleTermClose(params json.RawMessage) {
	var p protocol.TerminalData
	_ = json.Unmarshal(params, &p)
	h.closeSession(p.SessionID)
}

func (h *Handler) closeSession(id string) {
	h.mu.Lock()
	ts, ok := h.sessions[id]
	if ok {
		delete(h.sessions, id)
	}
	h.mu.Unlock()
	if !ok {
		return
	}
	ts.closed.Do(func() {
		close(ts.close)
		if ts.cmd != nil && ts.cmd.Process != nil {
			_ = ts.cmd.Process.Kill()
		}
		if ts.io != nil {
			_ = ts.io.Close()
		}
	})
	_ = h.peer.Notify(protocol.MethodTermClose, protocol.TerminalData{SessionID: id})
	log.Printf("terminal session %s closed", id)
}
