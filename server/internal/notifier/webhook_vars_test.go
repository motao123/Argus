package notifier

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/model"
)

// TestWebhookBodyRendersContextVars 渠道级 Body 模板用统一上下文渲染：
// {{title}}/{{content}} 行为不变，新增 {{event}}/{{server.name}} 等变量自动可用，
// 未提供的变量渲染为空字符串（JSON 中为 ""，模板输出仍合法）。
func TestWebhookBodyRendersContextVars(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Notification{}, &model.NotificationDelivery{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Notification{
		ID: 1, Name: "n", Type: "webhook", URL: srv.URL, Method: "POST",
		Body: `{"title":{{title}},"content":{{content}},"event":{{event}},"name":{{server.name}},"id":{{server.id}},"ipv4":{{server.ipv4}},"missing":{{server.ipv6}},"rule":{{rule}},"time":{{time}}}`,
	}).Error; err != nil {
		t.Fatal(err)
	}

	vars := map[string]string{
		"event": "triggered", "server.name": "node-1", "server.id": "7",
		"server.ipv4": "10.0.0.1", "rule": "cpu-rule", "time": "2026-08-17 10:00:00",
		"title": "默认标题", "content": "默认正文",
	}
	q := NewQueue(db)
	if err := q.EnqueueCtx(&model.Notification{ID: 1}, "默认标题", "默认正文", 0, vars); err != nil {
		t.Fatal(err)
	}

	var d model.NotificationDelivery
	if err := db.First(&d).Error; err != nil {
		t.Fatal(err)
	}
	if d.Status != model.DeliverySent {
		t.Fatalf("status = %s, want sent", d.Status)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body not valid JSON: %q: %v", gotBody, err)
	}
	for k, want := range map[string]string{
		"title": "默认标题", "content": "默认正文", "event": "triggered",
		"name": "node-1", "id": "7", "ipv4": "10.0.0.1",
		"missing": "", "rule": "cpu-rule", "time": "2026-08-17 10:00:00",
	} {
		if body[k] != want {
			t.Errorf("body[%q] = %q, want %q (raw %q)", k, body[k], want, gotBody)
		}
	}
}

// TestWebhookBodyDefaultAndBackCompat 空 Body 回退默认模板（占位符不带引号，
// 值 JSON 转义后输出合法 JSON）；无上下文（旧调用 Enqueue）时 {{title}}/{{content}} 照常渲染。
func TestWebhookBodyDefaultAndBackCompat(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Notification{}, &model.NotificationDelivery{}); err != nil {
		t.Fatal(err)
	}
	// Body 为空：走默认 {"title":{{title}},"content":{{content}}}（先清空列默认值 '{}'）
	if err := db.Create(&model.Notification{ID: 1, Name: "n", Type: "webhook", URL: srv.URL}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Notification{}).Where("id = 1").Update("body", "").Error; err != nil {
		t.Fatal(err)
	}
	q := NewQueue(db)
	if err := q.Enqueue(&model.Notification{ID: 1}, "标题", "内容", 0); err != nil { // 旧签名，无上下文
		t.Fatal(err)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("default body not valid JSON: %q: %v", gotBody, err)
	}
	if body["title"] != "标题" || body["content"] != "内容" {
		t.Errorf("default body = %q", gotBody)
	}
	// 值含引号/换行时正确 JSON 转义（回归）
	if err := db.Model(&model.Notification{}).Where("id = 1").Update("body", `{"t":{{title}},"c":{{content}}}`).Error; err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(&model.Notification{ID: 1}, "a\"b", "line1\nline2", 0); err != nil {
		t.Fatal(err)
	}
	var body2 map[string]string
	if err := json.Unmarshal([]byte(gotBody), &body2); err != nil {
		t.Fatalf("escaped body not valid JSON: %q: %v", gotBody, err)
	}
	if body2["t"] != "a\"b" || body2["c"] != "line1\nline2" {
		t.Errorf("escaped body = %q", gotBody)
	}
}
