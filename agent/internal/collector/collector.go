// Package collector 采集本机系统状态（CPU/内存/磁盘/网络/负载）。
package collector

import (
	"context"
	stdnet "net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

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

	lastNetIn                          uint64
	lastNetOut                         uint64
	lastNetTs                          time.Time
	lastDiskRead, lastDiskWrite        uint64
	lastDiskReads, lastDiskWrites      uint64
	lastDiskTs                         time.Time
	interfaceInclude, interfaceExclude []string
	mountInclude, mountExclude         []string
}

// Options bounds aggregate collection to selected interface names and mount paths.
type Options struct {
	InterfaceInclude []string
	InterfaceExclude []string
	MountInclude     []string
	MountExclude     []string
}

func New(agentVersion string, opts ...Options) *Collector {
	c := &Collector{agentVersion: agentVersion, lastNetTs: time.Now(), lastDiskTs: time.Now()}
	if len(opts) > 0 {
		c.interfaceInclude = opts[0].InterfaceInclude
		c.interfaceExclude = opts[0].InterfaceExclude
		c.mountInclude = opts[0].MountInclude
		c.mountExclude = opts[0].MountExclude
	}
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

// LocalIPs collects preferred non-loopback IPv4 and IPv6 addresses.
func LocalIPs() (ipv4, ipv6 string) {
	ifaces, err := stdnet.Interfaces()
	if err != nil {
		return "", ""
	}
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
			if !ok || ipnet.IP.IsLoopback() || ipnet.IP.IsLinkLocalUnicast() {
				continue
			}
			ip := ipnet.IP
			if ip.To4() != nil {
				if ipv4 == "" || (!ip.IsPrivate() && stdnet.ParseIP(ipv4).IsPrivate()) {
					ipv4 = ip.String()
				}
			} else if ipv6 == "" || (!ip.IsPrivate() && stdnet.ParseIP(ipv6).IsPrivate()) {
				ipv6 = ip.String()
			}
		}
	}
	return ipv4, ipv6
}

func LocalIP() string { ip, _ := LocalIPs(); return ip }

// HostInfo 返回静态主机信息（用于上报）。
func (c *Collector) HostInfo() protocol.HostInfo {
	ipv4, ipv6 := LocalIPs()
	kernel := ""
	if hi, err := host.Info(); err == nil {
		kernel = hi.KernelVersion
	}
	return protocol.HostInfo{
		Hostname: c.hostname, Platform: c.platform, PlatformVersion: c.platformVer,
		OS: runtime.GOOS, Arch: runtime.GOARCH, KernelVersion: kernel,
		CPUModel: c.cpuModel, CPUCores: c.cpuCores, MemTotal: c.memTotal,
		AgentVersion: c.agentVersion, IP: ipv4, IPv4: ipv4, IPv6: ipv6,
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
	// 磁盘使用量按挂载点过滤并聚合，默认保持旧行为只取根分区。
	if used, total, ok := c.diskUsage(); ok {
		r.DiskUsed, r.DiskTotal = used, total
	}
	// 网络速率：按网卡过滤后累计值差分
	if nio, err := gnet.IOCounters(true); err == nil {
		var in, out uint64
		for _, n := range nio {
			if c.match(n.Name, c.interfaceInclude, c.interfaceExclude) {
				in += n.BytesRecv
				out += n.BytesSent
			}
		}
		dt := now.Sub(c.lastNetTs).Seconds()
		if dt > 0 && c.lastNetIn <= in && c.lastNetOut <= out {
			r.NetInSpeed = round1(float64(in-c.lastNetIn) / dt)
			r.NetOutSpeed = round1(float64(out-c.lastNetOut) / dt)
		}
		r.NetInTransfer, r.NetOutTransfer, c.lastNetIn, c.lastNetOut = in, out, in, out
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
	if procs, err := process.Pids(); err == nil {
		r.ProcessCount = len(procs)
		r.ProcessAvailability.Available = true
	} else {
		r.ProcessAvailability.Reason = err.Error()
	}
	if tcp, err := gnet.Connections("tcp"); err == nil {
		r.TCPCount = len(tcp)
		r.SocketAvailability.Available = true
		for _, conn := range tcp {
			switch strings.ToUpper(conn.Status) {
			case "ESTABLISHED":
				r.TCPEstablished++
			case "LISTEN":
				r.TCPListen++
			}
		}
	} else {
		r.SocketAvailability.Reason = err.Error()
	}
	if udp, err := gnet.Connections("udp"); err == nil {
		r.UDPCount = len(udp)
	} else if r.SocketAvailability.Reason == "" {
		r.SocketAvailability.Available = false
		r.SocketAvailability.Reason = err.Error()
	}
	c.collectDiskIO(now, r)
	r.Temperature = CPUTemperature()
	r.TemperatureAvailability.Available = r.Temperature != 0
	if !r.TemperatureAvailability.Available {
		r.TemperatureAvailability.Reason = "temperature sensor unavailable"
	}
	r.GPU = GPUInfo()
	for _, gpu := range r.GPU.Devices {
		r.GPUUtil += gpu.Util
		r.GPUMemUsed += gpu.MemUsed
		r.GPUMemTotal += gpu.MemTotal
	}
	if len(r.GPU.Devices) > 0 {
		r.GPUUtil = round1(r.GPUUtil / float64(len(r.GPU.Devices)))
	}
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

func (c *Collector) match(value string, include, exclude []string) bool {
	matched := len(include) == 0
	for _, p := range include {
		if ok, _ := filepath.Match(p, value); ok {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	for _, p := range exclude {
		if ok, _ := filepath.Match(p, value); ok {
			return false
		}
	}
	return true
}

func (c *Collector) diskUsage() (uint64, uint64, bool) {
	if len(c.mountInclude) == 0 && len(c.mountExclude) == 0 {
		du, err := diskUsage()
		if err != nil {
			return 0, 0, false
		}
		return du.Used, du.Total, true
	}
	parts, err := disk.Partitions(false)
	if err != nil {
		return 0, 0, false
	}
	var used, total uint64
	for _, p := range parts {
		if c.match(p.Mountpoint, c.mountInclude, c.mountExclude) {
			if du, err := disk.Usage(p.Mountpoint); err == nil {
				used += du.Used
				total += du.Total
			}
		}
	}
	return used, total, total > 0
}

func (c *Collector) collectDiskIO(now time.Time, r *protocol.ReportParams) {
	stats, err := disk.IOCounters()
	if err != nil {
		r.DiskIOAvailability.Reason = err.Error()
		return
	}
	var read, write, reads, writes uint64
	for _, s := range stats {
		read += s.ReadBytes
		write += s.WriteBytes
		reads += s.ReadCount
		writes += s.WriteCount
	}
	dt := now.Sub(c.lastDiskTs).Seconds()
	if dt > 0 && read >= c.lastDiskRead && write >= c.lastDiskWrite && reads >= c.lastDiskReads && writes >= c.lastDiskWrites {
		r.DiskReadSpeed = round1(float64(read-c.lastDiskRead) / dt)
		r.DiskWriteSpeed = round1(float64(write-c.lastDiskWrite) / dt)
		r.DiskReadIOPS = round1(float64(reads-c.lastDiskReads) / dt)
		r.DiskWriteIOPS = round1(float64(writes-c.lastDiskWrites) / dt)
	}
	c.lastDiskRead, c.lastDiskWrite, c.lastDiskReads, c.lastDiskWrites, c.lastDiskTs = read, write, reads, writes, now
	r.DiskIOAvailability.Available = true
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
