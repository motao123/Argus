//go:build windows

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
	// Windows tracert 无协议选项（ICMP），忽略 protocol/timeout 参数
	args := []string{"-d", "-h", strconv.Itoa(maxHops), p.Target}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(maxHops)*3*time.Second+15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tracert", args...)
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

