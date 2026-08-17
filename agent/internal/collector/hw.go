//go:build linux

package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// rocmSMIArgs 单次调用采集 AMD GPU 名称/利用率/显存/温度（--json 输出）。
var rocmSMIArgs = []string{"--showproductname", "--showuse", "--showmeminfo", "vram", "--showtemp", "--json"}

// drmSysfsRoot 为 /sys/class/drm 路径，测试可覆盖。
var drmSysfsRoot = "/sys/class/drm"

// GPUInfo collects NVIDIA (nvidia-smi) and AMD (rocm-smi, falling back to sysfs)
// devices and merges them into one index-sorted, de-duplicated list.
// 任一来源可用即标记 Available；全部无法采集时才 unavailable（不伪造 0）。
func GPUInfo() protocol.GPUReport {
	var reasons []string
	nvidia, err := queryNvidiaSMI()
	if err != nil {
		reasons = append(reasons, err.Error())
	}
	amd, err := queryAMDGPU()
	if err != nil {
		reasons = append(reasons, err.Error())
	}
	devices := mergeGPUDevices(nvidia, amd)
	if len(devices) == 0 {
		return protocol.GPUReport{Availability: protocol.Availability{Reason: strings.Join(reasons, "; ")}}
	}
	return protocol.GPUReport{Availability: protocol.Availability{Available: true}, Devices: devices}
}

// queryNvidiaSMI reads all NVIDIA devices without collecting high-cardinality process data.
func queryNvidiaSMI() ([]protocol.GPUDevice, error) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil, errors.New("nvidia-smi not found")
	}
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, errors.New("nvidia-smi query failed")
	}
	var devices []protocol.GPUDevice
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 6 {
			continue
		}
		index, errIndex := strconv.Atoi(strings.TrimSpace(fields[0]))
		util, errUtil := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		used, errUsed := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		total, errTotal := strconv.ParseUint(strings.TrimSpace(fields[4]), 10, 64)
		if errIndex != nil || errUtil != nil || errUsed != nil || errTotal != nil {
			continue
		}
		temp, _ := strconv.ParseFloat(strings.TrimSpace(fields[5]), 64)
		devices = append(devices, protocol.GPUDevice{
			Index: index, Name: strings.TrimSpace(fields[1]), Util: util,
			MemUsed: used * 1024 * 1024, MemTotal: total * 1024 * 1024, Temp: temp,
		})
	}
	if len(devices) == 0 {
		return nil, errors.New("no NVIDIA GPU data")
	}
	return devices, nil
}

// queryAMDGPU prefers rocm-smi and falls back to reading amdgpu cards under /sys/class/drm.
func queryAMDGPU() ([]protocol.GPUDevice, error) {
	var rocmErr error
	if _, err := exec.LookPath("rocm-smi"); err != nil {
		rocmErr = errors.New("rocm-smi not found")
	} else if out, err := exec.Command("rocm-smi", rocmSMIArgs...).Output(); err != nil {
		rocmErr = errors.New("rocm-smi query failed")
	} else if devices := parseROCMSMI(out); len(devices) > 0 {
		return devices, nil
	} else {
		rocmErr = errors.New("rocm-smi returned no AMD GPU data")
	}
	if devices := amdgpuSysfsDevices(); len(devices) > 0 {
		return devices, nil
	}
	return nil, fmt.Errorf("%s; no amdgpu devices in %s", rocmErr, drmSysfsRoot)
}

// parseROCMSMI parses `rocm-smi --json` output into per-card devices.
// 值为字符串（部分版本为数字）；利用率为关键指标，缺失/不可解析的卡直接跳过。
func parseROCMSMI(out []byte) []protocol.GPUDevice {
	if idx := bytes.IndexByte(out, '{'); idx > 0 {
		out = out[idx:] // 容忍开头附带版本横幅等非 JSON 行
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}
	var devices []protocol.GPUDevice
	for key, card := range raw {
		index, ok := rocmCardIndex(key)
		if !ok {
			continue
		}
		util, ok := rocmNumber(card, "GPU use (%)")
		if !ok {
			continue // 利用率不可用 → 跳过该卡而非伪造 0
		}
		devices = append(devices, protocol.GPUDevice{
			Index:    index,
			Name:     rocmName(card, index),
			Util:     util,
			MemUsed:  rocmUint(card, "VRAM Total Used Memory (B)"),
			MemTotal: rocmUint(card, "VRAM Total Memory (B)"),
			Temp:     rocmTemp(card),
		})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Index < devices[j].Index })
	return devices
}

