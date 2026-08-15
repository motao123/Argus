package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// listPluginMarket 市场插件列表。
func (s *Server) listPluginMarket(c *gin.Context) {
	if s.Plugins == nil {
		ok(c, gin.H{"plugins": []any{}})
		return
	}
	ok(c, gin.H{"plugins": s.Plugins.ListMarket()})
}

// installPlugin 从市场安装插件。
func (s *Server) installPlugin(c *gin.Context) {
	if s.Plugins == nil {
		fail(c, http.StatusNotFound, "plugin manager disabled")
		return
	}
	name := c.Param("name")
	if err := s.Plugins.InstallFromMarket(name); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.Plugins.Load()
	s.auditLog(c, "plugin.install", name)
	ok(c, gin.H{"ok": true})
}
