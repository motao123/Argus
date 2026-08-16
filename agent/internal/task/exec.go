package task

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/motao123/Argus/protocol"
)

func (h *Handler) handleExec(params json.RawMessage) (any, *protocol.RPCError) {
	var p protocol.ExecParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	timeout := time.Duration(p.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := commandFor(ctx, p.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	result := protocol.ExecResult{Output: stdout.String() + stderr.String(), Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
	if err != nil {
		result.Error = err.Error()
	}
	return result, nil
}
