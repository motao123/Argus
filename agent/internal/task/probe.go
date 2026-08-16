package task

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
)

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
	default:
		return &protocol.ServiceCheckResult{Up: false, Error: "unknown type: " + p.Type}
	}
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
	if method != http.MethodGet && method != http.MethodHead {
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
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), method, target, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
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
	result.Up = resp.StatusCode >= minStatus && resp.StatusCode <= maxStatus
	if !result.Up {
		result.Error = fmt.Sprintf("HTTP %d outside expected range %d-%d", resp.StatusCode, minStatus, maxStatus)
	}
	return result
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
