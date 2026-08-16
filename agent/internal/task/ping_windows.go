//go:build windows

package task

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
)

var windowsPingStatsRE = regexp.MustCompile(`(?i)Sent\s*=\s*(\d+).*Received\s*=\s*(\d+)`)

func probePing(ctx context.Context, target string, count int) *protocol.ServiceCheckResult {
	if count <= 0 || count > 10 {
		count = 3
	}
	cmd := exec.CommandContext(ctx, "ping.exe", "-n", strconv.Itoa(count), "-w", "3000", target)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	text := string(out)
	result := &protocol.ServiceCheckResult{DelayMs: durationMS(time.Since(start)), Sent: count}
	if m := windowsPingStatsRE.FindStringSubmatch(text); len(m) == 3 {
		result.Sent, _ = strconv.Atoi(m[1])
		result.Received, _ = strconv.Atoi(m[2])
	}
	if result.Sent > 0 {
		result.LossPercent = float64(result.Sent-result.Received) / float64(result.Sent) * 100
	}
	result.Up = result.Received > 0
	if err != nil && !result.Up {
		result.Error = strings.TrimSpace(text)
		if result.Error == "" {
			result.Error = err.Error()
		}
	}
	return result
}
