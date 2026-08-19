package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/sentinel"
)

// serviceView 服务监控视图：配置 + 最近状态 + 今日可用率。
type serviceView struct {
	model.Service
	ServerIDs    []int64  `json:"server_ids"`
	LastUp       *bool    `json:"last_up"`
	LastDelay    *int     `json:"last_delay"`
	LastCheckAt  int64    `json:"last_check_at"`
	TodayUpRate  *float64 `json:"today_up_rate"`
	Availability *float64 `json:"availability"`
	MinDelay     *int     `json:"min_delay"`
	AvgDelay     *int     `json:"avg_delay"`
	MaxDelay     *int     `json:"max_delay"`
	DelayP50     *int     `json:"delay_p50"` // 滑动窗口分位数（窗口样本 < 30 时为 null）
	DelayP95     *int     `json:"delay_p95"`
	DelayP99     *int     `json:"delay_p99"`
	DelayStdDev  *int     `json:"delay_stddev_ms"`
	DelayJitter  *int     `json:"delay_jitter_ms"`
	LossRate     *float64 `json:"loss_rate"`
	StatusCode   *int     `json:"status_code"`
	CertDays     *int     `json:"cert_days"`
	DNSMs        *int     `json:"dns_ms"`
	ConnectMs    *int     `json:"connect_ms"`
	TLSMs        *int     `json:"tls_ms"`
	TTFBMs       *int     `json:"ttfb_ms"`
}

// listServices 服务监控列表。
func (s *Server) listServices(c *gin.Context) {
	p := principalFromContext(c)
	q := s.DB.Model(&model.Service{}).Order("id")
	switch {
	case p == nil:
		// 游客只看公开服务（私有站点模式一律不可见）
		if s.GetSetting(SettingForceAuth, "0") == "1" {
			q = q.Where("1 = 0")
		} else {
			q = q.Where("hidden = ?", false)
		}
	case p.IsAdmin:
		// 全部
	case p.IsPAT:
		if !p.hasScope(ScopeServiceRead) {
			fail(c, http.StatusForbidden, "insufficient scope: "+ScopeServiceRead)
			return
		}
		q = q.Where("owner_id = ?", p.UserID)
	default:
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
		v := serviceView{Service: services[i], ServerIDs: s.serviceProbeIDs(services[i])}
		// 最近一条
		var last model.ServiceHistory
		if err := s.DB.Where("service_id = ?", services[i].ID).Order("ts DESC, id DESC").First(&last).Error; err == nil {
			up := last.UpCount > 0
			delay := int(last.DelaySum / max64(1, int64(last.Total)))
			v.LastUp, v.LastDelay, v.LastCheckAt = &up, &delay, last.Ts
			if last.StatusCode > 0 {
				v.StatusCode = &last.StatusCode
			}
			v.CertDays = last.CertDays
			v.DNSMs, v.ConnectMs, v.TLSMs, v.TTFBMs = optionalDuration(last.DNSMs), optionalDuration(last.ConnectMs), optionalDuration(last.TLSMs), optionalDuration(last.TTFBMs)
		}
		// 今日可用率
		var agg struct{ Up, Total int64 }
		s.DB.Model(&model.ServiceHistory{}).
			Select("COALESCE(SUM(up_count),0) as up, COALESCE(SUM(total),0) as total").
			Where("service_id = ? AND ts >= ?", services[i].ID, todayStart).
			Scan(&agg)
		if agg.Total > 0 {
			rate := math.Round(float64(agg.Up)/float64(agg.Total)*1000) / 10
			v.TodayUpRate = &rate
		}
		var stats struct {
			Up, Total, Sent, Received, DelaySum int64
			MinDelay, MaxDelay                  int
		}
		s.DB.Model(&model.ServiceHistory{}).
			Select("COALESCE(SUM(up_count),0) up, COALESCE(SUM(total),0) total, COALESCE(SUM(sent),0) sent, COALESCE(SUM(received),0) received, COALESCE(SUM(delay_sum),0) delay_sum, COALESCE(MIN(CASE WHEN total > 0 THEN delay_min END),0) min_delay, COALESCE(MAX(delay_max),0) max_delay").
			Where("service_id = ? AND ts >= ?", services[i].ID, time.Now().Add(-24*time.Hour).Unix()).Scan(&stats)
		if stats.Total > 0 {
			availability := round2(float64(stats.Up) / float64(stats.Total) * 100)
			avg := int(stats.DelaySum / stats.Total)
			v.Availability, v.MinDelay, v.AvgDelay, v.MaxDelay = &availability, &stats.MinDelay, &avg, &stats.MaxDelay
			loss := 100 - availability
			if stats.Sent > 0 {
				loss = round2(float64(stats.Sent-stats.Received) / float64(stats.Sent) * 100)
			}
			v.LossRate = &loss
		}
		// 滑动窗口分位数：取最近 24h 内最新一个样本充足（≥ 30）的分钟桶快照。
		v.DelayP50, v.DelayP95, v.DelayP99, v.DelayStdDev, v.DelayJitter =
			s.latestDelayQuantiles(services[i].ID, time.Now().Add(-24*time.Hour).Unix())
		out = append(out, v)
	}
	okPage(c, gin.H{"services": out}, total, offset, limit)
}

