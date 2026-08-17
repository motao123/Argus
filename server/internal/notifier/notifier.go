// Package notifier 通知发送器（多渠道 + 持久队列重试）。
//
// 设计：每次通知落一条 NotificationDelivery 记录（status=pending），
// 由 Queue 后台 worker 处理；发送失败按指数退避（base*2^(attempt-1)，封顶）
// 重试，达到 MaxAttempts（默认 5 次）标记 failed，可在 UI 手动重试。
package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

// Queue 通知持久队列。
type Queue struct {
	db *gorm.DB

	// MaxAttempts 单条通知最大尝试次数（默认 5）。
	MaxAttempts int
	// BackoffBase 指数退避基数（默认 30s；测试可注入更小值）。
	BackoffBase time.Duration
	// BackoffCap 退避上限（默认 30 分钟）。
	BackoffCap time.Duration
	// PollInterval worker 轮询间隔（默认 5s；测试可注入更小值）。
	PollInterval time.Duration

	stop chan struct{}
	done chan struct{}
}

// NewQueue 创建持久队列（自动补齐默认参数）。
func NewQueue(db *gorm.DB) *Queue {
	return &Queue{
		db:           db,
		MaxAttempts:  5,
		BackoffBase:  30 * time.Second,
		BackoffCap:   30 * time.Minute,
		PollInterval: 5 * time.Second,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// Enqueue 创建一条送达记录并立即尝试发送（失败则进入退避重试）。
// ownerID 为触发方（报警规则 owner；0 = 系统/管理员流程），用于 owner/admin 隔离。
func (q *Queue) Enqueue(n *model.Notification, title, content string, ownerID int64) error {
	if q == nil || q.db == nil {
		return fmt.Errorf("notifier queue unavailable")
	}
	if len(content) > 4096 {
		content = content[:4096]
	}
	d := model.NotificationDelivery{
		WebhookID:   n.ID,
		OwnerID:     ownerID,
		Title:       title,
		Content:     content,
		Status:      model.DeliveryPending,
		MaxAttempts: q.maxAttempts(),
	}
	now := time.Now()
	d.NextRetry = &now
	if err := q.db.Create(&d).Error; err != nil {
		return err
	}
	// 立即处理（包含本单；重试任务由 Run 轮询）。
	q.ProcessDue()
	return nil
}

// Run 后台 worker：周期扫描到期（pending 且 next_retry<=now）的送达记录。
func (q *Queue) Run() {
	ticker := time.NewTicker(q.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-q.stop:
			close(q.done)
			return
		case <-ticker.C:
			q.ProcessDue()
		}
	}
}

// Stop 停止 worker（等待当前轮结束）。
func (q *Queue) Stop() {
	close(q.stop)
	<-q.done
}

// ProcessDue 处理一批到期记录（单次尝试；失败按退避排下次）。
func (q *Queue) ProcessDue() {
	if q == nil || q.db == nil {
		return
	}
	var due []model.NotificationDelivery
	if err := q.db.
		Where("status = ? AND (next_retry IS NULL OR next_retry <= ?)", model.DeliveryPending, time.Now()).
		Order("id").Limit(20).Find(&due).Error; err != nil {
		log.Printf("notifier: fetch due deliveries: %v", err)
		return
	}
	for i := range due {
		q.sendOne(&due[i])
	}
}

// sendOne 单次发送：成功 → sent；失败 → 退避重试或 failed（封顶）。
func (q *Queue) sendOne(d *model.NotificationDelivery) {
	var n model.Notification
	if err := q.db.First(&n, d.WebhookID).Error; err != nil {
		// 渠道已删除：无重试意义，直接失败
		q.db.Model(d).Updates(map[string]any{
			"status": model.DeliveryFailed, "next_retry": nil,
			"last_error": "notification channel deleted",
		})
		return
	}
	err := send(&n, d.Title, d.Content)
	attempts := d.Attempts + 1
	now := time.Now()
	if err == nil {
		q.db.Model(d).Updates(map[string]any{
			"status": model.DeliverySent, "attempts": attempts,
			"next_retry": nil, "last_error": "", "sent_at": now,
		})
		return
	}
	if attempts >= q.maxAttempts() {
		q.db.Model(d).Updates(map[string]any{
			"status": model.DeliveryFailed, "attempts": attempts,
			"next_retry": nil, "last_error": truncateErr(err),
		})
		return
	}
	next := now.Add(q.backoff(attempts))
	q.db.Model(d).Updates(map[string]any{
		"attempts": attempts, "last_error": truncateErr(err), "next_retry": next,
	})
}

// Retry 手动重试一条已失败记录：重置为 pending 并立即处理。
func (q *Queue) Retry(id int64) error {
	if q == nil || q.db == nil {
		return fmt.Errorf("notifier queue unavailable")
	}
	var d model.NotificationDelivery
	if err := q.db.First(&d, id).Error; err != nil {
		return err
	}
	if d.Status != model.DeliveryFailed {
		return fmt.Errorf("delivery %d is not failed", id)
	}
	now := time.Now()
	if err := q.db.Model(&d).Updates(map[string]any{
		"status": model.DeliveryPending, "attempts": 0, "next_retry": now, "last_error": "",
	}).Error; err != nil {
		return err
	}
	q.ProcessDue()
	return nil
}

// List 分页查询送达记录（owner/admin 隔离：非 admin 仅见自己的）。
func (q *Queue) List(admin bool, ownerID int64, offset, limit int) ([]model.NotificationDelivery, int64, error) {
	if q == nil || q.db == nil {
		return nil, 0, fmt.Errorf("notifier queue unavailable")
	}
	qq := q.db.Model(&model.NotificationDelivery{})
	if !admin {
		qq = qq.Where("owner_id = ?", ownerID)
	}
	var total int64
	if err := qq.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.NotificationDelivery
	if err := qq.Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (q *Queue) maxAttempts() int {
	if q == nil || q.MaxAttempts <= 0 {
		return 5
	}
	return q.MaxAttempts
}

// backoff 指数退避：base * 2^(attempt-1)，封顶 BackoffCap。
func (q *Queue) backoff(attempt int) time.Duration {
	base := q.BackoffBase
	if base <= 0 {
		base = 30 * time.Second
	}
	cap := q.BackoffCap
	if cap <= 0 {
		cap = 30 * time.Minute
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= cap {
			return cap
		}
	}
	if d > cap {
		return cap
	}
	return d
}

func (q *Queue) pollInterval() time.Duration {
	if q == nil || q.PollInterval <= 0 {
		return 5 * time.Second
	}
	return q.PollInterval
}

func truncateErr(err error) string {
	s := err.Error()
	if len(s) > 1024 {
		return s[:1024]
	}
	return s
}

// ---- 渠道发送（返回 error 供重试判定）----

func send(n *model.Notification, title, content string) error {
	switch n.Type {
	case "bark":
		return sendBark(n, title, content)
	case "telegram":
		return sendTelegram(n, title, content)
	case "email":
		return sendEmail(n, title, content)
	case "serverchan":
		return sendServerChan(n, title, content)
	case "javascript":
		return sendJS(n, title, content)
	case "dingtalk":
		return sendDingTalk(n, title, content)
	case "wecom":
		return sendWeCom(n, title, content)
	case "feishu":
		return sendFeishu(n, title, content)
	case "slack":
		return sendSlack(n, title, content)
	case "wxpusher":
		return sendWxPusher(n, title, content)
	case "matrix":
		return sendMatrix(n, title, content)
	default: // webhook
		return sendWebhook(n, title, content)
	}
}

// sendJS 执行 JS 通知脚本（借鉴 komari javascript 渠道）。
// 脚本位于 data/scripts/notify-<id>.js，注入 title/content 与 console.log。
func sendJS(n *model.Notification, title, content string) error {
	scriptDir := os.Getenv("ARGUS_DATA_DIR")
	if scriptDir == "" {
		scriptDir = "./data"
	}
	path := filepath.Join(scriptDir, "scripts", fmt.Sprintf("notify-%d.js", n.ID))
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("js notify: script %s not found: %w", path, err)
	}
	vm := goja.New()
	_ = vm.Set("title", title)
	_ = vm.Set("content", content)
	console := vm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, a := range call.Arguments {
			parts = append(parts, a.String())
		}
		log.Printf("js notify #%d: %s", n.ID, strings.Join(parts, " "))
		return goja.Undefined()
	})
	_ = vm.Set("console", console)
	if _, err := vm.RunString(string(src)); err != nil {
		return fmt.Errorf("js notify #%d error: %w", n.ID, err)
	}
	return nil
}

