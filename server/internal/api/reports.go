package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/traffic"
)

// 流量报告周期。
const (
	ReportPeriodDaily   = "daily"
	ReportPeriodWeekly  = "weekly"
	ReportPeriodMonthly = "monthly"
)

// ---- 流量定时报告（借鉴 komari 流量报告通知）----

func (s *Server) getTrafficReport(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var cfg model.TrafficReport
	if err := s.DB.First(&cfg).Error; err != nil {
		cfg = defaultTrafficReport()
	}
	ok(c, cfg)
}

func defaultTrafficReport() model.TrafficReport {
	return model.TrafficReport{Period: ReportPeriodDaily, Hour: 9, Weekday: 1, Day: 1}
}

func (s *Server) saveTrafficReport(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		WebhookID int64  `json:"webhook_id"`
		Period    string `json:"period"`
		Hour      int    `json:"hour"`
		Weekday   int    `json:"weekday"`
		Day       int    `json:"day"`
		Enabled   bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if req.Period == "" {
		req.Period = ReportPeriodDaily // 兼容旧客户端（仅 webhook_id/hour/enabled）
	}
	if err := validateTrafficReport(req.Period, req.Hour, req.Weekday, req.Day); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	var cfg model.TrafficReport
	if err := s.DB.First(&cfg).Error; err == nil {
		s.DB.Model(&cfg).Updates(map[string]any{
			"webhook_id": req.WebhookID, "period": req.Period, "hour": req.Hour,
			"weekday": req.Weekday, "day": req.Day, "enabled": req.Enabled,
		})
	} else {
		_ = s.DB.Create(&model.TrafficReport{
			WebhookID: req.WebhookID, Period: req.Period, Hour: req.Hour,
			Weekday: req.Weekday, Day: req.Day, Enabled: req.Enabled,
		}).Error
	}
	ok(c, gin.H{"ok": true})
}

// validateTrafficReport 校验报告计划参数。
func validateTrafficReport(period string, hour, weekday, day int) error {
	switch period {
	case ReportPeriodDaily, ReportPeriodWeekly, ReportPeriodMonthly:
	default:
		return fmt.Errorf("period must be daily, weekly or monthly")
	}
	if hour < 0 || hour > 23 {
		return fmt.Errorf("hour must be between 0 and 23")
	}
	if period == ReportPeriodWeekly && (weekday < 0 || weekday > 6) {
		return fmt.Errorf("weekday must be between 0 (Sunday) and 6 (Saturday)")
	}
	if period == ReportPeriodMonthly && (day < 1 || day > 28) {
		return fmt.Errorf("day must be between 1 and 28")
	}
	return nil
}

// RunScheduledReports 由 main 每小时调用：命中报告计划的时刻，汇总周期流量并经通知持久队列发送。
// 日报告为默认计划（period 空或 daily），保留旧行为：每日 Hour 点发送。
func (s *Server) RunScheduledReports(now time.Time) {
	var cfg model.TrafficReport
	if err := s.DB.First(&cfg).Error; err != nil || cfg.WebhookID <= 0 {
		return
	}
	if !trafficReportDue(&cfg, now) {
		return
	}
	var n model.Notification
	if err := s.DB.First(&n, cfg.WebhookID).Error; err != nil {
		return
	}
	title, content, err := buildTrafficReport(s.DB, cfg.Period, now)
	if err != nil {
		return
	}
	s.sendViaQueue(&n, title, content)
}

// trafficReportDue 判定 now 是否命中计划：daily=hour；weekly=weekday+hour；monthly=day+hour。
func trafficReportDue(cfg *model.TrafficReport, now time.Time) bool {
	if cfg == nil || !cfg.Enabled {
		return false
	}
	period := cfg.Period
	if period == "" {
		period = ReportPeriodDaily
	}
	if now.Hour() != cfg.Hour {
		return false
	}
	switch period {
	case ReportPeriodWeekly:
		return int(now.Weekday()) == cfg.Weekday
	case ReportPeriodMonthly:
		return now.Day() == cfg.Day
	default:
		return true
	}
}

