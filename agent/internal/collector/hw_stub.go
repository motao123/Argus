//go:build !linux

package collector

import "github.com/motao123/Argus/protocol"

// CPUTemperature is unavailable on this platform.
func CPUTemperature() float64 { return 0 }

// GPUInfo explicitly marks unsupported platform collectors unavailable.
func GPUInfo() protocol.GPUReport {
	return protocol.GPUReport{Availability: protocol.Availability{Reason: "GPU collector unsupported on this platform"}}
}
