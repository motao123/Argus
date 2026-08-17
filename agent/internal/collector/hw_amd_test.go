//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/motao123/Argus/protocol"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFakeBin(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// withPATH replaces PATH so GPUInfo resolves fake nvidia-smi/rocm-smi binaries.
// 不传 dirs 时 PATH 为空串，等价于两个工具都不存在。
func withPATH(t *testing.T, dirs ...string) {
	t.Helper()
	old := os.Getenv("PATH")
	os.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
	t.Cleanup(func() { os.Setenv("PATH", old) })
}

func withDRMRoot(t *testing.T, root string) {
	t.Helper()
	old := drmSysfsRoot
	drmSysfsRoot = root
	t.Cleanup(func() { drmSysfsRoot = old })
}

func TestParseROCMSMI(t *testing.T) {
	out := `{
  "card1": {
    "GPU use (%)": "15",
    "VRAM Total Memory (B)": "17163091968",
    "VRAM Total Used Memory (B)": "506298368",
    "Temperature (Sensor edge) (C)": "45.5",
    "Card series": "AMD Radeon RX 7800 XT"
  },
  "card0": {
    "GPU use (%)": "0",
    "VRAM Total Memory (B)": "8589934592",
    "VRAM Total Used Memory (B)": "0",
    "Temperature (Sensor edge) (C)": "38",
    "Card series": "AMD Radeon RX 6600"
  }
}`
	devs := parseROCMSMI([]byte(out))
	if len(devs) != 2 {
		t.Fatalf("parseROCMSMI devices = %d, want 2", len(devs))
	}
	if devs[0].Index != 0 || devs[1].Index != 1 {
		t.Fatalf("devices not sorted by index: %+v", devs)
	}
	if devs[0].Name != "AMD Radeon RX 6600" || devs[0].Util != 0 || devs[0].MemUsed != 0 ||
		devs[0].MemTotal != 8589934592 || devs[0].Temp != 38 {
		t.Fatalf("card0 fields wrong: %+v", devs[0])
	}
	if devs[1].Name != "AMD Radeon RX 7800 XT" || devs[1].Util != 15 ||
		devs[1].MemUsed != 506298368 || devs[1].MemTotal != 17163091968 || devs[1].Temp != 45.5 {
		t.Fatalf("card1 fields wrong: %+v", devs[1])
	}
}

func TestParseROCMSMINumericValues(t *testing.T) {
	out := `{"card0":{"GPU use (%)":7,"VRAM Total Memory (B)":1024,"VRAM Total Used Memory (B)":512,"Temperature (Sensor edge) (C)":40}}`
	devs := parseROCMSMI([]byte(out))
	if len(devs) != 1 {
		t.Fatalf("devices = %d, want 1", len(devs))
	}
	if devs[0].Util != 7 || devs[0].MemTotal != 1024 || devs[0].MemUsed != 512 || devs[0].Temp != 40 {
		t.Fatalf("numeric fields wrong: %+v", devs[0])
	}
}

func TestParseROCMSMIToleratesBanner(t *testing.T) {
	out := "ROCm System Management Interface\n" + `{"card0":{"GPU use (%)":"1","VRAM Total Memory (B)":"1024"}}`
	if devs := parseROCMSMI([]byte(out)); len(devs) != 1 || devs[0].Util != 1 {
		t.Fatalf("banner-prefixed output not parsed: %+v", devs)
	}
}

func TestParseROCMSMIInvalid(t *testing.T) {
	for _, out := range []string{"not json", "{}", `{"bogus":"x"}`} {
		if devs := parseROCMSMI([]byte(out)); len(devs) != 0 {
			t.Fatalf("parseROCMSMI(%q) = %+v, want empty", out, devs)
		}
	}
}

func TestParseROCMSMISkipsUnavailableUtil(t *testing.T) {
	out := `{"card0":{"GPU use (%)":"N/A","VRAM Total Memory (B)":"1024"},
	        "card1":{"GPU use (%)":"5","VRAM Total Memory (B)":"2048","VRAM Total Used Memory (B)":"512","Temperature (Sensor edge) (C)":"N/A"}}`
	devs := parseROCMSMI([]byte(out))
	if len(devs) != 1 || devs[0].Index != 1 {
		t.Fatalf("want only card1 with usable util, got %+v", devs)
	}
	if devs[0].Util != 5 || devs[0].MemUsed != 512 || devs[0].Temp != 0 {
		t.Fatalf("card1 fields wrong: %+v", devs[0])
	}
}

