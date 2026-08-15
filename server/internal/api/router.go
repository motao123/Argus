// Package api 提供 REST API、WebSocket 推送与终端中继。
package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/config"
	"github.com/motao123/Argus/server/internal/scheduler"
	"github.com/motao123/Argus/server/internal/store"
)

var errInvalidToken = errors.New("invalid token")

// Server API 上下文。
type Server struct {
	DB        *gorm.DB
	Cfg       *config.Config
	Store     *store.Hub
	Agents    *agent.Hub
	Scheduler *scheduler.Scheduler
}

// New 构建 gin 引擎并注册全部路由。
func New(s *Server) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api/v1")
	{
		api.POST("/auth/login", s.login)

		authed := api.Group("", s.authMiddleware())
		{
			authed.GET("/servers", s.listServers)
			authed.POST("/servers", s.createServer)
			authed.PUT("/servers/:id", s.updateServer)
			authed.DELETE("/servers/:id", s.deleteServer)
			authed.GET("/servers/:id/metrics", s.serverMetrics)
			authed.POST("/servers/:id/exec", s.serverExec)

			authed.GET("/alerts", s.listAlerts)
			authed.POST("/alerts", s.createAlert)
			authed.PUT("/alerts/:id", s.updateAlert)
			authed.DELETE("/alerts/:id", s.deleteAlert)

			authed.GET("/notifications", s.listNotifications)
			authed.POST("/notifications", s.createNotification)
			authed.PUT("/notifications/:id", s.updateNotification)
			authed.DELETE("/notifications/:id", s.deleteNotification)

			authed.GET("/crons", s.listCrons)
			authed.POST("/crons", s.createCron)
			authed.PUT("/crons/:id", s.updateCron)
			authed.DELETE("/crons/:id", s.deleteCron)
			authed.POST("/crons/:id/run", s.runCron)
		}
		// 仪表盘实时推送（带鉴权 Query 参数，便于浏览器 WS 连接）
		api.GET("/ws", s.authWS, s.dashboardWS)
		api.GET("/terminal/:serverId", s.authWS, s.terminalWS)
	}
	return r
}

// ---- JWT ----

type claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// issueToken 签发 JWT（30 天）。
func (s *Server) issueToken(username string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	return token.SignedString([]byte(s.Cfg.JWTSecret))
}

// authMiddleware 校验 Authorization: Bearer <token>。
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			c.Abort()
			return
		}
		if _, err := s.parseToken(strings.TrimPrefix(auth, "Bearer ")); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// authWS 供 WebSocket 端点校验 token（Query 参数形式）。
func (s *Server) authWS(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		token = c.GetHeader("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")
	}
	if _, err := s.parseToken(token); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		c.Abort()
		return
	}
	c.Next()
}

func (s *Server) parseToken(token string) (*claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &claims{}, func(t *jwt.Token) (any, error) {
		return []byte(s.Cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	cl, ok := parsed.Claims.(*claims)
	if !ok || !parsed.Valid {
		return nil, errInvalidToken
	}
	return cl, nil
}

