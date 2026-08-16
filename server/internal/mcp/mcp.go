// Package mcp 实现 MCP（Model Context Protocol）HTTP 端点：
// JSON-RPC 2.0 消息，PAT 认证，供 AI/LLM 或脚本调用服务器管理能力。
// 借鉴 nezha /mcp 端点设计（简化版）。
package mcp

import (
	"encoding/base64"
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
	// IdentifyPAT 校验 PAT 并返回完整授权身份。
	IdentifyPAT func(raw string) (*Principal, bool)
}

// Principal is the MCP authorization context supplied by the API package.
type Principal struct {
	UserID    int64
	IsAdmin   bool
	Scopes    map[string]bool
	ServerIDs map[int64]bool
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
	principal, ok := s.IdentifyPAT(auth)
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

	result, rpcErr := s.dispatch(msg.Method, msg.Params, principal)
	writeJSON(w, http.StatusOK, &rpcMsg{ID: msg.ID, Result: result, Error: rpcErr})
}

// dispatch 分发工具调用。
func (s *Server) dispatch(method string, params json.RawMessage, p *Principal) (any, *rpcErr) {
	if p == nil {
		return nil, &rpcErr{-32001, "unauthorized"}
	}
	need := func(scope string) *rpcErr {
		if p.IsAdmin || p.Scopes["argus:*"] || p.Scopes["argus:admin:*"] || p.Scopes[scope] {
			return nil
		}
		return &rpcErr{-32003, "insufficient scope: " + scope}
	}
	switch method {
	case "server.list":
		if e := need("argus:server:read"); e != nil {
			return nil, e
		}
		return s.serverList(p)
	case "server.get":
		if e := need("argus:server:read"); e != nil {
			return nil, e
		}
		return s.serverGet(params, p)
	case "server.exec":
		if e := need("argus:server:exec"); e != nil {
			return nil, e
		}
		return s.serverExec(params, p)
	case "fs.list", "fs.read":
		if e := need("argus:server:read"); e != nil {
			return nil, e
		}
		if method == "fs.list" {
			return s.fsList(params, p)
		}
		return s.fsRead(params, p)
	case "fs.write", "fs.delete":
		if e := need("argus:server:write"); e != nil {
			return nil, e
		}
		if method == "fs.write" {
			return s.fsWrite(params, p)
		}
		return s.fsDelete(params, p)
	case "meta.whoami":
		return map[string]any{"user_id": p.UserID}, nil
	default:
		return nil, &rpcErr{-32601, "unknown tool: " + method}
	}
}

