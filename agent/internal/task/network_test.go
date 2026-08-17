package task

import (
	"context"
	"strconv"
	"testing"

	"github.com/motao123/Argus/protocol"
)

func TestParseTraceHops(t *testing.T) {
	text := `traceroute to example.com (93.184.216.34), 30 hops max, 60 byte packets
 1  192.168.1.1  1.234 ms  1.100 ms  1.050 ms
 2  * * *
 3  10.0.0.1  5.000 ms  4.900 ms  4.950 ms
 4  93.184.216.34  12.000 ms  11.500 ms  11.800 ms`
	hops := parseTraceHops(text)
	if len(hops) != 4 {
		t.Fatalf("got %d hops, want 4", len(hops))
	}
	if hops[0].Hop != 1 || hops[0].IP != "192.168.1.1" {
		t.Errorf("hop0 = %+v", hops[0])
	}
	if hops[0].RTTMs <= 1.0 || hops[0].RTTMs >= 1.2 {
		t.Errorf("hop0 rtt = %v, want ~1.128", hops[0].RTTMs)
	}
	if hops[1].Loss != 100 || hops[1].IP != "" {
		t.Errorf("hop1 (unreachable) = %+v, want loss 100", hops[1])
	}
	if hops[2].RTTMs != 4.95 {
		t.Errorf("hop2 rtt = %v, want 4.95", hops[2].RTTMs)
	}
}

func TestParseTraceHopsWindows(t *testing.T) {
	text := `Tracing route to example.com [93.184.216.34] over a maximum of 30 hops:
  1     1 ms     1 ms     1 ms  192.168.1.1
  2     *        *        *     Request timed out.
  3     5 ms     5 ms     5 ms  10.0.0.1`
	hops := parseTraceHops(text)
	if len(hops) != 3 {
		t.Fatalf("got %d hops, want 3", len(hops))
	}
	if hops[0].IP != "192.168.1.1" || hops[1].Loss != 100 || hops[2].RTTMs != 5 {
		t.Errorf("hops = %+v", hops)
	}
}

// TestBandwidthServeProbeLoopback 验证带宽测速的服务端/客户端回环自测。
func TestBandwidthServeProbeLoopback(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	serve := serveBandwidth(protocol.BandwidthParams{Duration: 2})
	if !serve.OK || serve.Port == 0 {
		t.Fatalf("serve failed: %+v", serve)
	}
	probe := probeBandwidth(context.Background(), protocol.BandwidthParams{
		Target: "127.0.0.1:" + strconv.Itoa(serve.Port), Duration: 2,
	})
	if !probe.OK || probe.BitsPerSec <= 0 {
		t.Fatalf("probe failed: %+v", probe)
	}
}
