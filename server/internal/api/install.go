package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/model"
)

// SettingInstallBaseURL 一键安装命令的基础 URL（HTTPS 反代场景覆盖自动推导）。
const SettingInstallBaseURL = "install_base_url"

// installCommand 用户模式：生成使用当前用户 Agent 密钥的一键安装命令，
// 适用于批量部署新服务器（每台机器执行同一条命令自动注册）。
func (s *Server) installCommand(c *gin.Context) {
	p := principalFromContext(c)
	var user model.User
	if err := s.DB.First(&user, p.UserID).Error; err != nil {
		fail(c, http.StatusNotFound, "user not found")
		return
	}
	if user.AgentSecret == "" {
		fail(c, http.StatusBadRequest, "user agent secret not configured")
		return
	}
	ok(c, gin.H{
		"command":    s.buildInstallCommand(c, user.AgentSecret),
		"script_url": s.installBaseURL(c) + "/install.sh",
		"ws_url":     s.installWSUrl(c),
	})
}

// serverInstallCommand 服务器模式：生成绑定指定服务器专属密钥的安装命令（重连/补装）。
func (s *Server) serverInstallCommand(c *gin.Context) {
	id := mustID(c)
	if _, ok := s.authorizeServer(c, id, ScopeServerRead); !ok {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	var srv model.Server
	if err := s.DB.First(&srv, id).Error; err != nil {
		fail(c, http.StatusNotFound, "server not found")
		return
	}
	if srv.Secret == "" {
		fail(c, http.StatusBadRequest, "server secret not configured")
		return
	}
	ok(c, gin.H{
		"command":    s.buildInstallCommand(c, srv.Secret),
		"script_url": s.installBaseURL(c) + "/install.sh",
		"ws_url":     s.installWSUrl(c),
		"server_id":  id,
	})
}

// installBaseURL 安装命令的基础 URL：设置项优先（反代 HTTPS），否则按请求 Host 推导。
func (s *Server) installBaseURL(c *gin.Context) string {
	if base := strings.TrimRight(s.GetSetting(SettingInstallBaseURL, ""), "/"); base != "" {
		return base
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

// installWSUrl 从基础 URL 推导 Agent WebSocket 地址（http→ws、https→wss）。
func (s *Server) installWSUrl(c *gin.Context) string {
	base := s.installBaseURL(c)
	u, err := url.Parse(base)
	if err != nil {
		return base + "/ws/agent"
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ws/agent"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// buildInstallCommand 组装哪吒风格的一键安装命令。
func (s *Server) buildInstallCommand(c *gin.Context, secret string) string {
	return "curl -fsSL " + s.installBaseURL(c) + "/install.sh | sh -s -- -s " +
		s.installWSUrl(c) + " -k " + secret
}
