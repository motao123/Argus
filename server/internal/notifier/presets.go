package notifier

// 预设通知渠道（对标 nezha webhook 预设：钉钉、企业微信、飞书、Slack、wxpusher、Matrix）。
//
// 每种预设渠道在 Notification.Extra（JSON）中保存专属配置结构（token/secret/keyword/uid 等），
// url 字段可覆盖默认端点（测试注入、自建网关或代理场景）。
// 签名/关键词规则按各厂商官方 API 实现：
//   - 钉钉：安全设置「加签」timestamp+"\n"+secret → HMAC-SHA256 → base64 追加到 URL；
//     「自定义关键词」要求消息文本包含关键词。
//   - 飞书：签名校验 timestamp+"\n"+secret → HMAC-SHA256 → base64 写入 body 的 timestamp/sign 字段；
//     「自定义关键词」要求消息文本包含关键词。
//   - 企业微信：markdown 消息 + mentioned_list / mentioned_mobile_list。
//   - Slack：Incoming Webhook JSON 负载（text/channel/username/icon_emoji）。
//   - wxpusher：/api/send/message JSON 负载（appToken/content/summary/contentType/uids/topicIds）。
//   - Matrix：客户端-服务器 API send 事务，Authorization: Bearer <access_token>。

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/motao123/Argus/server/internal/model"
)

// ValidTypes 全部受支持的通知渠道类型。
var ValidTypes = map[string]bool{
	"webhook": true, "bark": true, "telegram": true, "email": true,
	"serverchan": true, "javascript": true,
	"dingtalk": true, "wecom": true, "feishu": true,
	"slack": true, "wxpusher": true, "matrix": true,
}

// IsValidType 判断通知渠道类型是否受支持（API 创建/更新校验用）。
func IsValidType(t string) bool {
	return ValidTypes[t]
}

// parseExtra 解析渠道专属 JSON 配置（Extra 为空或非法时返回错误）。
func parseExtra[T any](n *model.Notification) (*T, error) {
	var c T
	if err := json.Unmarshal([]byte(n.Extra), &c); err != nil {
		return nil, fmt.Errorf("%s: invalid config: %w", n.Type, err)
	}
	return &c, nil
}

// postJSON 发送 JSON POST 请求（预设渠道共用）。
func postJSON(endpoint string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}

// hmacBase64 计算 HMAC-SHA256(msg, key) 的 base64 编码（钉钉/飞书加签共用）。
func hmacBase64(key, msg string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ---- 钉钉自定义机器人 ----
// 文档：https://open.dingtalk.com/document/robots/custom-robot-access

// DingTalkConfig 钉钉机器人专属配置。
type DingTalkConfig struct {
	AccessToken string   `json:"access_token"` // 必填：机器人 access_token
	Secret      string   `json:"secret"`       // 安全设置「加签」密钥；空则不签名
	Keyword     string   `json:"keyword"`      // 安全设置「自定义关键词」；消息须包含
	AtAll       bool     `json:"at_all"`       // 是否 @所有人
	AtMobiles   []string `json:"at_mobiles"`   // @指定手机号
}

func sendDingTalk(n *model.Notification, title, content string) error {
	cfg, err := parseExtra[DingTalkConfig](n)
	if err != nil {
		return err
	}
	if cfg.AccessToken == "" {
		return fmt.Errorf("dingtalk: access_token required")
	}
	endpoint := n.URL
	if endpoint == "" {
		endpoint = "https://oapi.dingtalk.com/robot/send?access_token=" + url.QueryEscape(cfg.AccessToken)
	}
	text := "### " + title + "\n" + content
	if cfg.Keyword != "" && !strings.Contains(text, cfg.Keyword) {
		text += "\n" + cfg.Keyword
	}
	body := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": title,
			"text":  text,
		},
		"at": map[string]any{
			"atMobiles": cfg.AtMobiles,
			"isAtAll":   cfg.AtAll,
		},
	}
	// 加签：timestamp+"\n"+secret → HMAC-SHA256 → base64，追加到 URL
	if cfg.Secret != "" {
		ts := time.Now().UnixMilli()
		sign := hmacBase64(cfg.Secret, fmt.Sprintf("%d\n%s", ts, cfg.Secret))
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		endpoint = fmt.Sprintf("%s%stimestamp=%d&sign=%s", endpoint, sep, ts, url.QueryEscape(sign))
	}
	return postJSON(endpoint, body)
}

// ---- 企业微信群机器人 ----
// 文档：https://developer.work.weixin.qq.com/document/path/91770

// WeComConfig 企业微信机器人专属配置。
type WeComConfig struct {
	Key                 string   `json:"key"`                   // 必填：机器人 webhook key
	MentionedList       []string `json:"mentioned_list"`        // @成员 userid
	MentionedMobileList []string `json:"mentioned_mobile_list"` // @成员手机号
}

func sendWeCom(n *model.Notification, title, content string) error {
	cfg, err := parseExtra[WeComConfig](n)
	if err != nil {
		return err
	}
	if cfg.Key == "" {
		return fmt.Errorf("wecom: key required")
	}
	endpoint := n.URL
	if endpoint == "" {
		endpoint = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + url.QueryEscape(cfg.Key)
	}
	body := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"content":               "## " + title + "\n" + content,
			"mentioned_list":        cfg.MentionedList,
			"mentioned_mobile_list": cfg.MentionedMobileList,
		},
	}
	return postJSON(endpoint, body)
}

// ---- 飞书自定义机器人 ----
// 文档：https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot

