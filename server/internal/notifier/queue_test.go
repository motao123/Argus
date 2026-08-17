package notifier

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

func newTestQueue(t *testing.T, attempts int, base time.Duration) (*Queue, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Notification{}, &model.NotificationDelivery{}); err != nil {
		t.Fatal(err)
	}
	q := NewQueue(db)
	q.MaxAttempts = attempts
	q.BackoffBase = base
	q.BackoffCap = 30 * time.Second
	q.PollInterval = 10 * time.Millisecond
	return q, db
}

func TestEnqueueSendsImmediatelyOnSuccess(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	q, db := newTestQueue(t, 5, time.Second)
	if err := db.Create(&model.Notification{ID: 1, Name: "n", Type: "webhook", URL: srv.URL, Method: "POST"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(&model.Notification{ID: 1}, "标题", "内容", 42); err != nil {
		t.Fatal(err)
	}
	var d model.NotificationDelivery
	if err := db.First(&d).Error; err != nil {
		t.Fatal(err)
	}
	if d.Status != model.DeliverySent {
		t.Fatalf("status = %s, want sent", d.Status)
	}
	if d.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", d.Attempts)
	}
	if d.SentAt == nil || d.NextRetry != nil {
		t.Fatalf("sent_at=%v next_retry=%v", d.SentAt, d.NextRetry)
	}
	if d.WebhookID != 1 || d.OwnerID != 42 || d.Title != "标题" || d.Content != "内容" {
		t.Fatalf("delivery = %+v", d)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
}

func TestRetryBackoffAndMaxAttempts(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError) // 永远失败
	}))
	defer srv.Close()

	q, db := newTestQueue(t, 3, time.Second) // 3 次封顶
	if err := db.Create(&model.Notification{ID: 1, Name: "n", Type: "webhook", URL: srv.URL}).Error; err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 0); err != nil {
		t.Fatal(err)
	}
	// Enqueue 已完成第一次尝试（失败）
	var d model.NotificationDelivery
	db.First(&d)
	if d.Status != model.DeliveryPending {
		t.Fatalf("after 1st failure status = %s", d.Status)
	}
	if d.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", d.Attempts)
	}
	if d.NextRetry == nil {
		t.Fatal("next_retry should be scheduled after 1st failure")
	}
	// 退避值 = base * 2^(attempt-1)
	if got, want := d.NextRetry.Sub(time.Now()), time.Second; got < want-200*time.Millisecond || got > want+time.Second {
		t.Fatalf("first backoff = %v, want ~%v", got, want)
	}

	// 把 next_retry 拨回过去 → 触发第二次尝试（用新变量读取，避免 GORM 共享实例脏读）
	db.Model(&d).Update("next_retry", time.Now().Add(-time.Minute))
	q.ProcessDue()
	var d2 model.NotificationDelivery
	db.First(&d2)
	if d2.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", d2.Attempts)
	}
	if got, want := d2.NextRetry.Sub(time.Now()), 2*time.Second; got < want-300*time.Millisecond || got > want+2*time.Second {
		t.Fatalf("second backoff = %v, want ~%v", got, want)
	}

	// 第三次（达到封顶）→ failed，无 next_retry
	db.Model(&d2).Update("next_retry", time.Now().Add(-time.Minute))
	q.ProcessDue()
	var d3 model.NotificationDelivery
	db.First(&d3)
	if d3.Status != model.DeliveryFailed {
		t.Fatalf("status = %s, want failed", d3.Status)
	}
	if d3.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", d3.Attempts)
	}
	if d3.NextRetry != nil {
		t.Fatalf("next_retry = %v, want nil", d3.NextRetry)
	}
	if d3.LastError == "" {
		t.Fatal("last_error should be recorded")
	}
	if hits.Load() != 3 {
		t.Fatalf("hits = %d, want 3", hits.Load())
	}
}

func TestBackoffExponentialAndCap(t *testing.T) {
	q := &Queue{BackoffBase: time.Second, BackoffCap: 8 * time.Second}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 8 * time.Second}, // 封顶
		{10, 8 * time.Second},
	}
	for _, c := range cases {
		if got := q.backoff(c.attempt); got != c.want {
			t.Errorf("backoff(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
	// 默认参数
	def := &Queue{}
	if got := def.backoff(1); got != 30*time.Second {
		t.Errorf("default backoff(1) = %v", got)
	}
	if got := def.backoff(6); got != 16*time.Minute { // 30s*2^5，未达上限
		t.Errorf("default backoff(6) = %v", got)
	}
	if got := def.backoff(7); got != 30*time.Minute { // 封顶
		t.Errorf("default backoff cap = %v", got)
	}
}

func TestManualRetryAfterFailure(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	q, db := newTestQueue(t, 1, time.Second) // 1 次失败即 failed
	if err := db.Create(&model.Notification{ID: 1, Name: "n", Type: "webhook", URL: srv.URL}).Error; err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 7); err != nil {
		t.Fatal(err)
	}
	var d model.NotificationDelivery
	db.First(&d)
	if d.Status != model.DeliveryFailed {
		t.Fatalf("status = %s, want failed", d.Status)
	}
	// 渠道恢复 → 手动重试
	fail.Store(false)
	if err := q.Retry(d.ID); err != nil {
		t.Fatal(err)
	}
	var after model.NotificationDelivery
	db.First(&after)
	if after.Status != model.DeliverySent {
		t.Fatalf("after retry status = %s, want sent", after.Status)
	}
	if after.Attempts != 1 || after.NextRetry != nil || after.LastError != "" {
		t.Fatalf("after retry: attempts=%d next_retry=%v last_error=%q", after.Attempts, after.NextRetry, after.LastError)
	}
	// 已发送的记录不允许再重试
	if err := q.Retry(after.ID); err == nil {
		t.Fatal("retry of a sent delivery should fail")
	}
}

func TestChannelDeletedMarksFailed(t *testing.T) {
	q, db := newTestQueue(t, 5, time.Second)
	if err := q.Enqueue(&model.Notification{ID: 999}, "t", "c", 0); err != nil {
		t.Fatal(err)
	}
	var d model.NotificationDelivery
	if err := db.First(&d).Error; err != nil {
		t.Fatal(err)
	}
	if d.Status != model.DeliveryFailed {
		t.Fatalf("status = %s, want failed", d.Status)
	}
	if d.LastError == "" {
		t.Fatal("last_error should mention deleted channel")
	}
}

func TestListOwnerIsolation(t *testing.T) {
	q, db := newTestQueue(t, 5, time.Second)
	now := time.Now()
	for i, owner := range []int64{1, 2, 1} {
		d := model.NotificationDelivery{
			WebhookID: int64(i + 1), OwnerID: owner, Title: fmt.Sprintf("t%d", i),
			Status: model.DeliverySent, Attempts: 1, NextRetry: &now,
		}
		if err := db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	// admin 看全部
	all, total, err := q.List(true, 0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("admin: total=%d len=%d, want 3/3", total, len(all))
	}
	// owner 1 只见自己的
	mine, total, err := q.List(false, 1, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(mine) != 2 {
		t.Fatalf("owner1: total=%d len=%d, want 2/2", total, len(mine))
	}
	for _, d := range mine {
		if d.OwnerID != 1 {
			t.Fatalf("owner leak: delivery %d owner=%d", d.ID, d.OwnerID)
		}
	}
	// owner 3 无记录
	none, total, err := q.List(false, 3, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(none) != 0 {
		t.Fatalf("owner3: total=%d len=%d, want 0/0", total, len(none))
	}
}
