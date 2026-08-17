// Package traffic provides the canonical monthly traffic cycle and usage calculations.
package traffic

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

const (
	AccountingSum = "sum"
	AccountingIn  = "in"
	AccountingOut = "out"
	AccountingMax = "max"
)

// Window is a half-open traffic accounting cycle [Start, End).
type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Usage is the current cycle's canonical traffic quota view.
type Usage struct {
	CycleStart     time.Time `json:"cycle_start"`
	CycleEnd       time.Time `json:"cycle_end"`
	Timezone       string    `json:"timezone"`
	Accounting     string    `json:"accounting"`
	InBytes        uint64    `json:"in_bytes"`
	OutBytes       uint64    `json:"out_bytes"`
	AccountedBytes uint64    `json:"accounted_bytes"`
	QuotaBytes     uint64    `json:"quota_bytes"`
	RemainingBytes uint64    `json:"remaining_bytes"`
	Percentage     *float64  `json:"percentage,omitempty"`
}

// ValidateConfig validates values accepted by Server traffic quota settings.
func ValidateConfig(day int, timezone, accounting string) error {
	if day < 1 || day > 28 {
		return fmt.Errorf("traffic cycle day must be between 1 and 28")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("invalid traffic timezone")
	}
	switch accounting {
	case AccountingSum, AccountingIn, AccountingOut, AccountingMax:
		return nil
	default:
		return fmt.Errorf("traffic accounting must be sum, in, out, or max")
	}
}

// ValidUnit 报告单位是否为支持的流量周期单位（hour/day/week/month/year）。
func ValidUnit(unit string) bool {
	switch unit {
	case "hour", "day", "week", "month", "year":
		return true
	}
	return false
}

// AnchorWindow 以 anchor 为锚点、按 unit×interval 步进，返回包含 now 的周期窗口
// （借鉴 nezha GetTransferDurationStart/End 的锚点步进语义）。
// hour 用固定时长对齐（保留锚点的分/秒）；day/week/month/year 用日历步进
// （AddDate），窗口为 [start, end) 半开区间。
func AnchorWindow(now, anchor time.Time, unit string, interval int) (Window, error) {
	if !ValidUnit(unit) {
		return Window{}, fmt.Errorf("invalid cycle unit %q (must be hour, day, week, month, or year)", unit)
	}
	if interval <= 0 {
		interval = 1
	}
	step := func(t time.Time, n int) time.Time {
		switch unit {
		case "hour":
			return t.Add(time.Duration(n*interval) * time.Hour)
		case "day":
			return t.AddDate(0, 0, n*interval)
		case "week":
			return t.AddDate(0, 0, n*7*interval)
		case "month", "year":
			return addCalendar(t, unit, n*interval)
		default: // unreachable（ValidUnit 已校验）
			return t
		}
	}
	// 小时周期可用算术直接定位；日历周期循环步进（成本低，锚点异常偏远也有限）。
	if unit == "hour" {
		stepDur := time.Duration(interval) * time.Hour
		n := int(now.Sub(anchor) / stepDur)
		if now.Before(anchor.Add(stepDur * time.Duration(n))) {
			n--
		}
		start := anchor.Add(stepDur * time.Duration(n))
		return Window{Start: start, End: start.Add(stepDur)}, nil
	}
	start := anchor
	if start.After(now) {
		for start.After(now) {
			start = step(start, -1)
		}
	} else {
		for {
			next := step(start, 1)
			if next.After(now) {
				break
			}
			start = next
		}
	}
	return Window{Start: start, End: step(start, 1)}, nil
}

// addCalendar 按日历月/年步进 n 步（支持负数），并钳制溢出日
// （如 1/29 + 1 月 = 2/28，闰日 2/29 + 1 年 = 2/28），
// 避免 Go AddDate 的「日溢出归一化」（1/29 + 1 月 → 3/1）造成周期漂移。
func addCalendar(t time.Time, unit string, n int) time.Time {
	y, m, d := t.Date()
	if unit == "year" {
		y += n
	} else {
		total := (int(m) - 1) + n
		q := total / 12
		r := total % 12
		if r < 0 {
			q--
			r += 12
		}
		y += q
		m = time.Month(r + 1)
	}
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, t.Location()).Day()
	if d > last {
		d = last
	}
	return time.Date(y, m, d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// CycleWindow returns the monthly window containing now, using the configured local cycle day.
func CycleWindow(now time.Time, cycleDay int, timezone string) (Window, error) {
	if cycleDay < 1 || cycleDay > 28 {
		return Window{}, fmt.Errorf("traffic cycle day must be between 1 and 28")
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return Window{}, fmt.Errorf("invalid traffic timezone: %w", err)
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), cycleDay, 0, 0, 0, 0, loc)
	if local.Before(start) {
		start = start.AddDate(0, -1, 0)
	}
	return Window{Start: start, End: start.AddDate(0, 1, 0)}, nil
}

// Accounted applies a server's configured accounting mode to inbound/outbound bytes.
func Accounted(in, out uint64, accounting string) uint64 {
	switch accounting {
	case AccountingIn:
		return in
	case AccountingOut:
		return out
	case AccountingMax:
		if in > out {
			return in
		}
		return out
	default:
		return in + out
	}
}

// CurrentUsage sums persisted Transfer buckets in the server's current cycle.
func CurrentUsage(db *gorm.DB, server *model.Server, now time.Time) (Usage, error) {
	window, err := CycleWindow(now, server.TrafficCycleDay, server.TrafficTimezone)
	if err != nil {
		return Usage{}, err
	}
	var total struct {
		In  uint64
		Out uint64
	}
	if err := db.Model(&model.Transfer{}).
		Select("COALESCE(SUM(`in`),0) AS `in`, COALESCE(SUM(`out`),0) AS `out`").
		Where("server_id = ? AND ts >= ? AND ts < ?", server.ID, window.Start.Unix(), window.End.Unix()).
		Scan(&total).Error; err != nil {
		return Usage{}, err
	}
	accounted := Accounted(total.In, total.Out, server.TrafficAccounting)
	remaining := uint64(0)
	var percentage *float64
	if server.TrafficQuotaBytes > 0 {
		if accounted < server.TrafficQuotaBytes {
			remaining = server.TrafficQuotaBytes - accounted
		}
		p := float64(accounted) / float64(server.TrafficQuotaBytes) * 100
		percentage = &p
	}
	return Usage{
		CycleStart: window.Start, CycleEnd: window.End, Timezone: server.TrafficTimezone,
		Accounting: server.TrafficAccounting, InBytes: total.In, OutBytes: total.Out,
		AccountedBytes: accounted, QuotaBytes: server.TrafficQuotaBytes,
		RemainingBytes: remaining, Percentage: percentage,
	}, nil
}
