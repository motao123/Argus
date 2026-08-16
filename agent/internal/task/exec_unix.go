//go:build !windows

package task

import (
	"context"
	"os/exec"
)

func commandFor(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

func defaultShell() string { return "/bin/sh" }
