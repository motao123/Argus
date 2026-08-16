//go:build !windows

package task

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	args := strings.Join(quoteAll(os.Args[1:]), " ")
	// Keep the previous executable until the replacement has been installed. If
	// installation or launch fails, restore it and restart the known-good agent.
	script := fmt.Sprintf("sleep 1; if mv -f -- %s %s && chmod +x -- %s; then %s %s; fi; mv -f -- %s %s; chmod +x -- %s; exec %s %s", shellQuote(newPath), shellQuote(self), shellQuote(self), shellQuote(self), args, shellQuote(backupPath), shellQuote(self), shellQuote(self), shellQuote(self), args)
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("spawn replacement: %w", err)
	}
	return fmt.Sprintf("upgraded to %s, restarting", p.Version), nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellQuote(a)
	}
	return out
}
