package task

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
)

func prepareUpgrade(p *protocol.UpgradeParams, self, newPath, backupPath string) error {
	if err := downloadFile(p.URL, newPath); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	got, err := sha256File(newPath)
	if err != nil {
		_ = os.Remove(newPath)
		return err
	}
	if !equalSHA256(got, p.SHA256) {
		_ = os.Remove(newPath)
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, p.SHA256)
	}
	if err := copyFile(self, backupPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("backup self: %w", err)
	}
	return nil
}

func equalSHA256(got, want string) bool {
	return strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(want))
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
