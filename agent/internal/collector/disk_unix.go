//go:build !windows

package collector

import (
	"github.com/shirou/gopsutil/v4/disk"
)

func diskUsage() (*disk.UsageStat, error) { return disk.Usage("/") }
