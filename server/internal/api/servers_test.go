package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol"
)

// configDo 发起带身份的「配置下发」请求（复用 newAuthzEnv 的测试环境）。
func (e *authzTestEnv) configDo(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("{}")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("", e.srv.authMiddleware())
	authed.POST("/servers/:id/config", requireScope(ScopeServerWrite), e.srv.serverApplyConfig)
	r.ServeHTTP(w, req)
	return w
}

func TestApplyConfigError(t *testing.T) {
	// 被禁能力 → 稳定码 capability.disabled
	status, msg, apiCode := applyConfigError(protocol.NewError(protocol.ErrCapabilityDisabled, "capability disabled"))
	if status != http.StatusBadGateway || msg != "capability disabled" || apiCode != "capability.disabled" {
		t.Fatalf("capability disabled: got (%d, %q, %q)", status, msg, apiCode)
	}
	// 其它 Agent 错误 → 回退原始消息，无稳定码
	status, msg, apiCode = applyConfigError(protocol.NewError(protocol.ErrInternal, "boom"))
	if status != http.StatusBadGateway || msg != "boom" || apiCode != "" {
		t.Fatalf("generic error: got (%d, %q, %q)", status, msg, apiCode)
	}
}

func TestServerApplyConfigValidation(t *testing.T) {
	e := newAuthzEnv(t)
	aliceToken := e.token(t, e.alice)

	// 跨租户 → 403
	w := e.configDo(t, http.MethodPost, "/servers/"+itoa(e.bobS.ID)+"/config", aliceToken, `{}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-owner: got %d want 403", w.Code)
	}

	// 离线（无 Agent 连接）→ 409 + 稳定码 server.offline
	w = e.configDo(t, http.MethodPost, "/servers/"+itoa(e.aliceS.ID)+"/config", aliceToken, `{}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"server.offline"`) {
		t.Fatalf("offline: got %d %s", w.Code, w.Body.String())
	}

	// 能力名不合法 → 400（校验在离线判定之前执行）
	w = e.configDo(t, http.MethodPost, "/servers/"+itoa(e.aliceS.ID)+"/config", aliceToken, `{"capabilities":{"bogus":true}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown capability: got %d want 400: %s", w.Code, w.Body.String())
	}

	// 能力对象非对象（如数组）→ 400
	w = e.configDo(t, http.MethodPost, "/servers/"+itoa(e.aliceS.ID)+"/config", aliceToken, `{"capabilities":["metrics"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("capabilities array: got %d want 400: %s", w.Code, w.Body.String())
	}

	// 合法能力 + include/exclude glob → 校验通过，落到离线 409（而非 400）
	w = e.configDo(t, http.MethodPost, "/servers/"+itoa(e.aliceS.ID)+"/config", aliceToken,
		`{"capabilities":{"metrics":true,"probe":false},"interface_include":["eth*"],"mount_exclude":["/proc/*"]}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("valid config: got %d want 409 (offline): %s", w.Code, w.Body.String())
	}
}

func TestParseCapabilitiesWireFormat(t *testing.T) {
	// 与前端提交结构一致：7 个布尔能力 + include/exclude 数组
	body := `{"capabilities":{"metrics":true,"probe":true,"command":true,"terminal":true,"files":true,"upgrade":true,"nat":false},
	"interface_include":["eth0","eth1"],"interface_exclude":[],"mount_include":null,"mount_exclude":["/tmp"]}`
	var req struct {
		Capabilities     json.RawMessage `json:"capabilities"`
		InterfaceInclude []string        `json:"interface_include"`
		InterfaceExclude []string        `json:"interface_exclude"`
		MountInclude     []string        `json:"mount_include"`
		MountExclude     []string        `json:"mount_exclude"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	caps, err := protocol.ParseCapabilities(req.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if !caps.Metrics || !caps.Command || caps.NAT {
		t.Fatalf("wire caps = %+v", caps)
	}
	if len(req.InterfaceInclude) != 2 || req.InterfaceInclude[0] != "eth0" {
		t.Fatalf("interface_include = %v", req.InterfaceInclude)
	}
	if req.MountInclude != nil {
		t.Fatalf("mount_include = %v, want nil", req.MountInclude)
	}
}
