package retention

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Metric{}, &model.ServiceHistory{}, &model.Transfer{}, &model.AuditLog{}, &model.Setting{}, &model.TaskRun{}, &model.TaskRunResult{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDefaultsAndValidationBoundaries(t *testing.T) {
	p := Defaults()
	if p.Metric1mDays != 1 || p.Metric5mDays != 7 || p.Metric1hDays != 30 || p.ServiceHistoryDays != 30 {
		t.Fatalf("existing defaults changed: %+v", p)
	}
	if p.AuditDays != 365 || p.AuditMaxRows != 5000 {
		t.Fatalf("audit defaults = %+v", p)
	}
	valid := map[string]string{SettingMetric1mDays: "1", SettingAuditMaxRows: "1000000"}
	if err := ValidateSettings(valid); err != nil {
		t.Fatalf("valid boundaries rejected: %v", err)
	}
	for _, values := range []map[string]string{
		{SettingMetric1mDays: "0"}, {SettingMetric1mDays: "31"},
		{SettingAuditDays: "abc"}, {SettingAuditMaxRows: "99"},
	} {
		if err := ValidateSettings(values); err == nil {
			t.Fatalf("invalid value accepted: %v", values)
		}
	}
}

func TestLoadReflectsLatestSettings(t *testing.T) {
	db := testDB(t)
	if got := Load(db); got.TransferDays != 365 {
		t.Fatalf("default transfer days = %d", got.TransferDays)
	}
	db.Create(&model.Setting{Key: SettingTransferDays, Value: "90"})
	if got := Load(db); got.TransferDays != 90 {
		t.Fatalf("updated transfer days = %d", got.TransferDays)
	}
	db.Model(&model.Setting{}).Where("key = ?", SettingTransferDays).Update("value", "invalid")
	if got := Load(db); got.TransferDays != 365 {
		t.Fatalf("invalid persisted value should fall back, got %d", got.TransferDays)
	}
}

func TestCleanerDeletesExpiredRowsInBatchesAndKeepsBoundary(t *testing.T) {
	db := testDB(t)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -1).Unix()
	for i := 0; i < 7; i++ {
		db.Create(&model.Metric{ServerID: 1, TS: cutoff - int64(i+1), Granularity: GranMinute})
	}
	db.Create(&model.Metric{ServerID: 1, TS: cutoff, Granularity: GranMinute})
	db.Create(&model.Metric{ServerID: 1, TS: cutoff - 10, Granularity: Gran5m})

	cleaner := NewCleaner(db)
	cleaner.batchSize = 3
	cleaner.now = func() time.Time { return now }
	cleaner.Run()

	var minuteCount, fiveMinuteCount int64
	db.Model(&model.Metric{}).Where("granularity = ?", GranMinute).Count(&minuteCount)
	db.Model(&model.Metric{}).Where("granularity = ?", Gran5m).Count(&fiveMinuteCount)
	if minuteCount != 1 {
		t.Fatalf("minute rows after multi-batch cleanup = %d", minuteCount)
	}
	if fiveMinuteCount != 1 {
		t.Fatalf("5m row used wrong policy: %d", fiveMinuteCount)
	}
}

func TestCleanerDeletesTaskRunResultsWithExpiredRuns(t *testing.T) {
	db := testDB(t)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	oldRun := model.TaskRun{CronID: 1, OwnerID: 1, Trigger: "manual", Status: "done", Command: "true", CreatedAt: now.AddDate(0, 0, -31)}
	newRun := model.TaskRun{CronID: 1, OwnerID: 1, Trigger: "manual", Status: "done", Command: "true", CreatedAt: now.AddDate(0, 0, -30)}
	db.Create(&oldRun)
	db.Create(&newRun)
	db.Create(&model.TaskRunResult{RunID: oldRun.ID, ServerID: 1, Status: "done"})
	db.Create(&model.TaskRunResult{RunID: newRun.ID, ServerID: 1, Status: "done"})

	cleaner := NewCleaner(db)
	cleaner.batchSize = 1
	cleaner.now = func() time.Time { return now }
	cleaner.Run()

	var runs, results int64
	db.Model(&model.TaskRun{}).Count(&runs)
	db.Model(&model.TaskRunResult{}).Count(&results)
	if runs != 1 || results != 1 {
		t.Fatalf("task cleanup runs=%d results=%d", runs, results)
	}
}

func TestCleanerUsesUpdatedPolicyAndAuditRowCap(t *testing.T) {
	db := testDB(t)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 105; i++ {
		db.Create(&model.AuditLog{Action: "test", CreatedAt: now.Add(-time.Hour)})
	}
	for _, setting := range []model.Setting{
		{Key: SettingAuditMaxRows, Value: "100"},
		{Key: SettingTransferDays, Value: "10"},
	} {
		db.Create(&setting)
	}
	db.Create(&model.Transfer{ServerID: 1, Ts: now.AddDate(0, 0, -11).Unix()})

	cleaner := NewCleaner(db)
	cleaner.batchSize = 2
	cleaner.now = func() time.Time { return now }
	cleaner.Run()

	var audits, transfers int64
	db.Model(&model.AuditLog{}).Count(&audits)
	db.Model(&model.Transfer{}).Count(&transfers)
	if audits != 100 || transfers != 0 {
		t.Fatalf("updated policy not applied: audits=%d transfers=%d", audits, transfers)
	}
}
