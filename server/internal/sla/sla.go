// Package sla 计算单台服务器按月可用性（SLA）并对照 SLO 目标判定达标。
// 可用性 = 有分钟级指标数据的分钟数 / 当月应计入分钟数；
// 应计入分钟数 = 当月总分钟 - 维护窗口覆盖分钟（维护期不计入考核，也不计入分母）。
package sla

import (
	"math"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/maintenance"
	"github.com/motao123/Argus/server/internal/model"
)

// Month 单月可用性结果。
type Month struct {
	Month string // "2006-01"
	// UptimeMinutes 当月有指标数据的分钟数（口径：分钟级指标行数）。
	UptimeMinutes int64 `json:"uptime_minutes"`
	// EligibleMinutes 应计入分钟数 = 当月分钟数 - 维护分钟数。
	EligibleMinutes int64 `json:"eligible_minutes"`
	// MaintenanceMinutes 维护窗口覆盖分钟数。
	MaintenanceMinutes int64 `json:"maintenance_minutes"`
	// Availability 可用率百分比；无可计入分钟时为 nil。
	Availability *float64 `json:"availability"`
	// SloTarget 该服务器 SLO 目标（百分比，0 = 未启用）。
	SloTarget float64 `json:"slo_target"`
	// SloMet 是否达标；SLO 未启用或无可计入分钟时为 nil。
	SloMet *bool `json:"slo_met"`
}

// ComputeMonth 计算 serverID 在 monthStart 起一个自然月的可用性。
// now 用于截断当前月的结束时间；createdAt 用于服务器创建于月中时的起始校正。
func ComputeMonth(db *gorm.DB, serverID int64, createdAt, monthStart, now time.Time) Month {
	from := monthStart
	if createdAt.After(from) {
		from = createdAt.Truncate(time.Minute)
	}
	to := monthStart.AddDate(0, 1, 0)
	if to.After(now) {
		to = now
	}
	m := Month{Month: monthStart.Format("2006-01"), SloTarget: 0}
	if !to.After(from) {
		return m
	}

	// 维护期内的“在线分钟”同时从分子与分母扣除：加载当月全部在线分钟，
	// 排除落在维护窗口内的分钟。
	covered, err := maintenance.CoveredTS(db, serverID, from, to)
	if err != nil {
		return m
	}
	var upTs []int64
	if err := db.Model(&model.Metric{}).
		Where("server_id = ? AND granularity = 60 AND ts >= ? AND ts < ?", serverID, from.Unix(), to.Unix()).
		Distinct("ts").Pluck("ts", &upTs).Error; err != nil {
		return m
	}
	up := int64(0)
	for _, ts := range upTs {
		if _, skip := covered[ts]; !skip {
			up++
		}
	}
	maintMinutes, err := maintenance.CoveredMinutes(db, serverID, from, to)
	if err != nil {
		return m
	}
	total := int64(to.Sub(from) / time.Minute)
	if maintMinutes > total {
		maintMinutes = total
	}
	m.UptimeMinutes = up
	m.MaintenanceMinutes = maintMinutes
	m.EligibleMinutes = total - maintMinutes
	if m.EligibleMinutes <= 0 {
		return m
	}
	avail := round2(float64(up) / float64(m.EligibleMinutes) * 100)
	m.Availability = &avail
	return m
}

// Series 返回最近 months 个月（含当前月）的逐月可用性。
func Series(db *gorm.DB, serverID int64, createdAt, now time.Time, months int) []Month {
	if months <= 0 {
		months = 6
	}
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	out := make([]Month, 0, months)
	for i := months - 1; i >= 0; i-- {
		monthStart := cur.AddDate(0, -i, 0)
		out = append(out, ComputeMonth(db, serverID, createdAt, monthStart, now))
	}
	return out
}

// ApplySLO 填充 SLO 目标与达标判定（Server 上的 SloTarget 字段）。
func ApplySLO(m *Month, sloTarget float64) {
	m.SloTarget = sloTarget
	if sloTarget <= 0 || m.Availability == nil {
		return
	}
	met := *m.Availability >= sloTarget
	m.SloMet = &met
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