func (s *Server) serverList(p *Principal) (any, *rpcErr) {
	var servers []model.Server
	q := s.DB.Order("id")
	if !p.IsAdmin {
		q = q.Where("owner_id = ?", p.UserID)
	}
	if len(p.ServerIDs) > 0 {
		ids := make([]int64, 0, len(p.ServerIDs))
		for id := range p.ServerIDs {
			ids = append(ids, id)
		}
		q = q.Where("id IN ?", ids)
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

func (s *Server) authorizedServer(id int64, p *Principal) (*model.Server, *rpcErr) {
	var sv model.Server
	if err := s.DB.First(&sv, id).Error; err != nil {
		return nil, &rpcErr{-32002, "server not found"}
	}
	if !p.IsAdmin && (sv.OwnerID != p.UserID || (len(p.ServerIDs) > 0 && !p.ServerIDs[id])) {
		return nil, &rpcErr{-32003, "server access denied"}
	}
	return &sv, nil
}

func (s *Server) serverGet(params json.RawMessage, p *Principal) (any, *rpcErr) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.ID <= 0 {
		return nil, &rpcErr{-32602, "id required"}
	}
	sv, authErr := s.authorizedServer(req.ID, p)
	if authErr != nil {
		return nil, authErr
	}
	return map[string]any{"id": sv.ID, "name": sv.Name, "group": sv.Group, "note": sv.Note}, nil
}

func (s *Server) serverExec(params json.RawMessage, p *Principal) (any, *rpcErr) {
	var req struct {
		ID      int64  `json:"id"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.ID <= 0 || req.Command == "" {
		return nil, &rpcErr{-32602, "id and command required"}
	}
	if _, authErr := s.authorizedServer(req.ID, p); authErr != nil {
		return nil, authErr
	}
	peer := s.Peers()[req.ID]
	if peer == nil {
		return nil, &rpcErr{-32002, "server offline"}
	}
	resp, err := peer.Call(protocol.MethodExec, protocol.ExecParams{Command: req.Command, Timeout: req.Timeout}, 60*time.Second)
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

func (s *Server) fsList(params json.RawMessage, p *Principal) (any, *rpcErr) {
	var req struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.ID <= 0 {
		return nil, &rpcErr{-32602, "id required"}
	}
	if _, authErr := s.authorizedServer(req.ID, p); authErr != nil {
		return nil, authErr
	}
	peer := s.Peers()[req.ID]
	if peer == nil {
		return nil, &rpcErr{-32002, "server offline"}
	}
	if req.Path == "" {
		req.Path = "/"
	}
	resp, err := peer.Call(protocol.MethodFsList, protocol.FsListParams{Path: req.Path}, 30*time.Second)
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

func (s *Server) fsRead(params json.RawMessage, p *Principal) (any, *rpcErr) {
	var req struct {
		ID     int64  `json:"id"`
		Path   string `json:"path"`
		Offset int64  `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.ID <= 0 || req.Path == "" {
		return nil, &rpcErr{-32602, "id and path required"}
	}
	if _, authErr := s.authorizedServer(req.ID, p); authErr != nil {
		return nil, authErr
	}
	peer := s.Peers()[req.ID]
	if peer == nil {
		return nil, &rpcErr{-32002, "server offline"}
	}
	resp, err := peer.Call(protocol.MethodFsRead, protocol.FsReadParams{Path: req.Path, Offset: req.Offset, Limit: req.Limit}, 30*time.Second)
	if err != nil {
		return nil, &rpcErr{-32603, err.Error()}
	}
	if resp.Error != nil {
		return nil, &rpcErr{-32003, resp.Error.Message}
	}
	var result protocol.FsReadResult
	raw, _ := json.Marshal(resp.Result)
	_ = json.Unmarshal(raw, &result)
	return map[string]any{"data": base64.StdEncoding.EncodeToString(result.Data), "eof": result.EOF, "size": result.Size}, nil
}

func (s *Server) fsWrite(params json.RawMessage, p *Principal) (any, *rpcErr) {
	var req struct {
		ID     int64  `json:"id"`
		Path   string `json:"path"`
		Data   string `json:"data"` // base64
		Append bool   `json:"append"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.ID <= 0 || req.Path == "" {
		return nil, &rpcErr{-32602, "id/path/data required"}
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		return nil, &rpcErr{-32602, "invalid base64"}
	}
	if _, authErr := s.authorizedServer(req.ID, p); authErr != nil {
		return nil, authErr
	}
	peer := s.Peers()[req.ID]
	if peer == nil {
		return nil, &rpcErr{-32002, "server offline"}
	}
	resp, err := peer.Call(protocol.MethodFsWrite, protocol.FsWriteParams{Path: req.Path, Data: data, Append: req.Append}, 30*time.Second)
	if err != nil {
		return nil, &rpcErr{-32603, err.Error()}
	}
	if resp.Error != nil {
		return nil, &rpcErr{-32003, resp.Error.Message}
	}
	var result protocol.FsWriteResult
	raw, _ := json.Marshal(resp.Result)
	_ = json.Unmarshal(raw, &result)
	return map[string]any{"bytes": result.Bytes}, nil
}

func (s *Server) fsDelete(params json.RawMessage, p *Principal) (any, *rpcErr) {
	var req struct {
		ID        int64  `json:"id"`
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.ID <= 0 || req.Path == "" {
		return nil, &rpcErr{-32602, "id and path required"}
	}
	if _, authErr := s.authorizedServer(req.ID, p); authErr != nil {
		return nil, authErr
	}
	peer := s.Peers()[req.ID]
	if peer == nil {
		return nil, &rpcErr{-32002, "server offline"}
	}
	resp, err := peer.Call(protocol.MethodFsDelete, protocol.FsDeleteParams{Path: req.Path, Recursive: req.Recursive}, 30*time.Second)
	if err != nil {
		return nil, &rpcErr{-32603, err.Error()}
	}
	if resp.Error != nil {
		return nil, &rpcErr{-32003, resp.Error.Message}
	}
	return map[string]any{"ok": true}, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
