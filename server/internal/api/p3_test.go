package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"

	"github.com/motao123/Argus/server/internal/model"
)

// TestTempShareKey 私有站点模式下临时分享密钥：
// 正确密钥 + 未过期可访问公开端点；错误/过期密钥被拒。
func TestTempShareKey(t *testing.T) {
	e := newAuthzEnv(t)
	secret := "share-secret-123"
	sum := sha256.Sum256([]byte(secret))
	e.srv.DB.Create(&model.Setting{Key: SettingForceAuth, Value: "1"})
	e.srv.DB.Create(&model.Setting{Key: SettingTempShareKey, Value: hex.EncodeToString(sum[:])})
	e.srv.DB.Create(&model.Setting{Key: SettingTempShareExpiresAt, Value: time.Now().Add(1 * time.Hour).Format(time.RFC3339)})

	// 正确密钥 → 非 401（force_auth 放行；服务器无指标数据时为 404，鉴权已通过）
	if w := e.do(t, http.MethodGet, "/servers/1/metrics?temp_key="+secret, "", ""); w.Code == http.StatusUnauthorized {
		t.Fatalf("valid temp key rejected: got %d", w.Code)
	}
	// 错误密钥 → 401（force_auth 开启）
	if w := e.do(t, http.MethodGet, "/servers/1/metrics?temp_key=wrong", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid temp key: got %d, want 401", w.Code)
	}
	// 无密钥 → 401
	if w := e.do(t, http.MethodGet, "/servers/1/metrics", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no temp key: got %d, want 401", w.Code)
	}
	// 过期 → 401
	e.srv.DB.Model(&model.Setting{}).Where("key = ?", SettingTempShareExpiresAt).Update("value", time.Now().Add(-1*time.Hour).Format(time.RFC3339))
	if w := e.do(t, http.MethodGet, "/servers/1/metrics?temp_key="+secret, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired temp key: got %d, want 401", w.Code)
	}
}

// execRequest 带身份与可选 X-2FA-Code 头发起 exec 请求。
func execRequest(e *authzTestEnv, token, twoFACode, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/servers/1/exec", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if twoFACode != "" {
		req.Header.Set("X-2FA-Code", twoFACode)
	}
	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.POST("/servers/:id/exec", requireScope(ScopeServerExec), e.srv.serverExec)
	r.ServeHTTP(w, req)
	return w
}

// TestSensitive2FAExec 敏感操作二次验证：
// 启用 2FA 的 JWT 用户执行命令缺码/错码 → 428；PAT 豁免；JWT 用户禁用 2FA 无需验证。
func TestSensitive2FAExec(t *testing.T) {
	e := newAuthzEnv(t)
	alice := e.alice
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Argus", AccountName: alice.Username})
	if err != nil {
		t.Fatal(err)
	}
	e.srv.DB.Model(alice).Updates(map[string]any{"two_fa_secret": key.Secret(), "two_fa_enabled": true})
	goodCode, _ := totp.GenerateCode(key.Secret(), time.Now())
	aliceTok := e.token(t, alice)

	// 无 2FA 码 → 428
	if w := execRequest(e, aliceTok, "", `{"command":"echo hi"}`); w.Code != http.StatusPreconditionRequired {
		t.Fatalf("no 2fa code: got %d, want 428", w.Code)
	}
	// 错误 2FA 码 → 428
	if w := execRequest(e, aliceTok, "000000", `{"command":"echo hi"}`); w.Code != http.StatusPreconditionRequired {
		t.Fatalf("bad 2fa code: got %d, want 428", w.Code)
	}
	// 正确码（无 agent 在线）→ 非 428（应为 offline/conflict 之类）
	if w := execRequest(e, aliceTok, goodCode, `{"command":"echo hi"}`); w.Code == http.StatusPreconditionRequired {
		t.Fatal("valid 2fa code should not return 428")
	}
	// 关闭 2FA → 无需验证码（无 agent → 非 428）
	e.srv.DB.Model(alice).Update("two_fa_enabled", false)
	if w := execRequest(e, aliceTok, "", `{"command":"echo hi"}`); w.Code == http.StatusPreconditionRequired {
		t.Fatal("2fa disabled should not require code")
	}
	// PAT 豁免：启用 2FA 的 PAT 不带码 → 非 428
	e.srv.DB.Model(alice).Update("two_fa_enabled", true)
	pat := e.createPAT(t, alice, []string{ScopeServerExec}, "")
	if w := execRequest(e, pat, "", `{"command":"echo hi"}`); w.Code == http.StatusPreconditionRequired {
		t.Fatal("PAT should bypass sensitive 2FA")
	}
}
