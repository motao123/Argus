// Package maintenance 维护窗口判定：供离线检测/告警在维护期间不误报，
// 以及 SLA/SLO 按月可用性计算时排除维护时段。
// 窗口语义：非重复窗口为一次性 [StartAt, EndAt)；重复窗口按 StartAt 的
// 星期与时刻每周重复，时长 = EndAt-StartAt（支持跨午夜/跨周末，须小于 7 天）。
package maintenance

import (
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

// Window 覆盖某台服务器的维护窗口列表。
type Window struct {
	model.MaintenanceWindow
}

// coversServer 窗口是否覆盖指定服务器（ServerIDs 为空 = 全部）。
func coversServer(w *model.MaintenanceWindow, serverID int64) bool {
	if strings.TrimSpace(w.ServerIDs) == "" {
		return true
	}
	for _, part := range strings.Split(w.ServerIDs, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id == serverID {
			return true
		}
	}
	return false
}

// windowsForServer 返回覆盖 serverID 的全部维护窗口。
func windowsForServer(db *gorm.DB, serverID int64) ([]model.MaintenanceWindow, error) {
	var wins []model.MaintenanceWindow
	if err := db.Find(&wins).Error; err != nil {
		return nil, err
	}
	out := wins[:0]
	for _, w := range wins {
		if coversServer(&w, serverID) {
			out = append(out, w)
		}
	}
	return out, nil
}

// activeAt 窗口在时刻 t 是否生效（绝对时刻比较）。
func activeAt(w *model.MaintenanceWindow, t time.Time) bool {
	if w.EndAt.Sub(w.StartAt) <= 0 {
		return false
	}
	if !w.Recurring {
		return !t.Before(w.StartAt) && t.Before(w.EndAt)
	}
	occ, end := occurrence(w, t)
	return !t.Before(occ) && t.Before(end)
}

// occurrence 返回重复窗口在 t 所在周期内的生效区间（覆盖 t 或最接近 t 的一次）。
func occurrence(w *model.MaintenanceWindow, t time.Time) (start, end time.Time) {
	loc := w.StartAt.Location()
	ti := t.In(loc)
	days := (int(ti.Weekday()) - int(w.StartAt.Weekday()) + 7) % 7
	start = time.Date(ti.Year(), ti.Month(), ti.Day()-days,
		w.StartAt.Hour(), w.StartAt.Minute(), w.StartAt.Second(), 0, loc)
	end = start.Add(w.EndAt.Sub(w.StartAt))
	return start, end
}

// IsActive 服务器 serverID 在时刻 t 是否处于维护期。
func IsActive(db *gorm.DB, serverID int64, t time.Time) (bool, error) {
	wins, err := windowsForServer(db, serverID)
	if err != nil {
		return false, err
	}
	for i := range wins {
		if activeAt(&wins[i], t) {
			return true, nil
		}
	}
	return false, nil
}

// ActiveServerIDs 返回时刻 t 处于维护期的服务器 ID 集合。
// coversAll 为 true 表示存在覆盖全部服务器（ServerIDs 为空）的生效窗口，
// 此时 ids 集合不完整，调用方应直接判定所有服务器均在维护。
func ActiveServerIDs(db *gorm.DB, t time.Time) (ids map[int64]bool, coversAll bool, err error) {
	var wins []model.MaintenanceWindow
	if err := db.Find(&wins).Error; err != nil {
		return nil, false, err
	}
	ids = make(map[int64]bool)
	for i := range wins {
		w := &wins[i]
		if !activeAt(w, t) {
			continue
		}
		if strings.TrimSpace(w.ServerIDs) == "" {
			coversAll = true
			continue
		}
		for _, part := range strings.Split(w.ServerIDs, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > 0 {
				ids[id] = true
			}
		}
	}
	return ids, coversAll, nil
}

// CoveredTS 返回 [from, to) 内 serverID 处于维护期的整分钟时间戳集合
// （多窗口重叠自动去重），供 SLA 精确扣减维护期内的“在线分钟”。
func CoveredTS(db *gorm.DB, serverID int64, from, to time.Time) (map[int64]struct{}, error) {
	wins, err := windowsForServer(db, serverID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]struct{})
	for i := range wins {
		w := &wins[i]
		d := w.EndAt.Sub(w.StartAt)
		if d <= 0 {
			continue
		}
		add := func(occ time.Time) {
			for ts := occ; ts.Before(occ.Add(d)); ts = ts.Add(time.Minute) {
				if !ts.Before(from) && ts.Before(to) {
					out[ts.Unix()] = struct{}{}
				}
			}
		}
		if !w.Recurring {
			add(w.StartAt)
			continue
		}
		if d >= 7*24*time.Hour {
			for ts := from; ts.Before(to); ts = ts.Add(time.Minute) {
				out[ts.Unix()] = struct{}{}
			}
			continue
		}
		loc := w.StartAt.Location()
		ti := from.In(loc)
		days := (int(ti.Weekday()) - int(w.StartAt.Weekday()) + 7) % 7
		first := time.Date(ti.Year(), ti.Month(), ti.Day()-days,
			w.StartAt.Hour(), w.StartAt.Minute(), w.StartAt.Second(), 0, loc)
		if first.Before(from) {
			first = first.Add(7 * 24 * time.Hour)
		}
		for occ := first; occ.Before(to); occ = occ.Add(7 * 24 * time.Hour) {
			add(occ)
		}
	}
	return out, nil
}

// CoveredMinutes 统计 [from, to) 区间内 serverID 处于维护期覆盖的分钟数
// （多窗口重叠只计一次：按窗口逐个累加后与区间总长取上限，避免重复计入）。
func CoveredMinutes(db *gorm.DB, serverID int64, from, to time.Time) (int64, error) {
	wins, err := windowsForServer(db, serverID)
	if err != nil {
		return 0, err
	}
	total := int64(0)
	for i := range wins {
		total += overlapMinutes(&wins[i], from, to)
	}
	if total > int64(to.Sub(from)/time.Minute) {
		total = int64(to.Sub(from) / time.Minute)
	}
	return total, nil
}

// overlapMinutes 单个窗口与 [from, to) 的重叠分钟数。
func overlapMinutes(w *model.MaintenanceWindow, from, to time.Time) int64 {
	d := w.EndAt.Sub(w.StartAt)
	if d <= 0 {
		return 0
	}
	if !w.Recurring {
		return overlapRange(w.StartAt, w.EndAt, from, to)
	}
	// 每周重复：枚举覆盖区间的各次生效区间
	if d >= 7*24*time.Hour {
		return int64(to.Sub(from) / time.Minute) // 整周生效 = 全覆盖
	}
	loc := w.StartAt.Location()
	ti := from.In(loc)
	days := (int(ti.Weekday()) - int(w.StartAt.Weekday()) + 7) % 7
	first := time.Date(ti.Year(), ti.Month(), ti.Day()-days,
		w.StartAt.Hour(), w.StartAt.Minute(), w.StartAt.Second(), 0, loc)
	if first.Before(from) {
		first = first.Add(7 * 24 * time.Hour)
	}
	sum := int64(0)
	for occ := first; occ.Before(to); occ = occ.Add(7 * 24 * time.Hour) {
		sum += overlapRange(occ, occ.Add(d), from, to)
	}
	return sum
}

func overlapRange(aStart, aEnd, bStart, bEnd time.Time) int64 {
	start := aStart
	if bStart.After(start) {
		start = bStart
	}
	end := aEnd
	if bEnd.Before(end) {
		end = bEnd
	}
	if !end.After(start) {
		return 0
	}
	return int64(end.Sub(start) / time.Minute)
}
