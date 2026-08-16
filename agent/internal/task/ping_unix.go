//go:build !windows

package task

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
)

func probePing(ctx context.Context, target string) *protocol.ServiceCheckResult {
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "3", target)
	return runPing(cmd, time.Now())
}

func runPing(cmd *exec.Cmd, start time.Time) *protocol.ServiceCheckResult {
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