// ---- webhook（JSON/Form 模板，借鉴 nezha）----

func sendWebhook(n *model.Notification, title, content string) error {
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
		return fmt.Errorf("webhook build failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doRequest(req)
}

// ---- bark（https://github.com/Finb/Bark）----

func sendBark(n *model.Notification, title, content string) error {
	base := strings.TrimSuffix(n.URL, "/")
	if base == "" {
		base = "https://api.day.app"
	}
	// url 为 https://api.day.app/<key>，剩余路径拼接 title/content
	endpoint := fmt.Sprintf("%s/%s/%s", base, url.PathEscape(title), url.PathEscape(content))
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	return doRequest(req)
}

// ---- telegram bot ----（url 形如 https://api.telegram.org/bot<token>）

func sendTelegram(n *model.Notification, title, content string) error {
	base := strings.TrimSuffix(n.URL, "/")
	if base == "" || !strings.Contains(base, "bot") {
		return fmt.Errorf("telegram: invalid bot url")
	}
	payload, _ := json.Marshal(map[string]any{
		"chat_id":    n.ChatID,
		"text":       fmt.Sprintf("*%s*\n%s", title, content),
		"parse_mode": "Markdown",
	})
	req, err := http.NewRequest(http.MethodPost, base+"/sendMessage", bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}

// ---- email（SMTP）----（url 形如 smtp://user:pass@host:port，chat_id 为收件人）

func sendEmail(n *model.Notification, title, content string) error {
	u, err := url.Parse(n.URL)
	if err != nil || u.Scheme != "smtp" {
		return fmt.Errorf("email: invalid smtp url")
	}
	host := u.Host
	user := u.User.Username()
	pass, _ := u.User.Password()
	to := n.ChatID
	if to == "" {
		return fmt.Errorf("email: no recipient (chat_id)")
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		user, to, title, content)

	auth := smtp.PlainAuth("", user, pass, strings.Split(host, ":")[0])
	if err := smtp.SendMail(host, auth, user, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("email send failed: %w", err)
	}
	return nil
}

// ---- serverchan（https://sct.ftqq.com）----

func sendServerChan(n *model.Notification, title, content string) error {
	base := strings.TrimSuffix(n.URL, "/")
	if base == "" {
		base = "https://sctapi.ftqq.com"
	}
	form := url.Values{"title": {title}, "desp": {content}}
	req, err := http.NewRequest(http.MethodPost, base+"/send", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return doRequest(req)
}

// ---- 公共 ----

// httpClient 校验证书链（不再全局跳过 TLS 验证，防止通知凭据被中间人窃取）。
// 若使用自签端点，应配置受信 CA 而不是关闭校验。
var httpClient = &http.Client{Timeout: 10 * time.Second}

// doRequest 执行 HTTP 请求；传输错误或 4xx/5xx 视为失败（供重试判定）。
func doRequest(req *http.Request) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notification send failed: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("notification endpoint returned %d", resp.StatusCode)
	}
	return nil
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