func TestMergeGPUDevices(t *testing.T) {
	nv := []protocol.GPUDevice{{Index: 1, Name: "NVIDIA A", Util: 10}, {Index: 0, Name: "NVIDIA B", Util: 20}}
	amd := []protocol.GPUDevice{{Index: 1, Name: "AMD A", Util: 30}, {Index: 2, Name: "AMD B", Util: 40}}
	got := mergeGPUDevices(nv, amd)
	if len(got) != 3 {
		t.Fatalf("merged length = %d, want 3", len(got))
	}
	if got[0].Index != 0 || got[0].Name != "NVIDIA B" ||
		got[1].Index != 1 || got[1].Name != "NVIDIA A" || // 冲突时先出现者（nvidia）优先
		got[2].Index != 2 || got[2].Name != "AMD B" {
		t.Fatalf("merge result wrong: %+v", got)
	}
}

func TestGPUInfoNVIDIAOnly(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "nvidia-smi", "#!/bin/sh\necho '0, NVIDIA GeForce RTX 4090, 42, 4096, 24576, 55'\n")
	withPATH(t, dir)
	withDRMRoot(t, t.TempDir())
	r := GPUInfo()
	if !r.Available {
		t.Fatalf("expected available, reason %q", r.Reason)
	}
	if len(r.Devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(r.Devices))
	}
	d := r.Devices[0]
	if d.Name != "NVIDIA GeForce RTX 4090" || d.Util != 42 ||
		d.MemUsed != 4096*1024*1024 || d.MemTotal != 24576*1024*1024 || d.Temp != 55 {
		t.Fatalf("nvidia device wrong: %+v", d)
	}
}

func TestGPUInfoAMDOnly(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "rocm-smi", "#!/bin/sh\necho '{\"card0\":{\"GPU use (%)\":\"12\",\"VRAM Total Memory (B)\":\"17163091968\",\"VRAM Total Used Memory (B)\":\"256\",\"Temperature (Sensor edge) (C)\":\"41\",\"Card series\":\"AMD Radeon RX 6700 XT\"}}'\n")
	withPATH(t, dir)
	withDRMRoot(t, t.TempDir())
	r := GPUInfo()
	if !r.Available {
		t.Fatalf("expected available, reason %q", r.Reason)
	}
	if len(r.Devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(r.Devices))
	}
	d := r.Devices[0]
	if d.Index != 0 || d.Name != "AMD Radeon RX 6700 XT" || d.Util != 12 ||
		d.MemUsed != 256 || d.MemTotal != 17163091968 || d.Temp != 41 {
		t.Fatalf("amd device wrong: %+v", d)
	}
}

func TestGPUInfoMixedMerge(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "nvidia-smi", "#!/bin/sh\necho '0, NVIDIA GeForce RTX 4090, 42, 4096, 24576, 55'\n")
	writeFakeBin(t, dir, "rocm-smi", "#!/bin/sh\necho '{\"card1\":{\"GPU use (%)\":\"3\",\"VRAM Total Memory (B)\":\"1024\",\"VRAM Total Used Memory (B)\":\"512\",\"Temperature (Sensor edge) (C)\":\"35\",\"Card series\":\"AMD Radeon RX 6700 XT\"},\"card0\":{\"GPU use (%)\":\"9\",\"VRAM Total Memory (B)\":\"2048\",\"VRAM Total Used Memory (B)\":\"1024\",\"Temperature (Sensor edge) (C)\":\"40\",\"Card series\":\"AMD Radeon RX 6600\"}}'\n")
	withPATH(t, dir)
	withDRMRoot(t, t.TempDir())
	r := GPUInfo()
	if !r.Available {
		t.Fatalf("expected available, reason %q", r.Reason)
	}
	if len(r.Devices) != 2 {
		t.Fatalf("devices = %d, want 2 (merged, deduped)", len(r.Devices))
	}
	if r.Devices[0].Index != 0 || r.Devices[0].Name != "NVIDIA GeForce RTX 4090" || r.Devices[0].Util != 42 {
		t.Fatalf("index 0 should win nvidia (first source): %+v", r.Devices[0])
	}
	if r.Devices[1].Index != 1 || r.Devices[1].Name != "AMD Radeon RX 6700 XT" || r.Devices[1].Util != 3 {
		t.Fatalf("index 1 should come from amd: %+v", r.Devices[1])
	}
}

