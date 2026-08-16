package api

import (
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/gin-gonic/gin"
)

// PProfHandler 受 admin 保护的 pprof 端点（借鉴 komari admin pprof）。
func (s *Server) PProfHandler(c *gin.Context) {
	p := principalFromContext(c)
	if p == nil || (!p.IsPAT && !p.IsAdmin) || (p.IsPAT && !p.TokenScopes[ScopeAdmin] && !p.TokenScopes[ScopeAll]) {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	name := strings.TrimPrefix(c.Param("pprof"), "/")
	switch name {
	case "", "index":
		pprof.Index(c.Writer, c.Request)
	case "cmdline":
		pprof.Cmdline(c.Writer, c.Request)
	case "profile":
		pprof.Profile(c.Writer, c.Request)
	case "symbol":
		pprof.Symbol(c.Writer, c.Request)
	case "trace":
		pprof.Trace(c.Writer, c.Request)
	default:
		pprof.Handler(name).ServeHTTP(c.Writer, c.Request)
	}
}
