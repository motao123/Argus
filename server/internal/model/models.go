// Package model 定义 Argus 数据模型。
package model

import (
	"fmt"
	"time"
)

// Server 一台被监控服务器（持久化配置）。
type Server struct {
	ID           int64  `gorm:"primaryKey" json:"id"`
	Name         string `gorm:"size:64;not null" json:"name"`
	Secret       string `gorm:"size:64;not null;uniqueIndex" json:"-"`
	Group        string `gorm:"column:group_name;size:64;default:''" json:"group"` // group 是 SQL 保留字
	Note         string `gorm:"size:255;default:''" json:"note"`
	OwnerID      int64  `gorm:"index;default:0" json:"owner_id"` // 0 = admin 所有
	Protocol     string `gorm:"size:32;default:''" json:"protocol,omitempty"`
	Version      string `gorm:"size:64;default:''" json:"version,omitempty"`
	OS           string `gorm:"size:32;default:''" json:"os,omitempty"`
	Arch         string `gorm:"size:32;default:''" json:"arch,omitempty"`
	Capabilities string `gorm:"size:1024;default:''" json:"capabilities,omitempty"`
	// 计费信息（借鉴 komari，VPS 售卖场景）
	Price     float64    `gorm:"default:0" json:"price"`
	CycleDays int        `gorm:"default:0" json:"cycle_days"` // 计费周期（天），0 = 无
	ExpireAt  *time.Time `json:"expire_at"`
	AutoRenew bool       `gorm:"default:false" json:"auto_renew"`
	// 月度流量额度。周期日在服务器时区的本地零点切换；额度 0 表示不限制。
	TrafficQuotaBytes uint64 `gorm:"default:0" json:"traffic_quota_bytes"`
	TrafficCycleDay   int    `gorm:"default:1" json:"traffic_cycle_day"`
	TrafficTimezone   string `gorm:"size:64;default:'UTC'" json:"traffic_timezone"`
	TrafficAccounting string `gorm:"size:8;default:'sum'" json:"traffic_accounting"` // sum/in/out/max
	// 标签与展示（借鉴 komari 标签 + nezha 排序/隐藏）
	Tags      string `gorm:"size:512;default:''" json:"tags"` // 逗号分隔
	SortOrder int    `gorm:"default:0" json:"sort_order"`
	Hidden    bool   `gorm:"default:false" json:"hidden"` // guest 不可见
	// SloTarget 月度可用性目标（百分比，如 99.9）；0 = 不启用 SLO。
	SloTarget float64   `gorm:"default:99.9" json:"slo_target"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// 用户角色。readonly 为只读角色：可查看公开视图与自有服务器状态，
// 但禁止一切写操作（执行/文件/任务/告警/配置等）。
const (
	RoleAdmin    = "admin"
	RoleUser     = "user"
	RoleReadonly = "readonly"
)

// IsValidRole 校验角色是否合法（历史数据默认 user 兼容）。
func IsValidRole(role string) bool {
	return role == RoleAdmin || role == RoleUser || role == RoleReadonly
}

// User 用户账号（多用户：admin 全部权限，user 仅自己名下服务器）。
type User struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:32;not null;uniqueIndex" json:"username"`
	PasswordHash string    `gorm:"size:128;not null" json:"-"`
	Role         string    `gorm:"size:16;default:'user'" json:"role"`
	AgentSecret  string    `gorm:"size:64;default:''" json:"-"` // 用户专属 Agent 注册密钥
	TwoFASecret  string    `gorm:"size:64;default:''" json:"-"`
	TwoFAEnabled bool      `gorm:"default:false" json:"two_fa_enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// TrafficReport 流量定时报告配置（借鉴 komari 流量报告通知）。
// Period: daily（每日，默认，兼容旧配置）/ weekly（每周）/ monthly（每月）。
// 发送时刻：daily 用 Hour；weekly 用 Weekday+Hour；monthly 用 Day+Hour。
type TrafficReport struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	WebhookID int64     `json:"webhook_id"`
	Period    string    `gorm:"size:16;default:'daily'" json:"period"` // daily / weekly / monthly
	Hour      int       `gorm:"default:9" json:"hour"`                 // 发送时刻（小时 0-23）
	Weekday   int       `gorm:"default:1" json:"weekday"`              // weekly：星期几（0=周日 … 6=周六；默认 1=周一）
	Day       int       `gorm:"default:1" json:"day"`                  // monthly：每月几号（1-28）
	Enabled   bool      `gorm:"default:false" json:"enabled"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OfflineNotify 离线/上线通知配置（借鉴 komari notifier/offline）。
type OfflineNotify struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	WebhookID    int64     `json:"webhook_id"`                      // 0 = 不通知
	OfflineAfter int       `gorm:"default:60" json:"offline_after"` // 离线多少秒后通知
	Enabled      bool      `gorm:"default:true" json:"enabled"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// OfflineAfterStr 格式化离线时长（秒 → 可读）。
func (o *OfflineNotify) OfflineAfterStr() string {
	d := time.Duration(o.OfflineAfter) * time.Second
	if d >= time.Hour {
		return fmt.Sprintf("%.0f 小时", d.Hours())
	}
	if d >= time.Minute {
		return fmt.Sprintf("%.0f 分钟", d.Minutes())
	}
	return fmt.Sprintf("%d 秒", o.OfflineAfter)
}

// NotificationGroup 通知分组（多对多扇出，借鉴 nezha）。
type NotificationGroup struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OwnerID   int64     `gorm:"index;default:0" json:"owner_id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	MemberIDs string    `gorm:"size:1024;default:''" json:"member_ids"` // 逗号分隔的 Notification ID
	CreatedAt time.Time `json:"created_at"`
}

