//go:build !windows

package task

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
)

var (
	pingPacketsRE = regexp.MustCompile(`(?m)(\d+) packets transmitted,\s*(\d+)(?: packets)? received`)
	pingRTTRE     = regexp.MustCompile(`(?m)(?:round-trip|rtt).* = [\d.]+/([\d.]+)/`)
)

func probePing(ctx context.Context, target string, count int) *protocol.ServiceCheckResult {
	if count <= 0 || count > 10 {
		count = 3
	}
	args := []string{"-c", strconv.Itoa(count)}
	if runtime.GOOS == "darwin" || strings.Contains(runtime.GOOS, "bsd") {
		args = append(args, "-W", "3000") // milliseconds on BSD/macOS
	} else {
		args = append(args, "-W", "3") // seconds on iputils
	}
	args = append(args, target)
	return runPing(exec.CommandContext(ctx, "ping", args...), time.Now(), count)
}

func runPing(cmd *exec.Cmd, start time.Time, count int) *protocol.ServiceCheckResult {
	out, err := cmd.CombinedOutput()
	text := string(out)
	result := &protocol.ServiceCheckResult{DelayMs: durationMS(time.Since(start)), Sent: count}
	if m := pingPacketsRE.FindStringSubmatch(text); len(m) == 3 {
		result.Sent, _ = strconv.Atoi(m[1])
		result.Received, _ = strconv.Atoi(m[2])
	}
	if m := pingRTTRE.FindStringSubmatch(text); len(m) == 2 {
		if avg, parseErr := strconv.ParseFloat(m[1], 64); parseErr == nil {
			result.DelayMs = int(avg + .5)
		}
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
