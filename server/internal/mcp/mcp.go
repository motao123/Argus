// Package mcp 实现 MCP（Model Context Protocol）HTTP 端点：
// JSON-RPC 2.0 消息，PAT 认证，供 AI/LLM 或脚本调用服务器管理能力。
// 借鉴 nezha /mcp 端点设计（简化版）。
package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
	"github.com/motao123/Argus/server/internal/model"
)

// Server MCP 端点。
type Server struct {
	DB    *gorm.DB
	Peers func() map[int64]*rpc.Peer
	// IdentifyPAT 校验 Bearer token 返回用户 ID（由 API 层注入）。
	IdentifyPAT func(raw string) (int64, bool)
}

// 消息结构（MCP 用 JSON-RPC 2.0）。
type rpcMsg struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Handler MCP HTTP 处理器（POST /mcp）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handle)
	return mux
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// 仅 PAT 认证
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if s.IdentifyPAT == nil || !strings.HasPrefix(auth, "argus_") {
		writeJSON(w, http.StatusUnauthorized, &rpcMsg{Error: &rpcErr{-32001, "PAT required"}})
		return
	}
	userID, ok := s.IdentifyPAT(auth)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, &rpcMsg{Error: &rpcErr{-32001, "invalid PAT"}})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, &rpcMsg{Error: &rpcErr{-32700, "parse error"}})
		return
	}
	var msg rpcMsg
	if err := json.Unmarshal(body, &msg); err != nil {
		writeJSON(w, http.StatusBadRequest, &rpcMsg{Error: &rpcErr{-32700, "parse error"}})
		return
	}

	result, rpcErr := s.dispatch(msg.Method, msg.Params, userID)
	writeJSON(w, http.StatusOK, &rpcMsg{ID: msg.ID, Result: result, Error: rpcErr})
}

// dispatch 分发工具调用。
func (s *Server) dispatch(method string, params json.RawMessage, userID int64) (any, *rpcErr) {
	switch method {
	case "server.list":
		return s.serverList(userID)
	case "server.get":
		return s.serverGet(params, userID)
	case "server.exec":
		return s.serverExec(params, userID)
	case "fs.list":
		return s.fsList(params, userID)
	case "meta.whoami":
		return map[string]any{"user_id": userID}, nil
	default:
		return nil, &rpcErr{-32601, "unknown tool: " + method}
	}
}

func (s *Server) serverList(userID int64) (any, *rpcErr) {
	var servers []model.Server
	q := s.DB.Order("id")
	if userID != 0 {
		var u model.User
		if err := s.DB.First(&u, userID).Error; err == nil && u.Role != model.RoleAdmin {
			q = q.Where("owner_id = ?", userID)
		}
	}
	if err := q.Find(&servers).Error; err != nil {
		return nil, &rpcErr{-32603, err.Error()}
	}
	out := make([]map[string]any, 0, len(servers))
	for _, sv := range servers {
		out = append(out, map[string]any{"id": sv.ID, "name": sv.Name, "group": sv.Group})
	}
	return map[string]any{"servers": out}, nil
}

func (s *Server) serverGet(params json.RawMessage, userID int64) (any, *rpcErr) {
	var p struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.ID <= 0 {
		return nil, &rpcErr{-32602, "id required"}
	}
	var sv model.Server
	if err := s.DB.First(&sv, p.ID).Error; err != nil {
		return nil, &rpcErr{-32002, "server not found"}
	}
	return map[string]any{"id": sv.ID, "name": sv.Name, "group": sv.Group, "note": sv.Note}, nil
}

func (s *Server) serverExec(params json.RawMessage, userID int64) (any, *rpcErr) {
	var p struct {
		ID      int64  `json:"id"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.ID <= 0 || p.Command == "" {
		return nil, &rpcErr{-32602, "id and command required"}
	}
	peer := s.Peers()[p.ID]
	if peer == nil {
		return nil, &rpcErr{-32002, "server offline"}
	}
	resp, err := peer.Call(protocol.MethodExec, protocol.ExecParams{Command: p.Command, Timeout: p.Timeout}, 60*time.Second)
	if err != nil {
		return nil, &rpcErr{-32603, err.Error()}
	}
	if resp.Error != nil {
		return nil, &rpcErr{-32003, resp.Error.Message}
	}
	var result protocol.ExecResult
	raw, _ := json.Marshal(resp.Result)
	_ = json.Unmarshal(raw, &result)
	return map[string]any{"output": result.Output, "exit_code": result.Code}, nil
}

func (s *Server) fsList(params json.RawMessage, userID int64) (any, *rpcErr) {
	var p struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.ID <= 0 {
		return nil, &rpcErr{-32602, "id required"}
	}
	peer := s.Peers()[p.ID]
	if peer == nil {
		return nil, &rpcErr{-32002, "server offline"}
	}
	if p.Path == "" {
		p.Path = "/"
	}
	resp, err := peer.Call(protocol.MethodFsList, protocol.FsListParams{Path: p.Path}, 30*time.Second)
	if err != nil {
		return nil, &rpcErr{-32603, err.Error()}
	}
	if resp.Error != nil {
		return nil, &rpcErr{-32003, resp.Error.Message}
	}
	var result protocol.FsListResult
	raw, _ := json.Marshal(resp.Result)
	_ = json.Unmarshal(raw, &result)
	return map[string]any{"path": result.Path, "entries": result.Entries}, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
