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
	"github.com/motao123/Argus/server/internal/geoip"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/oauth"
	"github.com/motao123/Argus/server/internal/plugin"
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
	OAuth     *oauth.Client
	GeoIP     *geoip.Service
	Plugins   *plugin.Manager
}

// New 构建 gin 引擎并注册全部路由。
func New(s *Server) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api/v1")
	{
		api.POST("/auth/login", s.login)
		api.GET("/auth/oauth/:provider", s.oauthRedirect)
		api.GET("/auth/oauth/:provider/callback", s.oauthCallback)
		api.GET("/auth/2fa/setup", s.authMiddleware(), s.twoFASetup)
		api.GET("/auth/2fa/qrcode", s.authMiddleware(), s.twoFAQRCode)
		api.POST("/auth/2fa/enable", s.authMiddleware(), s.twoFAEnable)
		api.POST("/auth/2fa/disable", s.authMiddleware(), s.twoFADisable)

		// 读接口：可选认证（游客可访问公开视图，借鉴 nezha optionalAuth）
		pub := api.Group("", s.optionalAuthMiddleware())
		{
			pub.GET("/servers", s.listServers)
			pub.GET("/servers/:id/metrics", s.serverMetrics)
			pub.GET("/servers/:id/transfer", s.serverTransfer)
			pub.GET("/services", s.listServices)
			pub.GET("/services/:id/history", s.serviceHistory)
		}

		// 写接口：必须登录
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
			authed.POST("/servers", requireScope(ScopeServerWrite), s.createServer)
			authed.PUT("/servers/:id", requireScope(ScopeServerWrite), s.updateServer)
			authed.DELETE("/servers/:id", requireScope(ScopeServerDelete), s.deleteServer)
			authed.POST("/servers/:id/exec", requireScope(ScopeServerExec), s.serverExec)

			// 报警
			authed.GET("/alerts", requireScope(ScopeAlertRead), s.listAlerts)
			authed.POST("/alerts", requireScope(ScopeAlertWrite), s.createAlert)
			authed.PUT("/alerts/:id", requireScope(ScopeAlertWrite), s.updateAlert)
			authed.DELETE("/alerts/:id", requireScope(ScopeAlertDelete), s.deleteAlert)

			// 备份与数据库工具（admin）
			authed.GET("/admin/backup", s.backupDownload)
			authed.POST("/admin/backup/restore", s.backupRestore)
			authed.GET("/admin/db/size", s.dbSize)
			authed.POST("/admin/db/vacuum", s.dbVacuum)

			// 插件管理
			authed.GET("/plugins", s.listPlugins)
			authed.POST("/plugins/:name/toggle", s.togglePlugin)
			authed.POST("/plugins/:name/run", s.runPluginNow)
			authed.DELETE("/plugins/:name", s.deletePlugin)

			// 通知分组
			authed.GET("/notification-groups", s.listNotificationGroups)
			authed.POST("/notification-groups", s.saveNotificationGroup)
			authed.PUT("/notification-groups/:id", s.saveNotificationGroup)
			authed.DELETE("/notification-groups/:id", s.deleteNotificationGroup)

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

			// 服务监控（管理）
			authed.POST("/services", requireScope(ScopeServiceWrite), s.createService)
			authed.PUT("/services/:id", requireScope(ScopeServiceWrite), s.updateService)
			authed.DELETE("/services/:id", requireScope(ScopeServiceDelete), s.deleteService)

			// 文件管理（借用 server 资源 scope）
			authed.GET("/files/:serverId", requireScope(ScopeServerRead), s.listFiles)
			authed.POST("/files/:serverId/read", requireScope(ScopeServerRead), s.readFile)
			authed.POST("/files/:serverId/write", requireScope(ScopeServerWrite), s.writeFile)
			authed.POST("/files/:serverId/delete", requireScope(ScopeServerWrite), s.deleteFile)

			// 会话管理
			authed.GET("/sessions", s.listSessions)
			authed.DELETE("/sessions/:id", s.revokeSession)
			authed.DELETE("/sessions", s.revokeAllSessions)

			// 服务器过户（admin）
			authed.POST("/servers/:id/transfer", s.serverTransfer)

			// DDNS
			authed.GET("/ddns", s.listDDNS)
			authed.POST("/ddns", s.createDDNS)
			authed.PUT("/ddns/:id", s.updateDDNS)
			authed.DELETE("/ddns/:id", s.deleteDDNS)
			authed.POST("/ddns/:id/test", s.testDDNS)

			// OAuth provider 配置（admin）
			authed.GET("/oauth/providers", s.listOAuthConfigs)
			authed.POST("/oauth/providers", s.saveOAuthConfig)
			authed.DELETE("/oauth/providers/:name", s.deleteOAuthConfig)

			// NAT 内网穿透
			authed.GET("/nats", s.listNAT)
			authed.POST("/nats", s.createNAT)
			authed.PUT("/nats/:id", s.updateNAT)
			authed.DELETE("/nats/:id", s.deleteNAT)
		}
		// 仪表盘实时推送（游客可连，借鉴 komari 公开节点列表）
		api.GET("/ws", s.optionalAuthMiddleware(), s.dashboardWS)
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
	return s.issueTokenWithJTI(u, randomHex(8))
}

// issueTokenWithJTI 签发带 JTI 的 JWT（会话踢出用）。
func (s *Server) issueTokenWithJTI(u *model.User, jti string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
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

