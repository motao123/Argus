package ddns

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCloudflareCoUKAAAAUpsert(t *testing.T) {
	var zoneQuery string
	var putBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/zones":
			zoneQuery = r.URL.Query().Get("name")
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]string{{"id": "z1", "name": "example.co.uk"}}})
		case r.URL.Path == "/zones/z1/dns_records" && r.Method == http.MethodGet:
			if r.URL.Query().Get("type") != "AAAA" {
				t.Errorf("type=%s", r.URL.Query().Get("type"))
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]string{{"id": "r1", "content": "2001:db8::1"}}})
		case r.URL.Path == "/zones/z1/dns_records/r1" && r.Method == http.MethodPut:
			json.NewDecoder(r.Body).Decode(&putBody)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]string{"id": "r1"}})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()
	c := NewClient(ts.Client())
	c.CloudflareBaseURL = ts.URL
	err := c.Provider("cloudflare").Update(Request{Domain: "host.example.co.uk", RecordType: "AAAA", IP: "2001:db8::2", AccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if zoneQuery != "example.co.uk" {
		t.Fatalf("zone query=%q", zoneQuery)
	}
	if putBody["type"] != "AAAA" || putBody["content"] != "2001:db8::2" {
		t.Fatalf("body=%v", putBody)
	}
}

func TestCloudflare429500And401Classification(t *testing.T) {
	for _, tc := range []struct {
		status       int
		unauthorized bool
	}{{429, false}, {500, false}, {401, true}} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			w.Write([]byte(`{"success":false,"result":[]}`))
		}))
		c := NewClient(ts.Client())
		c.CloudflareBaseURL = ts.URL
		err := c.Provider("cloudflare").Update(Request{Domain: "a.example.com", RecordType: "A", IP: "192.0.2.1", AccessKey: "x"})
		ts.Close()
		if err == nil {
			t.Fatalf("status %d: expected error", tc.status)
		}
		if tc.unauthorized != strings.Contains(err.Error(), ErrUnauthorized.Error()) {
			t.Fatalf("status %d: %v", tc.status, err)
		}
	}
}

func TestWebhookMethodAndEscapedPlaceholders(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "PATCH" {
			t.Errorf("method=%s", r.Method)
		}
		if r.URL.Query().Get("ip") != "2001:db8::1" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		if r.Header.Get("X-Domain") != "xn--bcher-kva.example" {
			t.Errorf("header=%q", r.Header.Get("X-Domain"))
		}
	}))
	defer ts.Close()
	domain, err := NormalizeDomain("bücher.example")
	if err != nil {
		t.Fatal(err)
	}
	err = NewClient(ts.Client()).Provider("webhook").Update(Request{Domain: domain, RecordType: "AAAA", IP: "2001:db8::1", WebhookURL: ts.URL + "/update?ip={ip}&domain={domain}", WebhookMethod: "PATCH", WebhookHeaders: `{"X-Domain":"{domain}"}`})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("not called")
	}
}
