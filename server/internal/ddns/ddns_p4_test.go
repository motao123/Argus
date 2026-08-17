package ddns

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHEProvider 验证 HE 动态更新：basic auth + hostname/myip 参数 + 状态码解析。
func TestHEProvider(t *testing.T) {
	var gotUser, gotPass, gotQuery string
	var authHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		authHeader = r.Header.Get("Authorization")
		if u, p, ok := r.BasicAuth(); ok {
			gotUser, gotPass = u, p
		}
		w.Write([]byte("good 192.0.2.1"))
	}))
	defer ts.Close()
	origEndpoint := heEndpoint
	heEndpoint = ts.URL
	defer func() { heEndpoint = origEndpoint }()

	err := NewClient(ts.Client()).Provider("he").Update(Request{Domain: "host.example.com", RecordType: "A", IP: "192.0.2.1", AccessKey: "he-key"})
	if err != nil {
		t.Fatal(err)
	}
	if gotUser != "host.example.com" || gotPass != "he-key" {
		t.Fatalf("basic auth user=%q pass=%q", gotUser, gotPass)
	}
	if !strings.Contains(gotQuery, "hostname=host.example.com") || !strings.Contains(gotQuery, "myip=192.0.2.1") {
		t.Fatalf("query=%q", gotQuery)
	}
	if authHeader == "" {
		t.Fatal("no Authorization header")
	}
}

func TestHEProviderBadAuthAndErrors(t *testing.T) {
	origEndpoint := heEndpoint
	defer func() { heEndpoint = origEndpoint }()
	for _, tc := range []struct {
		body         string
		unauthorized bool
	}{
		{"badauth", true},
		{"nohost", false},
		{"911", false},
	} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(tc.body))
		}))
		heEndpoint = ts.URL
		err := NewClient(ts.Client()).Provider("he").Update(Request{Domain: "h.example.com", RecordType: "A", IP: "192.0.2.1", AccessKey: "k"})
		ts.Close()
		if err == nil {
			t.Fatalf("body=%q: expected error", tc.body)
		}
		if tc.unauthorized != strings.Contains(err.Error(), ErrUnauthorized.Error()) {
			t.Fatalf("body=%q: %v", tc.body, err)
		}
	}
	// 缺 key
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	if err := NewClient(ts.Client()).Provider("he").Update(Request{Domain: "h.example.com", RecordType: "A", IP: "192.0.2.1"}); err == nil {
		t.Fatal("missing key should error")
	}
}

// TestTencentSplitZone 验证域名拆分为 zone + subdomain。
func TestTencentSplitZone(t *testing.T) {
	cases := map[string][2]string{
		"example.com":          {"example.com", "@"},
		"host.example.com":     {"example.com", "host"},
		"a.b.example.co.uk":    {"example.co.uk", "a.b"},
		"deep.host.example.org": {"example.org", "deep.host"},
	}
	for domain, want := range cases {
		zone, sub := splitTencentZone(domain)
		if zone != want[0] || sub != want[1] {
			t.Errorf("splitTencentZone(%s) = %s/%s, want %s/%s", domain, zone, sub, want[0], want[1])
		}
	}
}

// TestTencentUpdateFlow 验证 TC3 签名请求：查询→无记录→创建，以及值相同→跳过。
func TestTencentUpdateFlow(t *testing.T) {
	var calls []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256") {
			t.Fatalf("missing TC3 auth header")
		}
		if r.Header.Get("X-TC-Action") == "" {
			t.Fatalf("missing X-TC-Action")
		}
		calls = append(calls, r.Header.Get("X-TC-Action"))
		switch r.Header.Get("X-TC-Action") {
		case "DescribeRecordList":
			w.Write([]byte(`{"Response":{"RecordCountInfo":{"TotalCount":0},"RecordList":[]}}`))
		case "CreateRecord":
			w.Write([]byte(`{"Response":{"RequestId":"x"}}`))
		case "ModifyRecord":
			w.Write([]byte(`{"Response":{"RequestId":"x"}}`))
		default:
			w.Write([]byte(`{"Response":{"Error":{"Code":"Unknown","Message":"unexpected"}}}`))
		}
	}))
	defer ts.Close()
	origEndpoint := tencentEndpoint
	tencentEndpoint = ts.URL
	defer func() { tencentEndpoint = origEndpoint }()

	c := NewClient(ts.Client())
	// 无记录 → 创建
	err := c.Provider("tencent").Update(Request{Domain: "host.example.com", RecordType: "A", IP: "192.0.2.1", SecretID: "sid", SecretKey: "skey"})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "DescribeRecordList" || calls[1] != "CreateRecord" {
		t.Fatalf("calls=%v", calls)
	}
}
