// Package protocol 定义 Argus Agent ↔ Server 之间的线上协议。
// 基于 WebSocket + JSON-RPC 2.0（借鉴 komari 的协议设计，免 protobuf 工具链）。
package protocol

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ProtocolVersion identifies the additive wire protocol version.
const ProtocolVersion = "2"

// Stable capability names advertised during registration.
const (
	CapabilityMetrics  = "metrics"
	CapabilityProbe    = "probe"
	CapabilityCommand  = "command"
	CapabilityTerminal = "terminal"
	CapabilityFiles    = "files"
	CapabilityUpgrade  = "upgrade"
	CapabilityNAT      = "nat"
	CapabilityTrace    = "trace" // 路由追踪 + 带宽测速
)

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
	ErrParse        = -32700
	ErrInvalid      = -32600
	ErrMethod       = -32601
	ErrParams       = -32602
	ErrInternal     = -32603
	ErrUnauthorized = -32001
	ErrNotFound     = -32002
	// ErrCapabilityDisabled 目标 Agent 上对应能力被禁用（如 command/terminal 被关闭）。
	ErrCapabilityDisabled = -32003
)

// ---- 方法名 ----

const (
	// Agent → Server
	MethodRegister    = "agent.register"
	MethodReport      = "agent.report"
	MethodCheckUpdate = "agent.check_update"

	// Server → Agent
	MethodExec         = "agent.exec"
	MethodTerminal     = "agent.terminal"
	MethodTermData     = "agent.terminal.data"
	MethodTermResize   = "agent.terminal.resize"
	MethodTermClose    = "agent.terminal.close"
	MethodServiceCheck = "agent.service.check"
	MethodFsList       = "agent.fs.list"
	MethodFsRead       = "agent.fs.read"
	MethodFsWrite      = "agent.fs.write"
	MethodFsDelete     = "agent.fs.delete"
	MethodNATConnect   = "agent.nat.connect"
	MethodNATData      = "agent.nat.data"
	MethodNATClose     = "agent.nat.close"
	MethodApplyConfig  = "agent.apply_config"
	MethodUpgrade      = "agent.upgrade"

	// 网络测试（借鉴 komari networkTest，能力开关为 CapabilityTrace）
	MethodTrace          = "agent.trace"
	MethodBandwidthServe = "agent.bandwidth.serve"
	MethodBandwidthProbe = "agent.bandwidth.probe"
)

// AgentConfig 服务端下发的 Agent 运行配置（借鉴 nezha ApplyConfig）。
type AgentConfig struct {
	ServerURL        string        `json:"server_url,omitempty"` // WS 地址
	Interval         int           `json:"interval,omitempty"`   // 上报间隔（秒）
	Secret           string        `json:"secret,omitempty"`     // 新密钥
	Capabilities     *Capabilities `json:"capabilities,omitempty"`
	AutoUpdate       *bool         `json:"auto_update,omitempty"` // 自动更新检查（nil = 保持现状）
	InterfaceInclude []string      `json:"interface_include,omitempty"`
	InterfaceExclude []string      `json:"interface_exclude,omitempty"`
	MountInclude     []string      `json:"mount_include,omitempty"`
	MountExclude     []string      `json:"mount_exclude,omitempty"`
}

// Capabilities controls optional Agent features.
type Capabilities struct {
	Metrics  bool `json:"metrics"`
	Probe    bool `json:"probe"`
	Command  bool `json:"command"`
	Terminal bool `json:"terminal"`
	Files    bool `json:"files"`
	Upgrade  bool `json:"upgrade"`
	NAT      bool `json:"nat"`
	Trace    bool `json:"trace"`
}

// CapabilityNames 返回全部受支持的能力名（与 Capabilities 字段一一对应）。
func CapabilityNames() []string {
	return []string{CapabilityMetrics, CapabilityProbe, CapabilityCommand, CapabilityTerminal, CapabilityFiles, CapabilityUpgrade, CapabilityNAT, CapabilityTrace}
}

