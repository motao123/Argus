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
	Tags      string    `gorm:"size:512;default:''" json:"tags"` // 逗号分隔
	SortOrder int       `gorm:"default:0" json:"sort_order"`
	Hidden    bool      `gorm:"default:false" json:"hidden"` // guest 不可见
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// 用户角色
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

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
type TrafficReport struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	WebhookID int64     `json:"webhook_id"`
	Hour      int       `gorm:"default:9" json:"hour"` // 每天几点发送
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
	ID            int64     `gorm:"primaryKey" json:"id"`
	OwnerID       int64     `gorm:"index;default:0" json:"owner_id"` // 0 = admin 所有（兼容旧数据）
	Name          string    `gorm:"size:64;not null" json:"name"`
	Metric        string    `gorm:"size:32;not null" json:"metric"` // cpu/mem/disk/net_in_speed/net_out_speed/load1/offline
	Min           *float64  `json:"min"`                            // 下限（nil 不检查）
	Max           *float64  `json:"max"`                            // 上限（nil 不检查）
	Duration      int       `json:"duration"`                       // 持续秒数
	Notify        bool      `gorm:"default:true" json:"notify"`
	WebhookID     int64     `json:"webhook_id"`                            // 单渠道（兼容）
	GroupID       int64     `json:"group_id"`                              // 通知分组（0=无）
	TriggerCronID int64     `json:"trigger_cron_id"`                       // 触发时执行的任务（0=无）
	ServerIDs     string    `gorm:"size:512;default:''" json:"server_ids"` // 逗号分隔；空 = 全部（仅 admin）
	TriggerRatio  *int      `json:"trigger_ratio"`                         // 采样达标比例（1-100，如 70=70% 采样超限才触发；nil=全部采样）
	Enabled       bool      `gorm:"default:true" json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
}

// Notification 通知渠道（借鉴 komari 多渠道设计 + nezha 模板）。
// Type: webhook / bark / telegram / email / serverchan
type Notification struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OwnerID   int64     `gorm:"index;default:0" json:"owner_id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	Type      string    `gorm:"size:16;default:'webhook'" json:"type"`
	URL       string    `gorm:"size:512;not null" json:"url"`
	Method    string    `gorm:"size:8;default:'POST'" json:"method"`
	Headers   string    `gorm:"size:2048;default:'{}'" json:"headers"` // JSON 对象
	Body      string    `gorm:"size:2048;default:'{}'" json:"body"`    // 支持 {{title}} {{content}} 模板
	ChatID    string    `gorm:"size:64;default:''" json:"chat_id"`     // telegram/email 目标
	CreatedAt time.Time `json:"created_at"`
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
	MaxRedirects      int    `gorm:"default:3" json:"max_redirects"`
	PingCount         int    `gorm:"default:4" json:"ping_count"`
	CertWarn          bool   `gorm:"default:true" json:"cert_warn"`
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
type ServiceHistory struct {
	ID         int64   `gorm:"primaryKey" json:"-"`
	ServiceID  int64   `gorm:"index:idx_svc_hist" json:"service_id"`
	Ts         int64   `gorm:"index:idx_svc_hist" json:"ts"`
	UpCount    int     `json:"up_count"`
	Total      int     `json:"total"`
	DelaySum   int64   `json:"delay_sum"`
	DelayMin   int     `json:"delay_min"`
	DelayMax   int     `json:"delay_max"`
	Sent       int     `json:"sent"`
	Received   int     `json:"received"`
	StatusCode int     `json:"status_code"`
	DNSMs      int     `json:"dns_ms"`
	ConnectMs  int     `json:"connect_ms"`
	TLSMs      int     `json:"tls_ms"`
	TTFBMs     int     `json:"ttfb_ms"`
	CertDays   *int    `json:"cert_days"`
	CertIssuer string  `gorm:"size:512;default:''" json:"cert_issuer"`
	CertExpiry int64   `json:"cert_expiry"`
	LossSum    float64 `json:"loss_sum"`
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
