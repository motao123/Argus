package store

import (
	"testing"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
)

// TestSetReportStoresLatency 验证上报中的 LatencyMs 存入 State 并随快照输出；
// 旧 Agent（不携带延迟）上报后回退为 0（无测量）。
func TestSetReportStoresLatency(t *testing.T) {
	h := NewHub()
	h.Upsert(&model.Server{ID: 1, Name: "t"})

	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{LatencyMs: 12, Timestamp: 100})
	if st := h.Get(1); st.LatencyMs != 12 {
		t.Fatalf("LatencyMs = %d, want 12", st.LatencyMs)
	}
	if snap := h.Snapshot(); snap[1].LatencyMs != 12 {
		t.Fatalf("snapshot LatencyMs = %d, want 12", snap[1].LatencyMs)
	}

	// 后续上报覆盖最近值
	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{LatencyMs: 34, Timestamp: 102})
	if st := h.Get(1); st.LatencyMs != 34 {
		t.Fatalf("更新后 LatencyMs = %d, want 34", st.LatencyMs)
	}

	// 旧 Agent 不上报延迟 → 0（无测量）
	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{Timestamp: 104})
	if st := h.Get(1); st.LatencyMs != 0 {
		t.Fatalf("legacy 上报后 LatencyMs = %d, want 0", st.LatencyMs)
	}
}
