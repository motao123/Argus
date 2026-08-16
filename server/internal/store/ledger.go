package store

import (
	"log"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

// TrafficLedger 周期流量账本：每次上报累加 reset-aware 增量到小时桶，
// 幂等 upsert 落库，持久化计数基线以支持重启恢复。
// 修复原实现「只在跨小时时记一次差值、大多数增量丢失」的缺陷。
type TrafficLedger struct {
	db *gorm.DB

	mu        sync.Mutex
	buckets   map[int64]*trafficBucket // serverID → 当前小时桶
	baselines map[int64]*trafficBaseline
}

type trafficBucket struct {
	hour int64
	in   uint64
	out  uint64
}

type trafficBaseline struct {
	in  uint64
	out uint64
	ts  int64
}

// maxReportGap 连续两次上报的最大可信间隔（秒）。超过则视为 Agent 重启/断档，
// 不把跨断档的累计差值记入当前桶（避免虚假尖峰）。
const maxReportGap = 300

func NewTrafficLedger(db *gorm.DB) *TrafficLedger {
	l := &TrafficLedger{db: db, buckets: make(map[int64]*trafficBucket), baselines: make(map[int64]*trafficBaseline)}
	l.loadBaselines()
	return l
}

// loadBaselines 启动时从 DB 恢复计数基线。
func (l *TrafficLedger) loadBaselines() {
	var rows []model.TrafficBaseline
	if err := l.db.Find(&rows).Error; err != nil {
		log.Printf("traffic ledger: load baselines: %v", err)
		return
	}
	for i := range rows {
		l.baselines[rows[i].ServerID] = &trafficBaseline{in: rows[i].In, out: rows[i].Out, ts: rows[i].TS}
	}
}

// Feed 消费一次上报的累计流量计数（reset-aware）。
func (l *TrafficLedger) Feed(serverID int64, ts int64, in, out uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	bl, ok := l.baselines[serverID]
	if ok && ts > bl.ts && ts-bl.ts <= maxReportGap {
		dIn := delta(bl.in, in)
		dOut := delta(bl.out, out)
		hour := ts / 3600 * 3600
		b, exists := l.buckets[serverID]
		if exists && b.hour != hour {
			// 跨小时：旧桶立即幂等落库，避免被新桶覆盖丢失
			l.upsertTransfer(serverID, b.hour, b.in, b.out)
			b = &trafficBucket{hour: hour}
			l.buckets[serverID] = b
		} else if !exists {
			b = &trafficBucket{hour: hour}
			l.buckets[serverID] = b
		}
		b.in += dIn
		b.out += dOut
	}
	// 无论是否累计，都推进基线（断档后从新值重新开始）
	l.baselines[serverID] = &trafficBaseline{in: in, out: out, ts: ts}
}

// delta 计算增量；计数器回绕/重置时以当前值为新增量。
func delta(prev, cur uint64) uint64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}

// Flush 把当前小时桶幂等累加写入 Transfer，并持久化基线。
// 读值后立即清零（同一临界区），避免重复计数或丢失。
func (l *TrafficLedger) Flush() {
	l.mu.Lock()
	type job struct {
		serverID int64
		hour     int64
		in       uint64
		out      uint64
	}
	jobs := make([]job, 0, len(l.buckets))
	for sid, b := range l.buckets {
		if b.in > 0 || b.out > 0 {
			jobs = append(jobs, job{serverID: sid, hour: b.hour, in: b.in, out: b.out})
			b.in = 0
			b.out = 0
		}
	}
	l.mu.Unlock()

	for _, j := range jobs {
		l.upsertTransfer(j.serverID, j.hour, j.in, j.out)
	}
	l.persistBaselines()
}

// upsertTransfer 幂等累加写入小时桶。
func (l *TrafficLedger) upsertTransfer(serverID, hour int64, in, out uint64) {
	if in == 0 && out == 0 {
		return
	}
	// 显式引用列名（in/out 为 SQL 关键字，需加引号）
	res := l.db.Model(&model.Transfer{}).
		Where("server_id = ? AND ts = ?", serverID, hour).
		Updates(map[string]any{
			"in":  gorm.Expr("`in` + ?", in),
			"out": gorm.Expr("`out` + ?", out),
		})
	if res.RowsAffected == 0 {
		l.db.Create(&model.Transfer{ServerID: serverID, Ts: hour, In: in, Out: out})
	}
}

// persistBaselines 持久化计数基线（重启恢复用）。
func (l *TrafficLedger) persistBaselines() {
	l.mu.Lock()
	snapshot := make(map[int64]*trafficBaseline, len(l.baselines))
	for k, v := range l.baselines {
		cp := *v
		snapshot[k] = &cp
	}
	l.mu.Unlock()
	for sid, bl := range snapshot {
		l.db.Model(&model.TrafficBaseline{}).Where("server_id = ?", sid).
			Updates(map[string]any{"in": bl.in, "out": bl.out, "ts": bl.ts})
	}
}

var _ = time.Now
