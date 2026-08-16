package collector

import "testing"

func TestCollectorFilters(t *testing.T) {
	c := New("test", Options{InterfaceInclude: []string{"eth*"}, InterfaceExclude: []string{"eth9"}})
	if !c.match("eth0", c.interfaceInclude, c.interfaceExclude) {
		t.Fatal("included interface rejected")
	}
	if c.match("eth9", c.interfaceInclude, c.interfaceExclude) {
		t.Fatal("exclude must override include")
	}
	if c.match("lo", c.interfaceInclude, c.interfaceExclude) {
		t.Fatal("non-included interface accepted")
	}
}

func TestCollectAvailabilityAndNoPerProcessData(t *testing.T) {
	r := New("test").Collect()
	if r.Timestamp == 0 {
		t.Fatal("timestamp missing")
	}
	if !r.ProcessAvailability.Available && r.ProcessAvailability.Reason == "" {
		t.Fatal("process availability has no reason")
	}
	if !r.SocketAvailability.Available && r.SocketAvailability.Reason == "" {
		t.Fatal("socket availability has no reason")
	}
	// Report only exposes an aggregate process count; protocol has no per-process collection field.
	if r.ProcessCount < 0 {
		t.Fatal("invalid process count")
	}
}
