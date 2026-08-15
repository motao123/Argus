package collector

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// GPUInfo 读取 GPU 利用率（nvidia-smi，不存在返回零值）。
func GPUInfo() (util float64, memUsed, memTotal uint64) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return 0, 0, 0
	}
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(fields) >= 3 {
		util, _ = strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
		memUsed, _ = strconv.ParseUint(strings.TrimSpace(fields[1]), 10, 64)
		memTotal, _ = strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
		memUsed *= 1024 * 1024
		memTotal *= 1024 * 1024
	}
	return util, memUsed, memTotal
}