func TestGPUInfoSysfsFallback(t *testing.T) {
	// 无 nvidia-smi / rocm-smi → 回退 /sys/class/drm
	withPATH(t)
	root := t.TempDir()
	// card0: amdgpu 完整数据
	writeFile(t, filepath.Join(root, "card0", "device", "uevent"), "DRIVER=amdgpu\n")
	writeFile(t, filepath.Join(root, "card0", "device", "gpu_busy_percent"), "23\n")
	writeFile(t, filepath.Join(root, "card0", "device", "mem_info_vram_total"), "17163091968\n")
	writeFile(t, filepath.Join(root, "card0", "device", "mem_info_vram_used"), "524288000\n")
	writeFile(t, filepath.Join(root, "card0", "device", "hwmon", "hwmon0", "temp1_input"), "45500\n")
	writeFile(t, filepath.Join(root, "card0", "device", "product_name"), "AMD Radeon RX 7800 XT\n")
	// card1: 非 amdgpu（i915）→ 忽略
	writeFile(t, filepath.Join(root, "card1", "device", "uevent"), "DRIVER=i915\n")
	writeFile(t, filepath.Join(root, "card1", "device", "gpu_busy_percent"), "50\n")
	// card1-DP-1: 连接器节点 → 忽略
	if err := os.MkdirAll(filepath.Join(root, "card1-DP-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	// card2: amdgpu 但缺 hwmon/显存/名称 → 仍上报（缺项为 0/通用名）
	writeFile(t, filepath.Join(root, "card2", "device", "uevent"), "DRIVER=amdgpu\n")
	writeFile(t, filepath.Join(root, "card2", "device", "gpu_busy_percent"), "7\n")
	withDRMRoot(t, root)

	r := GPUInfo()
	if !r.Available {
		t.Fatalf("sysfs fallback expected available, reason %q", r.Reason)
	}
	if len(r.Devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(r.Devices))
	}
	d0 := r.Devices[0]
	if d0.Index != 0 || d0.Name != "AMD Radeon RX 7800 XT" || d0.Util != 23 ||
		d0.MemUsed != 524288000 || d0.MemTotal != 17163091968 || d0.Temp != 45.5 {
		t.Fatalf("card0 wrong: %+v", d0)
	}
	d2 := r.Devices[1]
	if d2.Index != 2 || d2.Name != "AMD GPU (card2)" || d2.Util != 7 || d2.Temp != 0 {
		t.Fatalf("card2 wrong: %+v", d2)
	}
}

func TestGPUInfoSysfsSkipsUnavailableUtil(t *testing.T) {
	withPATH(t)
	root := t.TempDir()
	// card0 是 amdgpu 但无 gpu_busy_percent → 利用率不可用 → 整卡跳过，不伪造 0
	writeFile(t, filepath.Join(root, "card0", "device", "uevent"), "DRIVER=amdgpu\n")
	writeFile(t, filepath.Join(root, "card0", "device", "mem_info_vram_total"), "1024\n")
	withDRMRoot(t, root)

	r := GPUInfo()
	if r.Available {
		t.Fatal("expected unavailable when no card has usable utilization")
	}
	if !strings.Contains(r.Reason, "no amdgpu devices") {
		t.Fatalf("reason %q should mention missing amdgpu devices", r.Reason)
	}
}

func TestGPUInfoUnavailable(t *testing.T) {
	withPATH(t)
	withDRMRoot(t, t.TempDir())
	r := GPUInfo()
	if r.Available {
		t.Fatal("expected unavailable without nvidia-smi, rocm-smi or amdgpu sysfs data")
	}
	for _, want := range []string{"nvidia-smi not found", "rocm-smi not found", "no amdgpu devices"} {
		if !strings.Contains(r.Reason, want) {
			t.Fatalf("reason %q missing %q", r.Reason, want)
		}
	}
}
