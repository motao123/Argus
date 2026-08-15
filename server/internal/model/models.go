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
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// User 管理员账号。
type User struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:32;not null;uniqueIndex" json:"username"`
	PasswordHash string    `gorm:"size:128;not null" json:"-"`
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
