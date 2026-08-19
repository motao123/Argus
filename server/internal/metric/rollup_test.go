package metric

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func TestRollupExtendedFieldsUseLatestSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rollup-extended?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Metric{}); err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-2*time.Hour).Unix() / Gran5m * Gran5m
	rows := []model.Metric{
		{ServerID: 1, TS: base + 120, Granularity: GranMinute, CPU: 30, Load5: 6, Load15: 12, LatencyMs: 40, SwapUsed: 20, SwapTotal: 100, Uptime: 200, NetInTransfer: 1200, NetOutTransfer: 2400, GPUMemUsed: 512, GPUMemTotal: 1024, GPUDevices: `[{"name":"new"}]`},
		{ServerID: 1, TS: base + 60, Granularity: GranMinute, CPU: 10, Load5: 2, Load15: 4, LatencyMs: 0, SwapUsed: 10, SwapTotal: 100, Uptime: 100, NetInTransfer: 1000, NetOutTransfer: 2000, GPUMemUsed: 256, GPUMemTotal: 1024, GPUDevices: `[{"name":"old"}]`},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	New(db).aggregate(GranMinute, Gran5m, time.Now().Add(-3*time.Hour))
	var got model.Metric
	if err := db.Where("server_id = ? AND ts = ? AND granularity = ?", 1, base, Gran5m).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.CPU != 20 || got.Load5 != 4 || got.Load15 != 8 || got.LatencyMs != 40 {
		t.Fatalf("averaged historical values wrong: %+v", got)
	}
	if got.SwapUsed != 20 || got.Uptime != 200 || got.NetInTransfer != 1200 || got.NetOutTransfer != 2400 {
		t.Fatalf("latest counters wrong: %+v", got)
	}
	if got.GPUMemUsed != 512 || got.GPUDevices != `[{"name":"new"}]` {
		t.Fatalf("latest GPU snapshot wrong: %+v", got)
	}
}
