package sentinel

import (
	"testing"
	"time"

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
	s.record(1, protocol.ServiceCheckResult{Up: true, DelayMs: 20})
	certDays := 12
	s.record(1, protocol.ServiceCheckResult{Up: false, DelayMs: 80, Sent: 4, Received: 2,
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

func TestFailureRecoveryCallbacksUseSeparateTasks(t *testing.T) {
	s := New(nil)
	svc := &model.Service{ID: 7, Notify: true, FailureTriggerCronID: 11, RecoveryTriggerCronID: 12}
	var notices []string
	var tasks []bool
	s.NotifyCb = func(_ *model.Service, kind, _ string) { notices = append(notices, kind) }
	s.TriggerCb = func(_ *model.Service, up bool) { tasks = append(tasks, up) }
	for range 3 {
		s.failNotify(svc, false)
	}
	s.failNotify(svc, true)
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
	if err := db.AutoMigrate(&model.Service{}); err != nil {
		t.Fatal(err)
	}
	verify := true
	svc := &model.Service{OwnerID: 2, ServerID: 3, Name: "tls", Type: "http", Target: "https://example.com", CertWarn: true, VerifyTLS: &verify}
	if err := db.Create(svc).Error; err != nil {
		t.Fatal(err)
	}
	s := New(db)
	var kinds []string
	s.NotifyCb = func(_ *model.Service, kind, _ string) { kinds = append(kinds, kind) }
	r := protocol.ServiceCheckResult{CertIssuer: "CA1", CertNotAfter: 1000, CertDaysRemaining: 29}
	s.certNotify(svc, r)
	if len(kinds) != 1 || kinds[0] != "certificate_expiring" {
		t.Fatalf("first events = %v", kinds)
	}
	// 同一 30 天档不重复；进入 7 天档再告警。
	s.certNotify(svc, r)
	s.certNotify(svc, protocol.ServiceCheckResult{CertIssuer: "CA1", CertNotAfter: 1000, CertDaysRemaining: 6})
	if len(kinds) != 2 || kinds[1] != "certificate_expiring" {
		t.Fatalf("threshold events = %v", kinds)
	}
	svc.LastCertIdentity = "CA1:" + time.Unix(1000, 0).UTC().Format(time.RFC3339)
	s.certNotify(svc, protocol.ServiceCheckResult{CertIssuer: "CA2", CertNotAfter: 2000, CertDaysRemaining: 40})
	if kinds[len(kinds)-1] != "certificate_changed" {
		t.Fatalf("change events = %v", kinds)
	}
}
