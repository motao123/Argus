//go:build windows

package task

import (
	"context"
	"os"
	"os/exec"
)

func commandFor(ctx context.Context, command string) *exec.Cmd {
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		return exec.CommandContext(ctx, path, "-NoLogo", "-NonInteractive", "-Command", command)
	}
	return exec.CommandContext(ctx, defaultShell(), "/D", "/S", "/C", command)
}

func defaultShell() string {
	if shell := os.Getenv("COMSPEC"); shell != "" {
		return shell
	}
	return "cmd.exe"
}