// Alert 报警规则。
type Alert struct {
	ID            int64    `gorm:"primaryKey" json:"id"`
	OwnerID       int64    `gorm:"index;default:0" json:"owner_id"` // 0 = admin 所有（兼容旧数据）
	Name          string   `gorm:"size:64;not null" json:"name"`
	Metric        string   `gorm:"size:32;not null" json:"metric"` // cpu/mem/disk/net_in_speed/net_out_speed/load1/offline
	Min           *float64 `json:"min"`                            // 下限（nil 不检查）
	Max           *float64 `json:"max"`                            // 上限（nil 不检查）
	Duration      int      `json:"duration"`                       // 持续秒数
	Notify        bool     `gorm:"default:true" json:"notify"`
	WebhookID     int64    `json:"webhook_id"`                            // 单渠道（兼容）
	GroupID       int64    `json:"group_id"`                              // 通知分组（0=无）
	TriggerCronID int64    `json:"trigger_cron_id"`                       // 触发时执行的任务（0=无）
	ServerIDs     string   `gorm:"size:512;default:''" json:"server_ids"` // 逗号分隔；空 = 全部（仅 admin）
	TriggerRatio  *int     `json:"trigger_ratio"`                         // 采样达标比例（1-100，如 70=70% 采样超限才触发；nil=全部采样）
	// Template 自定义通知模板（可空）：首行为标题、其余为正文；
	// 支持 {{event}}/{{server.*}}/{{rule}}/{{metric}}/{{value}}/{{threshold}}/{{time}} 等变量；空 = 默认格式。
	Template string `gorm:"size:2048;default:''" json:"template"`
	Enabled  bool   `gorm:"default:true" json:"enabled"`
	// 确认：AckedAt/AckedBy 非空表示规则当前告警已被确认，确认期间不再发送触发通知；恢复时自动清除。
	AckedAt     *time.Time `json:"acked_at"`
	AckedBy     string     `gorm:"size:32;default:''" json:"acked_by"`
	SilenceFrom *time.Time `json:"silence_from"` // 静默开始（nil = 从现在起）
	SilenceTo   *time.Time `json:"silence_to"`   // 静默结束；静默期间不发送通知
	// 重复提醒：RepeatMinutes > 0 表示告警持续期间每 N 分钟重发一次通知（event=repeat）；0 = 不重复。
	RepeatMinutes int `json:"repeat_minutes"`
	// 升级：EscalateToChannelID > 0 且告警持续超过 EscalateAfterMinutes 分钟后，
	// 首次发送 event=escalated 并切换渠道，此后重复通知（event=repeat）改发该渠道；
	// 需校验渠道存在且 owner 匹配。EscalateToChannelID = 0 表示不升级；
	// EscalateAfterMinutes = 0 表示触发后立即升级。
	EscalateToChannelID  int64     `json:"escalate_to_channel_id"`
	EscalateAfterMinutes int       `json:"escalate_after_minutes"`
	// 周期流量规则的周期（借鉴 nezha CycleStart/CycleUnit/CycleInterval）：
	// CycleStart 为空（或 CycleUnit 为空）时回退服务器配置的月度周期
	// （TrafficCycleDay/TrafficTimezone）。
	CycleStart    *time.Time `json:"cycle_start,omitempty"`
	CycleUnit     string     `gorm:"size:8;default:''" json:"cycle_unit,omitempty"` // hour/day/week/month/year
	CycleInterval int        `gorm:"default:1" json:"cycle_interval,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// AlertState 告警持续状态（报警引擎持久化，重启后恢复重复提醒/升级进度）。
// 一条规则 × 一台服务器一行；告警恢复后由引擎清除。
type AlertState struct {
	AlertID      int64      `gorm:"primaryKey" json:"alert_id"`
	ServerID     int64      `gorm:"primaryKey" json:"server_id"`
	TriggeredAt  time.Time  `json:"triggered_at"`   // 首次触发时间（升级延迟计时基准）
	LastNotifyAt time.Time  `json:"last_notify_at"` // 上次通知时间（重复间隔基准）
	EscalatedAt  *time.Time `json:"escalated_at"`   // 升级时间（nil = 未升级）
	UpdatedAt    time.Time  `json:"updated_at"`
}

// AlertBaseline 累计流量规则（transfer_in/out/all）的计数基线：
// 规则首次评估时基线 = 当前累计值（自规则启用起计），触发通知后重置为当前值
// （衡量自上次告警以来的流量）。key = alert_id + server_id。
type AlertBaseline struct {
	AlertID   int64     `gorm:"primaryKey" json:"alert_id"`
	ServerID  int64     `gorm:"primaryKey" json:"server_id"`
	In        uint64    `json:"in"`
	Out       uint64    `json:"out"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Notification 通知渠道（借鉴 komari 多渠道设计 + nezha 模板）。
// Type: webhook / bark / telegram / email / serverchan / javascript /
// dingtalk / wecom / feishu / slack / wxpusher / matrix
// 预设渠道（dingtalk/wecom/feishu/slack/wxpusher/matrix）的专属配置存于 Extra（JSON）。
type Notification struct {
	ID      int64  `gorm:"primaryKey" json:"id"`
	OwnerID int64  `gorm:"index;default:0" json:"owner_id"`
	Name    string `gorm:"size:64;not null" json:"name"`
	Type    string `gorm:"size:16;default:'webhook'" json:"type"`
	URL     string `gorm:"size:512;not null" json:"url"`
	Method  string `gorm:"size:8;default:'POST'" json:"method"`
	Headers string `gorm:"size:2048;default:'{}'" json:"headers"` // JSON 对象
	Body    string `gorm:"size:2048;default:'{}'" json:"body"`    // 支持 {{title}} {{content}} 模板
	ChatID  string `gorm:"size:64;default:''" json:"chat_id"`     // telegram/email 目标
	Extra   string `gorm:"type:text;default:''" json:"extra"`     // 预设渠道专属 JSON 配置（脱敏不回显）
	// 渠道级限流（0 = 不限）。RateLimitPerMin 为每分钟补充的令牌数（持续补充），
	// BurstLimit 为令牌桶容量（允许的瞬时突发投递数）；两者都 > 0 时限流才生效，
	// 任一为 0 表示不限（旧渠道默认 0/0，行为不变）。
	RateLimitPerMin int       `gorm:"default:0" json:"rate_limit_per_min"`
	BurstLimit      int       `gorm:"default:0" json:"burst_limit"`
	CreatedAt       time.Time `json:"created_at"`
}

// 通知送达状态。
const (
	DeliveryPending = "pending"
	DeliverySent    = "sent"
	DeliveryFailed  = "failed"
)

// NotificationDelivery 通知送达记录（持久队列状态机）。
// 每次通知（报警/离线/报告/测试/服务事件等）落一条记录，由 notifier.Queue 负责
// 指数退避重试：Status pending → sent / failed；Attempts 达到 MaxAttempts 仍失败则标记 failed。
type NotificationDelivery struct {
	ID        int64  `gorm:"primaryKey" json:"id"`
	WebhookID int64  `gorm:"index;not null" json:"webhook_id"` // 通知渠道 ID
	OwnerID   int64  `gorm:"index;default:0" json:"owner_id"`  // 触发方（报警规则 owner；0 = 系统/管理员流程）
	Title     string `gorm:"size:256;default:''" json:"title"`
	Content   string `gorm:"size:4096;default:''" json:"content"`
	// ContextData 渠道级 Body 模板渲染上下文（notifyctx 变量表 JSON；
	// 含 {{event}}/{{server.name}} 等，重试/补发时保持同一份变量）。
	ContextData string     `gorm:"type:text;default:''" json:"-"`
	Status      string     `gorm:"size:16;index;not null;default:'pending'" json:"status"` // pending/sent/failed
	Attempts    int        `gorm:"default:0" json:"attempts"`
	MaxAttempts int        `gorm:"default:5" json:"max_attempts"`
	NextRetry   *time.Time `gorm:"index" json:"next_retry"` // 下次重试时间；failed/sent 后为 nil
	LastError   string     `gorm:"size:1024;default:''" json:"last_error"`
	SentAt      *time.Time `json:"sent_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Cron 定时任务。
type Cron struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	OwnerID       int64     `gorm:"index;default:0" json:"owner_id"` // 任务所有者；历史数据回填为管理员
	Name          string    `gorm:"size:64;not null" json:"name"`
	Expression    string    `gorm:"size:64;not null" json:"expression"` // cron 表达式
	Command       string    `gorm:"size:1024;not null" json:"command"`
	ServerIDs     string    `gorm:"size:512;default:''" json:"server_ids"` // 逗号分隔；空 = 全部
	Enabled       bool      `gorm:"default:true" json:"enabled"`
	SkipIfRunning bool      `gorm:"default:true" json:"skip_if_running"`
	LastResult    string    `gorm:"size:2048;default:''" json:"last_result"` // 兼容旧客户端
	LastRunAt     time.Time `json:"last_run_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// TaskRun 一次任务执行。Trigger: scheduled/manual/alert_failure/alert_recovery。
type TaskRun struct {
	ID          int64           `gorm:"primaryKey" json:"id"`
	CronID      int64           `gorm:"index;not null" json:"cron_id"`
	OwnerID     int64           `gorm:"index;not null" json:"owner_id"`
	Trigger     string          `gorm:"size:32;not null" json:"trigger"`
	Status      string          `gorm:"size:24;index;not null" json:"status"`
	Command     string          `gorm:"size:1024;not null" json:"command"`
	TargetCount int             `json:"target_count"`
	StartedAt   *time.Time      `json:"started_at"`
	FinishedAt  *time.Time      `json:"finished_at"`
	DurationMS  int64           `json:"duration_ms"`
	Error       string          `gorm:"size:2048;default:''" json:"error"`
	CreatedAt   time.Time       `gorm:"index" json:"created_at"`
	Results     []TaskRunResult `gorm:"foreignKey:RunID" json:"results,omitempty"`
}

// TaskRunResult 记录一次执行在单个目标上的结果。stdout/stderr 分别限制为 64KiB。
type TaskRunResult struct {
	ID         int64  `gorm:"primaryKey" json:"id"`
	RunID      int64  `gorm:"index;not null" json:"run_id"`
	ServerID   int64  `gorm:"index;not null" json:"server_id"`
	ServerName string `gorm:"size:64;default:''" json:"server_name"`
	Status     string `gorm:"size:24;not null" json:"status"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Stdout     string `gorm:"type:text" json:"stdout"`
	Stderr     string `gorm:"type:text" json:"stderr"`
	Error      string `gorm:"size:2048;default:''" json:"error"`
	Truncated  bool   `gorm:"default:false" json:"truncated"`
}

// Metric 降采样指标（granularity: 60=分钟 / 300=5分钟 / 3600=小时）。
type Metric struct {
	ID             int64     `gorm:"primaryKey" json:"-"`
	ServerID       int64     `gorm:"index:idx_metric_server_ts" json:"server_id"`
	TS             int64     `gorm:"index:idx_metric_server_ts" json:"ts"` // unix 秒（整周期）
	Granularity    int       `gorm:"index:idx_metric_gran" json:"-"`       // 60/300/3600
	CPU            float64   `json:"cpu"`
	MemUsed        uint64    `json:"mem_used"`
	MemTotal       uint64    `json:"mem_total"`
	DiskUsed       uint64    `json:"disk_used"`
	DiskTotal      uint64    `json:"disk_total"`
	NetInSpeed     float64   `json:"net_in_speed"`
	NetOutSpeed    float64   `json:"net_out_speed"`
	Load1          float64   `json:"load1"`
	Temperature    float64   `json:"temperature,omitempty"`
	GPUUtil        float64   `json:"gpu_util,omitempty"`
	ProcessCount   float64   `json:"process_count,omitempty"`
	TCPEstablished float64   `json:"tcp_established,omitempty"`
	TCPListen      float64   `json:"tcp_listen,omitempty"`
	UDPCount       float64   `json:"udp_count,omitempty"`
	DiskReadSpeed  float64   `json:"disk_read_speed,omitempty"`
	DiskWriteSpeed float64   `json:"disk_write_speed,omitempty"`
	DiskReadIOPS   float64   `json:"disk_read_iops,omitempty"`
	DiskWriteIOPS  float64   `json:"disk_write_iops,omitempty"`
	CreatedAt      time.Time `json:"-"`
}

// APIToken 个人访问令牌（PAT），借鉴 nezha 的 scope + 白名单设计。
type APIToken struct {
	ID        int64      `gorm:"primaryKey" json:"id"`
	UserID    int64      `gorm:"index" json:"user_id"`
	Name      string     `gorm:"size:128;not null" json:"name"`
	TokenHash string     `gorm:"size:64;not null" json:"-"`
	Scopes    string     `gorm:"size:1024;not null" json:"scopes"`       // 逗号分隔，如 argus:server:read,argus:server:exec
	ServerIDs string     `gorm:"size:2048;default:''" json:"server_ids"` // 逗号分隔；空 = 全部
	ExpiresAt *time.Time `json:"expires_at"`
	Revoked   bool       `gorm:"default:false" json:"revoked"`
	CreatedAt time.Time  `json:"created_at"`
}

// Service 服务监控定义（HTTP/TCP/Ping 探测，借鉴 nezha ServiceSentinel）。
type Service struct {
	ID       int64  `gorm:"primaryKey" json:"id"`
	OwnerID  int64  `gorm:"index;default:0" json:"owner_id"`
	ServerID int64  `gorm:"index;not null" json:"server_id"` // 由哪个 agent 执行探测
	Name     string `gorm:"size:64;not null" json:"name"`
	Type     string `gorm:"size:16;not null" json:"type"` // http / tcp / ping
	Target   string `gorm:"size:512;not null" json:"target"`
	Interval int    `gorm:"default:60" json:"interval"` // 秒
	Enabled  bool   `gorm:"default:true" json:"enabled"`
	Hidden   bool   `gorm:"default:false" json:"hidden"`
	// 探测参数。VerifyTLS 默认 true；旧记录由哨兵按 true 处理。
	HTTPMethod        string `gorm:"size:8;default:'GET'" json:"http_method"`
	VerifyTLS         *bool  `gorm:"default:true" json:"verify_tls"`
	Timeout           int    `gorm:"default:10" json:"timeout"`
	ExpectedStatusMin int    `gorm:"default:200" json:"expected_status_min"`
	ExpectedStatusMax int    `gorm:"default:399" json:"expected_status_max"`
	// ExpectedStatuses 期望状态码列表（逗号分隔，如 "200,301,404"）；空 = 使用
	// ExpectedStatusMin/Max 区间判定。两者同时设置时列表优先（agent 判定逻辑保证）。
	ExpectedStatuses string `gorm:"size:256;default:''" json:"expected_statuses"`
	MaxRedirects     int    `gorm:"default:3" json:"max_redirects"`
	PingCount        int    `gorm:"default:4" json:"ping_count"`
	CertWarn         bool   `gorm:"default:true" json:"cert_warn"`
	// 自定义请求（HTTP 专用，缺省为空 = 旧行为）。
	RequestHeaders string `gorm:"type:text;default:''" json:"request_headers"` // JSON: [{"key","value"}]
	RequestBody    string `gorm:"type:text;default:''" json:"request_body"`    // 仅 POST/PUT/PATCH 发送
	AssertContains string `gorm:"type:text;default:''" json:"assert_contains"` // 响应体关键字断言，空 = 不启用
	// 故障/恢复通知及任务分别接线，避免恢复误执行故障任务。
	Notify                bool      `gorm:"default:false" json:"notify"`
	NotifyWebhookID       int64     `json:"notify_webhook_id"` // 单渠道（兼容）
	NotificationGroupID   int64     `json:"notification_group_id"`
	FailureTriggerCronID  int64     `json:"failure_trigger_cron_id"`
	RecoveryTriggerCronID int64     `json:"recovery_trigger_cron_id"`
	LastCertIdentity      string    `gorm:"size:768;default:''" json:"-"`
	LastCertWarnDays      int       `gorm:"default:0" json:"-"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// ServiceHistory 探测历史（分钟级聚合，保留 30 天）。
// 延迟分位数（P50/P95/P99/标准差/抖动）为哨兵内存滑动窗口的快照：
// 窗口保存每服务最近 DelayWindowSize 次成功探测的延迟（跨分钟），
// 分钟桶落库时写入当前窗口值；DelaySamples < DelayMinSamples（30）时无意义，API 输出 null。
type ServiceHistory struct {
	ID            int64   `gorm:"primaryKey" json:"-"`
	ServiceID     int64   `gorm:"index:idx_svc_hist" json:"service_id"`
	Ts            int64   `gorm:"index:idx_svc_hist" json:"ts"`
	UpCount       int     `json:"up_count"`
	Total         int     `json:"total"`
	DelaySum      int64   `json:"delay_sum"`
	DelayMin      int     `json:"delay_min"`
	DelayMax      int     `json:"delay_max"`
	DelayP50      int     `json:"delay_p50"`
	DelayP95      int     `json:"delay_p95"`
	DelayP99      int     `json:"delay_p99"`
	DelayStdDevMs int     `gorm:"column:delay_stddev_ms" json:"delay_stddev_ms"`
	DelayJitterMs int     `json:"delay_jitter_ms"`
	DelaySamples  int     `json:"delay_samples"` // 窗口快照时的样本数
	Sent          int     `json:"sent"`
	Received      int     `json:"received"`
	StatusCode    int     `json:"status_code"`
	DNSMs         int     `json:"dns_ms"`
	ConnectMs     int     `json:"connect_ms"`
	TLSMs         int     `json:"tls_ms"`
	TTFBMs        int     `json:"ttfb_ms"`
	CertDays      *int    `json:"cert_days"`
	CertIssuer    string  `gorm:"size:512;default:''" json:"cert_issuer"`
	CertExpiry    int64   `json:"cert_expiry"`
	LossSum       float64 `json:"loss_sum"`
}

// DDNSProfile 动态解析配置。RecordType: A / AAAA / dual。
type DDNSProfile struct {
	ID             int64             `gorm:"primaryKey" json:"id"`
	OwnerID        int64             `gorm:"index;default:0" json:"owner_id"`
	ServerID       int64             `gorm:"index" json:"server_id"`
	Name           string            `gorm:"size:64;not null" json:"name"`
	Provider       string            `gorm:"size:32;default:'webhook'" json:"provider"`
	RecordType     string            `gorm:"size:8;default:'A'" json:"record_type"`
	AccessKey      string            `gorm:"size:256;default:''" json:"-"`
	Domains        string            `gorm:"size:2048;not null" json:"domains"`
	WebhookURL     string            `gorm:"size:1024;default:''" json:"webhook_url"`
	WebhookMethod  string            `gorm:"size:8;default:'GET'" json:"webhook_method"`
	WebhookHeaders string            `gorm:"size:4096;default:'{}'" json:"webhook_headers"`
	WebhookBody    string            `gorm:"type:text" json:"webhook_body"`
	LastIP         string            `gorm:"size:64;default:''" json:"last_ip"`
	LastUpdated    time.Time         `json:"last_updated"`
	Enabled        bool              `gorm:"default:true" json:"enabled"`
	CreatedAt      time.Time         `json:"created_at"`
	Records        []DDNSRecordState `gorm:"foreignKey:ProfileID" json:"records,omitempty"`
}

// DDNSRecordState 持久化每个域名和记录类型的独立状态，供部分失败与重启恢复。
type DDNSRecordState struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	ProfileID   int64      `gorm:"uniqueIndex:idx_ddns_record;index;not null" json:"profile_id"`
	OwnerID     int64      `gorm:"index;not null" json:"owner_id"`
	Domain      string     `gorm:"size:253;uniqueIndex:idx_ddns_record;not null" json:"domain"`
	RecordType  string     `gorm:"size:4;uniqueIndex:idx_ddns_record;not null" json:"record_type"`
	Status      string     `gorm:"size:16;default:'pending'" json:"status"`
	LastIP      string     `gorm:"size:64;default:''" json:"last_ip"`
	LastAttempt *time.Time `json:"last_attempt"`
	LastSuccess *time.Time `json:"last_success"`
	LastError   string     `gorm:"size:1024;default:''" json:"last_error"`
	RetryCount  int        `gorm:"default:0" json:"retry_count"`
	NextRetry   *time.Time `gorm:"index" json:"next_retry"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// NAT 内网穿透配置（借鉴 nezha NAT）：域名 → 服务器上的内网服务。
type NAT struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	OwnerID    int64     `gorm:"index;default:0" json:"owner_id"`
	ServerID   int64     `gorm:"index" json:"server_id"`
	Domain     string    `gorm:"size:256;not null;uniqueIndex" json:"domain"`
	TargetAddr string    `gorm:"size:256;not null" json:"target_addr"` // 内网 host:port
	Enabled    bool      `gorm:"default:true" json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	// Runtime-only HTTP tunnel state populated by the API.
	Status                 string `gorm:"-" json:"status,omitempty"`
	ActiveConnections      int    `gorm:"-" json:"active_connections"`
	ServerConnectionLimit  int    `gorm:"-" json:"server_connection_limit"`
	OwnerActiveConnections int    `gorm:"-" json:"owner_active_connections"`
	OwnerConnectionLimit   int    `gorm:"-" json:"owner_connection_limit"`
}

// OAuthConfig 持久化的 OAuth2 provider 配置（JSON）。
type OAuthConfig struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	Name          string    `gorm:"size:32;not null;uniqueIndex" json:"name"`
	ClientID      string    `gorm:"size:256;not null" json:"-"`
	ClientSecret  string    `gorm:"size:256;not null" json:"-"`
	AuthURL       string    `gorm:"size:512" json:"-"`
	TokenURL      string    `gorm:"size:512" json:"-"`
	UserInfoURL   string    `gorm:"size:512" json:"-"`
	UsernameField string    `gorm:"size:64;default:'login'" json:"-"`
	AdminLogins   string    `gorm:"size:512;default:''" json:"-"`
	Enabled       bool      `gorm:"default:true" json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
}

// Session 登录会话（借鉴 nezha JWTSession + komari Session）。
type Session struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `gorm:"index" json:"user_id"`
	JTI       string    `gorm:"size:64;not null;uniqueIndex" json:"-"` // JWT ID，踢出用
	UserAgent string    `gorm:"size:256;default:''" json:"user_agent"`
	IP        string    `gorm:"size:64;default:''" json:"ip"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RevokedSession 持久化 JWT 吊销状态；会话记录删除后仍可在重启后拒绝旧令牌。
type RevokedSession struct {
	ID        int64     `gorm:"primaryKey" json:"-"`
	JTI       string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	ExpiresAt time.Time `gorm:"index" json:"-"`
	CreatedAt time.Time `json:"-"`
}

// WAF 封禁来源：全局速率超限 / 登录失败限流 / 管理员手动封禁。
const (
	BanSourceRate   = "rate"
	BanSourceLogin  = "login"
	BanSourceManual = "manual"
)

// WAFBan IP 封禁记录（持久化；全局 WAF 与登录限流共用，支持手动封禁/解封）。
// ExpireAt 为空表示永久封禁；到期自动解封（中间件惰性清理，管理员解封即时生效）。
type WAFBan struct {
	ID        int64      `gorm:"primaryKey" json:"id"`
	IP        string     `gorm:"size:64;not null;uniqueIndex" json:"ip"`
	Reason    string     `gorm:"size:255;default:''" json:"reason"`
	Count     int        `gorm:"default:1" json:"count"`                          // 累计触发次数（同一 IP 重复触发递增）
	Source    string     `gorm:"size:16;not null;default:'manual'" json:"source"` // rate / login / manual
	BannedAt  time.Time  `json:"banned_at"`
	ExpireAt  *time.Time `json:"expire_at"` // nil = 永久
	CreatedAt time.Time  `json:"created_at"`
}

// Setting 站点设置键值（借鉴 komari DB 存储配置）。
type Setting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"size:1024" json:"value"`
}

// Clipboard 剪贴板条目（借鉴 komari CloudClipboard）。
type Clipboard struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `gorm:"index" json:"user_id"`
	Title     string    `gorm:"size:128" json:"title"`
	Content   string    `gorm:"size:8192" json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Transfer 周期流量打点（小时级，借鉴 nezha Transfer）。
type Transfer struct {
	ID       int64  `gorm:"primaryKey" json:"-"`
	ServerID int64  `gorm:"index:idx_transfer" json:"server_id"`
	Ts       int64  `gorm:"index:idx_transfer" json:"ts"` // 小时（unix/3600*3600）
	In       uint64 `json:"in"`
	Out      uint64 `json:"out"`
}

// TrafficBaseline 流量计数基线（重启恢复：Agent 累计计数器的最后已知值）。
type TrafficBaseline struct {
	ServerID int64  `gorm:"primaryKey" json:"server_id"`
	In       uint64 `json:"in"`
	Out      uint64 `json:"out"`
	TS       int64  `json:"ts"`
}

// TrafficQuotaEvent 记录每台服务器每个流量周期已发送的额度阈值事件。
// 唯一索引保证服务重启或并发检查时，80/90/100 通知在一个周期内各发送一次。
type TrafficQuotaEvent struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	ServerID   int64     `gorm:"uniqueIndex:idx_traffic_quota_event" json:"server_id"`
	CycleStart int64     `gorm:"uniqueIndex:idx_traffic_quota_event" json:"cycle_start"`
	Threshold  int       `gorm:"uniqueIndex:idx_traffic_quota_event" json:"threshold"`
	UsageBytes uint64    `json:"usage_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

// ServerTransfer 服务器过户（借鉴 nezha server transfer 状态机简化版）。
// 发起时轮换服务器密钥为 NewSecret：新 owner 用其重连即完成验证。
type ServerTransfer struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	ServerID       int64     `gorm:"index" json:"server_id"`
	ServerName     string    `gorm:"size:64" json:"server_name"`
	FromUserID     int64     `json:"from_user_id"`
	ToUserID       int64     `json:"to_user_id"`
	ToUsername     string    `gorm:"size:32" json:"to_username"`
	Status         string    `gorm:"size:16;default:'pending'" json:"status"` // pending/verified/cancelled/failed
	NewSecret      string    `gorm:"size:64;default:''" json:"-"`             // 一次性握手密钥（验证后即为服务器新密钥）
	RollbackSecret string    `gorm:"size:64;default:''" json:"-"`             // 原密钥（取消/超时回滚）
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// UpgradeJob 是一次持久化 Agent 批量升级任务。
type UpgradeJob struct {
	ID          int64           `gorm:"primaryKey" json:"id"`
	URL         string          `gorm:"size:2048;not null" json:"url"`
	SHA256      string          `gorm:"size:64;not null" json:"sha256"`
	Version     string          `gorm:"size:64;default:''" json:"version"`
	Status      string          `gorm:"size:24;index;not null" json:"status"` // pending/running/completed/interrupted
	Concurrency int             `gorm:"default:2" json:"concurrency"`
	TargetCount int             `json:"target_count"`
	CreatedBy   int64           `json:"created_by"`
	StartedAt   *time.Time      `json:"started_at"`
	FinishedAt  *time.Time      `json:"finished_at"`
	CreatedAt   time.Time       `gorm:"index" json:"created_at"`
	Results     []UpgradeResult `gorm:"foreignKey:JobID" json:"results"`
}

// UpgradeResult 记录升级任务中每台机器的独立结果。
type UpgradeResult struct {
	ID         int64      `gorm:"primaryKey" json:"id"`
	JobID      int64      `gorm:"uniqueIndex:idx_upgrade_target;not null" json:"job_id"`
	ServerID   int64      `gorm:"uniqueIndex:idx_upgrade_target;index;not null" json:"server_id"`
	ServerName string     `gorm:"size:64;default:''" json:"name"`
	Status     string     `gorm:"size:24;index;not null" json:"status"` // pending/running/success/failure/offline/interrupted
	Error      string     `gorm:"size:2048;default:''" json:"error,omitempty"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// AuditLog 审计日志（管理操作记录，借鉴 komari Log）。
type AuditLog struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `gorm:"size:32" json:"username"`
	Action    string    `gorm:"size:64" json:"action"` // 如 server.create / alert.delete
	Detail    string    `gorm:"size:512" json:"detail"`
	IP        string    `gorm:"size:64" json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

// ServerGroup 服务器分组（借鉴 nezha server-group）。
type ServerGroup struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OwnerID   int64     `gorm:"index;default:0" json:"owner_id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// 事故严重级别与状态（状态页事故时间线）。
const (
	IncidentSeverityMinor    = "minor"
	IncidentSeverityMajor    = "major"
	IncidentSeverityCritical = "critical"

	IncidentStatusOngoing  = "ongoing"
	IncidentStatusResolved = "resolved"
)

// Incident 状态页事故记录（公开时间线展示；管理端按 owner 隔离）。
type Incident struct {
	ID        int64      `gorm:"primaryKey" json:"id"`
	OwnerID   int64      `gorm:"index;default:0" json:"owner_id"` // 0 = admin 所有
	Title     string     `gorm:"size:128;not null" json:"title"`
	Severity  string     `gorm:"size:16;default:'minor'" json:"severity"` // minor/major/critical
	Status    string     `gorm:"size:16;default:'ongoing'" json:"status"` // ongoing/resolved
	ServerIDs string     `gorm:"size:512;default:''" json:"server_ids"`   // 逗号分隔；空 = 全部
	Notes     string     `gorm:"size:4096;default:''" json:"notes"`
	StartAt   time.Time  `json:"start_at"`
	EndAt     *time.Time `json:"end_at"` // nil = 尚未结束
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// MaintenanceWindow 维护窗口。Recurring = 每周按 StartAt 的星期/时刻重复，
// 窗口时长为 StartAt→EndAt 间隔（跨午夜/跨周末支持，须小于 7 天）。
// ServerIDs 为空表示覆盖全部服务器。
type MaintenanceWindow struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OwnerID   int64     `gorm:"index;default:0" json:"owner_id"`
	Title     string    `gorm:"size:128;not null" json:"title"`
	ServerIDs string    `gorm:"size:512;default:''" json:"server_ids"` // 逗号分隔；空 = 全部
	StartAt   time.Time `json:"start_at"`
	EndAt     time.Time `json:"end_at"`
	Recurring bool      `gorm:"default:false" json:"recurring"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BackupSchedule 定时加密备份计划（里程碑9：加密备份与恢复演练）。
// 执行流程：VACUUM INTO 一致性快照 → AES-256-GCM 加密 → 目标上传（HTTP PUT）或写入本地目录。
// 加密密钥由 KeyProvider（环境变量/密钥文件/JWT 密钥兜底）经 HKDF-SHA256 派生，
// 库中仅保存来源标签 KeySource、随机盐 KeySalt 与派生密钥指纹 KeyID，不落盘明文。
type BackupSchedule struct {
	ID      int64  `gorm:"primaryKey" json:"id"`
	Name    string `gorm:"size:64;not null" json:"name"`
	Enabled bool   `gorm:"default:false" json:"enabled"`
	Cron    string `gorm:"size:64;not null" json:"cron"`     // cron 表达式（5 段）
	Target  string `gorm:"size:1024;not null" json:"target"` // http(s) PUT URL 或本地绝对目录
	// KeepCount 保留份数：本地目标删除超出份数的旧文件；远程目标仅裁剪历史记录（对象删除由服务端策略管理）。
	KeepCount int    `gorm:"default:7" json:"keep_count"`
	KeySource string `gorm:"size:128;default:''" json:"key_source"` // 密钥来源标签（env:/file:/jwt:），非密钥本身
	KeySalt   string `gorm:"size:64;default:''" json:"-"`           // 每计划随机盐（hex），用于 HKDF
	KeyID     string `gorm:"size:16;default:''" json:"key_id"`      // 派生密钥指纹（hex，前 8 字节）
	// 最近一次执行状态
	LastRunAt  *time.Time `json:"last_run_at"`
	LastStatus string     `gorm:"size:16;default:''" json:"last_status"` // success / failed / running
	LastError  string     `gorm:"size:1024;default:''" json:"last_error"`
	LastSize   int64      `json:"last_size"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// BackupRun 一次备份执行记录（审计与保留清理依据）。
type BackupRun struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	ScheduleID int64     `gorm:"index;not null" json:"schedule_id"`
	Trigger    string    `gorm:"size:16;default:'cron'" json:"trigger"` // cron / manual
	Status     string    `gorm:"size:16;not null" json:"status"`        // success / failed
	Target     string    `gorm:"size:1024;default:''" json:"target"`
	Size       int64     `json:"size"`
	SHA256     string    `gorm:"size:64;default:''" json:"sha256"` // 密文 SHA-256
	Error      string    `gorm:"size:1024;default:''" json:"error"`
	DurationMS int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}
