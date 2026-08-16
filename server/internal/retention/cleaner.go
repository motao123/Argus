package retention

import (
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

const (
	GranMinute = 60
	Gran5m     = 300
	GranHour   = 3600

	DefaultBatchSize = 500
)

// Cleaner removes expired rows in bounded statements. Each batch is its own
// transaction, so cleanup never holds one transaction for the whole backlog.
type Cleaner struct {
	db        *gorm.DB
	batchSize int
	now       func() time.Time
}

func NewCleaner(db *gorm.DB) *Cleaner {
	return &Cleaner{db: db, batchSize: DefaultBatchSize, now: time.Now}
}

// Run loads the policy for every pass, so setting updates take effect without restart.
func (c *Cleaner) Run() {
	p := Load(c.db)
	now := c.now()
	c.deleteBefore("metrics", "ts", now.AddDate(0, 0, -p.Metric1mDays).Unix(), "granularity = ?", GranMinute)
	c.deleteBefore("metrics", "ts", now.AddDate(0, 0, -p.Metric5mDays).Unix(), "granularity = ?", Gran5m)
	c.deleteBefore("metrics", "ts", now.AddDate(0, 0, -p.Metric1hDays).Unix(), "granularity = ?", GranHour)
	c.deleteBefore("service_histories", "ts", now.AddDate(0, 0, -p.ServiceHistoryDays).Unix(), "")
	c.deleteBefore("transfers", "ts", now.AddDate(0, 0, -p.TransferDays).Unix(), "")
	c.cleanupTaskRuns(now.AddDate(0, 0, -p.TaskRunDays))
	c.deleteBefore("audit_logs", "created_at", now.AddDate(0, 0, -p.AuditDays), "")
	c.trimRows("audit_logs", p.AuditMaxRows)
	log.Printf("retention cleanup done")
}

func (c *Cleaner) deleteBefore(table, column string, cutoff any, extra string, args ...any) {
	if !c.db.Migrator().HasTable(table) {
		return
	}
	where := column + " < ?"
	params := []any{cutoff}
	if extra != "" {
		where += " AND " + extra
		params = append(params, args...)
	}
	for {
		res := c.db.Exec("DELETE FROM "+table+" WHERE id IN (SELECT id FROM "+table+" WHERE "+where+" ORDER BY id LIMIT ?)", append(params, c.batchSize)...)
		if res.Error != nil {
			log.Printf("retention cleanup %s: %v", table, res.Error)
			return
		}
		if res.RowsAffected < int64(c.batchSize) {
			return
		}
	}
}

// cleanupTaskRuns follows the latest TaskRun model and removes child results in
// the same bounded transaction before deleting each batch of parent runs.
func (c *Cleaner) cleanupTaskRuns(cutoff time.Time) {
	if !c.db.Migrator().HasTable("task_runs") {
		return
	}
	for {
		var ids []int64
		if err := c.db.Table("task_runs").Where("created_at < ?", cutoff).Order("id").Limit(c.batchSize).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
			if err != nil {
				log.Printf("retention select task_runs: %v", err)
			}
			return
		}
		if err := c.db.Transaction(func(tx *gorm.DB) error {
			if tx.Migrator().HasTable("task_run_results") {
				if err := tx.Exec("DELETE FROM task_run_results WHERE run_id IN ?", ids).Error; err != nil {
					return err
				}
			}
			return tx.Exec("DELETE FROM task_runs WHERE id IN ?", ids).Error
		}); err != nil {
			log.Printf("retention cleanup task_runs: %v", err)
			return
		}
		if len(ids) < c.batchSize {
			return
		}
	}
}

func (c *Cleaner) trimRows(table string, keep int) {
	if keep <= 0 || !c.db.Migrator().HasTable(table) {
		return
	}
	for {
		var count int64
		if err := c.db.Table(table).Count(&count).Error; err != nil || count <= int64(keep) {
			return
		}
		limit := c.batchSize
		if excess := int(count - int64(keep)); excess < limit {
			limit = excess
		}
		res := c.db.Exec("DELETE FROM "+table+" WHERE id IN (SELECT id FROM "+table+" ORDER BY id LIMIT ?)", limit)
		if res.Error != nil {
			log.Printf("retention trim %s: %v", table, res.Error)
			return
		}
		if res.RowsAffected == 0 {
			return
		}
	}
}

// Compile-time references keep table naming coupled to the GORM models used above.
var _ = []any{model.Metric{}, model.ServiceHistory{}, model.Transfer{}, model.AuditLog{}}
