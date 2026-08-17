package notifier

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/motao123/Argus/server/internal/model"
)

// captureServer 启动一个捕获请求（方法/URL/头/体）的 httptest 服务器，返回服务器与捕获结果。
func captureServer(t *testing.T) (*httptest.Server, *http.Request) {
	t.Helper()
	var captured http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.Method = r.Method
		captured.URL = r.URL
		captured.Header = r.Header.Clone()
		captured.Body = io.NopCloser(strings.NewReader(string(body)))
		captured.ContentLength = r.ContentLength
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body is not a JSON object: %v\nbody=%s", err, b)
	}
	return m
}

func jsonExtra(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- 钉钉 ----

func TestDingTalkRequestWithSignAndKeyword(t *testing.T) {
	srv, got := captureServer(t)
	extra := jsonExtra(t, DingTalkConfig{
		AccessToken: "tok123",
		Secret:      "sec456",
		Keyword:     "Argus报警",
		AtAll:       true,
		AtMobiles:   []string{"13800000000", "13900000000"},
	})
	n := &model.Notification{Type: "dingtalk", URL: srv.URL, Extra: extra}
	if err := send(n, "服务器离线", "host1 超过 60s 无心跳", nil); err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", got.Method)
	}
	if ct := got.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	// 加签：URL 追加 timestamp 与 sign（base64）
	q := got.URL.Query()
	if q.Get("timestamp") == "" || q.Get("sign") == "" {
		t.Fatalf("missing sign params: %v", q)
	}
	if _, err := base64.StdEncoding.DecodeString(q.Get("sign")); err != nil {
		t.Fatalf("sign is not valid base64: %v", err)
	}

	body := readBody(t, got)
	if body["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v", body["msgtype"])
	}
	md, _ := body["markdown"].(map[string]any)
	if md == nil || md["title"] != "服务器离线" {
		t.Fatalf("markdown = %v", body["markdown"])
	}
	text, _ := md["text"].(string)
	if !strings.Contains(text, "Argus报警") {
		t.Fatalf("keyword not appended to text: %q", text)
	}
	if !strings.Contains(text, "host1 超过 60s 无心跳") {
		t.Fatalf("content missing in text: %q", text)
	}
	at, _ := body["at"].(map[string]any)
	if at == nil || at["isAtAll"] != true {
		t.Fatalf("at = %v", body["at"])
	}
	mobiles, _ := at["atMobiles"].([]any)
	if len(mobiles) != 2 || mobiles[0] != "13800000000" {
		t.Fatalf("atMobiles = %v", at["atMobiles"])
	}
}

func TestDingTalkMissingToken(t *testing.T) {
	srv, _ := captureServer(t)
	n := &model.Notification{Type: "dingtalk", URL: srv.URL, Extra: `{}`}
	if err := send(n, "t", "c", nil); err == nil || !strings.Contains(err.Error(), "access_token") {
		t.Fatalf("want access_token error, got %v", err)
	}
}

// ---- 企业微信 ----

func TestWeComRequest(t *testing.T) {
	srv, got := captureServer(t)
	extra := jsonExtra(t, WeComConfig{
		Key:                 "wecom-key",
		MentionedList:       []string{"zhangsan"},
		MentionedMobileList: []string{"13800000000"},
	})
	n := &model.Notification{Type: "wecom", URL: srv.URL, Extra: extra}
	if err := send(n, "磁盘告警", "/data 使用率 92%", nil); err != nil {
		t.Fatal(err)
	}
	body := readBody(t, got)
	if body["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v", body["msgtype"])
	}
	md, _ := body["markdown"].(map[string]any)
	if md == nil || !strings.HasPrefix(md["content"].(string), "## 磁盘告警") {
		t.Fatalf("markdown = %v", body["markdown"])
	}
	if got := md["mentioned_list"].([]any); len(got) != 1 || got[0] != "zhangsan" {
		t.Fatalf("mentioned_list = %v", md["mentioned_list"])
	}
	if got := md["mentioned_mobile_list"].([]any); len(got) != 1 || got[0] != "13800000000" {
		t.Fatalf("mentioned_mobile_list = %v", md["mentioned_mobile_list"])
	}
}

// ---- 飞书 ----

func TestFeishuTextRequestWithSignAndKeyword(t *testing.T) {
	srv, got := captureServer(t)
	extra := jsonExtra(t, FeishuConfig{
		Token:   "fs-token",
		Secret:  "fs-secret",
		Keyword: "Argus",
	})
	n := &model.Notification{Type: "feishu", URL: srv.URL, Extra: extra}
	if err := send(n, "服务异常", "端口 8080 无响应", nil); err != nil {
		t.Fatal(err)
	}
	body := readBody(t, got)
	if body["msg_type"] != "text" {
		t.Fatalf("msg_type = %v", body["msg_type"])
	}
	if body["timestamp"] == "" || body["sign"] == "" {
		t.Fatalf("sign fields missing: %v", body)
	}
	if _, err := base64.StdEncoding.DecodeString(body["sign"].(string)); err != nil {
		t.Fatalf("sign not base64: %v", err)
	}
	content, _ := body["content"].(map[string]any)
	text, _ := content["text"].(string)
	if !strings.Contains(text, "服务异常") || !strings.Contains(text, "端口 8080 无响应") || !strings.Contains(text, "Argus") {
		t.Fatalf("content text = %q", text)
	}
}

