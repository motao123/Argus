// Package mcp 实现 MCP（Model Context Protocol）HTTP 端点：
// JSON-RPC 2.0 消息，PAT 认证，供 AI/LLM 或脚本调用服务器管理能力。
// 借鉴 nezha /mcp 端点设计（简化版）。
package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	Enabled     bool
	RateLimit   int
	TransferMax int64
	TransferTTL time.Duration
	Audit       func(p *Principal, action, detail, ip string)

	mu        sync.Mutex
	limits    map[string]*rateWindow
	transfers map[string]*transfer
}

// Principal is the MCP authorization context supplied by the API package.
type Principal struct {
	UserID     int64
	IsAdmin    bool
	IsReadonly bool // readonly 角色：仅允许只读工具
	Scopes     map[string]bool
	ServerIDs  map[int64]bool
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

type rateWindow struct {
	start time.Time
	count int
}

type transfer struct {
	OwnerID  int64
	Mode     string
	ServerID int64
	Path     string
	MaxBytes int64
	Expires  time.Time
	SHA256   string
	IfMatch  string
	Used     bool
}

const protocolVersion = "2025-03-26"

// version 服务端版本，用于 initialize 的 serverInfo.version。
// 发布构建通过 ldflags 注入真实版本：
//
//	-X github.com/motao123/Argus/server/internal/mcp.version=$VERSION
//
// （参见 scripts/release-build.sh）；本地构建未注入时回退到开发版本常量。
var version = "0.1.0"

var toolDefinitions = []map[string]any{
	{"name": "server.list", "description": "List authorized Argus servers", "inputSchema": map[string]any{"type": "object"}},
	{"name": "server.get", "description": "Get an authorized server", "inputSchema": map[string]any{"type": "object", "required": []string{"id"}, "properties": map[string]any{"id": map[string]any{"type": "integer"}}}},
	{"name": "server.exec", "description": "Execute a command on a server", "inputSchema": map[string]any{"type": "object", "required": []string{"id", "command"}}},
	{"name": "fs.list", "description": "List a remote directory", "inputSchema": map[string]any{"type": "object", "required": []string{"id"}}},
	{"name": "fs.read", "description": "Read a remote file (legacy base64 response)", "inputSchema": map[string]any{"type": "object", "required": []string{"id", "path"}}},
	{"name": "fs.write", "description": "Write a remote file (legacy base64 input)", "inputSchema": map[string]any{"type": "object", "required": []string{"id", "path", "data"}}},
	{"name": "fs.delete", "description": "Delete a remote file", "inputSchema": map[string]any{"type": "object", "required": []string{"id", "path"}}},
	{"name": "fs.download_url", "description": "Create an expiring one-time file download URL", "inputSchema": map[string]any{"type": "object", "required": []string{"id", "path"}}},
	{"name": "fs.upload_url", "description": "Create an expiring one-time file upload URL", "inputSchema": map[string]any{"type": "object", "required": []string{"id", "path", "sha256", "if_match"}}},
	{"name": "meta.whoami", "description": "Return the PAT identity", "inputSchema": map[string]any{"type": "object"}},
}

// Handler MCP HTTP 处理器。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handle)
	mux.HandleFunc("/mcp/transfer/", s.handleTransfer)
	return mux
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*Principal, string, bool) {
	if !s.Enabled {
		writeJSON(w, http.StatusNotFound, &rpcMsg{Error: &rpcErr{-32004, "MCP disabled"}})
		return nil, "", false
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeJSON(w, http.StatusUnauthorized, &rpcMsg{Error: &rpcErr{-32001, "PAT required"}})
		return nil, "", false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if s.IdentifyPAT == nil || !strings.HasPrefix(raw, "argus_") {
		writeJSON(w, http.StatusUnauthorized, &rpcMsg{Error: &rpcErr{-32001, "PAT required"}})
		return nil, "", false
	}
	principal, ok := s.IdentifyPAT(raw)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, &rpcMsg{Error: &rpcErr{-32001, "invalid PAT"}})
		return nil, "", false
	}
	keySum := sha256.Sum256([]byte(raw))
	key := hex.EncodeToString(keySum[:])
	if !s.allow(key) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, &rpcMsg{Error: &rpcErr{-32029, "rate limit exceeded"}})
		return nil, "", false
	}
	return principal, key, true
}

