// Package api 提供 REST API、WebSocket 推送与终端中继。
package api

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/config"
	"github.com/motao123/Argus/server/internal/model"
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
			// 用户管理（admin）
			authed.GET("/users", s.listUsers)
			authed.POST("/users", s.createUser)
			authed.PUT("/users/:id", s.updateUser)
			authed.DELETE("/users/:id", s.deleteUser)

			// PAT 令牌管理（仅 JWT）
			authed.GET("/tokens", s.listTokens)
			authed.POST("/tokens", s.createToken)
			authed.DELETE("/tokens/:id", s.revokeToken)

			// 服务器（PAT 需 scope + 白名单）
			authed.GET("/servers", requireScope(ScopeServerRead), s.listServers)
			authed.POST("/servers", requireScope(ScopeServerWrite), s.createServer)
			authed.PUT("/servers/:id", requireScope(ScopeServerWrite), s.updateServer)
			authed.DELETE("/servers/:id", requireScope(ScopeServerDelete), s.deleteServer)
			authed.GET("/servers/:id/metrics", requireScope(ScopeServerRead), s.serverMetrics)
			authed.POST("/servers/:id/exec", requireScope(ScopeServerExec), s.serverExec)

			// 报警
			authed.GET("/alerts", requireScope(ScopeAlertRead), s.listAlerts)
			authed.POST("/alerts", requireScope(ScopeAlertWrite), s.createAlert)
			authed.PUT("/alerts/:id", requireScope(ScopeAlertWrite), s.updateAlert)
			authed.DELETE("/alerts/:id", requireScope(ScopeAlertDelete), s.deleteAlert)

			// 通知
			authed.GET("/notifications", requireScope(ScopeNotificationRead), s.listNotifications)
			authed.POST("/notifications", requireScope(ScopeNotificationWrite), s.createNotification)
			authed.PUT("/notifications/:id", requireScope(ScopeNotificationWrite), s.updateNotification)
			authed.DELETE("/notifications/:id", requireScope(ScopeNotificationDelete), s.deleteNotification)

			// 定时任务
			authed.GET("/crons", requireScope(ScopeCronRead), s.listCrons)
			authed.POST("/crons", requireScope(ScopeCronWrite), s.createCron)
			authed.PUT("/crons/:id", requireScope(ScopeCronWrite), s.updateCron)
			authed.DELETE("/crons/:id", requireScope(ScopeCronDelete), s.deleteCron)
			authed.POST("/crons/:id/run", requireScope(ScopeCronWrite), s.runCron)

			// 服务监控
			authed.GET("/services", requireScope(ScopeServiceRead), s.listServices)
			authed.POST("/services", requireScope(ScopeServiceWrite), s.createService)
			authed.PUT("/services/:id", requireScope(ScopeServiceWrite), s.updateService)
			authed.DELETE("/services/:id", requireScope(ScopeServiceDelete), s.deleteService)
			authed.GET("/services/:id/history", requireScope(ScopeServiceRead), s.serviceHistory)
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
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// issueToken 签发 JWT（30 天），携带用户 ID 与角色。
func (s *Server) issueToken(u *model.User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	return token.SignedString([]byte(s.Cfg.JWTSecret))
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

