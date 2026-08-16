package alert

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func TestTrafficQuotaEventsDeduplicatePerCycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:quota-events?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Server{}, &model.Transfer{}, &model.TrafficQuotaEvent{}, &model.TrafficReport{}, &model.Notification{}); err != nil {
		t.Fatal(err)
	}
	s := model.Server{ID: 1, Name: "quota", Secret: "secret", TrafficQuotaBytes: 100, TrafficCycleDay: 1, TrafficTimezone: "UTC", TrafficAccounting: "sum"}
	if err := db.Create(&s).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Transfer{ServerID: 1, Ts: unixAt(t, "2024-02-10T00:00:00Z"), In: 101}).Error; err != nil {
		t.Fatal(err)
	}
	e := &Engine{db: db}
	now := parseAt(t, "2024-02-20T00:00:00Z")
	e.checkTrafficQuotas(now)
	e.checkTrafficQuotas(now)
	var events []model.TrafficQuotaEvent
	db.Order("threshold").Find(&events)
	if len(events) != 3 || events[0].Threshold != 80 || events[1].Threshold != 90 || events[2].Threshold != 100 {
		t.Fatalf("events = %+v", events)
	}
	// The next cycle gets independent threshold events.
	if err := db.Create(&model.Transfer{ServerID: 1, Ts: unixAt(t, "2024-03-10T00:00:00Z"), In: 100}).Error; err != nil {
		t.Fatal(err)
	}
	e.checkTrafficQuotas(parseAt(t, "2024-03-20T00:00:00Z"))
	var count int64
	db.Model(&model.TrafficQuotaEvent{}).Count(&count)
	if count != 6 {
		t.Fatalf("event count = %d, want 6", count)
	}
}

func parseAt(t *testing.T, value string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func unixAt(t *testing.T, value string) int64 { return parseAt(t, value).Unix() }
