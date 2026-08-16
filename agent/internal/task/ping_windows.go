//go:build windows

package task

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
)

func probePing(ctx context.Context, target string) *protocol.ServiceCheckResult {
	cmd := exec.CommandContext(ctx, "ping.exe", "-n", "1", "-w", "3000", target)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	result := &protocol.ServiceCheckResult{Up: err == nil, DelayMs: int(time.Since(start).Milliseconds())}
	if err != nil {
		result.Error = strings.TrimSpace(string(out))
		if result.Error == "" {
			result.Error = err.Error()
		}
	}
	return result
}