// ValidCapability 报告 name 是否为受支持的能力名。
func ValidCapability(name string) bool {
	for _, n := range CapabilityNames() {
		if n == name {
			return true
		}
	}
	return false
}

// ParseCapabilities 校验并规范化能力配置：仅接受已知能力名（未知名报错），
// 未提供的字段按禁用处理，保证返回的结构体八个字段全部显式设置。
// raw 为空或 null 时返回 nil（表示"不修改"，兼容旧客户端）。
func ParseCapabilities(raw json.RawMessage) (*Capabilities, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var m map[string]bool
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("capabilities: %w", err)
	}
	var caps Capabilities
	for name, enabled := range m {
		switch name {
		case CapabilityMetrics:
			caps.Metrics = enabled
		case CapabilityProbe:
			caps.Probe = enabled
		case CapabilityCommand:
			caps.Command = enabled
		case CapabilityTerminal:
			caps.Terminal = enabled
		case CapabilityFiles:
			caps.Files = enabled
		case CapabilityUpgrade:
			caps.Upgrade = enabled
		case CapabilityNAT:
			caps.NAT = enabled
		case CapabilityTrace:
			caps.Trace = enabled
		default:
			return nil, fmt.Errorf("unknown capability %q", name)
		}
	}
	return &caps, nil
}

// UpgradeParams Agent 自升级参数（下载 → SHA-256 校验 → 原子替换 → 重启）。
type UpgradeParams struct {
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

// ---- 自动更新检查 ----

// CheckUpdateParams Agent 自动更新检查请求（当前无需参数，版本号供服务端日志/后续使用）。
type CheckUpdateParams struct {
	Version string `json:"version,omitempty"` // 当前 Agent 版本
}

// CheckUpdateResult Agent 自动更新检查结果。全部字段为空表示无更新。
type CheckUpdateResult struct {
	Version string `json:"version,omitempty"` // 最新版本号
	URL     string `json:"url,omitempty"`     // 制品下载地址（http/https）
	SHA256  string `json:"sha256,omitempty"`  // 制品 SHA-256（64 位十六进制）
}

// ---- 上报结构 ----

// HostInfo 静态主机信息（首次上报与变更时携带）。
type HostInfo struct {
	Hostname        string `json:"hostname"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platform_version"`
	OS              string `json:"os,omitempty"`
	Arch            string `json:"arch,omitempty"`
	KernelVersion   string `json:"kernel_version,omitempty"`
	CPUModel        string `json:"cpu_model"`
	CPUCores        int    `json:"cpu_cores"`
	MemTotal        uint64 `json:"mem_total"`
	AgentVersion    string `json:"agent_version"`
	IP              string `json:"ip"` // 旧客户端/服务端兼容的主 IPv4
	IPv4            string `json:"ipv4,omitempty"`
	IPv6            string `json:"ipv6,omitempty"`
	CountryCode     string `json:"country_code"`
}

// Availability distinguishes a valid zero value from an unsupported or failed collector.
type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// GPUDevice is a bounded per-device summary; it intentionally contains no process data.
type GPUDevice struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Util     float64 `json:"util"`
	MemUsed  uint64  `json:"mem_used"`
	MemTotal uint64  `json:"mem_total"`
	Temp     float64 `json:"temp,omitempty"` // 摄氏度，传感器不可用时省略（0 表示无数据）
}

// GPUReport represents multi-GPU data and explicit platform/collector availability.
type GPUReport struct {
	Availability
	Devices []GPUDevice `json:"devices,omitempty"`
}

