// Package notifier 通知发送器（多渠道，借鉴 komari messageSender 设计）。
package notifier

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/motao123/Argus/server/internal/model"
)

// Send 按渠道类型发送通知。
func Send(n *model.Notification, title, content string) {
	switch n.Type {
	case "bark":
		sendBark(n, title, content)
	case "telegram":
		sendTelegram(n, title, content)
	case "email":
		sendEmail(n, title, content)
	case "serverchan":
		sendServerChan(n, title, content)
	default: // webhook
		sendWebhook(n, title, content)
	}
}

// ---- webhook（JSON/Form 模板，借鉴 nezha）----

func sendWebhook(n *model.Notification, title, content string) {
	method := n.Method
	if method == "" {
		method = "POST"
	}
	headers := map[string]string{}
	_ = json.Unmarshal([]byte(n.Headers), &headers)

	body := n.Body
	if strings.TrimSpace(body) == "" {
		body = `{"title":"{{title}}","content":"{{content}}"}`
	}
	body = strings.ReplaceAll(body, "{{title}}", escapeJSON(title))
	body = strings.ReplaceAll(body, "{{content}}", escapeJSON(content))

	req, err := http.NewRequest(method, n.URL, bytes.NewBufferString(body))
	if err != nil {
		log.Printf("webhook build failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	doRequest(req)
}

// ---- bark（https://github.com/Finb/Bark）----

func sendBark(n *model.Notification, title, content string) {
	base := strings.TrimSuffix(n.URL, "/")
	if base == "" {
		base = "https://api.day.app"
	}
	// url 为 https://api.day.app/<key>，剩余路径拼接 title/content
	endpoint := fmt.Sprintf("%s/%s/%s", base, url.PathEscape(title), url.PathEscape(content))
	req, _ := http.NewRequest(http.MethodPost, endpoint, nil)
	doRequest(req)
}

// ---- telegram bot ----（url 形如 https://api.telegram.org/bot<token>）

func sendTelegram(n *model.Notification, title, content string) {
	base := strings.TrimSuffix(n.URL, "/")
	if base == "" || !strings.Contains(base, "bot") {
		log.Printf("telegram: invalid bot url")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"chat_id": n.ChatID,
		"text":    fmt.Sprintf("*%s*\n%s", title, content),
		"parse_mode": "Markdown",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/sendMessage", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	doRequest(req)
}

// ---- email（SMTP）----（url 形如 smtp://user:pass@host:port，chat_id 为收件人）

func sendEmail(n *model.Notification, title, content string) {
	u, err := url.Parse(n.URL)
	if err != nil || u.Scheme != "smtp" {
		log.Printf("email: invalid smtp url")
		return
	}
	host := u.Host
	user := u.User.Username()
	pass, _ := u.User.Password()
	to := n.ChatID
	if to == "" {
		log.Printf("email: no recipient (chat_id)")
		return
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		user, to, title, content)

	auth := smtp.PlainAuth("", user, pass, strings.Split(host, ":")[0])
	if err := smtp.SendMail(host, auth, user, []string{to}, []byte(msg)); err != nil {
		log.Printf("email send failed: %v", err)
	}
}

// ---- serverchan（https://sct.ftqq.com）----

func sendServerChan(n *model.Notification, title, content string) {
	base := strings.TrimSuffix(n.URL, "/")
	if base == "" {
		base = "https://sctapi.ftqq.com"
	}
	form := url.Values{"title": {title}, "desp": {content}}
	req, _ := http.NewRequest(http.MethodPost, base+"/send", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	doRequest(req)
}

// ---- 公共 ----

var httpClient = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // 允许自签通知端点
}}

func doRequest(req *http.Request) {
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("notification send failed: %v", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Printf("notification endpoint returned %d", resp.StatusCode)
	}
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
