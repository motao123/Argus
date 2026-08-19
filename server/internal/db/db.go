// Package db 初始化 SQLite（GORM）并执行迁移。
package db

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/model"
)

// Init 打开数据库、执行迁移、确保初始管理员存在。
func Init(dbPath, adminUser, adminPass string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	// 多连接写并发下 SQLite 需要 WAL
	gdb, err := gorm.Open(sqlite.Open(dbPath+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err == nil {
		sqlDB.SetMaxOpenConns(10)
		sqlDB.SetMaxIdleConns(5)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	// ServiceHistory 的旧索引不含探测点维度；在 AutoMigrate 创建新唯一索引前先
	// 移除旧版可能存在的同服务同分钟重复行，保留最新一条。
	if gdb.Migrator().HasTable(&model.ServiceHistory{}) && !gdb.Migrator().HasColumn(&model.ServiceHistory{}, "server_id") {
		if err := gdb.Exec(`DELETE FROM service_histories WHERE id NOT IN (
			SELECT MAX(id) FROM service_histories GROUP BY service_id, ts
		)`).Error; err != nil {
			return nil, fmt.Errorf("dedupe legacy service history: %w", err)
		}
	}

	if err := gdb.AutoMigrate(
		&model.Server{}, &model.User{}, &model.Alert{},
		&model.Notification{}, &model.Cron{}, &model.TaskRun{}, &model.TaskRunResult{}, &model.Metric{},
		&model.APIToken{},
		&model.Service{}, &model.ServiceProbe{}, &model.ServiceHistory{},
		&model.DDNSProfile{}, &model.DDNSRecordState{},
		&model.NAT{},
		&model.OAuthConfig{},
		&model.Session{},
		&model.RevokedSession{},
		&model.WAFBan{},
		&model.NotificationGroup{},
		&model.Transfer{},
		&model.TrafficBaseline{},
		&model.TrafficQuotaEvent{},
		&model.ServerTransfer{},
		&model.UpgradeJob{}, &model.UpgradeResult{},
		&model.AuditLog{},
		&model.MCPAuditLog{},
		&model.Clipboard{},
		&model.Setting{},
		&model.ServerGroup{},
		&model.OfflineNotify{},
		&model.TrafficReport{},
		&model.Incident{},
		&model.MaintenanceWindow{},
		&model.NotificationDelivery{},
		&model.AlertState{},
		&model.AlertBaseline{},
		&model.BackupSchedule{},
		&model.BackupRun{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// 旧服务自动建立默认探测点；旧历史归属原 Service.ServerID。两条 SQL 均幂等。
	if err := gdb.Exec(`INSERT OR IGNORE INTO service_probes
		(service_id, server_id, last_cert_identity, last_cert_warn_days, created_at)
		SELECT id, server_id, COALESCE(last_cert_identity, ''), COALESCE(last_cert_warn_days, 0), CURRENT_TIMESTAMP
		FROM services WHERE server_id > 0`).Error; err != nil {
		return nil, fmt.Errorf("backfill service probes: %w", err)
	}
	if err := gdb.Exec(`UPDATE service_histories
		SET server_id = COALESCE((SELECT services.server_id FROM services WHERE services.id = service_histories.service_id), 0)
		WHERE server_id IS NULL OR server_id = 0`).Error; err != nil {
		return nil, fmt.Errorf("backfill service history probes: %w", err)
	}

	// 初始管理员（幂等）
	var count int64
	gdb.Model(&model.User{}).Count(&count)
	if count == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		u := &model.User{Username: adminUser, PasswordHash: string(hash), Role: model.RoleAdmin, AgentSecret: agent.GenSecret()}
		if err := gdb.Create(u).Error; err != nil {
			return nil, fmt.Errorf("create admin: %w", err)
		}
	}
	// 存量用户若缺 Agent 密钥则逐用户补生成（禁止复用同一随机值，避免重复密钥）。
	var users []model.User
	gdb.Where("agent_secret = '' OR agent_secret IS NULL").Find(&users)
	for i := range users {
		if err := gdb.Model(&users[i]).Update("agent_secret", agent.GenSecret()).Error; err != nil {
			return nil, fmt.Errorf("backfill agent secrets: %w", err)
		}
	}
	// 处理历史重复密钥（旧版本曾用同一随机值回填），冲突则重新生成。
	seen := make(map[string]struct{})
	var all []model.User
	gdb.Order("id").Find(&all)
	for i := range all {
		if _, dup := seen[all[i].AgentSecret]; dup {
			if err := gdb.Model(&all[i]).Update("agent_secret", agent.GenSecret()).Error; err != nil {
				return nil, fmt.Errorf("dedupe agent secrets: %w", err)
			}
		}
		seen[all[i].AgentSecret] = struct{}{}
	}
	// 历史 Cron 没有租户归属，统一归属首个管理员，避免迁移后泄露给普通用户。
	var admin model.User
	if err := gdb.Where("role = ?", model.RoleAdmin).Order("id").First(&admin).Error; err == nil {
		if err := gdb.Model(&model.Cron{}).Where("owner_id = 0 OR owner_id IS NULL").Update("owner_id", admin.ID).Error; err != nil {
			return nil, fmt.Errorf("backfill cron owners: %w", err)
		}
		if err := gdb.Model(&model.Notification{}).Where("owner_id = 0 OR owner_id IS NULL").Update("owner_id", admin.ID).Error; err != nil {
			return nil, fmt.Errorf("backfill notification owners: %w", err)
		}
		if err := gdb.Model(&model.NotificationGroup{}).Where("owner_id = 0 OR owner_id IS NULL").Update("owner_id", admin.ID).Error; err != nil {
			return nil, fmt.Errorf("backfill notification group owners: %w", err)
		}
	}
	return gdb, nil
}
