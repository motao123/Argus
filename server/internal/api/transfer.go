package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// FlushTransfers 消费流量差值队列并落库（由 main 定期调用，每小时一次）。
func (s *Server) FlushTransfers() {
	for _, d := range s.Store.TakeTransferQueue() {
		var existing model.Transfer
		err := s.DB.Where("server_id = ? AND ts = ?", d.ServerID, d.Ts).First(&existing).Error
		if err == nil {
			s.DB.Model(&existing).Updates(map[string]any{
				"in":  existing.In + d.In,
				"out": existing.Out + d.Out,
			})
		} else {
			s.DB.Create(&model.Transfer{ServerID: d.ServerID, Ts: d.Ts, In: d.In, Out: d.Out})
		}
	}
}

// serverTransfer 查询服务器周期流量。
// period: day（24 点）/ month（30 点）/ year（12 点）。
func (s *Server) serverTransfer(c *gin.Context) {
	id := mustID(c)
	period := c.DefaultQuery("period", "day")
	now := time.Now()

	var step, points int64
	switch period {
	case "month":
		step, points = 24*3600, 30
	case "year":
		step, points = 30*24*3600, 12
	default:
		period = "day"
		step, points = 3600, 24
	}

	from := now.Add(-time.Duration(points) * time.Duration(step) * time.Second).Unix()
	var rows []model.Transfer
	if err := s.DB.Where("server_id = ? AND ts >= ?", id, from).Order("ts").Find(&rows).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 聚合到步长
	type agg struct{ in, out uint64 }
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
		a.in += r.In
		a.out += r.Out
	}
	out := make([]gin.H, 0, len(order))
	for _, bts := range order {
		a := buckets[bts]
		out = append(out, gin.H{"ts": bts, "in": a.in, "out": a.out})
	}
	ok(c, gin.H{"period": period, "points": out})
}
