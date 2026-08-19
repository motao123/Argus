package db

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func TestInitMigratesLegacyServiceProbes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-services.db")
	legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec(`CREATE TABLE services (
		id integer PRIMARY KEY AUTOINCREMENT,
		owner_id integer,
		server_id integer NOT NULL,
		name text NOT NULL,
		type text NOT NULL,
		target text NOT NULL,
		interval integer,
		enabled numeric,
		hidden numeric,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec(`CREATE TABLE service_histories (
		id integer PRIMARY KEY AUTOINCREMENT,
		service_id integer,
		ts integer,
		up_count integer,
		total integer,
		delay_sum integer
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec("INSERT INTO services (id, owner_id, server_id, name, type, target, interval, enabled) VALUES (1, 1, 9, 'legacy', 'http', 'https://example.com', 60, 1)").Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec("INSERT INTO service_histories (service_id, ts, up_count, total, delay_sum) VALUES (1, 100, 1, 1, 20), (1, 100, 0, 1, 30)").Error; err != nil {
		t.Fatal(err)
	}
	legacySQL, err := legacy.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := legacySQL.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Init(path, "admin", "migration-test-password")
	if err != nil {
		t.Fatal(err)
	}
	migratedSQL, err := migrated.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migratedSQL.Close() })

	var probes []model.ServiceProbe
	if err := migrated.Find(&probes).Error; err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 || probes[0].ServiceID != 1 || probes[0].ServerID != 9 {
		t.Fatalf("legacy service probe backfill = %+v", probes)
	}
	var histories []model.ServiceHistory
	if err := migrated.Find(&histories).Error; err != nil {
		t.Fatal(err)
	}
	if len(histories) != 1 || histories[0].ServerID != 9 || histories[0].Ts != 100 {
		t.Fatalf("legacy service history migration = %+v", histories)
	}
	if err := migrated.Exec(`INSERT OR IGNORE INTO service_probes
		(service_id, server_id, last_cert_identity, last_cert_warn_days, created_at)
		SELECT id, server_id, COALESCE(last_cert_identity, ''), COALESCE(last_cert_warn_days, 0), CURRENT_TIMESTAMP
		FROM services WHERE server_id > 0`).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := migrated.Model(&model.ServiceProbe{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("service probe backfill is not idempotent: count=%d", count)
	}
}

func TestInitMigratesLegacyAuditLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-audit.db")
	legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec(`CREATE TABLE audit_logs (
		id integer PRIMARY KEY AUTOINCREMENT,
		user_id integer,
		username text,
		action text,
		detail text,
		ip text,
		created_at datetime
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec("INSERT INTO audit_logs (user_id, username, action, detail, ip, created_at) VALUES (1, 'admin', 'server.update', 'legacy', '127.0.0.1', CURRENT_TIMESTAMP)").Error; err != nil {
		t.Fatal(err)
	}
	legacySQL, err := legacy.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := legacySQL.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Init(path, "admin", "migration-test-password")
	if err != nil {
		t.Fatal(err)
	}
	migratedSQL, err := migrated.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migratedSQL.Close() })

	columns := []string{"resource_type", "resource_id", "outcome", "error_code", "duration_ms", "request_id"}
	for _, column := range columns {
		if !migrated.Migrator().HasColumn(&model.AuditLog{}, column) {
			t.Errorf("migrated audit_logs table missing column %q", column)
		}
	}
	var row model.AuditLog
	if err := migrated.First(&row, 1).Error; err != nil {
		t.Fatal(err)
	}
	if row.Action != "server.update" || row.Detail != "legacy" || row.Outcome != "success" {
		t.Fatalf("legacy audit row changed during migration: %+v", row)
	}
	if row.ResourceType != "" || row.ResourceID != "" || row.ErrorCode != "" || row.RequestID != "" || row.DurationMS != 0 {
		t.Fatalf("legacy audit defaults are incompatible: %+v", row)
	}
}

func TestInitMigratesLegacyMetricColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec(`CREATE TABLE metrics (
		id integer PRIMARY KEY AUTOINCREMENT,
		server_id integer,
		ts integer,
		granularity integer,
		cpu real,
		mem_used integer,
		mem_total integer,
		disk_used integer,
		disk_total integer,
		net_in_speed real,
		net_out_speed real,
		load1 real,
		created_at datetime
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec("INSERT INTO metrics (server_id, ts, granularity, cpu) VALUES (?, ?, ?, ?)", 7, 100, 60, 42.5).Error; err != nil {
		t.Fatal(err)
	}
	legacySQL, err := legacy.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := legacySQL.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Init(path, "admin", "migration-test-password")
	if err != nil {
		t.Fatal(err)
	}
	migratedSQL, err := migrated.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migratedSQL.Close() })

	columns := []string{
		"swap_used", "swap_total", "net_in_transfer", "net_out_transfer",
		"load5", "load15", "uptime", "latency_ms",
		"gpu_mem_used", "gpu_mem_total", "gpu_devices",
	}
	for _, column := range columns {
		if !migrated.Migrator().HasColumn(&model.Metric{}, column) {
			t.Errorf("migrated metrics table missing column %q", column)
		}
	}
	var row model.Metric
	if err := migrated.First(&row, 1).Error; err != nil {
		t.Fatal(err)
	}
	if row.ServerID != 7 || row.TS != 100 || row.CPU != 42.5 {
		t.Fatalf("legacy metric row changed during migration: %+v", row)
	}
}