func TestFeishuInteractiveCard(t *testing.T) {
	srv, got := captureServer(t)
	extra := jsonExtra(t, FeishuConfig{Token: "fs-token", MsgType: "interactive"})
	n := &model.Notification{Type: "feishu", URL: srv.URL, Extra: extra}
	if err := send(n, "证书告警", "cert 将于 7 天后过期", nil); err != nil {
		t.Fatal(err)
	}
	body := readBody(t, got)
	if body["msg_type"] != "interactive" {
		t.Fatalf("msg_type = %v", body["msg_type"])
	}
	card, _ := body["card"].(map[string]any)
	header, _ := card["header"].(map[string]any)
	title, _ := header["title"].(map[string]any)
	if title["content"] != "证书告警" {
		t.Fatalf("card header title = %v", header)
	}
	elements, _ := card["elements"].([]any)
	div, _ := elements[0].(map[string]any)
	text, _ := div["text"].(map[string]any)
	if !strings.Contains(text["content"].(string), "证书告警") {
		t.Fatalf("card element text = %v", text)
	}
}

// ---- Slack ----

func TestSlackRequest(t *testing.T) {
	srv, got := captureServer(t)
	extra := jsonExtra(t, SlackConfig{
		WebhookURL: srv.URL,
		Channel:    "#ops",
		Username:   "Argus",
		IconEmoji:  ":siren:",
	})
	n := &model.Notification{Type: "slack", Extra: extra}
	if err := send(n, "CPU 高负载", "load1 = 8.2", nil); err != nil {
		t.Fatal(err)
	}
	body := readBody(t, got)
	if body["text"] != "CPU 高负载\nload1 = 8.2" {
		t.Fatalf("text = %v", body["text"])
	}
	if body["channel"] != "#ops" || body["username"] != "Argus" || body["icon_emoji"] != ":siren:" {
		t.Fatalf("body = %v", body)
	}
}

func TestSlackMissingWebhook(t *testing.T) {
	n := &model.Notification{Type: "slack", Extra: `{}`}
	if err := send(n, "t", "c", nil); err == nil || !strings.Contains(err.Error(), "webhook_url") {
		t.Fatalf("want webhook_url error, got %v", err)
	}
}

// ---- wxpusher ----

func TestWxPusherRequest(t *testing.T) {
	srv, got := captureServer(t)
	extra := jsonExtra(t, WxPusherConfig{
		AppToken:    "AT_xxx",
		UIDs:        []string{"UID_1", "UID_2"},
		TopicIDs:    []int64{123},
		ContentType: 3,
	})
	n := &model.Notification{Type: "wxpusher", URL: srv.URL, Extra: extra}
	if err := send(n, "流量告警", "本月已用 90%", nil); err != nil {
		t.Fatal(err)
	}
	body := readBody(t, got)
	if body["appToken"] != "AT_xxx" {
		t.Fatalf("appToken = %v", body["appToken"])
	}
	if body["content"] != "流量告警\n本月已用 90%" {
		t.Fatalf("content = %v", body["content"])
	}
	if body["summary"] != "流量告警" {
		t.Fatalf("summary default = %v, want title", body["summary"])
	}
	if body["contentType"] != float64(3) {
		t.Fatalf("contentType = %v", body["contentType"])
	}
	if uids := body["uids"].([]any); len(uids) != 2 || uids[1] != "UID_2" {
		t.Fatalf("uids = %v", body["uids"])
	}
	if topics := body["topicIds"].([]any); len(topics) != 1 || topics[0] != float64(123) {
		t.Fatalf("topicIds = %v", body["topicIds"])
	}
}

func TestWxPusherMissingTarget(t *testing.T) {
	srv, _ := captureServer(t)
	n := &model.Notification{Type: "wxpusher", URL: srv.URL, Extra: `{"app_token":"AT_xxx"}`}
	if err := send(n, "t", "c", nil); err == nil || !strings.Contains(err.Error(), "uids or topic_ids") {
		t.Fatalf("want uids error, got %v", err)
	}
}

// ---- Matrix ----

func TestMatrixRequest(t *testing.T) {
	srv, got := captureServer(t)
	extra := jsonExtra(t, MatrixConfig{
		Homeserver:  "https://matrix.example.org",
		AccessToken: "syt_abc",
		RoomID:      "!room:example.org",
	})
	n := &model.Notification{Type: "matrix", URL: srv.URL, Extra: extra}
	if err := send(n, "节点离线", "node-01 无心跳", nil); err != nil {
		t.Fatal(err)
	}
	if auth := got.Header.Get("Authorization"); auth != "Bearer syt_abc" {
		t.Fatalf("authorization = %q", auth)
	}
	body := readBody(t, got)
	if body["msgtype"] != "m.text" || body["body"] != "节点离线\nnode-01 无心跳" {
		t.Fatalf("body = %v", body)
	}
}

func TestMatrixMissingConfig(t *testing.T) {
	srv, _ := captureServer(t)
	n := &model.Notification{Type: "matrix", URL: srv.URL, Extra: `{"homeserver":"https://x"}`}
	if err := send(n, "t", "c", nil); err == nil || !strings.Contains(err.Error(), "room_id") {
		t.Fatalf("want room_id error, got %v", err)
	}
}

// ---- 类型校验 ----

func TestIsValidType(t *testing.T) {
	for _, ty := range []string{"webhook", "bark", "telegram", "email", "serverchan", "javascript",
		"dingtalk", "wecom", "feishu", "slack", "wxpusher", "matrix"} {
		if !IsValidType(ty) {
			t.Errorf("IsValidType(%q) = false, want true", ty)
		}
	}
	for _, ty := range []string{"", "sms", "weixin"} {
		if IsValidType(ty) {
			t.Errorf("IsValidType(%q) = true, want false", ty)
		}
	}
}
