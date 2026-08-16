package task

import (
	"net"
	"net/http"
	"net/http/httptest"
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
