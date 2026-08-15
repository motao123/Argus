package api

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// serviceView 服务监控视图：配置 + 最近状态 + 今日可用率。
type serviceView struct {
	model.Service
	LastUp      bool    `json:"last_up"`
	LastDelay   int     `json:"last_delay"`
	LastCheckAt int64   `json:"last_check_at"`
	TodayUpRate float64 `json:"today_up_rate"` // 0-100
}

// listServices 服务监控列表。
func (s *Server) listServices(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Order("id")
	if p != nil && !p.IsAdmin && !p.IsPAT {
		q = q.Where("owner_id = ?", p.UserID)
	}
	offset, limit := pagination(c)
	var total int64
	q.Count(&total)
	var services []model.Service
	if err := q.Offset(offset).Limit(limit).Find(&services).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	todayStart := time.Now().Truncate(24 * time.Hour).Unix()
	out := make([]serviceView, 0, len(services))
	for i := range services {
		v := serviceView{Service: services[i]}
		// 最近一条
		var last model.ServiceHistory
		if err := s.DB.Where("service_id = ?", services[i].ID).Order("ts DESC, id DESC").First(&last).Error; err == nil {
			v.LastUp = last.UpCount > 0
			v.LastDelay = int(last.DelaySum / max64(1, int64(last.Total)))
			v.LastCheckAt = last.Ts
		}
		// 今日可用率
		var agg struct{ Up, Total int64 }
		s.DB.Model(&model.ServiceHistory{}).
			Select("COALESCE(SUM(up_count),0) as up, COALESCE(SUM(total),0) as total").
			Where("service_id = ? AND ts >= ?", services[i].ID, todayStart).
			Scan(&agg)
		if agg.Total > 0 {
			v.TodayUpRate = float64(agg.Up) / float64(agg.Total) * 100
		}
		out = append(out, v)
	}
	okPage(c, gin.H{"services": out}, total, offset, limit)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (s *Server) createService(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		ServerID int64  `json:"server_id"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		Target   string `json:"target"`
		Interval int    `json:"interval"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if req.ServerID <= 0 || req.Name == "" || req.Target == "" {
		fail(c, http.StatusBadRequest, "server_id/name/target required")
		return
	}
	switch req.Type {
	case "http", "tcp", "ping":
	default:
		req.Type = "http"
	}
	svc := model.Service{
		OwnerID:  p.UserID,
		ServerID: req.ServerID,
		Name:     req.Name,
		Type:     req.Type,
		Target:   req.Target,
		Interval: req.Interval,
		Enabled:  true,
	}
	if err := s.DB.Create(&svc).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, svc)
}

func (s *Server) updateService(c *gin.Context) {
	id := mustID(c)
	var svc model.Service
	if err := s.DB.First(&svc, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&svc.OwnerID, c) {
		fail(c, http.StatusForbidden, "not your service")
		return
	}
	var req struct {
		ServerID *int64  `json:"server_id"`
		Name     *string `json:"name"`
		Type     *string `json:"type"`
		Target   *string `json:"target"`
		Interval *int    `json:"interval"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	updates := map[string]any{}
	if req.ServerID != nil {
		updates["server_id"] = *req.ServerID
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Type != nil {
		updates["type"] = *req.Type
	}
	if req.Target != nil {
		updates["target"] = *req.Target
	}
	if req.Interval != nil {
		updates["interval"] = *req.Interval
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if err := s.DB.Model(&svc).Updates(updates).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

func (s *Server) deleteService(c *gin.Context) {
	id := mustID(c)
	var svc model.Service
	if err := s.DB.First(&svc, id).Error; err != nil {
		fail(c, http.StatusNotFound, "not found")
		return
	}
	if !s.canManage(&svc.OwnerID, c) {
		fail(c, http.StatusForbidden, "not your service")
		return
	}
	s.DB.Delete(&model.ServiceHistory{}, "service_id = ?", id)
	s.DB.Delete(&model.Service{}, id)
	ok(c, gin.H{"ok": true})
}

// serviceHistory 服务历史（1d 分钟级 / 7d 按小时聚合）。
func (s *Server) serviceHistory(c *gin.Context) {
	id := mustID(c)
	period := c.DefaultQuery("period", "1d")
	step := int64(60)
	seconds := int64(24 * 3600)
	switch period {
	case "7d":
		step = 3600
		seconds = 7 * 24 * 3600
	case "30d":
		step = 6 * 3600
		seconds = 30 * 24 * 3600
	default:
		period = "1d"
	}

	from := time.Now().Add(-time.Duration(seconds) * time.Second).Unix()
	var rows []model.ServiceHistory
	if err := s.DB.Where("service_id = ? AND ts >= ?", id, from).Order("ts").Find(&rows).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 聚合到 step
	type agg struct {
		up, total, delaySum int64
	}
	buckets := map[int64]*agg{}
	var order []int64
	for _, r := range rows {
		bts := r.Ts / step * step
		a, ok := buckets[bts]
		if !ok {
			a = &agg{}
			buckets[bts] = a
			order = append(order, bts)
		}
		a.up += int64(r.UpCount)
		a.total += int64(r.Total)
		a.delaySum += r.DelaySum
	}
	out := make([]gin.H, 0, len(order))
	for _, bts := range order {
		a := buckets[bts]
		rate := 0.0
		delay := 0
		if a.total > 0 {
			rate = float64(a.up) / float64(a.total) * 100
			delay = int(a.delaySum / a.total)
		}
		out = append(out, gin.H{"ts": bts, "up_rate": math.Round(rate*10) / 10, "delay": delay, "up": a.up, "total": a.total})
	}
	ok(c, gin.H{"period": period, "points": out})
}

// canManage 检查当前身份能否管理 owner 的资源。
func (s *Server) canManage(ownerID *int64, c *gin.Context) bool {
	p := principalFromContext(c)
	if p == nil {
		return false
	}
	if p.IsAdmin {
		return true
	}
	return *ownerID == p.UserID
}