func (s *Server) allow(key string) bool {
	limit := s.RateLimit
	if limit <= 0 {
		limit = 60
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.limits == nil {
		s.limits = make(map[string]*rateWindow)
	}
	window := s.limits[key]
	if window == nil || now.Sub(window.start) >= time.Minute {
		s.limits[key] = &rateWindow{start: now, count: 1}
		return true
	}
	if window.count >= limit {
		return false
	}
	window.count++
	return true
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		if _, _, ok := s.authenticate(w, r); !ok {
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet {
		if _, _, ok := s.authenticate(w, r); !ok {
			return
		}
		w.Header().Set("Allow", "POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, &rpcMsg{Error: &rpcErr{-32000, "SSE stream not available; use POST Streamable HTTP"}})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, _, ok := s.authenticate(w, r)
	if !ok {
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
	if s.Audit != nil {
		detail := msg.Method
		if rpcErr != nil {
			detail += " error=" + rpcErr.Message
		}
		s.Audit(principal, "mcp.call", detail, r.RemoteAddr)
	}
	// MCP 详细审计（对齐 nezha mcp_audit_logs）：仅 tools/call 记录
	// tool/args_hash/args_peek/outcome/error/ip。
	s.auditToolCall(msg, rpcErr, result, principal, r)
	if len(msg.ID) == 0 || string(msg.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, &rpcMsg{ID: msg.ID, Result: result, Error: rpcErr})
}

// auditToolCall 将一次 tools/call 调用写入 mcp_audit_logs 表。
func (s *Server) auditToolCall(msg rpcMsg, rpcErr *rpcErr, result any, p *Principal, r *http.Request) {
	if msg.Method != "tools/call" || s.DB == nil {
		return
	}
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	_ = json.Unmarshal(msg.Params, &params)
	entry := &model.MCPAuditLog{
		UserID: p.UserID,
		Tool:   params.Name,
		IP:     r.RemoteAddr,
	}
	// 参数指纹：sha256(JSON) + 截断预览（≤512 字符）
	if params.Arguments != nil {
		if b, err := json.Marshal(params.Arguments); err == nil {
			sum := sha256.Sum256(b)
			entry.ArgsHash = hex.EncodeToString(sum[:])
			peek := string(b)
			if len(peek) > 512 {
				peek = peek[:512]
			}
			entry.ArgsPeek = peek
			// 服务器级工具提取 server id
			if v, ok := params.Arguments["id"]; ok {
				switch x := v.(type) {
				case float64:
					entry.ServerID = int64(x)
				case int64:
					entry.ServerID = x
				case string:
					if n, err := strconv.ParseInt(x, 10, 64); err == nil {
						entry.ServerID = n
					}
				}
			}
		}
	}
	// outcome 分类：优先 rpcErr；tools/call 层把工具错误包装为
	// result.isError=true（text 携带错误信息），需要一并解析。
	isErrResult := false
	errText := ""
	if m, ok := result.(map[string]any); ok {
		if flag, ok := m["isError"].(bool); ok && flag {
			isErrResult = true
			switch content := m["content"].(type) {
			case []any:
				if len(content) > 0 {
					if first, ok := content[0].(map[string]any); ok {
						if text, ok := first["text"].(string); ok {
							errText = text
						}
					}
				}
			case []map[string]any:
				if len(content) > 0 {
					if text, ok := content[0]["text"].(string); ok {
						errText = text
					}
				}
			}
		}
	}
	switch {
	case rpcErr == nil && !isErrResult:
		entry.Outcome = "success"
	case rpcErr != nil && rpcErr.Code == -32602:
		entry.Outcome = "tool_not_found"
		entry.ErrorMsg = rpcErr.Message
	case rpcErr != nil && (rpcErr.Code == -32001 || rpcErr.Code == -32004):
		entry.Outcome = "unauthorized"
		entry.ErrorMsg = rpcErr.Message
	case rpcErr != nil && rpcErr.Code == -32029:
		entry.Outcome = "scope_denied"
		entry.ErrorMsg = rpcErr.Message
	case rpcErr == nil && isErrResult && strings.HasPrefix(errText, "unknown tool"):
		entry.Outcome = "tool_not_found"
		entry.ErrorMsg = errText
	default:
		entry.Outcome = "tool_error"
		if rpcErr != nil {
			entry.ErrorMsg = rpcErr.Message
		} else {
			entry.ErrorMsg = errText
		}
	}
	s.DB.Create(entry)
}

// readonlyTools readonly 角色可调用的 MCP 只读工具。
var readonlyTools = map[string]bool{
	"initialize":                true,
	"notifications/initialized": true,
	"ping":                      true,
	"tools/list":                true,
	"tools/call":                true,
	"server.list":               true,
	"server.get":                true,
	"fs.list":                   true,
	"fs.read":                   true,
	"fs.download_url":           true,
	"meta.whoami":               true,
}

// dispatch 分发工具调用。
func (s *Server) dispatch(method string, params json.RawMessage, p *Principal) (any, *rpcErr) {
	if p == nil {
		return nil, &rpcErr{-32001, "unauthorized"}
	}
	// readonly 角色仅放行只读工具（列表/详情/只读文件/下载），其余一律拒绝。
	if p.IsReadonly && !readonlyTools[method] {
		return nil, &rpcErr{-32003, "readonly role: read-only access only"}
	}
	need := func(scope string) *rpcErr {
		if p.IsAdmin || p.Scopes["argus:*"] || p.Scopes["argus:admin:*"] || p.Scopes[scope] {
			return nil
		}
		return &rpcErr{-32003, "insufficient scope: " + scope}
	}
	switch method {
	case "initialize":
		return map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "argus", "version": version}}, nil
	case "notifications/initialized":
		return map[string]any{}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefinitions}, nil
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(params, &call) != nil || call.Name == "" {
			return nil, &rpcErr{-32602, "tool name required"}
		}
		result, e := s.dispatch(call.Name, call.Arguments, p)
		if e != nil {
			return map[string]any{"content": []map[string]any{{"type": "text", "text": e.Message}}, "isError": true}, nil
		}
		raw, _ := json.Marshal(result)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(raw)}}, "structuredContent": result}, nil
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
	case "fs.write", "fs.delete", "fs.upload_url":
		if e := need("argus:server:write"); e != nil {
			return nil, e
		}
		if method == "fs.write" {
			return s.fsWrite(params, p)
		}
		if method == "fs.upload_url" {
			return s.transferURL(params, p, "upload")
		}
		return s.fsDelete(params, p)
	case "fs.download_url":
		if e := need("argus:server:read"); e != nil {
			return nil, e
		}
		return s.transferURL(params, p, "download")
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

func (s *Server) transferURL(params json.RawMessage, p *Principal, mode string) (any, *rpcErr) {
	var req struct {
		ID      int64  `json:"id"`
		Path    string `json:"path"`
		SHA256  string `json:"sha256"`
		IfMatch string `json:"if_match"`
		MaxSize int64  `json:"max_size"`
	}
	if json.Unmarshal(params, &req) != nil || req.ID <= 0 || req.Path == "" {
		return nil, &rpcErr{-32602, "id and path required"}
	}
	if _, e := s.authorizedServer(req.ID, p); e != nil {
		return nil, e
	}
	max := s.TransferMax
	if max <= 0 {
		max = 64 << 20
	}
	if req.MaxSize > 0 && req.MaxSize < max {
		max = req.MaxSize
	}
	if mode == "upload" {
		req.SHA256 = strings.ToLower(strings.TrimSpace(req.SHA256))
		if len(req.SHA256) != 64 || req.IfMatch == "" {
			return nil, &rpcErr{-32602, "sha256 and if_match required for upload"}
		}
		if _, err := hex.DecodeString(req.SHA256); err != nil {
			return nil, &rpcErr{-32602, "invalid sha256"}
		}
	}
	ttl := s.TransferTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, &rpcErr{-32603, "cannot create transfer"}
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	expires := time.Now().Add(ttl)
	s.mu.Lock()
	if s.transfers == nil {
		s.transfers = make(map[string]*transfer)
	}
	for key, item := range s.transfers {
		if item.Used || time.Now().After(item.Expires) {
			delete(s.transfers, key)
		}
	}
	s.transfers[token] = &transfer{OwnerID: p.UserID, Mode: mode, ServerID: req.ID, Path: req.Path, MaxBytes: max, Expires: expires, SHA256: req.SHA256, IfMatch: req.IfMatch}
	s.mu.Unlock()
	return map[string]any{"url": "/mcp/transfer/" + token, "method": map[string]string{"upload": "PUT", "download": "GET"}[mode], "expires_at": expires, "max_size": max, "sha256": req.SHA256, "if_match": req.IfMatch}, nil
}

func (s *Server) takeTransfer(token, method string) (*transfer, int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.transfers[token]
	if item == nil {
		return nil, http.StatusNotFound, "transfer not found"
	}
	if item.Used {
		return nil, http.StatusGone, "transfer already used"
	}
	if time.Now().After(item.Expires) {
		delete(s.transfers, token)
		return nil, http.StatusGone, "transfer expired"
	}
	expected := http.MethodGet
	if item.Mode == "upload" {
		expected = http.MethodPut
	}
	if method != expected {
		return nil, http.StatusMethodNotAllowed, "wrong transfer method"
	}
	// Consume before any remote I/O: failed attempts cannot be replayed.
	item.Used = true
	return item, 0, ""
}

func (s *Server) handleTransfer(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/mcp/transfer/")
	item, code, message := s.takeTransfer(token, r.Method)
	if item == nil {
		http.Error(w, message, code)
		return
	}
	peer := s.Peers()[item.ServerID]
	if peer == nil {
		http.Error(w, "server offline", http.StatusServiceUnavailable)
		return
	}
	if item.Mode == "download" {
		resp, err := peer.Call(protocol.MethodFsRead, protocol.FsReadParams{Path: item.Path, Limit: int(item.MaxBytes) + 1}, 2*time.Minute)
		if err != nil || resp.Error != nil {
			http.Error(w, "remote read failed", http.StatusBadGateway)
			return
		}
		var result protocol.FsReadResult
		raw, _ := json.Marshal(resp.Result)
		_ = json.Unmarshal(raw, &result)
		if int64(len(result.Data)) > item.MaxBytes || !result.EOF {
			http.Error(w, "file exceeds transfer size limit", http.StatusRequestEntityTooLarge)
			return
		}
		hash := sha256.Sum256(result.Data)
		etag := `"sha256:` + hex.EncodeToString(hash[:]) + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("X-Content-SHA256", hex.EncodeToString(hash[:]))
		w.Header().Set("Content-Length", strconv.Itoa(len(result.Data)))
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(result.Data)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, item.MaxBytes+1))
	if err != nil || int64(len(data)) > item.MaxBytes {
		http.Error(w, "upload exceeds transfer size limit", http.StatusRequestEntityTooLarge)
		return
	}
	hash := sha256.Sum256(data)
	got := hex.EncodeToString(hash[:])
	if got != item.SHA256 {
		http.Error(w, "sha256 mismatch", http.StatusUnprocessableEntity)
		return
	}
	current, currentErr := peer.Call(protocol.MethodFsRead, protocol.FsReadParams{Path: item.Path, Limit: int(item.MaxBytes) + 1}, 2*time.Minute)
	currentETag := `"missing"`
	if currentErr == nil && current.Error == nil {
		var existing protocol.FsReadResult
		raw, _ := json.Marshal(current.Result)
		_ = json.Unmarshal(raw, &existing)
		h := sha256.Sum256(existing.Data)
		currentETag = `"sha256:` + hex.EncodeToString(h[:]) + `"`
	}
	if item.IfMatch != "*" && item.IfMatch != currentETag {
		http.Error(w, "if-match precondition failed", http.StatusPreconditionFailed)
		return
	}
	resp, callErr := peer.Call(protocol.MethodFsWrite, protocol.FsWriteParams{Path: item.Path, Data: data}, 2*time.Minute)
	if callErr != nil || resp.Error != nil {
		http.Error(w, "remote write failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("ETag", `"sha256:`+got+`"`)
	w.Header().Set("X-Content-SHA256", got)
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
