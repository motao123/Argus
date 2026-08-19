package store

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
)

func TestBatcher(t *testing.T) {
	_ = os.MkdirAll("/tmp/batcher-test", 0o755)
	_ = os.Remove("/tmp/batcher-test/test.db")
	gdb, err := gorm.Open(sqlite.Open("/tmp/batcher-test/test.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.Metric{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	old := time.Now().Add(-2 * time.Minute).Unix()
	now := time.Now().Unix()
	b := NewMetricBatcher(gdb)

	// 1) 旧分钟数据 → 新分钟首报触发 rollover，flush 后旧分钟落库
	b.Feed(1, &protocol.ReportParams{Timestamp: old, CPU: 42.5, ProcessCount: 10, TCPEstablished: 4, DiskReadSpeed: 100,
		SwapUsed: 10, SwapTotal: 100, Load5: 2, Load15: 4, Uptime: 100, LatencyMs: 10, NetInTransfer: 1000, NetOutTransfer: 2000,
		GPUMemUsed: 256, GPUMemTotal: 1024, GPU: protocol.GPUReport{Devices: []protocol.GPUDevice{{Index: 0, Name: "GPU-A", Util: 20}}}})
	b.Feed(1, &protocol.ReportParams{Timestamp: old, CPU: 43.5, ProcessCount: 20, TCPEstablished: 8, DiskReadSpeed: 300,
		SwapUsed: 20, SwapTotal: 100, Load5: 4, Load15: 8, Uptime: 102, LatencyMs: 30, NetInTransfer: 1200, NetOutTransfer: 2400,
		GPUMemUsed: 512, GPUMemTotal: 1024, GPU: protocol.GPUReport{Devices: []protocol.GPUDevice{{Index: 0, Name: "GPU-A", Util: 40}}}})
	b.Feed(1, &protocol.ReportParams{Timestamp: now, CPU: 99})
	b.Flush()

	var rows []model.Metric
	gdb.Find(&rows)
	fmt.Printf("after-1: rows=%d\n", len(rows))
	if len(rows) != 1 || rows[0].CPU != 43 {
		t.Fatalf("expected 1 row cpu=43, got %+v", rows)
	}
	if rows[0].ProcessCount != 15 || rows[0].TCPEstablished != 6 || rows[0].DiskReadSpeed != 200 {
		t.Fatalf("extended metrics not aggregated: %+v", rows[0])
	}
	if rows[0].SwapUsed != 20 || rows[0].Load5 != 3 || rows[0].Load15 != 6 || rows[0].LatencyMs != 20 {
		t.Fatalf("historical averages/latest values wrong: %+v", rows[0])
	}
	if rows[0].Uptime != 102 || rows[0].NetInTransfer != 1200 || rows[0].NetOutTransfer != 2400 || rows[0].GPUMemUsed != 512 {
		t.Fatalf("historical latest counters wrong: %+v", rows[0])
	}
	if !strings.Contains(rows[0].GPUDevices, `"GPU-A"`) || !strings.Contains(rows[0].GPUDevices, `"util":40`) {
		t.Fatalf("GPU device snapshot not stored: %s", rows[0].GPUDevices)
	}

	// 2) 迟到的旧数据并入当前桶（不丢不重）
	b.Feed(1, &protocol.ReportParams{Timestamp: old, CPU: 10})
	b.Feed(1, &protocol.ReportParams{Timestamp: now, CPU: 20})
	// 模拟分钟前进：触发 rollover，当前桶收尾落库
	future := now + 120
	b.Feed(1, &protocol.ReportParams{Timestamp: future, CPU: 0})
	b.Flush()

	gdb.Find(&rows)
	fmt.Printf("after-2: rows=%d\n", len(rows))
	for _, r := range rows {
		fmt.Printf("  server=%d ts=%d cpu=%.1f\n", r.ServerID, r.TS, r.CPU)
	}
	// 第二条 = 当前分钟桶的均值 (99+10+20)/3 = 43.0（迟到数据已并入）
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[len(rows)-1].CPU != 43 {
		t.Fatalf("expected merged avg 43, got %.1f", rows[len(rows)-1].CPU)
	}
	fmt.Println("PASS")
}
