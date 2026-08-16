package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestOAuthCodeExchange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}
	r := gin.New()
	r.POST("/consume", s.consumeOAuthCode)

	consume := func(code string) (int, string) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/consume", strings.NewReader(`{"code":"`+code+`"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}

	// 无效 code → 401
	if code, _ := consume("nope"); code != http.StatusUnauthorized {
		t.Fatalf("invalid code: got %d want 401", code)
	}
	// 空 code → 400
	if code, _ := consume(""); code != http.StatusBadRequest {
		t.Fatalf("empty code: got %d want 400", code)
	}
	// 发放 → 消费成功 → 再次消费失败（单次使用）
	issued := issueOAuthCode("jwt-token-abc", time.Minute)
	code, body := consume(issued)
	if code != http.StatusOK {
		t.Fatalf("valid code: got %d want 200", code)
	}
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil || resp.Data.Token != "jwt-token-abc" {
		t.Fatalf("token mismatch: %s", body)
	}
	if code, _ := consume(issued); code != http.StatusUnauthorized {
		t.Fatalf("reuse code: got %d want 401", code)
	}
	// 过期 code → 401
	expired := issueOAuthCode("t", -time.Second)
	if code, _ := consume(expired); code != http.StatusUnauthorized {
		t.Fatalf("expired code: got %d want 401", code)
	}
}
