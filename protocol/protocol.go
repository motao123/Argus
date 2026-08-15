// Package protocol 定义 Argus Agent ↔ Server 之间的线上协议。
// 基于 WebSocket + JSON-RPC 2.0（借鉴 komari 的协议设计，免 protobuf 工具链）。
package protocol

import "encoding/json"

// ---- JSON-RPC 2.0 信封 ----

// Request 是请求或通知（无 ID 即为通知）。
type Request struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response 是请求的应答。
type Response struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// RPCError 是 JSON-RPC 2.0 错误对象。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewError(code int, message string) *RPCError {
	return &RPCError{Code: code, Message: message}
}

// 标准错误码
const (
	ErrParse     = -32700
	ErrInvalid   = -32600
	ErrMethod    = -32601
	ErrParams    = -32602
	ErrInternal  = -32603
	ErrUnauthorized = -32001
	ErrNotFound  = -32002
)

// ---- 方法名 ----

const (
	// Agent → Server
	MethodRegister = "agent.register"
	MethodReport   = "agent.report"

	// Server → Agent
	MethodExec        = "agent.exec"
	MethodTerminal    = "agent.terminal"
	MethodTermData    = "agent.terminal.data"
	MethodTermClose   = "agent.terminal.close"
	MethodServiceCheck = "agent.service.check"
)

// ---- 上报结构 ----

// HostInfo 静态主机信息（首次上报与变更时携带）。
type HostInfo struct {
	Hostname        string `json:"hostname"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	CPUModel        string `json:"cpu_model"`
	CPUCores        int    `json:"cpu_cores"`
	MemTotal        uint64 `json:"mem_total"`
	AgentVersion    string `json:"agent_version"`
	IP              string `json:"ip"`
	CountryCode     string `json:"country_code"`
}

// ReportParams 周期状态上报（默认 2s 一次）。
type ReportParams struct {
	Host           HostInfo `json:"host,omitempty"` // 仅在变更/首报时填充
	CPU            float64  `json:"cpu"`
	MemUsed        uint64   `json:"mem_used"`
	MemTotal       uint64   `json:"mem_total"`
	SwapUsed       uint64   `json:"swap_used"`
	SwapTotal      uint64   `json:"swap_total"`
	DiskUsed       uint64   `json:"disk_used"`
	DiskTotal      uint64   `json:"disk_total"`
	NetInTransfer  uint64   `json:"net_in_transfer"`
	NetOutTransfer uint64   `json:"net_out_transfer"`
	NetInSpeed     float64  `json:"net_in_speed"`
	NetOutSpeed    float64  `json:"net_out_speed"`
	Load1          float64  `json:"load1"`
	Load5          float64  `json:"load5"`
	Load15         float64  `json:"load15"`
	TCPCount       int      `json:"tcp_count"`
	Uptime         uint64   `json:"uptime"`
	Timestamp      int64    `json:"ts"`
}

// RegisterParams 注册参数。Secret 为空表示首次注册，由服务端生成。
type RegisterParams struct {
	Secret string `json:"secret"`
}

// RegisterResult 注册结果。
type RegisterResult struct {
	ServerID int64  `json:"server_id"`
	Secret   string `json:"secret"`
}

// ---- 任务下发 ----

// ExecParams 远程命令执行参数。
type ExecParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"` // 秒，0 表示默认 30s
}

// ExecResult 远程命令执行结果。
type ExecResult struct {
	Output string `json:"output"`
	Code   int    `json:"code"`
	Error  string `json:"error,omitempty"`
}

// TerminalParams 打开终端会话。
type TerminalParams struct {
	SessionID string `json:"session_id"`
	Command   string `json:"command,omitempty"` // 默认使用用户 shell
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// TerminalData 终端字节（base64 编码传输）。
type TerminalData struct {
	SessionID string `json:"session_id"`
	Data      []byte `json:"data"`
}

// ---- 服务监控 ----

// ServiceCheckParams 服务探测参数（server → agent）。
type ServiceCheckParams struct {
	Type    string `json:"type"`    // http / tcp / ping
	Target  string `json:"target"`  // URL / host:port / host
	Timeout int    `json:"timeout"` // 秒
}

// ServiceCheckResult 服务探测结果（agent → server）。
type ServiceCheckResult struct {
	Up      bool   `json:"up"`
	DelayMs int    `json:"delay_ms"`
	Error   string `json:"error,omitempty"`
}
