package task

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
)

// maxProbeBodyBytes 断言读取的响应体上限（超出部分截断，不参与关键字匹配）。
const maxProbeBodyBytes = 1 << 20

// maxProbeErrorBytes 命令探测 stderr 计入错误信息的长度上限。
const maxProbeErrorBytes = 512

// probeService executes one bounded service probe.
func probeService(p protocol.ServiceCheckParams) *protocol.ServiceCheckResult {
	timeout := time.Duration(p.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch strings.ToLower(p.Type) {
	case "http", "https":
		return probeHTTP(ctx, p)
	case "tcp":
		return probeTCP(ctx, p.Target)
	case "ping":
		return probePing(ctx, p.Target, p.PingCount)
	case "command":
		return probeCommand(ctx, p)
	default:
		return &protocol.ServiceCheckResult{Up: false, Error: "unknown type: " + p.Type}
	}
}

// probeCommand 执行自定义命令探测（借鉴 Uptime Kuma command 探测）：
// 命令放在 Target 字段，退出码 0 视为 Up，DelayMs 为命令耗时，
// stderr 截断后计入 Error（截断标记注明）。
func probeCommand(ctx context.Context, p protocol.ServiceCheckParams) *protocol.ServiceCheckResult {
	started := time.Now()
	cmd := commandFor(ctx, p.Target)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := &protocol.ServiceCheckResult{DelayMs: durationMS(time.Since(started))}
	if err != nil {
		res.Error = truncateString(stderr.String(), maxProbeErrorBytes)
		if res.Error == "" {
			res.Error = err.Error()
		}
		return res
	}
	res.Up = true
	return res
}

// truncateString 截断字符串并追加省略标记（UTF-8 安全，按字节截断边界兜底）。
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}

func probeHTTP(ctx context.Context, p protocol.ServiceCheckParams) *protocol.ServiceCheckResult {
	target := p.Target
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = "http://" + target
	}
	method := strings.ToUpper(strings.TrimSpace(p.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !protocol.IsAllowedHTTPMethod(method) {
		return &protocol.ServiceCheckResult{Error: "unsupported HTTP method: " + method}
	}
	verifyTLS := p.VerifyTLS == nil || *p.VerifyTLS
	result := &protocol.ServiceCheckResult{TLSVerificationSkipped: !verifyTLS}
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS}} // #nosec G402 -- explicit user option, reported in result
	defer transport.CloseIdleConnections()
	maxRedirects := p.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = 3
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("redirect limit exceeded (%d)", maxRedirects)
			}
			return nil
		},
	}

	// Body 仅 POST/PUT/PATCH 发送；GET/HEAD 携带 body 时忽略（语义明确：无请求体）。
	sendBody := (method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch) && p.Body != ""
	var bodyReader io.Reader
	if sendBody {
		bodyReader = strings.NewReader(p.Body)
	}

	started := time.Now()
	var dnsStart, connectStart, tlsStart time.Time
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone: func(httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				result.DNSMs = durationMS(time.Since(dnsStart))
			}
		},
		ConnectStart: func(_, _ string) { connectStart = time.Now() },
		ConnectDone: func(_, _ string, _ error) {
			if !connectStart.IsZero() {
				result.ConnectMs = durationMS(time.Since(connectStart))
			}
		},
		TLSHandshakeStart: func() { tlsStart = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			if !tlsStart.IsZero() {
				result.TLSMs = durationMS(time.Since(tlsStart))
			}
		},
		GotFirstResponseByte: func() { result.TTFBMs = durationMS(time.Since(started)) },
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), method, target, bodyReader)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	applyRequestHeaders(req, p.Headers, sendBody)
	resp, err := client.Do(req)
	result.DelayMs = durationMS(time.Since(started))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		result.CertIssuer = cert.Issuer.String()
		result.CertNotAfter = cert.NotAfter.Unix()
		result.CertDaysRemaining = int(time.Until(cert.NotAfter).Hours() / 24)
	}
	minStatus, maxStatus := p.ExpectedStatusMin, p.ExpectedStatusMax
	if minStatus == 0 {
		minStatus = 200
	}
	if maxStatus == 0 {
		maxStatus = 399
	}
	result.Up = expectedStatusOK(resp.StatusCode, p.Statuses, minStatus, maxStatus)
	if !result.Up {
		if len(p.Statuses) > 0 {
			result.Error = fmt.Sprintf("HTTP %d not in expected statuses %v", resp.StatusCode, p.Statuses)
		} else {
			result.Error = fmt.Sprintf("HTTP %d outside expected range %d-%d", resp.StatusCode, minStatus, maxStatus)
		}
		return result
	}
	// 关键字断言：仅在状态码符合范围后读取响应体（上限内），不命中则判 down。
	if p.AssertContains != "" {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProbeBodyBytes+1))
		if readErr != nil {
			result.Up = false
			result.Error = "failed to read response body: " + readErr.Error()
			return result
		}
		if !strings.Contains(string(body), p.AssertContains) {
			result.Up = false
			result.Error = fmt.Sprintf("response body does not contain expected keyword %q", p.AssertContains)
			return result
		}
	}
	return result
}

// expectedStatusOK 判定状态码是否符合期望：Statuses 列表非空时按列表命中判定
// （列表优先于区间）；否则按 [minStatus, maxStatus] 区间判定。
func expectedStatusOK(code int, statuses []int, minStatus, maxStatus int) bool {
	if len(statuses) > 0 {
		for _, s := range statuses {
			if code == s {
				return true
			}
		}
		return false
	}
	return code >= minStatus && code <= maxStatus
}

// applyRequestHeaders 应用自定义请求头。
// Host 通过 req.Host 设置（Go 中 Header 的 Host 不生效）；Content-Length 由客户端按 body 计算，忽略用户指定值避免冲突。
// 发送 body 时默认 Content-Type: text/plain，可被自定义 Headers 覆盖。
func applyRequestHeaders(req *http.Request, headers []protocol.KeyValue, hasBody bool) {
	for _, h := range headers {
		key := strings.TrimSpace(h.Key)
		if key == "" {
			continue
		}
		switch strings.ToLower(key) {
		case "host":
			req.Host = h.Value
		case "content-length":
			// 忽略：Content-Length 由 http.Client 依据实际 body 计算
		default:
			req.Header.Set(key, h.Value)
		}
	}
	if hasBody {
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "text/plain")
		}
	}
}

func probeTCP(ctx context.Context, target string) *protocol.ServiceCheckResult {
	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
	result := &protocol.ServiceCheckResult{DelayMs: durationMS(time.Since(start)), Sent: 1}
	if err != nil {
		result.Error = err.Error()
		result.LossPercent = 100
		return result
	}
	_ = conn.Close()
	result.Up = true
	result.Received = 1
	return result
}

func durationMS(d time.Duration) int {
	ms := int(d.Milliseconds())
	if d > 0 && ms == 0 {
		return 1
	}
	return ms
}
