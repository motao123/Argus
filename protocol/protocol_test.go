package protocol

import (
	"encoding/json"
	"testing"
)

func TestReportBackwardCompatibility(t *testing.T) {
	var old ReportParams
	if err := json.Unmarshal([]byte(`{"cpu":12.5,"tcp_count":7,"gpu_util":0,"ts":123}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.CPU != 12.5 || old.TCPCount != 7 || old.Timestamp != 123 {
		t.Fatalf("legacy fields changed: %+v", old)
	}
	if old.GPU.Available || old.DiskIOAvailability.Available {
		t.Fatalf("missing additive fields must remain unavailable: %+v", old)
	}

	next := ReportParams{TCPEstablished: 3, GPU: GPUReport{Availability: Availability{Available: true}, Devices: []GPUDevice{{Index: 0, Name: "GPU", Util: 0}}}}
	data, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	if _, ok := generic["tcp_established"]; !ok {
		t.Fatal("new socket metric missing")
	}
	if _, ok := generic["gpu"]; !ok {
		t.Fatal("new GPU structure missing")
	}
}

func TestValidCapability(t *testing.T) {
	for _, name := range []string{CapabilityMetrics, CapabilityProbe, CapabilityCommand, CapabilityTerminal, CapabilityFiles, CapabilityUpgrade, CapabilityNAT} {
		if !ValidCapability(name) {
			t.Errorf("ValidCapability(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "metrics ", "Metrics", "shell", "bogus"} {
		if ValidCapability(name) {
			t.Errorf("ValidCapability(%q) = true, want false", name)
		}
	}
	if got := len(CapabilityNames()); got != 7 {
		t.Errorf("CapabilityNames() = %d names, want 7", got)
	}
}

func TestParseCapabilities(t *testing.T) {
	// 空 / null → nil（不修改，兼容旧客户端）
	for _, raw := range []json.RawMessage{nil, json.RawMessage(""), json.RawMessage("null")} {
		caps, err := ParseCapabilities(raw)
		if err != nil {
			t.Fatalf("ParseCapabilities(%q) error: %v", raw, err)
		}
		if caps != nil {
			t.Fatalf("ParseCapabilities(%q) = %+v, want nil", raw, caps)
		}
	}

	// 合法输入 → 规范化：所有字段显式设置，未提供的按 false
	caps, err := ParseCapabilities(json.RawMessage(`{"metrics":true,"nat":false}`))
	if err != nil {
		t.Fatalf("ParseCapabilities valid: %v", err)
	}
	if !caps.Metrics || caps.Probe || caps.Command || caps.Terminal || caps.Files || caps.Upgrade || caps.NAT {
		t.Fatalf("normalized caps = %+v, want metrics=true, rest false", caps)
	}

	// 全开
	caps, err = ParseCapabilities(json.RawMessage(`{"metrics":true,"probe":true,"command":true,"terminal":true,"files":true,"upgrade":true,"nat":true}`))
	if err != nil {
		t.Fatalf("ParseCapabilities all: %v", err)
	}
	if !caps.Metrics || !caps.Probe || !caps.Command || !caps.Terminal || !caps.Files || !caps.Upgrade || !caps.NAT {
		t.Fatalf("all-on caps = %+v", caps)
	}

	// 未知名能力 → 报错
	if _, err := ParseCapabilities(json.RawMessage(`{"bogus":true}`)); err == nil {
		t.Fatal("ParseCapabilities unknown name: want error, got nil")
	}

	// 非法 JSON / 非对象 → 报错
	if _, err := ParseCapabilities(json.RawMessage(`{`)); err == nil {
		t.Fatal("ParseCapabilities bad json: want error, got nil")
	}
	if _, err := ParseCapabilities(json.RawMessage(`["metrics"]`)); err == nil {
		t.Fatal("ParseCapabilities list: want error, got nil")
	}
}
