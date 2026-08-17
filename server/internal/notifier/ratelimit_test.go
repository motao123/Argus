package notifier

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

// newRateQueue 创建带可推进假时钟的测试队列，用于限流测试。
// 返回的 *time.Time 可被测试直接改写以推进时间。
func newRateQueue(t *testing.T) (*Queue, *gorm.DB, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Notification{}, &model.NotificationDelivery{}); err != nil {
		t.Fatal(err)
	}
	q := NewQueue(db)
	q.MaxAttempts = 5
	q.BackoffBase = time.Second
	q.BackoffCap = 30 * time.Second
	q.PollInterval = 10 * time.Millisecond
	fake := time.Now()
	q.Now = func() time.Time { return fake }
	return q, db, &fake
}

// okServer 返回固定 200 的 webhook 目标，并统计命中次数。
func okServer(hits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
}

// deliveryOf 按 id 读取送达记录。
func deliveryOf(t *testing.T, db *gorm.DB, id int64) model.NotificationDelivery {
	t.Helper()
	var d model.NotificationDelivery
	if err := db.First(&d, id).Error; err != nil {
		t.Fatal(err)
	}
	return d
}

// TestRateLimitDelaysOverLimitDelivery 限流生效：超过速率后投递被延迟到 next_retry，
// 之后令牌补充完毕由重试路径投递成功；限流不消耗 attempts、不标记 failed。
func TestRateLimitDelaysOverLimitDelivery(t *testing.T) {
	var hits atomic.Int32
	srv := okServer(&hits)
	defer srv.Close()

	q, db, fake := newRateQueue(t)
	if err := db.Create(&model.Notification{ID: 1, Name: "n", Type: "webhook", URL: srv.URL, RateLimitPerMin: 2, BurstLimit: 2}).Error; err != nil {
		t.Fatal(err)
	}
	// 桶容量 2：前两条立即发送，第三条超限延迟
	for i := int64(1); i <= 3; i++ {
		if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 0); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2 (burst consumed)", hits.Load())
	}
	var d3 model.NotificationDelivery
	if err := db.Order("id DESC").First(&d3).Error; err != nil {
		t.Fatal(err)
	}
	if d3.Status != model.DeliveryPending {
		t.Fatalf("status = %s, want pending (delayed, not dropped)", d3.Status)
	}
	if d3.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0 (throttle must not consume attempts)", d3.Attempts)
	}
	if d3.NextRetry == nil {
		t.Fatal("next_retry should be scheduled for the throttled delivery")
	}
	// 速率 2/分钟：空桶补 1 个令牌需约 30s
	if wait := d3.NextRetry.Sub(*fake); wait < 29*time.Second || wait > 31*time.Second {
		t.Fatalf("next_retry wait = %v, want ~30s", wait)
	}

	// 时间未推进时反复轮询：保持 pending，不因限流而失败
	for i := 0; i < 3; i++ {
		q.ProcessDue()
	}
	d3 = deliveryOf(t, db, d3.ID)
	if d3.Status != model.DeliveryPending || d3.Attempts != 0 || hits.Load() != 2 {
		t.Fatalf("before refill: status=%s attempts=%d hits=%d, want pending/0/2", d3.Status, d3.Attempts, hits.Load())
	}

	// 推进 31s：令牌补充 → 重试路径投递成功
	*fake = fake.Add(31 * time.Second)
	q.ProcessDue()
	d3 = deliveryOf(t, db, d3.ID)
	if d3.Status != model.DeliverySent {
		t.Fatalf("after refill status = %s, want sent", d3.Status)
	}
	if d3.Attempts != 1 || d3.NextRetry != nil || d3.LastError != "" {
		t.Fatalf("after refill: attempts=%d next_retry=%v last_error=%q", d3.Attempts, d3.NextRetry, d3.LastError)
	}
	if hits.Load() != 3 {
		t.Fatalf("hits = %d, want 3", hits.Load())
	}
}

