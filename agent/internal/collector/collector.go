// Package collector 采集本机系统状态（CPU/内存/磁盘/网络/负载）。
package collector

import (
	"context"
	stdnet "net"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"

	"github.com/motao123/Argus/protocol"
)

// Collector 周期性采集系统状态。
type Collector struct {
	agentVersion string
	hostname     string
	platform     string
	platformVer  string
	cpuModel     string
	cpuCores     int
	memTotal     uint64

	lastNetIn  uint64
	lastNetOut uint64
	lastNetTs  time.Time
}

func New(agentVersion string) *Collector {
	c := &Collector{agentVersion: agentVersion, lastNetTs: time.Now()}
	c.initStatic()
	return c
}

func (c *Collector) initStatic() {
	hi, err := host.Info()
	if err == nil {
		c.hostname = hi.Hostname
		c.platform = hi.Platform
		c.platformVer = hi.PlatformVersion
	}
	info, err := cpu.Info()
	if err == nil && len(info) > 0 {
		c.cpuModel = info[0].ModelName
	}
	if n, err := cpu.Counts(true); err == nil {
		c.cpuCores = n
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		c.memTotal = vm.Total
	}
	if hn, err := os.Hostname(); err == nil && c.hostname == "" {
		c.hostname = hn
	}
	if c.platform == "" {
		c.platform = runtime.GOOS
	}
}

// LocalIP 采集本机非回环 IPv4（优先公网地址）。
func LocalIP() string {
	ifaces, err := stdnet.Interfaces()
	if err != nil {
		return ""
	}
	best := ""
	for _, iface := range ifaces {
		if iface.Flags&stdnet.FlagUp == 0 || iface.Flags&stdnet.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*stdnet.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ip := ipnet.IP.String()
			if ipnet.IP.IsPrivate() || ipnet.IP.IsLoopback() {
				if best == "" {
					best = ip // 保底内网 IP
				}
				continue
			}
			return ip // 公网 IP 优先
		}
	}
	return best
}

// HostInfo 返回静态主机信息（用于上报）。
func (c *Collector) HostInfo() protocol.HostInfo {
	return protocol.HostInfo{
		Hostname:        c.hostname,
		Platform:        c.platform,
		PlatformVersion: c.platformVer,
		CPUModel:        c.cpuModel,
		CPUCores:        c.cpuCores,
		MemTotal:        c.memTotal,
		AgentVersion:    c.agentVersion,
		IP:              LocalIP(),
	}
}

// Collect 采集一次动态状态。
func (c *Collector) Collect() *protocol.ReportParams {
	now := time.Now()
	r := &protocol.ReportParams{
		MemTotal:  c.memTotal,
		Timestamp: now.Unix(),
	}

	if p, err := cpu.Percent(0, false); err == nil && len(p) > 0 {
		r.CPU = round1(p[0])
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		r.MemUsed = vm.Used
		r.MemTotal = vm.Total
	}
	if sw, err := mem.SwapMemory(); err == nil {
		r.SwapUsed = sw.Used
		r.SwapTotal = sw.Total
	}
	// 磁盘取根分区使用量
	if du, err := disk.Usage("/"); err == nil {
		r.DiskUsed = du.Used
		r.DiskTotal = du.Total
	}
	// 网络速率：累计值差分
	if nio, err := net.IOCounters(false); err == nil && len(nio) > 0 {
		cur := nio[0]
		dt := now.Sub(c.lastNetTs).Seconds()
		if dt > 0 && !c.lastNetTs.IsZero() {
			r.NetInSpeed = round1(float64(cur.BytesRecv-c.lastNetIn) / dt)
			r.NetOutSpeed = round1(float64(cur.BytesSent-c.lastNetOut) / dt)
		}
		r.NetInTransfer = cur.BytesRecv
		r.NetOutTransfer = cur.BytesSent
		c.lastNetIn = cur.BytesRecv
		c.lastNetOut = cur.BytesSent
	}
	c.lastNetTs = now

	if l, err := load.Avg(); err == nil {
		r.Load1 = round2(l.Load1)
		r.Load5 = round2(l.Load5)
		r.Load15 = round2(l.Load15)
	}
	if hi, err := host.Info(); err == nil {
		r.Uptime = hi.Uptime
	}
	if tcp, err := net.Connections("tcp"); err == nil {
		r.TCPCount = len(tcp)
	}
	r.Temperature = CPUTemperature()
	r.GPUUtil, r.GPUMemUsed, r.GPUMemTotal = GPUInfo()
	return r
}

// Run 以 interval 间隔循环采集并调用 fn。
func (c *Collector) Run(ctx context.Context, interval time.Duration, fn func(*protocol.ReportParams)) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			fn(c.Collect())
		}
	}
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
