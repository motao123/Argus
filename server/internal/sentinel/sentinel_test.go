package sentinel

import (
	"sync"
	"testing"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecordAggregatesAndKeepsLegacyAgentCompatible(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ServiceHistory{}); err != nil {
		t.Fatal(err)
	}
	s := New(db)

	// Legacy agents only return up/delay. Treat that as one successful packet.
	s.record(1, 10, protocol.ServiceCheckResult{Up: true, DelayMs: 20})
	certDays := 12
	s.record(1, 10, protocol.ServiceCheckResult{Up: false, DelayMs: 80, Sent: 4, Received: 2,
		StatusCode: 503, CertNotAfter: 1234, CertDaysRemaining: certDays})

	var got model.ServiceHistory
	if err := db.Where("service_id = ?", 1).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.UpCount != 1 || got.Total != 2 || got.DelaySum != 100 || got.DelayMin != 20 || got.DelayMax != 80 {
		t.Fatalf("delay/count aggregate = %+v", got)
	}
	if got.Sent != 5 || got.Received != 3 || got.StatusCode != 503 {
		t.Fatalf("packet/status aggregate = %+v", got)
	}
	if got.CertDays == nil || *got.CertDays != certDays {
		t.Fatalf("cert minimum = %+v", got.CertDays)
	}
}

// TestRecordPersistsWindowQuantiles 分钟桶落库时写入当前滑动窗口分位数：
// 30 次成功探测后窗口快照（样本不足 30 时各分位字段为 0，delay_samples 记录实际样本数）。
func TestRecordPersistsWindowQuantiles(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// :memory: 库每个连接都是独立数据库，必须单连接才能跨查询可见。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.ServiceHistory{}); err != nil {
		t.Fatal(err)
	}
	s := New(db)

	// 样本不足：10 次成功探测 → 分位字段全部为 0，delay_samples = 10。
	for i := 0; i < 10; i++ {
		s.record(1, 10, protocol.ServiceCheckResult{Up: true, DelayMs: 20})
	}
	var got model.ServiceHistory
	if err := db.Where("service_id = ?", 1).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.DelaySamples != 10 || got.DelayP50 != 0 || got.DelayP95 != 0 || got.DelayP99 != 0 || got.DelayStdDevMs != 0 || got.DelayJitterMs != 0 {
		t.Fatalf("insufficient window bucket = %+v", got)
	}

	// 服务 2：30 次交替 0/100ms → 同一分钟桶合并，窗口快照写入（p50=50/p95=99=100/σ=50/抖动=100）。
	for i := 0; i < 15; i++ {
		s.record(2, 20, protocol.ServiceCheckResult{Up: true, DelayMs: 0})
		s.record(2, 20, protocol.ServiceCheckResult{Up: true, DelayMs: 100})
	}
	got = model.ServiceHistory{}
	if err := db.Where("service_id = ?", 2).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Total != 30 || got.DelaySamples != 30 || got.DelayP50 != 50 || got.DelayP95 != 100 || got.DelayP99 != 100 || got.DelayStdDevMs != 50 || got.DelayJitterMs != 100 {
		t.Fatalf("window snapshot bucket = %+v", got)
	}

	// 失败探测不进入窗口：同一分钟桶的分位字段被覆盖为 0、delay_samples = 0，up_count 不变。
	s.record(2, 20, protocol.ServiceCheckResult{Up: false, DelayMs: 0})
	got = model.ServiceHistory{}
	if err := db.Where("service_id = ?", 2).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.DelaySamples != 0 || got.DelayP50 != 0 || got.UpCount != 30 {
		t.Fatalf("failed probe bucket = %+v", got)
	}

	// 服务间窗口相互隔离：恒定 5ms → p50=5、σ=0、抖动=0。
	for i := 0; i < DelayMinSamples; i++ {
		s.record(3, 30, protocol.ServiceCheckResult{Up: true, DelayMs: 5})
	}
	var got3 model.ServiceHistory
	if err := db.Where("service_id = ?", 3).First(&got3).Error; err != nil {
		t.Fatal(err)
	}
	if got3.DelaySamples != 30 || got3.DelayP50 != 5 || got3.DelayStdDevMs != 0 || got3.DelayJitterMs != 0 {
		t.Fatalf("isolated window bucket = %+v", got3)
	}
}

func TestRecordIsolatedByProbeAndConcurrent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.ServiceHistory{}); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		serverID := int64(10)
		if i%2 == 1 {
			serverID = 20
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.record(1, serverID, protocol.ServiceCheckResult{Up: true, DelayMs: 10})
		}()
	}
	wg.Wait()
	var rows []model.ServiceHistory
	if err := db.Order("server_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ServerID != 10 || rows[1].ServerID != 20 || rows[0].Total != 10 || rows[1].Total != 10 {
		t.Fatalf("probe buckets = %+v", rows)
	}
}

func TestFailureRecoveryCallbacksUseSeparateTasks(t *testing.T) {
	s := New(nil)
	svc := &model.Service{ID: 7, Notify: true, FailureTriggerCronID: 11, RecoveryTriggerCronID: 12}
	var notices []string
	var tasks []bool
	s.NotifyCb = func(_ *model.Service, kind, _ string) { notices = append(notices, kind) }
	s.TriggerCb = func(_ *model.Service, up bool) { tasks = append(tasks, up) }
	for range 3 {
		s.failNotify(svc, 10, false)
	}
	s.failNotify(svc, 10, true)
	if len(notices) != 2 || notices[0] != "failure" || notices[1] != "recovered" {
		t.Fatalf("notices = %v", notices)
	}
	if len(tasks) != 2 || tasks[0] || !tasks[1] {
		t.Fatalf("tasks = %v", tasks)
	}
}

func TestCertificateThresholdsAndChange(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Service{}, &model.ServiceProbe{}); err != nil {
		t.Fatal(err)
	}
	verify := true
	svc := &model.Service{OwnerID: 2, ServerID: 3, Name: "tls", Type: "http", Target: "https://example.com", CertWarn: true, VerifyTLS: &verify}
	if err := db.Create(svc).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ServiceProbe{ServiceID: svc.ID, ServerID: 3}).Error; err != nil {
		t.Fatal(err)
	}
	s := New(db)
	var kinds []string
	s.NotifyCb = func(_ *model.Service, kind, _ string) { kinds = append(kinds, kind) }
	r := protocol.ServiceCheckResult{CertIssuer: "CA1", CertNotAfter: 1000, CertDaysRemaining: 29}
	s.certNotify(svc, 3, r)
	if len(kinds) != 1 || kinds[0] != "certificate_expiring" {
		t.Fatalf("first events = %v", kinds)
	}
	// 同一 30 天档不重复；进入 7 天档再告警。
	s.certNotify(svc, 3, r)
	s.certNotify(svc, 3, protocol.ServiceCheckResult{CertIssuer: "CA1", CertNotAfter: 1000, CertDaysRemaining: 6})
	if len(kinds) != 2 || kinds[1] != "certificate_expiring" {
		t.Fatalf("threshold events = %v", kinds)
	}
	s.certNotify(svc, 3, protocol.ServiceCheckResult{CertIssuer: "CA2", CertNotAfter: 2000, CertDaysRemaining: 40})
	if kinds[len(kinds)-1] != "certificate_changed" {
		t.Fatalf("change events = %v", kinds)
	}
}