// ReportParams 周期状态上报（默认 2s 一次）。
type ReportParams struct {
	Host                    HostInfo     `json:"host,omitempty"` // 仅在变更/首报时填充
	CPU                     float64      `json:"cpu"`
	MemUsed                 uint64       `json:"mem_used"`
	MemTotal                uint64       `json:"mem_total"`
	SwapUsed                uint64       `json:"swap_used"`
	SwapTotal               uint64       `json:"swap_total"`
	DiskUsed                uint64       `json:"disk_used"`
	DiskTotal               uint64       `json:"disk_total"`
	NetInTransfer           uint64       `json:"net_in_transfer"`
	NetOutTransfer          uint64       `json:"net_out_transfer"`
	NetInSpeed              float64      `json:"net_in_speed"`
	NetOutSpeed             float64      `json:"net_out_speed"`
	Load1                   float64      `json:"load1"`
	Load5                   float64      `json:"load5"`
	Load15                  float64      `json:"load15"`
	TCPCount                int          `json:"tcp_count"` // legacy: all TCP sockets
	ProcessCount            int          `json:"process_count,omitempty"`
	TCPEstablished          int          `json:"tcp_established,omitempty"`
	TCPListen               int          `json:"tcp_listen,omitempty"`
	UDPCount                int          `json:"udp_count,omitempty"`
	DiskReadSpeed           float64      `json:"disk_read_speed,omitempty"`
	DiskWriteSpeed          float64      `json:"disk_write_speed,omitempty"`
	DiskReadIOPS            float64      `json:"disk_read_iops,omitempty"`
	DiskWriteIOPS           float64      `json:"disk_write_iops,omitempty"`
	Uptime                  uint64       `json:"uptime"`
	Temperature             float64      `json:"temperature"` // CPU 温度（摄氏度）
	GPUUtil                 float64      `json:"gpu_util"`    // 旧协议兼容：多卡平均利用率
	GPUMemUsed              uint64       `json:"gpu_mem_used"`
	GPUMemTotal             uint64       `json:"gpu_mem_total"`
	ProcessAvailability     Availability `json:"process_availability,omitempty"`
	SocketAvailability      Availability `json:"socket_availability,omitempty"`
	DiskIOAvailability      Availability `json:"disk_io_availability,omitempty"`
	TemperatureAvailability Availability `json:"temperature_availability,omitempty"`
	GPU                     GPUReport    `json:"gpu,omitempty"`
	// LatencyMs 最近一次 Ping→Pong 往返延迟（毫秒）；0 = 尚无测量（兼容旧 Agent）。
	LatencyMs int   `json:"latency_ms,omitempty"`
	Timestamp int64 `json:"ts"`
}

// RegisterParams 注册参数。Secret 为空表示首次注册，由服务端生成。
type RegisterParams struct {
	Secret       string        `json:"secret"`
	Protocol     string        `json:"protocol,omitempty"`
	Version      string        `json:"version,omitempty"`
	OS           string        `json:"os,omitempty"`
	Arch         string        `json:"arch,omitempty"`
	Capabilities *Capabilities `json:"capabilities,omitempty"`
}

// RegisterResult 注册结果。
type RegisterResult struct {
	ServerID     int64         `json:"server_id"`
	Secret       string        `json:"secret"`
	Capabilities *Capabilities `json:"capabilities,omitempty"`
}

// ---- 任务下发 ----

// ExecParams 远程命令执行参数。
type ExecParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"` // 秒，0 表示默认 30s
}

