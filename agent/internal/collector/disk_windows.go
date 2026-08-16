//go:build windows

package collector

import (
	"os"
	"path/filepath"

	"github.com/shirou/gopsutil/v4/disk"
)

func diskUsage() (*disk.UsageStat, error) {
	root := filepath.VolumeName(os.Getenv("SystemDrive"))
	if root == "" {
		root = `C:`
	}
	return disk.Usage(root + `\`)
}