// trafficReportWindow 返回报告覆盖的半开区间 [Start, End)（复用 traffic.Window 语义）：
// daily=今日零点起；weekly=过去 7 天；monthly=本月 1 日起。
func trafficReportWindow(period string, now time.Time) traffic.Window {
	loc := now.Location()
	switch period {
	case ReportPeriodWeekly:
		return traffic.Window{Start: now.AddDate(0, 0, -7), End: now}
	case ReportPeriodMonthly:
		return traffic.Window{Start: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc), End: now}
	default:
		return traffic.Window{Start: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc), End: now}
	}
}

// buildTrafficReport 汇总周期内各服务器 in/out/accounted。
// accounted 按各服务器计费方式（sum/in/out/max，复用 traffic.Accounted）。
func buildTrafficReport(db *gorm.DB, period string, now time.Time) (title, content string, err error) {
	win := trafficReportWindow(period, now)
	var servers []model.Server
	if err := db.Find(&servers).Error; err != nil {
		return "", "", err
	}
	lines := make([]string, 0, len(servers))
	for _, sv := range servers {
		var agg struct{ In, Out uint64 }
		db.Model(&model.Transfer{}).
			Select("COALESCE(SUM(`in`),0) AS `in`, COALESCE(SUM(`out`),0) AS `out`").
			Where("server_id = ? AND ts >= ? AND ts < ?", sv.ID, win.Start.Unix(), win.End.Unix()).
			Scan(&agg)
		accounted := traffic.Accounted(agg.In, agg.Out, sv.TrafficAccounting)
		lines = append(lines, fmt.Sprintf("%s: ↓%s ↑%s · 计费 %s",
			sv.Name, fmtBytes(int64(agg.In)), fmtBytes(int64(agg.Out)), fmtBytes(int64(accounted))))
	}
	head, title := "今日流量报告", "[Argus] 流量日报"
	switch period {
	case ReportPeriodWeekly:
		head, title = "本周流量报告", "[Argus] 流量周报"
	case ReportPeriodMonthly:
		head, title = "本月流量报告", "[Argus] 流量月报"
	}
	return title, head + "\n" + joinLines(lines), nil
}

func fmtBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

// ---- 到期提醒（借鉴 komari renewal）----

// RunExpireCheck 检查 expire_notify_days 天内到期的服务器并通知（每日调用）。
func (s *Server) RunExpireCheck() {
	// 复用第一个通知渠道（简化：与流量报告同一渠道）
	var n model.Notification
	if err := s.DB.Order("id").First(&n).Error; err != nil {
		return
	}
	deadline := time.Now().Add(time.Duration(s.expireNotifyDays()) * 24 * time.Hour)
	var servers []model.Server
	s.DB.Where("expire_at IS NOT NULL AND expire_at <= ?", deadline).Find(&servers)
	if len(servers) == 0 {
		return
	}
	lines := make([]string, 0, len(servers))
	for _, sv := range servers {
		if sv.ExpireAt != nil && sv.ExpireAt.After(time.Now()) {
			lines = append(lines, fmt.Sprintf("%s: %s 到期（%s）", sv.Name, sv.ExpireAt.Format("2006-01-02"), remainingStr(sv.ExpireAt)))
		}
	}
	if len(lines) == 0 {
		return
	}
	s.sendViaQueue(&n, "[Argus] 服务器到期提醒", joinLines(lines))
}

// expireNotifyDays 读「到期提前提醒天数」设置（默认 3，范围 1-30；越界/非法回退默认）。
func (s *Server) expireNotifyDays() int {
	raw := s.GetSetting(SettingExpireNotifyDays, "3")
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 || v > 30 {
		return 3
	}
	return v
}

// sendViaQueue 通过持久队列发送（未接线时仅记录日志，不阻塞每日任务）。
func (s *Server) sendViaQueue(n *model.Notification, title, content string) {
	if s.Notifier == nil {
		return
	}
	_ = s.Notifier.Enqueue(n, title, content, 0)
}

func remainingStr(t *time.Time) string {
	d := time.Until(*t)
	if d < 24*time.Hour {
		return fmt.Sprintf("剩余 %.0f 小时", d.Hours())
	}
	return fmt.Sprintf("剩余 %.0f 天", d.Hours()/24)
}
