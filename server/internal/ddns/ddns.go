// Package ddns 动态解析（借鉴 nezha DDNSClass）：
// 服务器 IP 变化时调用 provider 更新 DNS 记录。
package ddns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Provider 定义 DNS 更新器。
type Provider interface {
	Name() string
	// Update 将 domain 解析到 ip。
	Update(domain, ip string, accessKey string) error
}

// NewProvider 按名称创建 provider。
func NewProvider(name string) Provider {
	switch name {
	case "cloudflare":
		return &cloudflareProvider{}
	default:
		return &webhookProvider{}
	}
}

// ---- webhook provider（URL 含 {ip} 占位符，GET 请求）----

type webhookProvider struct{}

func (w *webhookProvider) Name() string { return "webhook" }

func (w *webhookProvider) Update(domain, ip, webhookURL string) error {
	if webhookURL == "" {
		return fmt.Errorf("webhook url required")
	}
	u := strings.ReplaceAll(webhookURL, "{ip}", ip)
	u = strings.ReplaceAll(u, "{domain}", domain)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook %s -> %d: %s", u, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// ---- cloudflare provider（API Token，自动查 zone 后 upsert 记录）----

type cloudflareProvider struct{}

func (c *cloudflareProvider) Name() string { return "cloudflare" }

type cfZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cfResp struct {
	Success bool     `json:"success"`
	Errors  []cfErr  `json:"errors"`
	Result  []cfZone `json:"result"`
}

type cfErr struct {
	Message string `json:"message"`
}

func (c *cloudflareProvider) Update(domain, ip, apiToken string) error {
	if apiToken == "" {
		return fmt.Errorf("cloudflare api token required")
	}
	client := &http.Client{Timeout: 15 * time.Second}

	// 1. 找 zone（取域名最后两级）
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return fmt.Errorf("invalid domain: %s", domain)
	}
	zoneName := strings.Join(parts[len(parts)-2:], ".")
	req, _ := http.NewRequest(http.MethodGet,
		"https://api.cloudflare.com/client/v4/zones?name="+zoneName, nil)
	req.Header.Set("Authorization", "Bearer "+apiToken)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	var zoneData cfResp
	_ = json.NewDecoder(resp.Body).Decode(&zoneData)
	resp.Body.Close()
	if !zoneData.Success || len(zoneData.Result) == 0 {
		return fmt.Errorf("cloudflare zone not found: %s", zoneName)
	}
	zoneID := zoneData.Result[0].ID

	// 2. 查现有记录（A 记录）
	listReq, _ := http.NewRequest(http.MethodGet,
		fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?type=A&name=%s", zoneID, domain), nil)
	listReq.Header.Set("Authorization", "Bearer "+apiToken)
	listResp, err := client.Do(listReq)
	if err != nil {
		return err
	}
	var listData struct {
		Success bool `json:"success"`
		Result  []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"result"`
	}
	_ = json.NewDecoder(listResp.Body).Decode(&listData)
	listResp.Body.Close()

	payload, _ := json.Marshal(map[string]string{"type": "A", "name": domain, "content": ip, "ttl": "120"})

	// 3. upsert
	var method, url string
	if len(listData.Result) > 0 {
		recordID := listData.Result[0].ID
		if listData.Result[0].Content == ip {
			return nil // 已是最新
		}
		method = http.MethodPut
		url = fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, recordID)
	} else {
		method = http.MethodPost
		url = fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", zoneID)
	}
	upReq, _ := http.NewRequest(method, url, bytes.NewBuffer(payload))
	upReq.Header.Set("Authorization", "Bearer "+apiToken)
	upReq.Header.Set("Content-Type", "application/json")
	upResp, err := client.Do(upReq)
	if err != nil {
		return err
	}
	defer upResp.Body.Close()
	if upResp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(upResp.Body, 512))
		return fmt.Errorf("cloudflare upsert %d: %s", upResp.StatusCode, strings.TrimSpace(string(body)))
	}
	log.Printf("ddns: updated %s -> %s (zone %s)", domain, ip, zoneName)
	return nil
}
