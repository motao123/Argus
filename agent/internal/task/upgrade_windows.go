//go:build windows

package task

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/motao123/Argus/protocol"
)

func upgradeSelf(p *protocol.UpgradeParams) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve self: %w", err)
	}
	self, err = filepath.Abs(self)
	if err != nil {
		return "", err
	}
	newPath, backupPath := self+".new", self+".bak"
	if err := prepareUpgrade(p, self, newPath, backupPath); err != nil {
		return "", err
	}
	args := windowsCommandLine(os.Args[1:])
	var cmd *exec.Cmd
	if powershell, lookErr := exec.LookPath("powershell.exe"); lookErr == nil {
		script := fmt.Sprintf("Start-Sleep -Seconds 1; try { Move-Item -LiteralPath %s -Destination %s -Force; Start-Process -FilePath %s -ArgumentList %s -ErrorAction Stop } catch { Move-Item -LiteralPath %s -Destination %s -Force; Start-Process -FilePath %s -ArgumentList %s; exit 1 }", psQuote(newPath), psQuote(self), psQuote(self), psQuote(args), psQuote(backupPath), psQuote(self), psQuote(self), psQuote(args))
		cmd = exec.Command(powershell, "-NoProfile", "-NonInteractive", "-Command", script)
	} else {
		scriptPath := self + ".upgrade.cmd"
		script := fmt.Sprintf("@echo off\r\nping 127.0.0.1 -n 2 >nul\r\nmove /Y \"%s\" \"%s\" >nul\r\nif errorlevel 1 goto rollback\r\nstart \"\" \"%s\" %s\r\nif errorlevel 1 goto rollback\r\ngoto cleanup\r\n:rollback\r\nmove /Y \"%s\" \"%s\" >nul\r\nstart \"\" \"%s\" %s\r\n:cleanup\r\ndel \"%%~f0\"\r\n", newPath, self, self, args, backupPath, self, self, args)
		if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
			_ = os.Remove(newPath)
			return "", fmt.Errorf("write replacement script: %w", err)
		}
		cmd = exec.Command(defaultShell(), "/D", "/S", "/C", scriptPath)
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("spawn replacement: %w", err)
	}
	return fmt.Sprintf("upgraded to %s, restarting", p.Version), nil
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func windowsCommandLine(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strconv.Quote(a)
	}
	return strings.Join(out, " ")
}
