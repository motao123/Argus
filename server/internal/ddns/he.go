package ddns

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// heEndpoint 是 Hurricane Electric DNS 的动态更新端点（测试可覆盖）。
var heEndpoint = "https://dyn.dns.he.net/nic/update"

// heProvider 使用 Hurricane Electric 的 DDNS API：
// GET /nic/update?hostname=<domain>&myip=<ip>，HTTP Basic Auth
// （用户名 = 域名，密码 = HE DDNS key）。
type heProvider struct{ c *Client }

func (p *heProvider) Update(r Request) error {
	if err := validateIP(r.RecordType, r.IP); err != nil {
		return err
	}
	if r.AccessKey == "" {
		return fmt.Errorf("HE DDNS key required")
	}
	q := url.Values{}
	q.Set("hostname", r.Domain)
	q.Set("myip", r.IP)
	endpoint := heEndpoint + "?" + q.Encode()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(r.Domain, r.AccessKey)
	resp, err := p.c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body := make([]byte, 0, 128)
	buf := make([]byte, 128)
	for {
		n, err := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
	}
	text := strings.TrimSpace(string(body))
	switch {
	case strings.HasPrefix(text, "good"), strings.HasPrefix(text, "nochg"):
		return nil
	case strings.Contains(text, "badauth"):
		return fmt.Errorf("%w: HE bad authentication", ErrUnauthorized)
	case resp.StatusCode >= 500:
		return fmt.Errorf("HE temporary HTTP %d: %s", resp.StatusCode, text)
	default:
		return fmt.Errorf("HE update failed: %s", text)
	}
}
