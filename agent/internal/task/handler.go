// Package task 处理服务端下发的任务：远程命令执行与网页终端隧道。
package task

import (
	"context"
	"encoding/json"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/protocol"
)

// Handler 实现 rpc.Handler 接口。
type Handler struct {
	conn   *websocket.Conn
	peer *rpc.Peer

	mu       sync.Mutex
	sessions map[string]*termSession
}

// termSession 一个终端会话：pty 进程 + 输出转发 goroutine。
type termSession struct {
	cmd    *exec.Cmd
	stdin  chan []byte
	close  chan struct{}
	closed sync.Once
}

func NewHandler(conn *websocket.Conn) *Handler {
	return &Handler{
		conn:     conn,
		sessions: make(map[string]*termSession),
	}
}

// SetPeer 设置 JSON-RPC 对等端（用于流式通知）。
func (h *Handler) SetPeer(peer *rpc.Peer) { h.peer = peer }

// Handle 分发服务端调用。
func (h *Handler) Handle(method string, params json.RawMessage) (any, *protocol.RPCError) {
	switch method {
	case protocol.MethodExec:
		return h.handleExec(params)
	case protocol.MethodTerminal:
		return h.handleTerminalOpen(params)
	case protocol.MethodTermData:
		h.handleTermData(params)
		return nil, nil
	case protocol.MethodTermClose:
		h.handleTermClose(params)
		return nil, nil
	case protocol.MethodServiceCheck:
		return h.handleServiceCheck(params)
	default:
		return nil, protocol.NewError(protocol.ErrMethod, "unknown method: "+method)
	}
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

// ---- exec ----

func (h *Handler) handleExec(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.ExecParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	timeout := time.Duration(p.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)
	out, err := cmd.CombinedOutput()
	result := protocol.ExecResult{Output: string(out), Code: cmd.ProcessState.ExitCode()}
	if err != nil {
		// CommandContext 超时/失败时 exit code 可能为 -1
		result.Error = err.Error()
	}
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
		shell = "sh"
	}

	cmd := exec.Command(shell)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternal, err.Error())
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternal, err.Error())
	}
	cmd.Stderr = cmd.Stdout // 合并 stderr 到 stdout
	if err := cmd.Start(); err != nil {
		return nil, protocol.NewError(protocol.ErrInternal, err.Error())
	}

	ts := &termSession{cmd: cmd, stdin: make(chan []byte, 64), close: make(chan struct{})}
	h.mu.Lock()
	h.sessions[p.SessionID] = ts
	h.mu.Unlock()

	// stdout → server
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
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
				_, _ = stdin.Write(data)
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
	})
	_ = h.peer.Notify(protocol.MethodTermClose, protocol.TerminalData{SessionID: id})
	log.Printf("terminal session %s closed", id)
}