// TestRateLimitBurstCapacity 突发容量：桶容量 BurstLimit 内瞬时放行，
// 超出后按速率（每分钟 RateLimitPerMin 个）逐个补充放行。
func TestRateLimitBurstCapacity(t *testing.T) {
	var hits atomic.Int32
	srv := okServer(&hits)
	defer srv.Close()

	q, db, fake := newRateQueue(t)
	if err := db.Create(&model.Notification{ID: 1, Name: "n", Type: "webhook", URL: srv.URL, RateLimitPerMin: 60, BurstLimit: 5}).Error; err != nil {
		t.Fatal(err)
	}
	// 突发容量 5：前 5 条瞬时全部发送
	for i := int64(1); i <= 5; i++ {
		if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 0); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 5 {
		t.Fatalf("hits = %d, want 5 (full burst)", hits.Load())
	}
	// 第 6 条超限：延迟 ~1s（60/分钟 = 1 令牌/秒）
	if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 0); err != nil {
		t.Fatal(err)
	}
	var d6 model.NotificationDelivery
	if err := db.Order("id DESC").First(&d6).Error; err != nil {
		t.Fatal(err)
	}
	if d6.Status != model.DeliveryPending {
		t.Fatalf("6th status = %s, want pending", d6.Status)
	}
	if wait := d6.NextRetry.Sub(*fake); wait < 900*time.Millisecond || wait > 1500*time.Millisecond {
		t.Fatalf("6th next_retry wait = %v, want ~1s", wait)
	}

	// 推进 1.1s：补充 1 个令牌 → 第 6 条发送
	*fake = fake.Add(1100 * time.Millisecond)
	q.ProcessDue()
	d6 = deliveryOf(t, db, d6.ID)
	if d6.Status != model.DeliverySent || hits.Load() != 6 {
		t.Fatalf("after 1.1s: status=%s hits=%d, want sent/6", d6.Status, hits.Load())
	}
	// 第 7 条（同一时刻入队）：桶中仅剩 0.1 令牌 → 仍需等待
	if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 0); err != nil {
		t.Fatal(err)
	}
	var d7 model.NotificationDelivery
	if err := db.Order("id DESC").First(&d7).Error; err != nil {
		t.Fatal(err)
	}
	if d7.Status != model.DeliveryPending || hits.Load() != 6 {
		t.Fatalf("7th: status=%s hits=%d, want pending/6", d7.Status, hits.Load())
	}
	*fake = fake.Add(time.Second)
	q.ProcessDue()
	d7 = deliveryOf(t, db, d7.ID)
	if d7.Status != model.DeliverySent || hits.Load() != 7 {
		t.Fatalf("after another 1s: status=%s hits=%d, want sent/7", d7.Status, hits.Load())
	}
}

