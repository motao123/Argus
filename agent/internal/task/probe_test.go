package task

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/motao123/Argus/protocol"
)

func boolPtr(v bool) *bool { return &v }

func TestProbeHTTPSVerificationAndDetails(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	verified := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Timeout: 2})
	if verified.Up || verified.Error == "" {
		t.Fatalf("default TLS verification should reject test cert: %+v", verified)
	}

	insecure := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Timeout: 2,
		Method: "HEAD", VerifyTLS: boolPtr(false), ExpectedStatusMin: 204, ExpectedStatusMax: 204})
	if !insecure.Up || insecure.StatusCode != 204 || !insecure.TLSVerificationSkipped {
		t.Fatalf("explicit TLS skip probe = %+v", insecure)
	}
	if insecure.CertIssuer == "" || insecure.CertNotAfter == 0 || insecure.CertDaysRemaining <= 0 {
		t.Fatalf("missing certificate details: %+v", insecure)
	}
	if insecure.ConnectMs <= 0 || insecure.TLSMs <= 0 || insecure.TTFBMs <= 0 || insecure.DelayMs <= 0 {
		t.Fatalf("missing timings: %+v", insecure)
	}
}

func TestProbeHTTPExpectedStatusAndRedirectLimit(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, server.URL+"/redirect", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	good := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, ExpectedStatusMin: 418, ExpectedStatusMax: 418})
	if !good.Up || good.StatusCode != 418 {
		t.Fatalf("status range probe = %+v", good)
	}
	redirect := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL + "/redirect", MaxRedirects: 1})
	if redirect.Up || redirect.Error == "" {
		t.Fatalf("redirect limit probe = %+v", redirect)
	}
}

func TestProbeTCPListenerLatency(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, e := ln.Accept()
		if e == nil {
			_ = conn.Close()
		}
	}()
	result := probeService(protocol.ServiceCheckParams{Type: "tcp", Target: ln.Addr().String(), Timeout: 2})
	if !result.Up || result.DelayMs <= 0 || result.Sent != 1 || result.Received != 1 {
		t.Fatalf("tcp probe = %+v", result)
	}
}

func TestProbeHTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(150 * time.Millisecond) }))
	defer server.Close()
	result := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Timeout: 0})
	if !result.Up {
		t.Fatalf("default timeout unexpectedly failed: %+v", result)
	}
}

func TestProbeHTTPCustomHeadersAndHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Custom"); got != "hello" {
			t.Errorf("X-Custom = %q, want hello", got)
		}
		if got := r.Header.Get("X-Empty"); got != "" {
			t.Errorf("X-Empty = %q, want empty", got)
		}
		if got := r.Host; got != "example.test" {
			t.Errorf("Host = %q, want example.test", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	result := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Method: "GET", Timeout: 2,
		Headers: []protocol.KeyValue{
			{Key: "X-Custom", Value: "hello"},
			{Key: "Host", Value: "example.test"},
			{Key: "X-Empty", Value: ""},
			{Key: "", Value: "ignored"},
		}})
	if !result.Up {
		t.Fatalf("header probe = %+v", result)
	}
}

func TestProbeHTTPPostBodyAndContentType(t *testing.T) {
	var gotMethod, gotBody, gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	result := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Method: "POST", Timeout: 2,
		Body: "hello=world", AssertContains: "ok"})
	if !result.Up {
		t.Fatalf("POST probe = %+v", result)
	}
	if gotMethod != http.MethodPost || gotBody != "hello=world" {
		t.Fatalf("POST request = %s %q", gotMethod, gotBody)
	}
	if gotContentType != "text/plain" {
		t.Fatalf("default content type = %q, want text/plain", gotContentType)
	}
}

func TestProbeHTTPContentTypeOverriddenByHeader(t *testing.T) {
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	result := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Method: "PATCH", Timeout: 2,
		Body: "{}", Headers: []protocol.KeyValue{{Key: "Content-Type", Value: "application/json"}}})
	if !result.Up {
		t.Fatalf("PATCH probe = %+v", result)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", gotContentType)
	}
}

func TestProbeHTTPGetWithBodyIgnored(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	result := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Method: "GET", Timeout: 2, Body: "should-not-be-sent"})
	if !result.Up {
		t.Fatalf("GET probe = %+v", result)
	}
	if gotBody != "" {
		t.Fatalf("GET request carried body %q; body must be ignored for GET/HEAD", gotBody)
	}
}

func TestProbeHTTPAssertContainsHitAndMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("service is healthy, version 1.2.3"))
	}))
	defer server.Close()
	hit := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Timeout: 2, AssertContains: "healthy"})
	if !hit.Up {
		t.Fatalf("assert hit probe = %+v", hit)
	}
	miss := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Timeout: 2, AssertContains: "down"})
	if miss.Up || !strings.Contains(miss.Error, `"down"`) {
		t.Fatalf("assert miss probe = %+v", miss)
	}
	// 状态码不符时不读 body，直接判失败
	teapot := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Timeout: 2,
		ExpectedStatusMin: 500, ExpectedStatusMax: 599, AssertContains: "healthy"})
	if teapot.Up {
		t.Fatalf("status range failure with assert = %+v", teapot)
	}
}

func TestProbeHTTPUnsupportedMethod(t *testing.T) {
	result := probeService(protocol.ServiceCheckParams{Type: "http", Target: "http://127.0.0.1:1", Method: "OPTIONS"})
	if result.Up || result.Error == "" {
		t.Fatalf("unsupported method probe = %+v", result)
	}
}

func TestProbeHTTPDeleteMethodAllowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	result := probeService(protocol.ServiceCheckParams{Type: "http", Target: server.URL, Method: "DELETE", Timeout: 2})
	if !result.Up {
		t.Fatalf("DELETE probe = %+v", result)
	}
}