// ExecResult 远程命令执行结果。
type ExecResult struct {
	Output string `json:"output"` // 兼容旧 Agent 的合并输出
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
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

// TerminalResize 调整终端窗口大小。
type TerminalResize struct {
	SessionID string `json:"session_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// ---- 服务监控 ----

// KeyValue 自定义请求头键值对。
type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// AllowedHTTPMethods HTTP 探测支持的方法（server 与 agent 共用同一校验）。
var AllowedHTTPMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete,
}

// IsAllowedHTTPMethod 判断 HTTP 方法是否在支持列表内（大小写不敏感）。
func IsAllowedHTTPMethod(method string) bool {
	m := strings.ToUpper(strings.TrimSpace(method))
	for _, allowed := range AllowedHTTPMethods {
		if m == allowed {
			return true
		}
	}
	return false
}

// ServiceCheckParams 服务探测参数（server → agent）。
// Headers/Body/AssertContains 为可选字段：缺省时行为与旧版本一致。
type ServiceCheckParams struct {
	Type              string     `json:"type"`
	Target            string     `json:"target"`
	Timeout           int        `json:"timeout"`
	Method            string     `json:"method,omitempty"`
	VerifyTLS         *bool      `json:"verify_tls,omitempty"`
	ExpectedStatusMin int        `json:"expected_status_min,omitempty"`
	ExpectedStatusMax int        `json:"expected_status_max,omitempty"`
	Statuses          []int      `json:"statuses,omitempty"` // 期望状态码列表；非空时优先于 ExpectedStatusMin/Max
	MaxRedirects      int        `json:"max_redirects,omitempty"`
	PingCount         int        `json:"ping_count,omitempty"`
	Headers           []KeyValue `json:"headers,omitempty"`         // 自定义请求头（Host 经特殊处理）
	Body              string     `json:"body,omitempty"`            // 仅 POST/PUT/PATCH 发送
	AssertContains    string     `json:"assert_contains,omitempty"` // 响应体关键字断言，空 = 不启用
}

// ParseStatuses 解析逗号分隔的期望状态码列表（如 "200,301,404"），
// 返回去重保序的 []int；空串/纯空白返回 nil（表示使用状态码区间）。
// 任意一项为空、非数字或超出 100-599 时返回错误。
func ParseStatuses(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	seen := make(map[int]bool)
	var out []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty status code in list")
		}
		code, err := strconv.Atoi(part)
		if err != nil || code < 100 || code > 599 {
			return nil, fmt.Errorf("invalid status code %q (must be 100-599)", part)
		}
		if !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out, nil
}

// FormatStatuses 将状态码列表序列化为逗号分隔字符串（ParseStatuses 的逆操作）。
func FormatStatuses(statuses []int) string {
	parts := make([]string, len(statuses))
	for i, s := range statuses {
		parts[i] = strconv.Itoa(s)
	}
	return strings.Join(parts, ",")
}

// ServiceCheckResult 服务探测结果（agent → server）。新增字段可被旧 server 安全忽略。
type ServiceCheckResult struct {
	Up                     bool    `json:"up"`
	DelayMs                int     `json:"delay_ms"`
	Error                  string  `json:"error,omitempty"`
	StatusCode             int     `json:"status_code,omitempty"`
	DNSMs                  int     `json:"dns_ms,omitempty"`
	ConnectMs              int     `json:"connect_ms,omitempty"`
	TLSMs                  int     `json:"tls_ms,omitempty"`
	TTFBMs                 int     `json:"ttfb_ms,omitempty"`
	TLSVerificationSkipped bool    `json:"tls_verification_skipped,omitempty"`
	CertIssuer             string  `json:"cert_issuer,omitempty"`
	CertNotAfter           int64   `json:"cert_not_after,omitempty"`
	CertDaysRemaining      int     `json:"cert_days_remaining,omitempty"`
	Sent                   int     `json:"sent,omitempty"`
	Received               int     `json:"received,omitempty"`
	LossPercent            float64 `json:"loss_percent,omitempty"`
}

// ---- 文件管理 ----

// FsListParams 列目录参数。
type FsListParams struct {
	Path string `json:"path"`
}

// FsEntry 目录项。
type FsEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Mode     string `json:"mode"`
	IsDir    bool   `json:"is_dir"`
	Modified int64  `json:"modified"`
}

// FsListResult 目录列表结果。
type FsListResult struct {
	Path    string    `json:"path"`
	Entries []FsEntry `json:"entries"`
	Error   string    `json:"error,omitempty"`
}

// FsReadParams 读文件参数（分片）。
type FsReadParams struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
	Limit  int    `json:"limit"`
}

// FsReadResult 读文件结果（base64）。
type FsReadResult struct {
	Data  []byte `json:"data"`
	EOF   bool   `json:"eof"`
	Size  int64  `json:"size"`
	Error string `json:"error,omitempty"`
}

// FsWriteParams 写文件参数。
type FsWriteParams struct {
	Path   string `json:"path"`
	Data   []byte `json:"data"`
	Append bool   `json:"append"`
}

// FsWriteResult 写文件结果。
type FsWriteResult struct {
	Bytes int    `json:"bytes"`
	Error string `json:"error,omitempty"`
}

// NATConnectParams HTTP 隧道到 agent 后端的连接参数（server → agent）。
type NATConnectParams struct {
	SessionID string `json:"session_id"`
	Target    string `json:"target"` // HTTP 后端 host:port；不对外提供通用 TCP/UDP 入口
}

// NATConnectResult reports whether the backend socket was established.
type NATConnectResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// FsDeleteParams 删除参数。
type FsDeleteParams struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

// FsDeleteResult 删除结果。
type FsDeleteResult struct {
	Error string `json:"error,omitempty"`
}

// ---- 网络测试（路由追踪 / 带宽测速）----

// TraceProtocol 路由追踪协议：icmp（默认）/ tcp / udp。
type TraceProtocol string

const (
	TraceICMP TraceProtocol = "icmp"
	TraceTCP  TraceProtocol = "tcp"
	TraceUDP  TraceProtocol = "udp"
)

// TraceParams 路由追踪参数（server → agent）。
type TraceParams struct {
	Target    string        `json:"target"`
	Protocol  TraceProtocol `json:"protocol,omitempty"` // icmp/tcp/udp；空 = icmp
	MaxHops   int           `json:"max_hops,omitempty"` // 默认 30
	TimeoutSec int          `json:"timeout_sec,omitempty"` // 单跳探测超时；默认 3
}

// TraceHop 一跳的结果。
type TraceHop struct {
	Hop     int     `json:"hop"`
	IP      string  `json:"ip"`
	RTTMs   float64 `json:"rtt_ms"` // 平均往返毫秒；0 = 无响应
	Loss    float64 `json:"loss"`   // 丢包率 0-100；0 = 未知
	Reached bool    `json:"reached"` // 目标节点
}

// TraceResult 路由追踪结果（agent → server）。
type TraceResult struct {
	OK       bool       `json:"ok"`
	Error    string     `json:"error,omitempty"`
	Hops     []TraceHop `json:"hops,omitempty"`
	RawText  string     `json:"raw_text,omitempty"` // 原始输出（截断后）
	ExitCode int        `json:"exit_code,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
}

// BandwidthParams 带宽测速参数。
// Serve 模式：Agent 监听随机 TCP 端口等待连接，返回实际端口。
// Probe 模式：Agent 作为客户端向目标 host:port 持续发送 N 秒数据，返回吞吐。
type BandwidthParams struct {
	// Serve
	ListenAddr string `json:"listen_addr,omitempty"` // 监听地址（如 127.0.0.1:0）；空 = 0.0.0.0:0
	// Probe
	Target   string `json:"target,omitempty"` // host:port
	Duration int    `json:"duration,omitempty"` // 测速秒数；默认 5，上限 60
	Parallel int    `json:"parallel,omitempty"` // 并发连接数；默认 1，上限 8
}

// BandwidthResult 带宽测速结果（agent → server）。
type BandwidthResult struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	Port        int    `json:"port,omitempty"`         // Serve 模式：监听端口
	BitsPerSec  float64 `json:"bits_per_sec,omitempty"` // Probe 模式：吞吐（bit/s）
	BytesSent   uint64 `json:"bytes_sent,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
}
