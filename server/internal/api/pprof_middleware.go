package api

import "github.com/gin-gonic/gin"

// AuthMiddlewareForPProf pprof 专用的认证中间件（JWT 或 PAT）。
func (s *Server) AuthMiddlewareForPProf() gin.HandlerFunc {
	return s.authMiddleware()
}
