package ddns

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// tencentEndpoint 是腾讯云 DNSPod 的 TC3 签名 API 端点（测试可覆盖）。
var tencentEndpoint = "https://dnspod.tencentcloudapi.com/"

// tencentProvider 使用腾讯云 DNSPod API（TC3-HMAC-SHA256 签名）。
// 凭据：SecretID + SecretKey（Request.SecretID / Request.SecretKey）。
type tencentProvider struct{ c *Client }

// tencentEnvelope 解析 DNSPod 统一响应壳。
type tencentEnvelope struct {
	Response struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
		RequestId string `json:"RequestId"`
	} `json:"Response"`
}

// tencentRecordList 描述记录列表结果（仅取所需字段）。
type tencentRecordList struct {
	RecordCountInfo struct {
		TotalCount int `json:"TotalCount"`
	} `json:"RecordCountInfo"`
	RecordList []struct {
		RecordId uint64 `json:"RecordId"`
		Value    string `json:"Value"`
	} `json:"RecordList"`
}

func (p *tencentProvider) Update(r Request) error {
	if err := validateIP(r.RecordType, r.IP); err != nil {
		return err
	}
	if r.SecretID == "" || r.SecretKey == "" {
		return fmt.Errorf("tencent SecretId and SecretKey required")
	}
	zone, sub := splitTencentZone(r.Domain)
	// 查询现有记录
	var list tencentRecordList
	if err := p.call(r, map[string]any{
		"Action": "DescribeRecordList", "Version": "2021-03-23",
		"Domain": zone, "Subdomain": sub, "RecordType": r.RecordType, "Limit": 100,
	}, &list); err != nil {
		return err
	}
	for _, rec := range list.RecordList {
		if rec.Value == r.IP {
			return nil // 已是最新
		}
	}
	// 无记录 → 新建；有记录 → 修改
	action := "CreateRecord"
	payload := map[string]any{
		"Action": "CreateRecord", "Version": "2021-03-23",
		"Domain": zone, "SubDomain": sub, "RecordType": r.RecordType,
		"RecordLine": "默认", "Value": r.IP, "TTL": 120,
	}
	if len(list.RecordList) > 0 {
		action = "ModifyRecord"
		payload = map[string]any{
			"Action": "ModifyRecord", "Version": "2021-03-23",
			"Domain": zone, "SubDomain": sub, "RecordType": r.RecordType,
			"RecordLine": "默认", "Value": r.IP, "TTL": 120,
			"RecordId": list.RecordList[0].RecordId,
		}
	}
	_ = action
	var out tencentEnvelope
	return p.call(r, payload, &out)
}

// splitTencentZone 把完整域名拆成 zone（可注册域）与 subdomain。
// apex（如 example.com）subdomain 用 "@"。
func splitTencentZone(domain string) (zone, sub string) {
	if root, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		if root == domain {
			return domain, "@"
		}
		return root, strings.TrimSuffix(domain, "."+root)
	}
	// 回退：取最后两级为 zone
	labels := strings.Split(domain, ".")
	if len(labels) >= 2 {
		zone = strings.Join(labels[len(labels)-2:], ".")
		sub = strings.Join(labels[:len(labels)-2], ".")
		if sub == "" {
			sub = "@"
		}
		return zone, sub
	}
	return domain, "@"
}

// call 发送 TC3 签名请求并校验响应壳。
func (p *tencentProvider) call(r Request, payload map[string]any, out any) error {
	body, _ := json.Marshal(payload)
	timestamp := time.Now().Unix()
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")

	// CanonicalRequest
	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:dnspod.tencentcloudapi.com\n"
	signedHeaders := "content-type;host"
	hashedPayload := sha256Hex(body)
	canonicalRequest := strings.Join([]string{
		http.MethodPost, "/", "", canonicalHeaders, signedHeaders, hashedPayload,
	}, "\n")

	// StringToSign
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256", fmt.Sprintf("%d", timestamp), date + "/dnspod/tc3_request",
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// 派生密钥并签名
	secretDate := hmacSHA256([]byte("TC3"+r.SecretKey), date)
	secretService := hmacSHA256(secretDate, "dnspod")
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	auth := fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s/dnspod/tc3_request, SignedHeaders=%s, Signature=%s",
		r.SecretID, date, signedHeaders, signature,
	)

	req, err := http.NewRequest(http.MethodPost, tencentEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Host", "dnspod.tencentcloudapi.com")
	req.Header.Set("X-TC-Action", actionOf(payload))
	req.Header.Set("X-TC-Version", "2021-03-23")
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("Authorization", auth)

	resp, err := p.c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("tencent read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: tencent HTTP %d", ErrUnauthorized, resp.StatusCode)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("tencent temporary HTTP %d", resp.StatusCode)
	}
	var env tencentEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("tencent invalid response")
	}
	if env.Response.Error != nil {
		msg := env.Response.Error.Message
		if strings.Contains(strings.ToLower(msg), "auth") ||
			strings.Contains(env.Response.Error.Code, "AuthFailure") {
			return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
		}
		return fmt.Errorf("tencent %s: %s", env.Response.Error.Code, msg)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("tencent invalid result")
		}
	}
	return nil
}

func actionOf(payload map[string]any) string {
	if v, ok := payload["Action"].(string); ok {
		return v
	}
	return ""
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(data))
	return m.Sum(nil)
}
