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
	ID         int64     `gorm:"primaryKey" json:"id"`
	OwnerID    int64     `gorm:"index;default:0" json:"owner_id"` // 任务所有者；历史数据回填为管理员
	Name       string    `gorm:"size:64;not null" json:"name"`
	Expression string    `gorm:"size:64;not null" json:"expression"` // cron 表达式
	Command    string    `gorm:"size:1024;not null" json:"command"`
	ServerIDs  string    `gorm:"size:512;default:''" json:"server_ids"` // 逗号分隔；空 = 全部
	Enabled    bool      `gorm:"default:true" json:"enabled"`
	LastResult string    `gorm:"size:2048;default:''" json:"last_result"`
	LastRunAt  time.Time `json:"last_run_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// Metric 降采样指标（granularity: 60=分钟 / 300=5分钟 / 3600=小时）。
type Metric struct {
	ID          int64     `gorm:"primaryKey" json:"-"`
	ServerID    int64     `gorm:"index:idx_metric_server_ts" json:"server_id"`
	TS          int64     `gorm:"index:idx_metric_server_ts" json:"ts"` // unix 秒（整周期）
	Granularity int       `gorm:"index:idx_metric_gran" json:"-"`       // 60/300/3600
	CPU         float64   `json:"cpu"`
	MemUsed     uint64    `json:"mem_used"`
	MemTotal    uint64    `json:"mem_total"`
	DiskUsed    uint64    `json:"disk_used"`
	DiskTotal   uint64    `json:"disk_total"`
	NetInSpeed  float64   `json:"net_in_speed"`
	NetOutSpeed float64   `json:"net_out_speed"`
	Load1       float64   `json:"load1"`
	Temperature float64   `json:"temperature,omitempty"`
	GPUUtil     float64   `json:"gpu_util,omitempty"`
	CreatedAt   time.Time `json:"-"`
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
	// 故障通知（借鉴 nezha 服务故障通知到通知组）
	Notify          bool      `gorm:"default:false" json:"notify"`
	NotifyWebhookID int64     `json:"notify_webhook_id"` // 0 = 无
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ServiceHistory 探测历史（分钟级聚合，保留 30 天）。
type ServiceHistory struct {
	ID        int64 `gorm:"primaryKey" json:"-"`
	ServiceID int64 `gorm:"index:idx_svc_hist" json:"service_id"`
	Ts        int64 `gorm:"index:idx_svc_hist" json:"ts"`
	UpCount   int   `json:"up_count"`
	Total     int   `json:"total"`
	DelaySum  int64 `json:"delay_sum"`
}

// DDNSProfile 动态解析配置（借鉴 nezha DDNS）。
type DDNSProfile struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	OwnerID     int64     `gorm:"index;default:0" json:"owner_id"`
	ServerID    int64     `gorm:"index" json:"server_id"` // 监听该服务器 IP 变化
	Name        string    `gorm:"size:64;not null" json:"name"`
	Provider    string    `gorm:"size:32;default:'webhook'" json:"provider"` // webhook / cloudflare
	AccessKey   string    `gorm:"size:256;default:''" json:"-"`              // API Token
	Domains     string    `gorm:"size:1024;not null" json:"domains"`         // 逗号分隔域名
	WebhookURL  string    `gorm:"size:512;default:''" json:"webhook_url"`    // 含 {ip} 占位符
	LastIP      string    `gorm:"size:64;default:''" json:"last_ip"`
	LastUpdated time.Time `json:"last_updated"`
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
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