// TestRateLimitZeroUnlimited 0 值不限：rate=0 或 burst=0 均不限流；
// 各渠道桶相互独立（受限渠道不影响其他渠道）。
func TestRateLimitZeroUnlimited(t *testing.T) {
	var hits atomic.Int32
	srv := okServer(&hits)
	defer srv.Close()

	q, db, _ := newRateQueue(t)
	// rate=0 / burst=0：完全不限流
	if err := db.Create(&model.Notification{ID: 1, Name: "zero", Type: "webhook", URL: srv.URL}).Error; err != nil {
		t.Fatal(err)
	}
	// burst=0（突发不限）：仅设速率也不限流
	if err := db.Create(&model.Notification{ID: 2, Name: "no-burst", Type: "webhook", URL: srv.URL, RateLimitPerMin: 1}).Error; err != nil {
		t.Fatal(err)
	}
	// rate=0（速率不限）：仅设突发也不限流
	if err := db.Create(&model.Notification{ID: 3, Name: "no-rate", Type: "webhook", URL: srv.URL, BurstLimit: 1}).Error; err != nil {
		t.Fatal(err)
	}
	// 受限渠道（用于验证独立性）
	if err := db.Create(&model.Notification{ID: 4, Name: "limited", Type: "webhook", URL: srv.URL, RateLimitPerMin: 1, BurstLimit: 1}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		for _, id := range []int64{1, 2, 3} {
			if err := q.Enqueue(&model.Notification{ID: id}, "t", "c", 0); err != nil {
				t.Fatal(err)
			}
		}
		// 渠道 4 只允许 1 条，其余延迟；且不影响渠道 1/2/3
		if err := q.Enqueue(&model.Notification{ID: 4}, "t", "c", 0); err != nil {
			t.Fatal(err)
		}
	}
	// 渠道 1/2/3 共 15 条全部发送；渠道 4 仅 1 条
	if hits.Load() != 16 {
		t.Fatalf("hits = %d, want 16 (15 unlimited + 1 limited burst)", hits.Load())
	}
	var pending []model.NotificationDelivery
	if err := db.Where("status = ?", model.DeliveryPending).Find(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if len(pending) != 4 {
		t.Fatalf("pending = %d, want 4 (all throttled deliveries of channel 4)", len(pending))
	}
	for _, d := range pending {
		if d.WebhookID != 4 {
			t.Fatalf("pending delivery belongs to channel %d, want 4 (buckets must be independent)", d.WebhookID)
		}
	}
}

// TestRateLimitRetryAfterThrottle 重试后成功：被限流的投递进入既有重试路径，
// 长时间超限不会被标记 failed，令牌恢复后投递成功并清空错误。
func TestRateLimitRetryAfterThrottle(t *testing.T) {
	var hits atomic.Int32
	srv := okServer(&hits)
	defer srv.Close()

	q, db, fake := newRateQueue(t)
	if err := db.Create(&model.Notification{ID: 1, Name: "n", Type: "webhook", URL: srv.URL, RateLimitPerMin: 1, BurstLimit: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 0); err != nil {
		t.Fatal(err)
	}
	// 第 2 条超限：延迟 ~60s
	if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 0); err != nil {
		t.Fatal(err)
	}
	var d model.NotificationDelivery
	if err := db.Order("id DESC").First(&d).Error; err != nil {
		t.Fatal(err)
	}
	if d.Status != model.DeliveryPending || d.Attempts != 0 {
		t.Fatalf("throttled: status=%s attempts=%d, want pending/0", d.Status, d.Attempts)
	}
	if d.LastError == "" {
		t.Fatal("last_error should record the rate-limit reason")
	}

	// 反复轮询但时间不推进：限流 6 次也不会消耗 attempts 或标记 failed
	for i := 0; i < 6; i++ {
		q.ProcessDue()
	}
	d = deliveryOf(t, db, d.ID)
	if d.Status != model.DeliveryPending {
		t.Fatalf("after repeated polls status = %s, want pending (must not fail due to throttle)", d.Status)
	}
	if d.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0", d.Attempts)
	}

	// 推进 61s：令牌补充 → 重试成功
	*fake = fake.Add(61 * time.Second)
	q.ProcessDue()
	d = deliveryOf(t, db, d.ID)
	if d.Status != model.DeliverySent {
		t.Fatalf("after refill status = %s, want sent", d.Status)
	}
	if d.Attempts != 1 || d.NextRetry != nil || d.LastError != "" {
		t.Fatalf("sent: attempts=%d next_retry=%v last_error=%q", d.Attempts, d.NextRetry, d.LastError)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2", hits.Load())
	}
}

// TestRateLimitBucketResetsOnConfigChange 限流配置变更后桶重置为满桶，
// 新配置立即生效（旧桶的欠账不延续）。
func TestRateLimitBucketResetsOnConfigChange(t *testing.T) {
	var hits atomic.Int32
	srv := okServer(&hits)
	defer srv.Close()

	q, db, _ := newRateQueue(t)
	if err := db.Create(&model.Notification{ID: 1, Name: "n", Type: "webhook", URL: srv.URL, RateLimitPerMin: 1, BurstLimit: 1}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 0); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1 (second delivery throttled)", hits.Load())
	}
	// 提高限流并立即验证：新配置满桶放行
	if err := db.Model(&model.Notification{}).Where("id = 1").Updates(map[string]any{"rate_limit_per_min": 10, "burst_limit": 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(&model.Notification{ID: 1}, "t", "c", 0); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("after config change hits = %d, want 2 (bucket reset to full)", hits.Load())
	}
}
