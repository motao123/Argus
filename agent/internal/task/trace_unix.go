//go:build !windows

package task

import (
	"context"
	"os/exec"
	"strconv"
	"time"

	"github.com/motao123/Argus/protocol"
)

// maxTraceRawBytes 保留的原始输出上限（超出截断并标记）。
const maxTraceRawBytes = 64 << 10

func runTrace(ctx context.Context, p protocol.TraceParams) *protocol.TraceResult {
	maxHops := p.MaxHops
	if maxHops <= 0 {
		maxHops = 30
	}
	if maxHops > 64 {
		maxHops = 64
	}
	timeout := p.TimeoutSec
	if timeout <= 0 {
		timeout = 3
	}
	args := []string{"-n", "-m", strconv.Itoa(maxHops)}
	switch p.Protocol {
	case protocol.TraceTCP:
		args = append(args, "-T")
	case protocol.TraceUDP:
		args = append(args, "-U")
	default:
		// icmp 默认（部分发行版 traceroute 默认 UDP，显式指定 ICMP 更符合直觉）
		args = append(args, "-I")
	}
	args = append(args, "-w", strconv.Itoa(timeout))
	args = append(args, p.Target)

	ctx, cancel := context.WithTimeout(ctx, time.Duration(maxHops)*time.Duration(timeout)*time.Second+15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "traceroute", args...)
	out, err := cmd.CombinedOutput()
	text := string(out)
	res := &protocol.TraceResult{OK: err == nil, RawText: text, ExitCode: cmd.ProcessState.ExitCode()}
	if len(text) > maxTraceRawBytes {
		res.RawText = text[:maxTraceRawBytes]
		res.Truncated = true
	}
	if err != nil {
		res.Error = err.Error()
	}
	res.Hops = parseTraceHops(text)
	return res
}