// amdgpuSysfsDevices scans /sys/class/drm for amdgpu primary cards (rocm-smi 缺失时的回退)。
func amdgpuSysfsDevices() []protocol.GPUDevice {
	matches, err := filepath.Glob(filepath.Join(drmSysfsRoot, "card*"))
	if err != nil {
		return nil
	}
	var devices []protocol.GPUDevice
	for _, m := range matches {
		index, ok := cardIndex(filepath.Base(m))
		if !ok {
			continue // 排除连接器节点（card0-DP-1 等）
		}
		if d, ok := readAMDGPUCard(m, index); ok {
			devices = append(devices, d)
		}
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Index < devices[j].Index })
	return devices
}

// readAMDGPUCard reads one amdgpu card from sysfs; ok=false 表示非 amdgpu 或关键指标不可用。
func readAMDGPUCard(cardPath string, index int) (protocol.GPUDevice, bool) {
	devDir := filepath.Join(cardPath, "device")
	uevent, err := os.ReadFile(filepath.Join(devDir, "uevent"))
	if err != nil || !strings.Contains(string(uevent), "DRIVER=amdgpu") {
		return protocol.GPUDevice{}, false
	}
	util, ok := readIntFile(filepath.Join(devDir, "gpu_busy_percent"))
	if !ok {
		return protocol.GPUDevice{}, false // 利用率不可用 → 跳过该卡而非伪造 0
	}
	return protocol.GPUDevice{
		Index:    index,
		Name:     amdGPUName(devDir, index),
		Util:     util,
		MemUsed:  readUintFile(filepath.Join(devDir, "mem_info_vram_used")),
		MemTotal: readUintFile(filepath.Join(devDir, "mem_info_vram_total")),
		Temp:     amdGPUTemp(devDir),
	}, true
}

// mergeGPUDevices 合并多个来源并按 Index 升序去重（先出现者优先）。
func mergeGPUDevices(lists ...[]protocol.GPUDevice) []protocol.GPUDevice {
	seen := make(map[int]bool)
	var out []protocol.GPUDevice
	for _, list := range lists {
		for _, d := range list {
			if !seen[d.Index] {
				seen[d.Index] = true
				out = append(out, d)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// rocmCardIndex extracts the numeric index from a "cardN" JSON key.
func rocmCardIndex(key string) (int, bool) {
	return cardIndex(key)
}

// cardIndex parses "card<N>" (digits only, excluding connector nodes like card0-DP-1).
func cardIndex(base string) (int, bool) {
	rest, ok := strings.CutPrefix(base, "card")
	if !ok || rest == "" {
		return 0, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

// rocmNumber reads a JSON value emitted either as string ("0") or number (0).
func rocmNumber(card map[string]json.RawMessage, key string) (float64, bool) {
	raw, ok := card[key]
	if !ok {
		return 0, false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return v, err == nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, true
	}
	return 0, false
}

func rocmUint(card map[string]json.RawMessage, key string) uint64 {
	v, ok := rocmNumber(card, key)
	if !ok || v < 0 {
		return 0
	}
	return uint64(v)
}

func rocmName(card map[string]json.RawMessage, index int) string {
	for _, key := range []string{"Card series", "Card model"} {
		if raw, ok := card[key]; ok {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return fmt.Sprintf("AMD GPU (card%d)", index)
}

// rocmTemp prefers the edge sensor, then junction/memory, then any temperature key.
func rocmTemp(card map[string]json.RawMessage) float64 {
	for _, key := range []string{"Temperature (Sensor edge) (C)", "Temperature (Sensor junction) (C)", "Temperature (Sensor memory) (C)"} {
		if v, ok := rocmNumber(card, key); ok {
			return v
		}
	}
	for key, raw := range card {
		if strings.HasPrefix(key, "Temperature") {
			if v, ok := rocmNumber(map[string]json.RawMessage{key: raw}, key); ok {
				return v
			}
		}
	}
	return 0
}

func amdGPUName(devDir string, index int) string {
	if data, err := os.ReadFile(filepath.Join(devDir, "product_name")); err == nil {
		if name := strings.TrimSpace(string(data)); name != "" {
			return name
		}
	}
	return fmt.Sprintf("AMD GPU (card%d)", index)
}

// amdGPUTemp reads the edge temperature from hwmon (毫摄氏度 → 摄氏度)。
func amdGPUTemp(devDir string) float64 {
	inputs, err := filepath.Glob(filepath.Join(devDir, "hwmon", "hwmon*", "temp1_input"))
	if err != nil {
		return 0
	}
	for _, in := range inputs {
		if milli, ok := readIntFile(in); ok {
			return milli / 1000.0
		}
	}
	return 0
}

func readIntFile(path string) (float64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false
	}
	return float64(n), true
}

func readUintFile(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
