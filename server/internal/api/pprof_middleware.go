package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddlewareForPProf pprof 专用的认证中间件（JWT 或 PAT）。
func (s *Server) AuthMiddlewareForPProf() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			fail(c, http.StatusUnauthorized, "missing token")
			c.Abort()
			return
		}
		p, err := s.identify(strings.TrimPrefix(auth, "Bearer "))
		if err != nil {
			fail(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}
		if (!p.IsPAT && !p.IsAdmin) || (p.IsPAT && !p.TokenScopes[ScopeAdmin] && !p.TokenScopes[ScopeAll]) {
			fail(c, http.StatusForbidden, "admin only")
			c.Abort()
			return
		}
		c.Set("principal", p)
		c.Next()
	}
}
