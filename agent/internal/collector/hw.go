//go:build linux

package collector

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/motao123/Argus/protocol"
)

// CPUTemperature 读取 CPU 温度（/sys/class/thermal 平均，摄氏度）。
func CPUTemperature() float64 {
	zones, err := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	if err != nil || len(zones) == 0 {
		return 0
	}
	var sum float64
	var n int
	for _, z := range zones {
		data, err := os.ReadFile(z)
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		sum += milli / 1000.0 // 毫摄氏度 → 摄氏度
		n++
	}
	if n == 0 {
		return 0
	}
	return round1(sum / float64(n))
}

// GPUInfo reads all NVIDIA devices without collecting high-cardinality process data.
func GPUInfo() protocol.GPUReport {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return protocol.GPUReport{Availability: protocol.Availability{Reason: "nvidia-smi not found"}}
	}
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return protocol.GPUReport{Availability: protocol.Availability{Reason: "nvidia-smi query failed"}}
	}
	report := protocol.GPUReport{Availability: protocol.Availability{Available: true}}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 5 {
			continue
		}
		index, errIndex := strconv.Atoi(strings.TrimSpace(fields[0]))
		util, errUtil := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		used, errUsed := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		total, errTotal := strconv.ParseUint(strings.TrimSpace(fields[4]), 10, 64)
		if errIndex != nil || errUtil != nil || errUsed != nil || errTotal != nil {
			continue
		}
		report.Devices = append(report.Devices, protocol.GPUDevice{
			Index: index, Name: strings.TrimSpace(fields[1]), Util: util,
			MemUsed: used * 1024 * 1024, MemTotal: total * 1024 * 1024,
		})
	}
	if len(report.Devices) == 0 {
		report.Available = false
		report.Reason = "no NVIDIA GPU data"
	}
	return report
}
