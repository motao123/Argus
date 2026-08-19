package api

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/config"
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

func TestOAuthRedirectURIUsesPublicURLWhenConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name, publicURL, requestURL, provider, want string
	}{
		{
			name: "public https with path", publicURL: "https://argus.example.com/panel/", requestURL: "http://internal:8080/oauth", provider: "github",
			want: "https://argus.example.com/panel/api/v1/auth/oauth/github/callback",
		},
		{
			name: "fallback tls", requestURL: "https://argus.example.com/oauth", provider: "oidc/provider",
			want: "https://argus.example.com/api/v1/auth/oauth/oidc%2Fprovider/callback",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Cfg: &config.Config{PublicURL: tc.publicURL}}
			r := gin.New()
			r.GET("/oauth", func(c *gin.Context) {
				if got := s.oauthRedirectURI(c, tc.provider); got != tc.want {
					t.Fatalf("redirect URI = %q, want %q", got, tc.want)
				}
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.requestURL, nil)
			if strings.HasPrefix(tc.requestURL, "https://") {
				req.TLS = &tls.ConnectionState{}
			}
			r.ServeHTTP(w, req)
		})
	}
}

func TestOAuthStateCookieSecureByScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, target, publicURL string
		tls, secure             bool
	}{
		{name: "http", target: "http://localhost/oauth"},
		{name: "https", target: "https://example.com/oauth", tls: true, secure: true},
		{name: "proxy tls termination", target: "http://internal:8080/oauth", publicURL: "https://argus.example.com", secure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Cfg: &config.Config{PublicURL: tc.publicURL}}
			r := gin.New()
			r.GET("/oauth", func(c *gin.Context) {
				c.SetCookie("oauth_state", "state", 600, "/", "", s.oauthCookieSecure(c), true)
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			r.ServeHTTP(w, req)
			cookie := w.Header().Get("Set-Cookie")
			if strings.Contains(cookie, "Secure") != tc.secure {
				t.Fatalf("cookie=%q secure=%v", cookie, tc.secure)
			}
		})
	}
}
