package store

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func newLedger(t *testing.T) *TrafficLedger {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.Transfer{}, &model.TrafficBaseline{}); err != nil {
		t.Fatal(err)
	}
	return NewTrafficLedger(gdb)
}

func TestLedgerAccumulatesEveryReport(t *testing.T) {
	l := newLedger(t)
	// 每次上报都累计差值（修复「只在跨小时记一次」缺陷）
	l.Feed(1, 3600, 100, 200)
	l.Feed(1, 3602, 150, 250) // +50/+50
	l.Feed(1, 3604, 200, 300) // +50/+50
	l.Flush()
	var rows []model.Transfer
	if err := l.db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].In != 100 || rows[0].Out != 100 || rows[0].Ts != 3600 {
		t.Fatalf("expected hour bucket 100/100@3600, got %+v", rows)
	}
}

func TestLedgerCounterReset(t *testing.T) {
	l := newLedger(t)
	l.Feed(1, 3600, 1000, 2000)
	// 计数器重置（Agent 重启）：当前值小于上次 → 以当前值为重置后的新增量
	l.Feed(1, 3602, 50, 60)
	l.Flush()
	var rows []model.Transfer
	l.db.Find(&rows)
	if len(rows) != 1 || rows[0].In != 50 || rows[0].Out != 60 {
		t.Fatalf("reset delta expected 50/60, got %+v", rows)
	}
}

func TestLedgerHourRollover(t *testing.T) {
	l := newLedger(t)
	l.Feed(1, 3598, 100, 200)
	l.Feed(1, 3600, 150, 250) // +50/+50 进 3600 桶
	l.Feed(1, 3602, 200, 300) // +50/+50 进 3600 桶 → 100/100
	// 长时间断档后恢复：不产生虚假桶，基线从新值重新开始
	l.Feed(1, 7198, 280, 380)
	l.Feed(1, 7200, 300, 400) // +20/+20 进 7200 桶
	l.Feed(1, 7202, 310, 410) // +10/+10 进 7200 桶 → 30/30
	l.Flush()
	var rows []model.Transfer
	l.db.Find(&rows)
	byTs := map[int64]model.Transfer{}
	for _, r := range rows {
		byTs[r.Ts] = r
	}
	if v, ok := byTs[3600]; !ok || v.In != 100 || v.Out != 100 {
		t.Fatalf("hour 1 bucket wrong: %+v", byTs)
	}
	if v, ok := byTs[7200]; !ok || v.In != 30 || v.Out != 30 {
		t.Fatalf("hour 2 bucket wrong: %+v", byTs)
	}
}

func TestLedgerLongGapNoBogusSpike(t *testing.T) {
	l := newLedger(t)
	l.Feed(1, 3600, 100, 200)
	// 断档 10 分钟（> maxReportGap）：不把跨断档差值记入，仅更新基线
	l.Feed(1, 4200, 10000, 20000)
	l.Flush()
	var rows []model.Transfer
	l.db.Find(&rows)
	if len(rows) != 0 {
		t.Fatalf("gap should not create bucket, got %+v", rows)
	}
	// 断档后恢复连续上报：正常累计
	l.Feed(1, 4202, 10100, 20100)
	l.Flush()
	l.db.Find(&rows)
	if len(rows) != 1 || rows[0].In != 100 || rows[0].Out != 100 {
		t.Fatalf("post-gap delta wrong: %+v", rows)
	}
}
