package task

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
)

// upgradeSelf 下载新二进制、校验 SHA-256、备份原文件并原子替换后重启。
// 通过 detached 子进程在 1 秒后完成替换与 re-exec，避免替换运行中的自身。
func upgradeSelf(p *protocol.UpgradeParams) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve self: %w", err)
	}
	selfAbs, err := filepath.Abs(self)
	if err != nil {
		return "", err
	}
	newPath := selfAbs + ".new"
	backupPath := selfAbs + ".bak"

	// 1. 下载
	if err := downloadFile(p.URL, newPath); err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	// 2. SHA-256 校验
	got, err := sha256File(newPath)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(got, p.SHA256) {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("sha256 mismatch: got %s want %s", got, p.SHA256)
	}

	// 3. 备份当前二进制
	if err := copyFile(selfAbs, backupPath); err != nil {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("backup self: %w", err)
	}

	// 4. 后台执行替换 + 重启（detached，父进程退出后继续）
	args := append([]string{"-c"},
		fmt.Sprintf("sleep 1; mv -f %s %s; chmod +x %s; exec %s %s",
			shellQuote(newPath), shellQuote(selfAbs), shellQuote(selfAbs), shellQuote(selfAbs), strings.Join(quoteAll(os.Args[1:]), " ")))
	cmd := exec.Command("/bin/sh", args...)
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("spawn replacement: %w", err)
	}
	return fmt.Sprintf("upgraded to %s, restarting", p.Version), nil
}

func downloadFile(url, dst string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func quoteAll(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, shellQuote(a))
	}
	return out
}
