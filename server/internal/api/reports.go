package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/notifier"
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
		cfg = model.TrafficReport{Hour: 9}
	}
	ok(c, cfg)
}

func (s *Server) saveTrafficReport(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		WebhookID int64 `json:"webhook_id"`
		Hour      int   `json:"hour"`
		Enabled   bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var cfg model.TrafficReport
	if err := s.DB.First(&cfg).Error; err == nil {
		s.DB.Model(&cfg).Updates(map[string]any{
			"webhook_id": req.WebhookID, "hour": req.Hour, "enabled": req.Enabled,
		})
	} else {
		_ = s.DB.Create(&model.TrafficReport{
			WebhookID: req.WebhookID, Hour: req.Hour, Enabled: req.Enabled,
		}).Error
	}
	ok(c, gin.H{"ok": true})
}

// RunTrafficReport 发送今日流量报告（由 main 每日定时调用）。
func (s *Server) RunTrafficReport() {
	var cfg model.TrafficReport
	if err := s.DB.First(&cfg).Error; err != nil || !cfg.Enabled || cfg.WebhookID <= 0 {
		return
	}
	var n model.Notification
	if err := s.DB.First(&n, cfg.WebhookID).Error; err != nil {
		return
	}
	todayStart := time.Now().Truncate(24 * time.Hour).Unix()
	var servers []model.Server
	s.DB.Find(&servers)
	lines := make([]string, 0, len(servers))
	for _, sv := range servers {
		var agg struct{ In, Out int64 }
		s.DB.Model(&model.Transfer{}).
			Select("COALESCE(SUM(in),0) as in, COALESCE(SUM(out),0) as out").
			Where("server_id = ? AND ts >= ?", sv.ID, todayStart).
			Scan(&agg)
		lines = append(lines, fmt.Sprintf("%s: ↓%s ↑%s",
			sv.Name, fmtBytes(agg.In), fmtBytes(agg.Out)))
	}
	content := "今日流量报告\n" + joinLines(lines)
	go notifier.Send(&n, "[Argus] 流量日报", content)
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

// RunExpireCheck 检查 3 天内到期的服务器并通知（每日调用）。
func (s *Server) RunExpireCheck() {
	// 复用第一个通知渠道（简化：与流量报告同一渠道）
	var n model.Notification
	if err := s.DB.Order("id").First(&n).Error; err != nil {
		return
	}
	deadline := time.Now().Add(3 * 24 * time.Hour)
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
	go notifier.Send(&n, "[Argus] 服务器到期提醒", joinLines(lines))
}

func remainingStr(t *time.Time) string {
	d := time.Until(*t)
	if d < 24*time.Hour {
		return fmt.Sprintf("剩余 %.0f 小时", d.Hours())
	}
	return fmt.Sprintf("剩余 %.0f 天", d.Hours()/24)
}
