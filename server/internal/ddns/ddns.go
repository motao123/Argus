// Package ddns implements DDNS providers without logging credentials or rendered URLs.
package ddns

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

var ErrUnauthorized = errors.New("provider unauthorized")

type Request struct {
	Domain, RecordType, IP, AccessKey                      string
	WebhookURL, WebhookMethod, WebhookHeaders, WebhookBody string
}

type Provider interface{ Update(Request) error }

type Client struct {
	HTTP              *http.Client
	CloudflareBaseURL string
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{HTTP: httpClient, CloudflareBaseURL: "https://api.cloudflare.com/client/v4"}
}

func (c *Client) Provider(name string) Provider {
	if name == "cloudflare" {
		return &cloudflareProvider{c: c}
	}
	return &webhookProvider{c: c}
}

func NormalizeDomain(domain string) (string, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	ascii, err := idna.Lookup.ToASCII(domain)
	if err != nil || ascii == "" || len(ascii) > 253 || !strings.Contains(ascii, ".") {
		return "", fmt.Errorf("invalid domain")
	}
	return ascii, nil
}

// Domains accepts comma, whitespace, and newline separated names and de-duplicates them.
func Domains(raw string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' ' }) {
		d, err := NormalizeDomain(item)
		if err != nil {
			return nil, err
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("domains required")
	}
	return out, nil
}

func RecordTypes(profile string) ([]string, error) {
	switch strings.ToUpper(profile) {
	case "A":
		return []string{"A"}, nil
	case "AAAA":
		return []string{"AAAA"}, nil
	case "DUAL":
		return []string{"A", "AAAA"}, nil
	}
	return nil, fmt.Errorf("record_type must be A, AAAA, or dual")
}

func validateIP(recordType, ip string) error {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return fmt.Errorf("invalid IP")
	}
	if recordType == "A" && (parsed.To4() == nil || strings.Contains(ip, ":")) {
		return fmt.Errorf("IPv4 unavailable")
	}
	if recordType == "AAAA" && parsed.To4() != nil {
		return fmt.Errorf("IPv6 unavailable")
	}
	return nil
}

// ---- webhook ----
type webhookProvider struct{ c *Client }

func (w *webhookProvider) Update(r Request) error {
	if err := validateIP(r.RecordType, r.IP); err != nil {
		return err
	}
	method := strings.ToUpper(strings.TrimSpace(r.WebhookMethod))
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return fmt.Errorf("unsupported webhook method")
	}
	replace := func(s string) string {
		s = strings.ReplaceAll(s, "{domain}", url.QueryEscape(r.Domain))
		s = strings.ReplaceAll(s, "{ip}", url.QueryEscape(r.IP))
		s = strings.ReplaceAll(s, "{record_type}", url.QueryEscape(r.RecordType))
		return s
	}
	renderText := func(s string) string {
		s = strings.ReplaceAll(s, "{domain}", r.Domain)
		s = strings.ReplaceAll(s, "{ip}", r.IP)
		return strings.ReplaceAll(s, "{record_type}", r.RecordType)
	}
	u := replace(r.WebhookURL)
	parsed, err := url.ParseRequestURI(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid webhook URL")
	}
	body := renderText(r.WebhookBody)
	req, err := http.NewRequest(method, u, strings.NewReader(body))
	if err != nil {
		return err
	}
	if strings.TrimSpace(r.WebhookHeaders) != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(r.WebhookHeaders), &headers); err != nil {
			return fmt.Errorf("invalid webhook headers")
		}
		for k, v := range headers {
			req.Header.Set(k, renderText(v))
		}
	}
	resp, err := w.c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: webhook HTTP %d", ErrUnauthorized, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return fmt.Errorf("webhook temporary HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook HTTP %d", resp.StatusCode)
	}
	return nil
}

// ---- Cloudflare ----
type cloudflareProvider struct{ c *Client }
type cfEnvelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}
type cfZone struct{ ID, Name string }
type cfRecord struct{ ID, Content string }

func (p *cloudflareProvider) call(method, path, token string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, strings.TrimRight(p.c.CloudflareBaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("%w: cloudflare HTTP %d", ErrUnauthorized, resp.StatusCode)
	}
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return fmt.Errorf("cloudflare temporary HTTP %d", resp.StatusCode)
	}
	var env cfEnvelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&env); err != nil {
		return fmt.Errorf("cloudflare invalid response")
	}
	if resp.StatusCode >= 400 || !env.Success {
		return fmt.Errorf("cloudflare request failed")
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("cloudflare invalid result")
		}
	}
	return nil
}

func (p *cloudflareProvider) findZone(domain, token string) (cfZone, error) {
	candidates := []string{}
	if root, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		candidates = append(candidates, root)
	}
	labels := strings.Split(domain, ".")
	for i := 0; i < len(labels)-1; i++ {
		candidate := strings.Join(labels[i:], ".")
		if len(candidates) == 0 || candidate != candidates[0] {
			candidates = append(candidates, candidate)
		}
	}
	for _, candidate := range candidates {
		var zones []cfZone
		err := p.call(http.MethodGet, "/zones?name="+url.QueryEscape(candidate), token, nil, &zones)
		if err != nil {
			return cfZone{}, err
		}
		if len(zones) > 0 {
			return zones[0], nil
		}
	}
	return cfZone{}, fmt.Errorf("cloudflare zone not found")
}

func (p *cloudflareProvider) Update(r Request) error {
	if r.AccessKey == "" {
		return fmt.Errorf("cloudflare API token required")
	}
	if err := validateIP(r.RecordType, r.IP); err != nil {
		return err
	}
	zone, err := p.findZone(r.Domain, r.AccessKey)
	if err != nil {
		return err
	}
	var records []cfRecord
	path := fmt.Sprintf("/zones/%s/dns_records?type=%s&name=%s", url.PathEscape(zone.ID), url.QueryEscape(r.RecordType), url.QueryEscape(r.Domain))
	if err := p.call(http.MethodGet, path, r.AccessKey, nil, &records); err != nil {
		return err
	}
	if len(records) > 0 && records[0].Content == r.IP {
		return nil
	}
	payload := map[string]any{"type": r.RecordType, "name": r.Domain, "content": r.IP, "ttl": 120}
	method := http.MethodPost
	upsertPath := fmt.Sprintf("/zones/%s/dns_records", url.PathEscape(zone.ID))
	if len(records) > 0 {
		method = http.MethodPut
		upsertPath += "/" + url.PathEscape(records[0].ID)
	}
	return p.call(method, upsertPath, r.AccessKey, payload, nil)
}
