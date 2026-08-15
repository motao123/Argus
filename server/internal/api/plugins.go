package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// listPlugins 插件列表。
func (s *Server) listPlugins(c *gin.Context) {
	if s.Plugins == nil {
		ok(c, gin.H{"plugins": []any{}})
		return
	}
	ok(c, gin.H{"plugins": s.Plugins.List()})
}

// togglePlugin 启停插件。
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
