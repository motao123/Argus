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
