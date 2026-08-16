// Package metric rollup 阶梯聚合与保留清理（借鉴 komari pkg/metric rollup 简化版）。
package metric

import (
	"time"

	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

// Granularity 常量。
const (
	GranMinute = 60
	Gran5m     = 300
	GranHour   = 3600
)

// Rollup 聚合器：5 分钟从分钟表聚合，1 小时从 5 分钟表聚合并清理过期。
type Rollup struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Rollup { return &Rollup{db: db} }

type agg struct {
	count                                                      int
	cpu, netIn, netOut, load, temp, gpu                        float64
	process, tcpEstablished, tcpListen, udp                    float64
	diskReadSpeed, diskWriteSpeed, diskReadIOPS, diskWriteIOPS float64
	memUsed, memTotal, diskUsed, diskTotal                     uint64
}

// aggregate 把 srcGran 数据聚合成 dstGran。
// 只聚合已完成的桶（ts <= now-dstGran），且幂等覆盖，避免不完整桶被固化或重复累计。
func (r *Rollup) aggregate(srcGran, dstGran int, cutoff time.Time) {
	rows := []model.Metric{}
	r.db.Where("granularity = ? AND ts >= ?", srcGran, cutoff.Unix()).Find(&rows)

	byKey := map[int64]map[int64]*agg{}
	for _, row := range rows {
		bucket := row.TS / int64(dstGran) * int64(dstGran)
		m, ok := byKey[row.ServerID]
		if !ok {
			m = map[int64]*agg{}
			byKey[row.ServerID] = m
		}
		a, ok := m[bucket]
		if !ok {
			a = &agg{}
			m[bucket] = a
		}
		a.count++
		a.cpu += row.CPU
		a.memUsed = row.MemUsed
		a.memTotal = row.MemTotal
		a.diskUsed = row.DiskUsed
		a.diskTotal = row.DiskTotal
		a.netIn += row.NetInSpeed
		a.netOut += row.NetOutSpeed
		a.load += row.Load1
		a.temp += row.Temperature
		a.gpu += row.GPUUtil
		a.process += row.ProcessCount
		a.tcpEstablished += row.TCPEstablished
		a.tcpListen += row.TCPListen
		a.udp += row.UDPCount
		a.diskReadSpeed += row.DiskReadSpeed
		a.diskWriteSpeed += row.DiskWriteSpeed
		a.diskReadIOPS += row.DiskReadIOPS
		a.diskWriteIOPS += row.DiskWriteIOPS
	}

	now := time.Now()
	// 只写已完成的桶：当前进行中的桶跳过，等下一次运行再聚合
	completeBefore := now.Unix() - int64(dstGran)
	for serverID, buckets := range byKey {
		for ts, a := range buckets {
			if ts >= completeBefore {
				continue // 未完成桶：不固化，等待补齐
			}
			n := float64(a.count)
			if n == 0 {
				n = 1
			}
			row := model.Metric{
				ServerID:     serverID,
				TS:           ts,
				Granularity:  dstGran,
				CPU:          a.cpu / n,
				MemUsed:      a.memUsed,
				MemTotal:     a.memTotal,
				DiskUsed:     a.diskUsed,
				DiskTotal:    a.diskTotal,
				NetInSpeed:   a.netIn / n,
				NetOutSpeed:  a.netOut / n,
				Load1:        a.load / n,
				Temperature:  a.temp / n,
				GPUUtil:      a.gpu / n,
				ProcessCount: a.process / n, TCPEstablished: a.tcpEstablished / n, TCPListen: a.tcpListen / n, UDPCount: a.udp / n,
				DiskReadSpeed: a.diskReadSpeed / n, DiskWriteSpeed: a.diskWriteSpeed / n, DiskReadIOPS: a.diskReadIOPS / n, DiskWriteIOPS: a.diskWriteIOPS / n,
				CreatedAt: now,
			}
			// 幂等覆盖：已存在则更新（原始分钟数据不变，重算结果一致）
			res := r.db.Model(&model.Metric{}).
				Where("server_id = ? AND ts = ? AND granularity = ?", serverID, ts, dstGran).
				Updates(map[string]any{
					"cpu": row.CPU, "mem_used": row.MemUsed, "mem_total": row.MemTotal,
					"disk_used": row.DiskUsed, "disk_total": row.DiskTotal,
					"net_in_speed": row.NetInSpeed, "net_out_speed": row.NetOutSpeed,
					"load1": row.Load1, "temperature": row.Temperature, "gpu_util": row.GPUUtil,
					"process_count": row.ProcessCount, "tcp_established": row.TCPEstablished, "tcp_listen": row.TCPListen, "udp_count": row.UDPCount,
					"disk_read_speed": row.DiskReadSpeed, "disk_write_speed": row.DiskWriteSpeed, "disk_read_iops": row.DiskReadIOPS, "disk_write_iops": row.DiskWriteIOPS,
				})
			if res.RowsAffected == 0 {
				r.db.Create(&row)
			}
		}
	}
}

// Aggregate5m 将分钟数据聚合到 5 分钟粒度（最近 24h 数据）。
func (r *Rollup) Aggregate5m() {
	r.aggregate(GranMinute, Gran5m, time.Now().Add(-24*time.Hour))
}

// AggregateHour 将 5 分钟数据聚合到小时粒度（最近 7 天）。
func (r *Rollup) AggregateHour() {
	r.aggregate(Gran5m, GranHour, time.Now().Add(-7*24*time.Hour))
}

// Cleanup was moved to retention.Cleaner so all retained data shares one policy.