// FeishuConfig 飞书机器人专属配置。
type FeishuConfig struct {
	Token   string `json:"token"`    // 必填：机器人 webhook token
	Secret  string `json:"secret"`   // 签名校验密钥；空则不签名
	Keyword string `json:"keyword"`  // 自定义关键词；消息须包含
	MsgType string `json:"msg_type"` // text / interactive（默认 text）
}

func sendFeishu(n *model.Notification, title, content string) error {
	cfg, err := parseExtra[FeishuConfig](n)
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("feishu: token required")
	}
	endpoint := n.URL
	if endpoint == "" {
		endpoint = "https://open.feishu.cn/open-apis/bot/v2/hook/" + url.PathEscape(cfg.Token)
	}
	text := title + "\n" + content
	if cfg.Keyword != "" && !strings.Contains(text, cfg.Keyword) {
		text += "\n" + cfg.Keyword
	}
	var body map[string]any
	switch cfg.MsgType {
	case "interactive":
		body = map[string]any{
			"msg_type": "interactive",
			"card": map[string]any{
				"header": map[string]any{
					"template": "blue",
					"title":    map[string]any{"tag": "plain_text", "content": title},
				},
				"elements": []any{
					map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": text}},
				},
			},
		}
	default:
		body = map[string]any{
			"msg_type": "text",
			"content":  map[string]any{"text": text},
		}
	}
	// 签名校验：timestamp+"\n"+secret → HMAC-SHA256 → base64，写入 body
	if cfg.Secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		body["timestamp"] = ts
		body["sign"] = hmacBase64(cfg.Secret, ts+"\n"+cfg.Secret)
	}
	return postJSON(endpoint, body)
}

// ---- Slack Incoming Webhook ----
// 文档：https://api.slack.com/messaging/webhooks

// SlackConfig Slack 专属配置。
type SlackConfig struct {
	WebhookURL string `json:"webhook_url"` // 必填：Incoming Webhook URL
	Channel    string `json:"channel"`     // 可选：覆盖默认频道
	Username   string `json:"username"`    // 可选：覆盖默认用户名
	IconEmoji  string `json:"icon_emoji"`  // 可选：覆盖默认图标
}

func sendSlack(n *model.Notification, title, content string) error {
	cfg, err := parseExtra[SlackConfig](n)
	if err != nil {
		return err
	}
	endpoint := n.URL
	if endpoint == "" {
		endpoint = cfg.WebhookURL
	}
	if endpoint == "" {
		return fmt.Errorf("slack: webhook_url required")
	}
	body := map[string]any{"text": title + "\n" + content}
	if cfg.Channel != "" {
		body["channel"] = cfg.Channel
	}
	if cfg.Username != "" {
		body["username"] = cfg.Username
	}
	if cfg.IconEmoji != "" {
		body["icon_emoji"] = cfg.IconEmoji
	}
	return postJSON(endpoint, body)
}

// ---- wxpusher ----
// 文档：https://wxpusher.zjiecode.com/docs/

// WxPusherConfig wxpusher 专属配置。
type WxPusherConfig struct {
	AppToken    string   `json:"app_token"`    // 必填：应用 token
	UIDs        []string `json:"uids"`         // 接收者 uid（与 topic_ids 至少一个）
	TopicIDs    []int64  `json:"topic_ids"`    // 主题 id
	Summary     string   `json:"summary"`      // 摘要；默认取 title
	ContentType int      `json:"content_type"` // 1 文本 / 2 链接 / 3 markdown；默认 1
}

func sendWxPusher(n *model.Notification, title, content string) error {
	cfg, err := parseExtra[WxPusherConfig](n)
	if err != nil {
		return err
	}
	if cfg.AppToken == "" {
		return fmt.Errorf("wxpusher: app_token required")
	}
	if len(cfg.UIDs) == 0 && len(cfg.TopicIDs) == 0 {
		return fmt.Errorf("wxpusher: uids or topic_ids required")
	}
	endpoint := n.URL
	if endpoint == "" {
		endpoint = "https://wxpusher.zjiecode.com/api/send/message"
	}
	summary := cfg.Summary
	if summary == "" {
		summary = title
	}
	contentType := cfg.ContentType
	if contentType == 0 {
		contentType = 1
	}
	body := map[string]any{
		"appToken":    cfg.AppToken,
		"content":     title + "\n" + content,
		"summary":     summary,
		"contentType": contentType,
		"uids":        cfg.UIDs,
		"topicIds":    cfg.TopicIDs,
		"url":         "",
	}
	return postJSON(endpoint, body)
}

// ---- Matrix 客户端-服务器 API ----
// 文档：https://spec.matrix.org/latest/client-server-api/#send-message-event

// MatrixConfig Matrix 专属配置。
type MatrixConfig struct {
	Homeserver  string `json:"homeserver"`   // 必填：如 https://matrix.org
	AccessToken string `json:"access_token"` // 必填：用户 access token
	RoomID      string `json:"room_id"`      // 必填：目标房间 ID
}

func sendMatrix(n *model.Notification, title, content string) error {
	cfg, err := parseExtra[MatrixConfig](n)
	if err != nil {
		return err
	}
	if cfg.Homeserver == "" || cfg.RoomID == "" {
		return fmt.Errorf("matrix: homeserver and room_id required")
	}
	if cfg.AccessToken == "" {
		return fmt.Errorf("matrix: access_token required")
	}
	endpoint := n.URL
	if endpoint == "" {
		endpoint = strings.TrimSuffix(cfg.Homeserver, "/") +
			"/_matrix/client/r0/rooms/" + url.PathEscape(cfg.RoomID) +
			"/send/m.room.message/" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	body := map[string]any{
		"msgtype": "m.text",
		"body":    title + "\n" + content,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.AccessToken)
	return doRequest(req)
}
