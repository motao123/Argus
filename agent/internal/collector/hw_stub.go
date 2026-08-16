//go:build !linux

package collector

// CPUTemperature is unavailable on this platform.
func CPUTemperature() float64 { return 0 }

// GPUInfo is unavailable without a platform collector.
func GPUInfo() (float64, uint64, uint64) { return 0, 0, 0 }
