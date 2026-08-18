package store

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/metric"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/tdigest"
)

// openTestDB 打开独立临时文件库（避免共享内存库跨测试残留）。
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.Metric{}); err != nil {
		t.Fatal(err)
	}
	return gdb
}

// TestBatcherDigest 验证 MetricBatcher 生成 CPU t-digest：样本分位数可还原。
func TestBatcherDigest(t *testing.T) {
	gdb := openTestDB(t)
	b := NewMetricBatcher(gdb)
	// 用上一分钟（已完成）喂样本，Flush 才会落库
	base := time.Now().Unix()/60*60 - 60
	// 同一分钟内喂 100 个 CPU 样本：0..99 各一次（近似均匀，p95≈95）
	for i := 0; i < 100; i++ {
		b.Feed(1, &protocol.ReportParams{Timestamp: base, CPU: float64(i)})
	}
	b.Flush()
	var rows []model.Metric
	if err := gdb.Where("granularity = 60").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 minute row, got %d", len(rows))
	}
	if rows[0].Samples != 100 || len(rows[0].Digest) == 0 {
		t.Fatalf("samples=%d digest_len=%d", rows[0].Samples, len(rows[0].Digest))
	}
	d, err := tdigest.Decode(rows[0].Digest)
	if err != nil {
		t.Fatal(err)
	}
	p95 := d.Percentile(95)
	if p95 < 93 || p95 > 97 {
		t.Errorf("cpu p95 = %.2f, want ~95", p95)
	}
}

// TestRollupDigestMerge 验证 5 分钟聚合合并分钟 digest，父桶分位数仍可还原。
func TestRollupDigestMerge(t *testing.T) {
	gdb := openTestDB(t)
	// 手工构造两个分钟桶（各 60 样本，0..59 与 40..99 → 合并后近似均匀 0..99）。
	// 用两个已完成的 5 分钟窗口（避免当前进行中桶被跳过）。
	base := time.Now().Unix()/300*300 - 600
	var rows []model.Metric
	for m := 0; m < 2; m++ {
		d := tdigest.New(0)
		start := m * 40
		for i := 0; i < 60; i++ {
			d.Add(float64(start + i))
		}
		rows = append(rows, model.Metric{
			ServerID: 1, TS: base + int64(m*60), Granularity: metric.GranMinute,
			CPU: 50, Samples: 60, Digest: d.Encode(),
		})
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	r := metric.New(gdb)
	r.Aggregate5m()
	var five []model.Metric
	if err := gdb.Where("granularity = 300").Find(&five).Error; err != nil {
		t.Fatal(err)
	}
	if len(five) != 1 {
		t.Fatalf("expected 1 five-min row, got %d", len(five))
	}
	if five[0].Samples != 120 || len(five[0].Digest) == 0 {
		t.Fatalf("samples=%d digest_len=%d", five[0].Samples, len(five[0].Digest))
	}
	d, err := tdigest.Decode(five[0].Digest)
	if err != nil {
		t.Fatal(err)
	}
	// 合并后分布近似均匀 [0,100)，p95 ≈ 95
	if p95 := d.Percentile(95); p95 < 92 || p95 > 98 {
		t.Errorf("merged 5m cpu p95 = %.2f, want ~95", p95)
	}
	if p50 := d.Percentile(50); p50 < 47 || p50 > 53 {
		t.Errorf("merged 5m cpu p50 = %.2f, want ~50", p50)
	}
}
