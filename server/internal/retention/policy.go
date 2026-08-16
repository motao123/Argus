// Package retention defines and applies the global data retention policy.
package retention

import (
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

const (
	SettingMetric1mDays       = "retention_metric_1m_days"
	SettingMetric5mDays       = "retention_metric_5m_days"
	SettingMetric1hDays       = "retention_metric_1h_days"
	SettingServiceHistoryDays = "retention_service_history_days"
	SettingTransferDays       = "retention_transfer_days"
	SettingTaskRunDays        = "retention_task_run_days"
	SettingAuditDays          = "retention_audit_days"
	SettingAuditMaxRows       = "retention_audit_max_rows"
)

// Policy is the single retention configuration used by the API and cleaner.
type Policy struct {
	Metric1mDays       int `json:"metric_1m_days"`
	Metric5mDays       int `json:"metric_5m_days"`
	Metric1hDays       int `json:"metric_1h_days"`
	ServiceHistoryDays int `json:"service_history_days"`
	TransferDays       int `json:"transfer_days"`
	TaskRunDays        int `json:"task_run_days"`
	AuditDays          int `json:"audit_days"`
	AuditMaxRows       int `json:"audit_max_rows"`
}

// Defaults preserves the existing metric/service retention and audit 5000-row cap.
func Defaults() Policy {
	return Policy{
		Metric1mDays: 1, Metric5mDays: 7, Metric1hDays: 30,
		ServiceHistoryDays: 30, TransferDays: 365, TaskRunDays: 30,
		AuditDays: 365, AuditMaxRows: 5000,
	}
}

type fieldSpec struct {
	key      string
	min, max int
	get      func(Policy) int
	set      func(*Policy, int)
}

var fields = []fieldSpec{
	{SettingMetric1mDays, 1, 30, func(p Policy) int { return p.Metric1mDays }, func(p *Policy, v int) { p.Metric1mDays = v }},
	{SettingMetric5mDays, 1, 365, func(p Policy) int { return p.Metric5mDays }, func(p *Policy, v int) { p.Metric5mDays = v }},
	{SettingMetric1hDays, 1, 3650, func(p Policy) int { return p.Metric1hDays }, func(p *Policy, v int) { p.Metric1hDays = v }},
	{SettingServiceHistoryDays, 1, 3650, func(p Policy) int { return p.ServiceHistoryDays }, func(p *Policy, v int) { p.ServiceHistoryDays = v }},
	{SettingTransferDays, 1, 3650, func(p Policy) int { return p.TransferDays }, func(p *Policy, v int) { p.TransferDays = v }},
	{SettingTaskRunDays, 1, 3650, func(p Policy) int { return p.TaskRunDays }, func(p *Policy, v int) { p.TaskRunDays = v }},
	{SettingAuditDays, 1, 3650, func(p Policy) int { return p.AuditDays }, func(p *Policy, v int) { p.AuditDays = v }},
	{SettingAuditMaxRows, 100, 1000000, func(p Policy) int { return p.AuditMaxRows }, func(p *Policy, v int) { p.AuditMaxRows = v }},
}

// SettingDefaults returns policy defaults in the settings storage format.
func SettingDefaults() map[string]string {
	p := Defaults()
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		out[f.key] = strconv.Itoa(f.get(p))
	}
	return out
}

// ValidateSettings validates policy keys present in a settings update. Other keys are ignored.
func ValidateSettings(values map[string]string) error {
	for _, f := range fields {
		raw, ok := values[f.key]
		if !ok {
			continue
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < f.min || v > f.max {
			return fmt.Errorf("%s must be an integer between %d and %d", f.key, f.min, f.max)
		}
	}
	return nil
}

// Load reads the latest persisted policy. Invalid legacy values safely fall back to defaults.
func Load(db *gorm.DB) Policy {
	p := Defaults()
	var settings []model.Setting
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		keys = append(keys, f.key)
	}
	if err := db.Where("key IN ?", keys).Find(&settings).Error; err != nil {
		return p
	}
	values := make(map[string]string, len(settings))
	for _, setting := range settings {
		values[setting.Key] = setting.Value
	}
	for _, f := range fields {
		if raw, ok := values[f.key]; ok {
			if v, err := strconv.Atoi(raw); err == nil && v >= f.min && v <= f.max {
				f.set(&p, v)
			}
		}
	}
	return p
}
