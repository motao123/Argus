// Package model 定义 Argus 数据模型。
package model

import "time"

// Server 一台被监控服务器（持久化配置）。
type Server struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	Secret    string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	Group     string    `gorm:"size:64;default:''" json:"group"`
	Note      string    `gorm:"size:255;default:''" json:"note"`
	OwnerID   int64     `gorm:"index;default:0" json:"owner_id"` // 0 = admin 所有
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
	CreatedAt    time.Time `json:"created_at"`
}

// Alert 报警规则。
type Alert struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	Metric    string    `gorm:"size:32;not null" json:"metric"` // cpu/mem/disk/net_in_speed/net_out_speed/load1/offline
	Min       *float64  `json:"min"`                            // 下限（nil 不检查）
	Max       *float64  `json:"max"`                            // 上限（nil 不检查）
	Duration  int       `json:"duration"`                       // 持续秒数
	Notify    bool      `gorm:"default:true" json:"notify"`
	WebhookID int64     `json:"webhook_id"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// Notification 通知渠道（Webhook）。
type Notification struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	URL       string    `gorm:"size:512;not null" json:"url"`
	Method    string    `gorm:"size:8;default:'POST'" json:"method"`
	Headers   string    `gorm:"size:2048;default:'{}'" json:"headers"` // JSON 对象
	Body      string    `gorm:"size:2048;default:'{}'" json:"body"`    // 支持 {{title}} {{content}} 模板
	CreatedAt time.Time `json:"created_at"`
}

// Cron 定时任务。
type Cron struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	Name       string    `gorm:"size:64;not null" json:"name"`
	Expression string    `gorm:"size:64;not null" json:"expression"` // cron 表达式
	Command    string    `gorm:"size:1024;not null" json:"command"`
	ServerIDs  string    `gorm:"size:512;default:''" json:"server_ids"` // 逗号分隔；空 = 全部
	Enabled    bool      `gorm:"default:true" json:"enabled"`
	LastResult string    `gorm:"size:2048;default:''" json:"last_result"`
	LastRunAt  time.Time `json:"last_run_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// Metric 分钟级降采样指标。
type Metric struct {
	ID         int64     `gorm:"primaryKey" json:"-"`
	ServerID   int64     `gorm:"index:idx_metric_server_ts" json:"server_id"`
	TS         int64     `gorm:"index:idx_metric_server_ts" json:"ts"` // unix 秒（整分钟）
	CPU        float64   `json:"cpu"`
	MemUsed    uint64    `json:"mem_used"`
	MemTotal   uint64    `json:"mem_total"`
	DiskUsed   uint64    `json:"disk_used"`
	DiskTotal  uint64    `json:"disk_total"`
	NetInSpeed float64   `json:"net_in_speed"`
	NetOutSpeed float64  `json:"net_out_speed"`
	Load1      float64   `json:"load1"`
	CreatedAt  time.Time `json:"-"`
}

// APIToken 个人访问令牌（PAT），借鉴 nezha 的 scope + 白名单设计。
type APIToken struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `gorm:"index" json:"user_id"`
	Name      string    `gorm:"size:128;not null" json:"name"`
	TokenHash string    `gorm:"size:64;not null" json:"-"`
	Scopes    string    `gorm:"size:1024;not null" json:"scopes"`   // 逗号分隔，如 argus:server:read,argus:server:exec
	ServerIDs string    `gorm:"size:2048;default:''" json:"server_ids"` // 逗号分隔；空 = 全部
	ExpiresAt *time.Time `json:"expires_at"`
	Revoked   bool      `gorm:"default:false" json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}

// Service 服务监控定义（HTTP/TCP/Ping 探测，借鉴 nezha ServiceSentinel）。
type Service struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OwnerID   int64     `gorm:"index;default:0" json:"owner_id"`
	ServerID  int64     `gorm:"index;not null" json:"server_id"` // 由哪个 agent 执行探测
	Name      string    `gorm:"size:64;not null" json:"name"`
	Type      string    `gorm:"size:16;not null" json:"type"` // http / tcp / ping
	Target    string    `gorm:"size:512;not null" json:"target"`
	Interval  int       `gorm:"default:60" json:"interval"` // 秒
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
