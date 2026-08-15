package store

import (
	"testing"

	"github.com/motao123/Argus/protocol"
)

func TestTransferQueue(t *testing.T) {
	h := NewHub()
	h.Upsert(testServer(1))

	// 第一小时：累计 1000/2000
	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{
		Timestamp:      3600,
		NetInTransfer:  1000,
		NetOutTransfer: 2000,
	})
	// 同一小时：累计 1500/2500（无差值）
	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{
		Timestamp:      3600,
		NetInTransfer:  1500,
		NetOutTransfer: 2500,
	})
	if q := h.TakeTransferQueue(); len(q) != 0 {
		t.Fatalf("same hour should not queue, got %d", len(q))
	}

	// 第二小时：累计 2500/3500 → 上一小时差值 = 2500-1500=1000（小时区间累计）
	h.SetReport(1, protocol.HostInfo{}, &protocol.ReportParams{
		Timestamp:      7200,
		NetInTransfer:  2500,
		NetOutTransfer: 3500,
	})
	q := h.TakeTransferQueue()
	if len(q) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(q))
	}
	d := q[0]
	if d.In != 1000 || d.Out != 1000 || d.Ts != 3600 {
		t.Fatalf("delta mismatch: %+v", d)
	}
	t.Logf("PASS: hour delta in=%d out=%d ts=%d", d.In, d.Out, d.Ts)
}