func (s *Server) serviceProbeIDs(svc model.Service) []int64 {
	var probes []model.ServiceProbe
	if err := s.DB.Where("service_id = ?", svc.ID).Order("server_id").Find(&probes).Error; err == nil && len(probes) > 0 {
		ids := make([]int64, 0, len(probes))
		for _, probe := range probes {
			ids = append(ids, probe.ServerID)
		}
		return ids
	}
	if svc.ServerID > 0 {
		return []int64{svc.ServerID}
	}
	return []int64{}
}

func normalizeServiceServerIDs(serverID int64, serverIDs []int64) []int64 {
	out := make([]int64, 0, len(serverIDs)+1)
	seen := make(map[int64]struct{}, len(serverIDs)+1)
	for _, id := range append([]int64{serverID}, serverIDs...) {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func optionalDuration(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
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
		ServerID              int64   `json:"server_id"`
		ServerIDs             []int64 `json:"server_ids"`
		Name                  string  `json:"name"`
		Type                  string  `json:"type"`
		Target                string  `json:"target"`
		Interval              int     `json:"interval"`
		Notify                bool    `json:"notify"`
		NotifyWebhookID       int64   `json:"notify_webhook_id"`
		NotificationGroupID   int64   `json:"notification_group_id"`
		HTTPMethod            string  `json:"http_method"`
		VerifyTLS             *bool   `json:"verify_tls"`
		Timeout               int     `json:"timeout"`
		ExpectedStatusMin     int     `json:"expected_status_min"`
		ExpectedStatusMax     int     `json:"expected_status_max"`
		ExpectedStatuses      string  `json:"expected_statuses"` // 逗号分隔状态码列表；空 = 按区间判定（列表优先）
		MaxRedirects          int     `json:"max_redirects"`
		PingCount             int     `json:"ping_count"`
		RequestHeaders        string  `json:"request_headers"` // JSON: [{"key","value"}]
		RequestBody           string  `json:"request_body"`
		AssertContains        string  `json:"assert_contains"`
		CertWarn              bool    `json:"cert_warn"`
		Hidden                bool    `json:"hidden"`
		FailureTriggerCronID  int64   `json:"failure_trigger_cron_id"`
		RecoveryTriggerCronID int64   `json:"recovery_trigger_cron_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	ids := normalizeServiceServerIDs(req.ServerID, req.ServerIDs)
	if len(ids) == 0 || req.Name == "" || req.Target == "" {
		fail(c, http.StatusBadRequest, "server_id/server_ids/name/target required")
		return
	}
	for _, serverID := range ids {
		if _, ok := s.authorizeServer(c, serverID, ScopeServiceWrite); !ok {
			fail(c, http.StatusForbidden, "server access denied")
			return
		}
	}
	switch req.Type {
	case "http", "tcp", "ping", "command":
	default:
		req.Type = "http"
	}
	if req.Type == "command" && len(req.Target) > 512 {
		fail(c, http.StatusBadRequest, "command too long")
		return
	}
	if req.HTTPMethod == "" {
		req.HTTPMethod = "GET"
	}
	if !protocol.IsAllowedHTTPMethod(req.HTTPMethod) {
		fail(c, http.StatusBadRequest, "http_method must be one of "+strings.Join(protocol.AllowedHTTPMethods, "/"))
		return
	}
	headers, err := normalizeRequestHeaders(req.RequestHeaders)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateHTTPBody(req.HTTPMethod, req.RequestBody); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.AssertContains) > 1024 {
		fail(c, http.StatusBadRequest, "assert_contains too long")
		return
	}
	if req.Timeout <= 0 {
		req.Timeout = 10
	}
	if req.ExpectedStatusMin == 0 {
		req.ExpectedStatusMin = 200
	}
	if req.ExpectedStatusMax == 0 {
		req.ExpectedStatusMax = 399
	}
	if req.ExpectedStatusMin < 100 || req.ExpectedStatusMax > 599 || req.ExpectedStatusMin > req.ExpectedStatusMax {
		fail(c, http.StatusBadRequest, "invalid expected status range")
		return
	}
	expectedStatuses, err := normalizeExpectedStatuses(req.ExpectedStatuses)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.MaxRedirects <= 0 {
		req.MaxRedirects = 3
	}
	if req.MaxRedirects > 10 {
		fail(c, http.StatusBadRequest, "max_redirects must be at most 10")
		return
	}
	if req.PingCount <= 0 {
		req.PingCount = 3
	}
	if req.PingCount > 10 {
		fail(c, http.StatusBadRequest, "ping_count must be at most 10")
		return
	}
	verifyTLS := true
	if req.VerifyTLS != nil {
		verifyTLS = *req.VerifyTLS
	}
	svc := model.Service{
		OwnerID:               p.UserID,
		ServerID:              ids[0],
		Name:                  req.Name,
		Type:                  req.Type,
		Target:                req.Target,
		Interval:              req.Interval,
		Enabled:               true,
		Hidden:                req.Hidden,
		Notify:                req.Notify,
		NotifyWebhookID:       req.NotifyWebhookID,
		NotificationGroupID:   req.NotificationGroupID,
		HTTPMethod:            req.HTTPMethod,
		VerifyTLS:             &verifyTLS,
		Timeout:               req.Timeout,
		ExpectedStatusMin:     req.ExpectedStatusMin,
		ExpectedStatusMax:     req.ExpectedStatusMax,
		ExpectedStatuses:      expectedStatuses,
		MaxRedirects:          req.MaxRedirects,
		PingCount:             req.PingCount,
		RequestHeaders:        headers,
		RequestBody:           req.RequestBody,
		AssertContains:        req.AssertContains,
		CertWarn:              req.CertWarn,
		FailureTriggerCronID:  req.FailureTriggerCronID,
		RecoveryTriggerCronID: req.RecoveryTriggerCronID,
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&svc).Error; err != nil {
			return err
		}
		probes := make([]model.ServiceProbe, 0, len(ids))
		for _, serverID := range ids {
			probes = append(probes, model.ServiceProbe{ServiceID: svc.ID, ServerID: serverID})
		}
		return tx.Create(&probes).Error
	}); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, serviceView{Service: svc, ServerIDs: ids})
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
		ServerID              *int64   `json:"server_id"`
		ServerIDs             *[]int64 `json:"server_ids"`
		Name                  *string  `json:"name"`
		Type                  *string  `json:"type"`
		Target                *string  `json:"target"`
		Interval              *int     `json:"interval"`
		Timeout               *int     `json:"timeout"`
		HTTPMethod            *string  `json:"http_method"`
		VerifyTLS             *bool    `json:"verify_tls"`
		ExpectedStatusMin     *int     `json:"expected_status_min"`
		ExpectedStatusMax     *int     `json:"expected_status_max"`
		ExpectedStatuses      *string  `json:"expected_statuses"` // 逗号分隔状态码列表；空串 = 清空列表回到区间判定
		MaxRedirects          *int     `json:"max_redirects"`
		PingCount             *int     `json:"ping_count"`
		RequestHeaders        *string  `json:"request_headers"`
		RequestBody           *string  `json:"request_body"`
		AssertContains        *string  `json:"assert_contains"`
		Enabled               *bool    `json:"enabled"`
		Hidden                *bool    `json:"hidden"`
		Notify                *bool    `json:"notify"`
		NotifyWebhookID       *int64   `json:"notify_webhook_id"`
		NotificationGroupID   *int64   `json:"notification_group_id"`
		CertWarn              *bool    `json:"cert_warn"`
		FailureTriggerCronID  *int64   `json:"failure_trigger_cron_id"`
		RecoveryTriggerCronID *int64   `json:"recovery_trigger_cron_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	var probeIDs []int64
	if req.ServerIDs != nil {
		defaultID := int64(0)
		if req.ServerID != nil {
			defaultID = *req.ServerID
		}
		probeIDs = normalizeServiceServerIDs(defaultID, *req.ServerIDs)
	} else if req.ServerID != nil {
		probeIDs = normalizeServiceServerIDs(*req.ServerID, nil)
	}
	for _, serverID := range probeIDs {
		if _, ok := s.authorizeServer(c, serverID, ScopeServiceWrite); !ok {
			fail(c, http.StatusForbidden, "server access denied")
			return
		}
	}
	if (req.ServerIDs != nil || req.ServerID != nil) && len(probeIDs) == 0 {
		fail(c, http.StatusBadRequest, "at least one probe server required")
		return
	}
	updates := map[string]any{}
	if len(probeIDs) > 0 {
		updates["server_id"] = probeIDs[0]
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Type != nil {
		switch *req.Type {
		case "http", "tcp", "ping", "command":
		default:
			fail(c, http.StatusBadRequest, "unsupported service type: "+*req.Type)
			return
		}
		updates["type"] = *req.Type
	}
	if req.Target != nil {
		updates["target"] = *req.Target
	}
	if req.Interval != nil {
		updates["interval"] = *req.Interval
	}
	if req.Timeout != nil {
		updates["timeout"] = *req.Timeout
	}
	if req.HTTPMethod != nil {
		if !protocol.IsAllowedHTTPMethod(*req.HTTPMethod) {
			fail(c, http.StatusBadRequest, "http_method must be one of "+strings.Join(protocol.AllowedHTTPMethods, "/"))
			return
		}
		updates["http_method"] = *req.HTTPMethod
	}
	if req.VerifyTLS != nil {
		updates["verify_tls"] = *req.VerifyTLS
	}
	if req.ExpectedStatusMin != nil {
		updates["expected_status_min"] = *req.ExpectedStatusMin
	}
	if req.ExpectedStatusMax != nil {
		updates["expected_status_max"] = *req.ExpectedStatusMax
	}
	if req.MaxRedirects != nil {
		updates["max_redirects"] = *req.MaxRedirects
	}
	if req.PingCount != nil {
		updates["ping_count"] = *req.PingCount
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Hidden != nil {
		updates["hidden"] = *req.Hidden
	}
	if req.Notify != nil {
		updates["notify"] = *req.Notify
	}
	if req.NotifyWebhookID != nil {
		updates["notify_webhook_id"] = *req.NotifyWebhookID
	}
	for key, value := range map[string]any{
		"notification_group_id": req.NotificationGroupID, "http_method": req.HTTPMethod,
		"verify_tls": req.VerifyTLS, "timeout": req.Timeout, "expected_status_min": req.ExpectedStatusMin,
		"expected_status_max": req.ExpectedStatusMax, "ping_count": req.PingCount, "cert_warn": req.CertWarn,
		"failure_trigger_cron_id": req.FailureTriggerCronID, "recovery_trigger_cron_id": req.RecoveryTriggerCronID,
	} {
		switch v := value.(type) {
		case *int64:
			if v != nil {
				updates[key] = *v
			}
		case *int:
			if v != nil {
				updates[key] = *v
			}
		case *string:
			if v != nil {
				updates[key] = *v
			}
		case *bool:
			if v != nil {
				updates[key] = *v
			}
		}
	}
	minStatus, maxStatus := svc.ExpectedStatusMin, svc.ExpectedStatusMax
	if req.ExpectedStatusMin != nil {
		minStatus = *req.ExpectedStatusMin
	}
	if req.ExpectedStatusMax != nil {
		maxStatus = *req.ExpectedStatusMax
	}
	if minStatus < 100 || maxStatus > 599 || minStatus > maxStatus {
		fail(c, http.StatusBadRequest, "invalid expected status range")
		return
	}
	// 期望状态码列表（空串 = 清空列表回到区间判定；设置后列表优先于区间）。
	if req.ExpectedStatuses != nil {
		expectedStatuses, err := normalizeExpectedStatuses(*req.ExpectedStatuses)
		if err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		updates["expected_statuses"] = expectedStatuses
	}
	if req.MaxRedirects != nil && (*req.MaxRedirects < 0 || *req.MaxRedirects > 10) {
		fail(c, http.StatusBadRequest, "max_redirects must be 0-10")
		return
	}
	if req.PingCount != nil && (*req.PingCount < 1 || *req.PingCount > 10) {
		fail(c, http.StatusBadRequest, "ping_count must be 1-10")
		return
	}
	// 自定义请求参数（HTTP 专用）：方法与 body 的搭配、请求头 JSON、断言关键字长度。
	method := svc.HTTPMethod
	if req.HTTPMethod != nil {
		method = *req.HTTPMethod
	}
	if req.RequestBody != nil {
		if err := validateHTTPBody(method, *req.RequestBody); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		updates["request_body"] = *req.RequestBody
	}
	if req.RequestHeaders != nil {
		headers, err := normalizeRequestHeaders(*req.RequestHeaders)
		if err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		updates["request_headers"] = headers
	}
	if req.AssertContains != nil {
		if len(*req.AssertContains) > 1024 {
			fail(c, http.StatusBadRequest, "assert_contains too long")
			return
		}
		updates["assert_contains"] = *req.AssertContains
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&svc).Updates(updates).Error; err != nil {
			return err
		}
		if req.ServerIDs == nil && req.ServerID == nil {
			return nil
		}
		if err := tx.Where("service_id = ?", svc.ID).Delete(&model.ServiceProbe{}).Error; err != nil {
			return err
		}
		probes := make([]model.ServiceProbe, 0, len(probeIDs))
		for _, serverID := range probeIDs {
			probes = append(probes, model.ServiceProbe{ServiceID: svc.ID, ServerID: serverID})
		}
		return tx.Create(&probes).Error
	}); err != nil {
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
	s.DB.Delete(&model.ServiceProbe{}, "service_id = ?", id)
	s.DB.Delete(&model.Service{}, id)
	ok(c, gin.H{"ok": true})
}

// canViewService 服务可见性：游客只看公开；普通用户/PAT 只看自己名下（PAT 需 read scope）。
func (s *Server) canViewService(c *gin.Context, svc *model.Service) bool {
	p := principalFromContext(c)
	if p == nil {
		return s.GetSetting(SettingForceAuth, "0") != "1" && !svc.Hidden
	}
	if p.IsAdmin {
		return true
	}
	if svc.OwnerID != p.UserID {
		return false
	}
	if p.IsPAT && !p.hasScope(ScopeServiceRead) {
		return false
	}
	for _, serverID := range s.serviceProbeIDs(*svc) {
		if _, ok := s.authorizeServer(c, serverID, ScopeServiceRead); !ok {
			return false
		}
	}
	return true
}

// serviceHistory 服务历史（1d 分钟级 / 7d 按小时聚合）。
func (s *Server) serviceHistory(c *gin.Context) {
	id := mustID(c)
	var svc model.Service
	if err := s.DB.First(&svc, id).Error; err != nil {
		fail(c, http.StatusNotFound, "service not found")
		return
	}
	if !s.canViewService(c, &svc) {
		fail(c, http.StatusNotFound, "service not found")
		return
	}
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

// serviceStats 服务统计汇总（最近 24h：可用率/平均延迟/最大延迟/丢包率）。
func (s *Server) serviceStats(c *gin.Context) {
	id := mustID(c)
	var svc model.Service
	if err := s.DB.First(&svc, id).Error; err != nil {
		fail(c, http.StatusNotFound, "service not found")
		return
	}
	if !s.canViewService(c, &svc) {
		fail(c, http.StatusNotFound, "service not found")
		return
	}
	from := time.Now().Add(-24 * time.Hour).Unix()
	var agg struct {
		Up, Total                    int64
		DelaySum, MinDelay, MaxDelay int64
		Sent, Received               int64
		CertDaysMin                  *int
	}
	s.DB.Model(&model.ServiceHistory{}).
		Select(`COALESCE(SUM(up_count),0) as up, COALESCE(SUM(total),0) as total,
		        COALESCE(SUM(delay_sum),0) as delay_sum, COALESCE(MIN(delay_min),0) as min_delay,
		        COALESCE(MAX(delay_max),0) as max_delay, COALESCE(SUM(sent),0) as sent,
		        COALESCE(SUM(received),0) as received, MIN(cert_days) as cert_days_min`).
		Where("service_id = ? AND ts >= ?", id, from).
		Scan(&agg)
	upRate := 0.0
	lossRate := 0.0
	avgDelay := 0
	if agg.Total > 0 {
		upRate = float64(agg.Up) / float64(agg.Total) * 100
		avgDelay = int(agg.DelaySum / agg.Total)
	}
	if agg.Sent > 0 {
		lossRate = float64(agg.Sent-agg.Received) / float64(agg.Sent) * 100
	}
	// 滑动窗口分位数：最近 24h 内最新一个样本充足（≥ 30）的分钟桶快照；缺样本为 null。
	p50, p95, p99, stddev, jitter := s.latestDelayQuantiles(id, from)
	ok(c, gin.H{
		"up_rate":   round2(upRate),
		"loss_rate": round2(lossRate),
		"min_delay": int(agg.MinDelay), "avg_delay": avgDelay, "max_delay": int(agg.MaxDelay),
		"delay_p50": p50, "delay_p95": p95, "delay_p99": p99,
		"delay_stddev_ms": stddev, "delay_jitter_ms": jitter,
		"total_probes": agg.Total, "sent": agg.Sent, "received": agg.Received,
		"failures": agg.Sent - agg.Received, "cert_days_min": agg.CertDaysMin,
	})
}

// latestDelayQuantiles 读取服务在 [from, now] 内最新一个样本充足
// （滑动窗口样本数 ≥ sentinel.DelayMinSamples）的分钟桶分位数快照；
// 无满足条件的桶时返回全 nil（API 输出 null）。
func (s *Server) latestDelayQuantiles(serviceID int64, from int64) (p50, p95, p99, stddev, jitter *int) {
	var row model.ServiceHistory
	if err := s.DB.Model(&model.ServiceHistory{}).
		Where("service_id = ? AND delay_samples >= ? AND ts >= ?", serviceID, sentinel.DelayMinSamples, from).
		Order("ts DESC, id DESC").First(&row).Error; err != nil {
		return nil, nil, nil, nil, nil
	}
	return &row.DelayP50, &row.DelayP95, &row.DelayP99, &row.DelayStdDevMs, &row.DelayJitterMs
}

// normalizeRequestHeaders 校验并规范化请求头 JSON（[{"key","value"}]）。
// 空串/空数组返回 ""；key 为空、或 JSON 非法时返回错误。
func normalizeRequestHeaders(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return "", nil
	}
	var headers []protocol.KeyValue
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return "", fmt.Errorf("request_headers must be a JSON array of {\"key\",\"value\"} objects")
	}
	out := make([]protocol.KeyValue, 0, len(headers))
	for _, h := range headers {
		if strings.TrimSpace(h.Key) == "" {
			return "", fmt.Errorf("request_headers contains an empty header key")
		}
		out = append(out, protocol.KeyValue{Key: strings.TrimSpace(h.Key), Value: h.Value})
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("invalid request_headers")
	}
	return string(b), nil
}

// normalizeExpectedStatuses 校验并规范化期望状态码列表（逗号分隔）：
// 空串/纯空白返回 ""（区间判定）；合法则返回去重保序的规范形式（如 "200,301,404"）；
// 非法（空项/非数字/超出 100-599）返回错误。
func normalizeExpectedStatuses(raw string) (string, error) {
	statuses, err := protocol.ParseStatuses(raw)
	if err != nil {
		return "", fmt.Errorf("invalid expected_statuses: %v", err)
	}
	return protocol.FormatStatuses(statuses), nil
}

// validateHTTPBody 校验方法与请求体的搭配：仅 POST/PUT/PATCH 允许携带 body。
func validateHTTPBody(method, body string) error {
	method = strings.ToUpper(strings.TrimSpace(method))
	if body == "" {
		return nil
	}
	if method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch {
		return fmt.Errorf("request_body only allowed for POST/PUT/PATCH methods")
	}
	return nil
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
