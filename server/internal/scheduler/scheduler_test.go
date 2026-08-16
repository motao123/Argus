package scheduler

import (
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func TestTargetIDsEnforcesCronOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Server{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "user", Role: model.RoleUser}
	admin := model.User{Username: "admin", Role: model.RoleAdmin}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	own := model.Server{Name: "own", Secret: "own-secret", OwnerID: user.ID}
	other := model.Server{Name: "other", Secret: "other-secret", OwnerID: admin.ID}
	if err := db.Create(&own).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	s := &Scheduler{db: db}
	online := map[int64]bool{own.ID: true, other.ID: true}

	cr := model.Cron{OwnerID: user.ID, ServerIDs: ""}
	if got := s.targetIDs(&cr, online); len(got) != 1 || got[0] != own.ID {
		t.Fatalf("user empty targets = %v, want [%d]", got, own.ID)
	}
	cr.ServerIDs = "" + formatID(other.ID)
	if got := s.targetIDs(&cr, online); len(got) != 0 {
		t.Fatalf("user explicit foreign target = %v, want none", got)
	}
	cr.OwnerID = admin.ID
	if got := s.targetIDs(&cr, online); len(got) != 1 || got[0] != other.ID {
		t.Fatalf("admin explicit target = %v, want [%d]", got, other.ID)
	}
}

func formatID(id int64) string {
	return fmt.Sprintf("%d", id)
}
