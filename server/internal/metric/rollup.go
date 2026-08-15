// Package metric rollup 阶梯聚合与保留清理（借鉴 komari pkg/metric rollup 简化版）。
package metric

import (
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

// Granularity 常量。
const (
	GranMinute = 60
	Gran5m     = 300
	GranHour   = 3600
)

// Rollup 聚合器：5 分钟从分钟表聚合，1 小时从 5 分钟表聚合并清理过期。
type Rollup struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Rollup { return &Rollup{db: db} }

type agg struct {
	count                    int
	cpu, netIn, netOut, load float64
	memUsed, memTotal, diskUsed, diskTotal uint64
}

// Aggregate5m 将分钟数据聚合到 5 分钟粒度（最近 24h 数据）。
func (r *Rollup) Aggregate5m() {
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	rows := []model.Metric{}
	r.db.Where("granularity = ? AND ts >= ?", GranMinute, cutoff).Find(&rows)

	byKey := map[int64]map[int64]*agg{}
	for _, row := range rows {
		bucket := row.TS / Gran5m * Gran5m
		m, ok := byKey[row.ServerID]
		if !ok {
			m = map[int64]*agg{}
			byKey[row.ServerID] = m
		}
		a, ok := m[bucket]
		if !ok {
			a = &agg{}
			m[bucket] = a
		}
		a.count++
		a.cpu += row.CPU
		a.memUsed = row.MemUsed
		a.memTotal = row.MemTotal
		a.diskUsed = row.DiskUsed
		a.diskTotal = row.DiskTotal
		a.netIn += row.NetInSpeed
		a.netOut += row.NetOutSpeed
		a.load += row.Load1
	}

	now := time.Now()
	for serverID, buckets := range byKey {
		for ts, a := range buckets {
			var exists int64
			r.db.Model(&model.Metric{}).
				Where("server_id = ? AND ts = ? AND granularity = ?", serverID, ts, Gran5m).
				Count(&exists)
			if exists > 0 {
				continue
			}
			n := float64(a.count)
			r.db.Create(&model.Metric{
				ServerID:    serverID,
				TS:          ts,
				Granularity: Gran5m,
				CPU:         a.cpu / n,
				MemUsed:     a.memUsed,
				MemTotal:    a.memTotal,
				DiskUsed:    a.diskUsed,
				DiskTotal:   a.diskTotal,
				NetInSpeed:  a.netIn / n,
				NetOutSpeed: a.netOut / n,
				Load1:       a.load / n,
				CreatedAt:   now,
			})
		}
	}
}

// AggregateHour 将 5 分钟数据聚合到小时粒度（最近 7 天）。
func (r *Rollup) AggregateHour() {
	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	rows := []model.Metric{}
	r.db.Where("granularity = ? AND ts >= ?", Gran5m, cutoff).Find(&rows)

	byKey := map[int64]map[int64]*agg{}
	for _, row := range rows {
		bucket := row.TS / GranHour * GranHour
		m, ok := byKey[row.ServerID]
		if !ok {
			m = map[int64]*agg{}
			byKey[row.ServerID] = m
		}
		a, ok := m[bucket]
		if !ok {
			a = &agg{}
			m[bucket] = a
		}
		a.count++
		a.cpu += row.CPU
		a.memUsed = row.MemUsed
		a.memTotal = row.MemTotal
		a.diskUsed = row.DiskUsed
		a.diskTotal = row.DiskTotal
		a.netIn += row.NetInSpeed
		a.netOut += row.NetOutSpeed
		a.load += row.Load1
	}

	now := time.Now()
	for serverID, buckets := range byKey {
		for ts, a := range buckets {
			var exists int64
			r.db.Model(&model.Metric{}).
				Where("server_id = ? AND ts = ? AND granularity = ?", serverID, ts, GranHour).
				Count(&exists)
			if exists > 0 {
				continue
			}
			n := float64(a.count)
			r.db.Create(&model.Metric{
				ServerID:    serverID,
				TS:          ts,
				Granularity: GranHour,
				CPU:         a.cpu / n,
				MemUsed:     a.memUsed,
				MemTotal:    a.memTotal,
				DiskUsed:    a.diskUsed,
				DiskTotal:   a.diskTotal,
				NetInSpeed:  a.netIn / n,
				NetOutSpeed: a.netOut / n,
				Load1:       a.load / n,
				CreatedAt:   now,
			})
		}
	}
}

// Cleanup 保留策略：分钟 24h / 5 分钟 7d / 小时 30d。
func (r *Rollup) Cleanup() {
	now := time.Now()
	r.db.Where("granularity = ? AND ts < ?", GranMinute, now.Add(-24*time.Hour).Unix()).
		Delete(&model.Metric{})
	r.db.Where("granularity = ? AND ts < ?", Gran5m, now.Add(-7*24*time.Hour).Unix()).
		Delete(&model.Metric{})
	r.db.Where("granularity = ? AND ts < ?", GranHour, now.Add(-30*24*time.Hour).Unix()).
		Delete(&model.Metric{})
	r.db.Where("ts < ?", now.Add(-30*24*time.Hour).Unix()).Delete(&model.ServiceHistory{})
	log.Printf("metric rollup cleanup done")
}
