package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// serverTransfer 查询服务器周期流量（日历桶：24h 小时桶 / 30d 自然日 / 12m 自然月）。
func (s *Server) serverTransfer(c *gin.Context) {
	id := mustID(c)
	if _, ok := s.authorizePublicServer(c, id); !ok {
		fail(c, http.StatusNotFound, "server not found")
		return
	}
	period := c.DefaultQuery("period", "day")
	now := time.Now()

	// 按自然日历对齐起点与步长
	var step int64
	var points int64
	switch period {
	case "month":
		step, points = 30*24*3600, 12
		// 起点对齐到自然月：12 个月
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -(int(points) - 1), 0)
		from := first.Unix()
		rows := s.queryTransfers(id, from)
		out := make([]gin.H, 0, points)
		for i := int64(0); i < points; i++ {
			bStart := first.AddDate(0, int(i), 0)
			bEnd := bStart.AddDate(0, 1, 0)
			in, outB := sumRange(rows, bStart.Unix(), bEnd.Unix())
			out = append(out, gin.H{"ts": bStart.Unix(), "in": in, "out": outB})
		}
		ok(c, gin.H{"period": "month", "points": out})
		return
	case "year":
		step, points = 365*24*3600, 1
		// 自然年
		first := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		rows := s.queryTransfers(id, first.Unix())
		in, outB := sumRange(rows, first.Unix(), first.AddDate(1, 0, 0).Unix())
		ok(c, gin.H{"period": "year", "points": []gin.H{{"ts": first.Unix(), "in": in, "out": outB}}})
		return
	default:
		period = "day"
		step, points = 3600, 24
	}

	// 24 小时：对齐到整点
	start := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location()).Add(-time.Duration(points-1) * time.Hour)
	rows := s.queryTransfers(id, start.Unix())
	out := make([]gin.H, 0, points)
	for i := int64(0); i < points; i++ {
		bStart := start.Add(time.Duration(i) * time.Hour)
		bEnd := bStart.Add(time.Hour)
		in, outB := sumRange(rows, bStart.Unix(), bEnd.Unix())
		out = append(out, gin.H{"ts": bStart.Unix(), "in": in, "out": outB})
	}
	ok(c, gin.H{"period": "day", "points": out})
	_ = step
}

// queryTransfers 查询指定服务器 from 之后的小时桶。
func (s *Server) queryTransfers(serverID, from int64) []model.Transfer {
	var rows []model.Transfer
	s.DB.Where("server_id = ? AND ts >= ?", serverID, from).Order("ts").Find(&rows)
	return rows
}

// sumRange 汇总 [start,end) 小时内桶的流量。
func sumRange(rows []model.Transfer, start, end int64) (uint64, uint64) {
	var in, out uint64
	for _, r := range rows {
		if r.Ts >= start && r.Ts < end {
			in += r.In
			out += r.Out
		}
	}
	return in, out
}
