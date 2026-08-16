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
	script := fmt.Sprintf("sleep 1; mv -f -- %s %s && chmod +x -- %s && exec %s %s", shellQuote(newPath), shellQuote(self), shellQuote(self), shellQuote(self), strings.Join(quoteAll(os.Args[1:]), " "))
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
