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

	if err := gdb.AutoMigrate(
		&model.Server{}, &model.User{}, &model.Alert{},
		&model.Notification{}, &model.Cron{}, &model.Metric{},
		&model.APIToken{},
		&model.Service{}, &model.ServiceHistory{},
		&model.DDNSProfile{},
		&model.NAT{},
		&model.OAuthConfig{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	// 初始管理员（幂等）
	var count int64
	gdb.Model(&model.User{}).Count(&count)
	if count == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		u := &model.User{Username: adminUser, PasswordHash: string(hash), Role: model.RoleAdmin}
		if err := gdb.Create(u).Error; err != nil {
			return nil, fmt.Errorf("create admin: %w", err)
		}
	}
	return gdb, nil
}
