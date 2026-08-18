package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/server/internal/plugin"
)

// requireAdmin 管理接口必须由 admin 调用。
func requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := principalFromContext(c)
		if p == nil || !p.IsAdmin {
			fail(c, http.StatusForbidden, "admin only")
			c.Abort()
			return
		}
		c.Next()
	}
}

// pluginRouteMaxBody 插件 route 请求体上限。
const pluginRouteMaxBody = 4 << 20

// callPluginRPC 调用插件通过 argus.registerRPC 暴露的 RPC 方法（admin）。
func (s *Server) callPluginRPC(c *gin.Context) {
	if s.Plugins == nil {
		fail(c, http.StatusNotFound, "plugin manager disabled")
		return
	}
	name := c.Param("name")
	method := c.Param("method")
	var params any
	if err := c.ShouldBindJSON(&params); err != nil && err != io.EOF {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if !s.Plugins.Has(name) {
		fail(c, http.StatusNotFound, "plugin not found")
		return
	}
	result, err := s.Plugins.CallRPC(name, method, params)
	if err != nil {
		fail(c, http.StatusBadGateway, err.Error())
		return
	}
	ok(c, gin.H{"result": result})
}

// dispatchPluginRoute 派发插件注册的 HTTP 路由（admin；method + path 精确匹配）。
func (s *Server) dispatchPluginRoute(c *gin.Context) {
	if s.Plugins == nil {
		fail(c, http.StatusNotFound, "plugin manager disabled")
		return
	}
	name := c.Param("name")
	path := c.Param("path")
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, pluginRouteMaxBody+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if len(body) > pluginRouteMaxBody {
		fail(c, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	headers := map[string]string{}
	for k, v := range c.Request.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	req := &plugin.RouteRequest{
		Method:  c.Request.Method,
		Path:    path,
		Query:   c.Request.URL.Query(),
		Headers: headers,
		Body:    string(body),
	}
	result, err := s.Plugins.DispatchRoute(name, c.Request.Method, path, req)
	if err != nil {
		fail(c, http.StatusNotFound, err.Error())
		return
	}
	for k, v := range result.Headers {
		c.Header(k, v)
	}
	status := result.StatusCode
	if status <= 0 {
		status = http.StatusOK
	}
	if status == http.StatusNoContent {
		c.Status(status)
		return
	}
	c.Data(status, detectContentType(result.Body, result.Headers), []byte(result.Body))
}

// getPluginConfig 读取插件配置（admin；manifest 默认值 + 覆盖值合并）。
func (s *Server) getPluginConfig(c *gin.Context) {
	if s.Plugins == nil {
		fail(c, http.StatusNotFound, "plugin manager disabled")
		return
	}
	name := c.Param("name")
	if !s.Plugins.Has(name) {
		fail(c, http.StatusNotFound, "plugin not found")
		return
	}
	ok(c, gin.H{"config": s.Plugins.Config(name)})
}

// setPluginConfig 保存插件配置（admin）。
func (s *Server) setPluginConfig(c *gin.Context) {
	if s.Plugins == nil {
		fail(c, http.StatusNotFound, "plugin manager disabled")
		return
	}
	name := c.Param("name")
	var req struct {
		Config map[string]any `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.Plugins.SetConfig(name, req.Config); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	s.auditLog(c, "plugin.config", name)
	ok(c, gin.H{"ok": true})
}

// detectContentType 插件响应体 MIME 嗅探（显式 Content-Type 优先）。
func detectContentType(body string, headers map[string]string) string {
	if ct, ok := headers["Content-Type"]; ok && ct != "" {
		return ct
	}
	if ct, ok := headers["content-type"]; ok && ct != "" {
		return ct
	}
	if strings.TrimSpace(body) == "" {
		return "text/plain; charset=utf-8"
	}
	return http.DetectContentType([]byte(body))
}

// listPlugins 插件列表（admin），附带宿主 API 暴露的 RPC 方法 / 路由。
func (s *Server) listPlugins(c *gin.Context) {
	if s.Plugins == nil {
		ok(c, gin.H{"plugins": []any{}})
		return
	}
	list := s.Plugins.List()
	type pluginView struct {
		*plugin.Plugin
		RPCs   []string `json:"rpcs"`
		Routes []string `json:"routes"`
	}
	out := make([]pluginView, 0, len(list))
	for _, p := range list {
		out = append(out, pluginView{Plugin: p, RPCs: s.Plugins.RPCs(p.Name), Routes: s.Plugins.Routes(p.Name)})
	}
	ok(c, gin.H{"plugins": out})
}

// togglePlugin 启停插件（admin）。
func (s *Server) togglePlugin(c *gin.Context) {
	if s.Plugins == nil {
		fail(c, http.StatusNotFound, "plugin manager disabled")
		return
	}
	name := c.Param("name")
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if !s.Plugins.SetEnabled(name, req.Enabled) {
		fail(c, http.StatusNotFound, "plugin not found")
		return
	}
	ok(c, gin.H{"ok": true})
}

// runPluginNow 立即执行插件一次。
func (s *Server) runPluginNow(c *gin.Context) {
	if s.Plugins == nil {
		fail(c, http.StatusNotFound, "plugin manager disabled")
		return
	}
	name := c.Param("name")
	if err := s.Plugins.Run(name); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, gin.H{"ok": true})
}

// approvePlugin 批准插件高危权限（管理员）。
func (s *Server) approvePlugin(c *gin.Context) {
	if s.Plugins == nil {
		fail(c, http.StatusNotFound, "plugin manager disabled")
		return
	}
	name := c.Param("name")
	var req struct {
		Approved bool `json:"approved"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if !s.Plugins.SetApproved(name, req.Approved) {
		fail(c, http.StatusNotFound, "plugin not found")
		return
	}
	s.auditLog(c, "plugin.approve", name)
	ok(c, gin.H{"ok": true})
}

// deletePlugin 删除插件。
func (s *Server) deletePlugin(c *gin.Context) {
	if s.Plugins == nil {
		fail(c, http.StatusNotFound, "plugin manager disabled")
		return
	}
	name := c.Param("name")
	if !s.Plugins.Delete(name) {
		fail(c, http.StatusNotFound, "plugin not found")
		return
	}
	ok(c, gin.H{"ok": true})
}
