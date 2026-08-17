package traffic

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func TestAnchorWindow(t *testing.T) {
	must := func(s string) time.Time {
		t.Helper()
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	tests := []struct {
		name, unit string
		anchor     string
		now        string
		interval   int
		start, end string
	}{
		{"hour in same window", "hour", "2026-08-17T10:30:00Z", "2026-08-17T10:45:00Z", 1, "2026-08-17T10:30:00Z", "2026-08-17T11:30:00Z"},
		{"hour next window", "hour", "2026-08-17T10:30:00Z", "2026-08-17T12:10:00Z", 1, "2026-08-17T11:30:00Z", "2026-08-17T12:30:00Z"},
		{"hour interval 6 aligned", "hour", "2026-08-17T00:00:00Z", "2026-08-17T13:00:00Z", 6, "2026-08-17T12:00:00Z", "2026-08-17T18:00:00Z"},
		{"hour before anchor", "hour", "2026-08-17T10:30:00Z", "2026-08-17T09:00:00Z", 1, "2026-08-17T08:30:00Z", "2026-08-17T09:30:00Z"},
		{"day", "day", "2026-08-10T08:00:00Z", "2026-08-17T12:00:00Z", 1, "2026-08-17T08:00:00Z", "2026-08-18T08:00:00Z"},
		{"day before anchor", "day", "2026-08-20T08:00:00Z", "2026-08-17T12:00:00Z", 1, "2026-08-17T08:00:00Z", "2026-08-18T08:00:00Z"},
		{"week", "week", "2026-08-03T00:00:00Z", "2026-08-17T00:00:00Z", 1, "2026-08-17T00:00:00Z", "2026-08-24T00:00:00Z"},
		{"month", "month", "2026-01-15T00:00:00Z", "2026-08-17T00:00:00Z", 1, "2026-08-15T00:00:00Z", "2026-09-15T00:00:00Z"},
		{"month interval 3", "month", "2026-01-15T00:00:00Z", "2026-08-17T00:00:00Z", 3, "2026-07-15T00:00:00Z", "2026-10-15T00:00:00Z"},
		{"month before anchor", "month", "2026-10-15T00:00:00Z", "2026-08-17T00:00:00Z", 1, "2026-08-15T00:00:00Z", "2026-09-15T00:00:00Z"},
		{"year", "year", "2024-06-01T00:00:00Z", "2026-08-17T00:00:00Z", 1, "2026-06-01T00:00:00Z", "2027-06-01T00:00:00Z"},
		{"leap day anchor", "month", "2024-02-29T00:00:00Z", "2025-02-01T00:00:00Z", 1, "2025-01-29T00:00:00Z", "2025-02-28T00:00:00Z"},
		{"leap day year clamp", "year", "2024-02-29T00:00:00Z", "2026-06-01T00:00:00Z", 1, "2026-02-28T00:00:00Z", "2027-02-28T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := AnchorWindow(must(tt.now), must(tt.anchor), tt.unit, tt.interval)
			if err != nil {
				t.Fatal(err)
			}
			if w.Start.Format(time.RFC3339) != tt.start || w.End.Format(time.RFC3339) != tt.end {
				t.Fatalf("window = %s..%s, want %s..%s", w.Start.Format(time.RFC3339), w.End.Format(time.RFC3339), tt.start, tt.end)
			}
			if w.Start.After(must(tt.now)) || !w.End.After(must(tt.now)) {
				t.Fatalf("window %s..%s does not contain now %s", w.Start.Format(time.RFC3339), w.End.Format(time.RFC3339), tt.now)
			}
		})
	}
	if _, err := AnchorWindow(must("2026-08-17T00:00:00Z"), must("2026-08-17T00:00:00Z"), "fortnight", 1); err == nil {
		t.Fatal("invalid unit should error")
	}
}

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
