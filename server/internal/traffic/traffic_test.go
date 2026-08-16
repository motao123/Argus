package traffic

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func TestCycleWindowCalendarBoundaries(t *testing.T) {
	tests := []struct {
		name, now, zone, start, end string
		day                         int
	}{
		{"day1 February", "2024-02-29T12:00:00Z", "UTC", "2024-02-01T00:00:00Z", "2024-03-01T00:00:00Z", 1},
		{"day15 before boundary", "2024-02-14T23:59:59Z", "UTC", "2024-01-15T00:00:00Z", "2024-02-15T00:00:00Z", 15},
		{"day28 leap February", "2024-02-29T00:00:00Z", "UTC", "2024-02-28T00:00:00Z", "2024-03-28T00:00:00Z", 28},
		{"cross year", "2025-01-02T00:00:00Z", "UTC", "2024-12-15T00:00:00Z", "2025-01-15T00:00:00Z", 15},
		{"timezone before local day", "2024-03-14T15:59:59Z", "Asia/Shanghai", "2024-02-15T00:00:00+08:00", "2024-03-15T00:00:00+08:00", 15},
		{"timezone local boundary", "2024-03-14T16:00:00Z", "Asia/Shanghai", "2024-03-15T00:00:00+08:00", "2024-04-15T00:00:00+08:00", 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tt.now)
			w, err := CycleWindow(now, tt.day, tt.zone)
			if err != nil {
				t.Fatal(err)
			}
			if w.Start.Format(time.RFC3339) != tt.start || w.End.Format(time.RFC3339) != tt.end {
				t.Fatalf("window = %s..%s, want %s..%s", w.Start.Format(time.RFC3339), w.End.Format(time.RFC3339), tt.start, tt.end)
			}
		})
	}
}

func TestAccountingModes(t *testing.T) {
	for mode, want := range map[string]uint64{"sum": 17, "in": 10, "out": 7, "max": 10} {
		if got := Accounted(10, 7, mode); got != want {
			t.Errorf("%s = %d, want %d", mode, got, want)
		}
	}
}

func TestCurrentUsageCycleResetAndNoQuotaPercentage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Transfer{}); err != nil {
		t.Fatal(err)
	}
	s := model.Server{ID: 42, TrafficCycleDay: 15, TrafficTimezone: "UTC", TrafficAccounting: "sum", TrafficQuotaBytes: 1000}
	rows := []model.Transfer{
		{ServerID: 42, Ts: mustUnix(t, "2024-01-20T00:00:00Z"), In: 900, Out: 100},
		{ServerID: 42, Ts: mustUnix(t, "2024-02-14T23:00:00Z"), In: 50, Out: 25},
		{ServerID: 42, Ts: mustUnix(t, "2024-02-15T00:00:00Z"), In: 20, Out: 30},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	now, _ := time.Parse(time.RFC3339, "2024-02-16T00:00:00Z")
	u, err := CurrentUsage(db, &s, now)
	if err != nil {
		t.Fatal(err)
	}
	if u.InBytes != 20 || u.OutBytes != 30 || u.AccountedBytes != 50 || u.RemainingBytes != 950 {
		t.Fatalf("unexpected reset usage: %+v", u)
	}
	s.TrafficQuotaBytes = 0
	u, err = CurrentUsage(db, &s, now)
	if err != nil || u.Percentage != nil {
		t.Fatalf("no quota percentage = %v, err=%v", u.Percentage, err)
	}
}

func mustUnix(t *testing.T, value string) int64 {
	t.Helper()
	v, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return v.Unix()
}
