package task

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
)

// probeService 执行一次服务探测（借鉴 nezha ServiceSentinel 的探测语义）。
func probeService(kind, target string, timeout time.Duration) *protocol.ServiceCheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch kind {
	case "http":
		return probeHTTP(ctx, target)
	case "tcp":
		return probeTCP(ctx, target)
	case "ping":
		return probePing(ctx, target)
	default:
		return &protocol.ServiceCheckResult{Up: false, Error: "unknown type: " + kind}
	}
}

func probeHTTP(ctx context.Context, target string) *protocol.ServiceCheckResult {
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = "http://" + target
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return &protocol.ServiceCheckResult{Up: false, DelayMs: int(elapsed.Milliseconds()), Error: err.Error()}
	}
	defer resp.Body.Close()
	// HTTP 4xx/5xx 视为服务异常（借鉴 nezha：状态码 >= 400 判故障）
	up := resp.StatusCode < 400
	return &protocol.ServiceCheckResult{Up: up, DelayMs: int(elapsed.Milliseconds()), Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
}

func probeTCP(ctx context.Context, target string) *protocol.ServiceCheckResult {
	start := time.Now()
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", target)
	elapsed := time.Since(start)
	if err != nil {
		return &protocol.ServiceCheckResult{Up: false, DelayMs: int(elapsed.Milliseconds()), Error: err.Error()}
	}
	conn.Close()
	return &protocol.ServiceCheckResult{Up: true, DelayMs: int(elapsed.Milliseconds())}
}

func probePing(ctx context.Context, target string) *protocol.ServiceCheckResult {
	// ICMP 需要 root/raw socket，用系统 ping 命令（-W 超时秒）
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "3", target)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		return &protocol.ServiceCheckResult{Up: false, DelayMs: int(elapsed.Milliseconds()), Error: strings.TrimSpace(string(out))}
	}
	return &protocol.ServiceCheckResult{Up: true, DelayMs: int(elapsed.Milliseconds())}
}
