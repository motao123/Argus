// Package api 提供 REST API、WebSocket 推送与终端中继。
package api

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/backup"
	"github.com/motao123/Argus/server/internal/config"
	"github.com/motao123/Argus/server/internal/ddns"
	"github.com/motao123/Argus/server/internal/geoip"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/nat"
	"github.com/motao123/Argus/server/internal/notifier"
	"github.com/motao123/Argus/server/internal/oauth"
	"github.com/motao123/Argus/server/internal/plugin"
	"github.com/motao123/Argus/server/internal/scheduler"
	"github.com/motao123/Argus/server/internal/store"
	"github.com/motao123/Argus/server/internal/theme"
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
	Themes    *theme.Manager
	DDNS      *ddns.Client
	NAT       *nat.Proxy
	Notifier  *notifier.Queue // 通知持久队列（送达记录 + 重试）
	Backups   *backup.Manager // 定时加密备份管理器

	// waf 全局 IP 封禁管理器（WAF 限流 / 登录限流 / 手动封禁共用，持久化封禁表）。
	waf *wafBanManager
	// online 在线访客/用户跟踪（最近请求 IP + 活跃会话 + WS/终端长连接）。
	online *onlineTracker

	// upgradeResumeDelay 控制重启后恢复 pending 升级任务的宽限期（仅测试覆盖）。
	upgradeResumeDelay time.Duration
}

// New 构建 gin 引擎并注册全部路由。
func New(s *Server) *gin.Engine {
	// 先初始化 WAF 封禁管理器（载入持久化封禁）与在线跟踪器，再挂中间件
	s.wafMgr()
	s.onlineMgr()
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), s.wafMiddleware())

	api := r.Group("/api/v1")
	{
		api.POST("/auth/login", s.login)
		api.POST("/auth/oauth/consume", s.consumeOAuthCode)
		api.GET("/auth/oauth/providers", s.listPublicOAuthProviders)
		api.GET("/auth/oauth/:provider", s.oauthRedirect)
		api.GET("/auth/oauth/:provider/callback", s.oauthCallback)
		api.GET("/auth/me", s.authMiddleware(), s.me)
		api.GET("/auth/2fa/setup", s.authMiddleware(), s.twoFASetup)
		api.GET("/auth/2fa/qrcode", s.authMiddleware(), s.twoFAQRCode)
		api.POST("/auth/2fa/enable", s.authMiddleware(), s.twoFAEnable)
		api.POST("/auth/2fa/disable", s.authMiddleware(), s.twoFADisable)

		// 读接口：可选认证（游客可访问公开视图，借鉴 nezha optionalAuth）
		pub := api.Group("", s.optionalAuthMiddleware())
		{
			pub.GET("/public/settings", s.getPublicSettings)
			pub.GET("/public/term-settings", s.getTermSettings)
			pub.GET("/servers", s.forceAuth, s.listServers)
			pub.GET("/servers/:id/metrics", s.forceAuth, s.serverMetrics)
			pub.GET("/servers/:id/transfer", s.forceAuth, s.serverTransfer)
			pub.GET("/servers/:id/traffic", s.forceAuth, s.serverTransfer)
			pub.GET("/services", s.forceAuth, s.listServices)
			pub.GET("/services/:id/history", s.forceAuth, s.serviceHistory)
			pub.GET("/services/:id/stats", s.forceAuth, s.serviceStats)

			// 状态页：事故时间线 / 维护窗口 / 月度 SLA（公开读取）
			pub.GET("/incidents", s.listIncidents)
			pub.GET("/maintenance-windows", s.listMaintenanceWindows)
			pub.GET("/servers/:id/sla", s.serverSLA)
		}

		// 写接口：必须登录。readonlyGate 拦截只读角色的一切写操作（白名单读接口除外）。
		authed := api.Group("", s.authMiddleware(), s.readonlyGate())
		{
			// 用户管理（admin）
			authed.GET("/users", s.listUsers)
			authed.GET("/users/:id/secret", s.getUserSecret)
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
			authed.GET("/servers/:id/install-command", requireScope(ScopeServerRead), s.serverInstallCommand)

			// 多节点指标对比（admin/owner 逐 id 校验，最多 10 台）
			authed.GET("/metrics/compare", s.compareMetrics)

			// 资源排行 TOP N（admin 全量 / owner 自有，实时快照取数；PAT 需 server:read scope）
			authed.GET("/admin/top", requireScope(ScopeServerRead), s.serverTop)

			// 一键安装命令（用户 Agent 密钥模式）
			authed.GET("/install/command", s.installCommand)

			// 报警
			authed.GET("/alerts", requireScope(ScopeAlertRead), s.listAlerts)
			authed.POST("/alerts", requireScope(ScopeAlertWrite), s.createAlert)
			authed.PUT("/alerts/:id", requireScope(ScopeAlertWrite), s.updateAlert)
			authed.DELETE("/alerts/:id", requireScope(ScopeAlertDelete), s.deleteAlert)

			// 告警确认 / 静默（owner 或 admin；审计）
			authed.POST("/alerts/:id/ack", requireScope(ScopeAlertWrite), s.ackAlert)
			authed.DELETE("/alerts/:id/ack", requireScope(ScopeAlertWrite), s.unackAlert)
			authed.POST("/alerts/:id/silence", requireScope(ScopeAlertWrite), s.silenceAlert)
			authed.DELETE("/alerts/:id/silence", requireScope(ScopeAlertWrite), s.unsilenceAlert)

			// 流量报告配置（admin）
			authed.GET("/traffic-report", s.getTrafficReport)
			authed.POST("/traffic-report", s.saveTrafficReport)

			// 消息发送测试（admin）
			authed.POST("/test-message", s.testSendMessage)

			// 离线/上线通知（admin）
			authed.GET("/offline-notify", s.getOfflineNotify)
			authed.POST("/offline-notify", s.saveOfflineNotify)

			// 站点设置（admin）
			authed.GET("/settings", s.getSettings)
			authed.POST("/settings", s.saveSettings)

			// 备份与数据库工具（admin）
			authed.GET("/admin/backup", s.backupDownload)
			authed.POST("/admin/backup/restore", s.backupRestore)
			authed.GET("/admin/db/size", s.dbSize)
			authed.POST("/admin/db/vacuum", s.dbVacuum)

			// 定时加密备份（admin：整库数据 + 密钥派生信息，仅管理员可管）
			authed.GET("/admin/backup-schedules", requireAdmin(), s.listBackupSchedules)
			authed.POST("/admin/backup-schedules", requireAdmin(), s.createBackupSchedule)
			authed.PUT("/admin/backup-schedules/:id", requireAdmin(), s.updateBackupSchedule)
			authed.DELETE("/admin/backup-schedules/:id", requireAdmin(), s.deleteBackupSchedule)
			authed.POST("/admin/backup-schedules/:id/run", requireAdmin(), s.runBackupSchedule)
			authed.GET("/admin/backup-schedules/:id/runs", requireAdmin(), s.listBackupRuns)
			authed.POST("/admin/backup-schedules/:id/drill", requireAdmin(), s.backupDrill)

			// 服务器分组
			authed.GET("/groups", s.listGroups)
			authed.POST("/groups", s.createGroup)
			authed.DELETE("/groups/:id", s.deleteGroup)

			// 剪贴板
			authed.GET("/clipboard", s.listClipboard)
			authed.POST("/clipboard", s.createClipboard)
			authed.PUT("/clipboard/:id", s.updateClipboard)
			authed.DELETE("/clipboard/:id", s.deleteClipboard)

			// 审计日志（admin）
			authed.GET("/admin/logs", s.listAuditLogs)

			// 插件（admin；插件可执行任意代码并访问网络）
			authed.GET("/plugins/market", requireAdmin(), s.listPluginMarket)
			authed.POST("/plugins/market/:name/install", requireAdmin(), s.installPlugin)
			authed.GET("/plugins", requireAdmin(), s.listPlugins)
			authed.POST("/plugins/:name/toggle", requireAdmin(), s.togglePlugin)
			authed.POST("/plugins/:name/approve", requireAdmin(), s.approvePlugin)
			authed.POST("/plugins/:name/run", requireAdmin(), s.runPluginNow)
			authed.DELETE("/plugins/:name", requireAdmin(), s.deletePlugin)

			// 主题包（admin；仅 CSS 变量/CSS + 受限静态资源，禁止 JS）
			authed.GET("/themes", requireAdmin(), s.listThemes)
			authed.POST("/themes/upload", requireAdmin(), s.uploadTheme)
			authed.POST("/themes/:name/activate", requireAdmin(), s.activateTheme)
			authed.POST("/themes/:name/rollback", requireAdmin(), s.rollbackTheme)
			authed.DELETE("/themes/:name", requireAdmin(), s.deleteTheme)
			authed.GET("/themes/market", requireAdmin(), s.listThemeMarket)
			authed.POST("/themes/market/:name/install", requireAdmin(), s.installThemeMarket)

			// 通知分组
			authed.GET("/notification-groups", s.listNotificationGroups)
			authed.POST("/notification-groups", s.saveNotificationGroup)
			authed.PUT("/notification-groups/:id", s.saveNotificationGroup)
			authed.DELETE("/notification-groups/:id", s.deleteNotificationGroup)

			// 通知（全局资源，仅 admin；避免敏感凭据被普通用户读取）
			authed.GET("/notifications", requireAdmin(), s.listNotifications)
			authed.POST("/notifications", requireAdmin(), s.createNotification)
			authed.PUT("/notifications/:id", requireAdmin(), s.updateNotification)
			authed.DELETE("/notifications/:id", requireAdmin(), s.deleteNotification)

			// 通知送达记录（owner/admin 隔离）与手动重试
			authed.GET("/notifications/deliveries", s.listNotificationDeliveries)
			authed.POST("/notifications/deliveries/:id/retry", s.retryNotificationDelivery)

			// 定时任务
			authed.GET("/crons", requireScope(ScopeCronRead), s.listCrons)
			authed.POST("/crons", requireScope(ScopeCronWrite), s.createCron)
			authed.PUT("/crons/:id", requireScope(ScopeCronWrite), s.updateCron)
			authed.DELETE("/crons/:id", requireScope(ScopeCronDelete), s.deleteCron)
			authed.POST("/crons/:id/run", requireScope(ScopeCronWrite), s.runCron)
			authed.GET("/crons/:id/runs", requireScope(ScopeCronRead), s.listCronRuns)
			authed.GET("/crons/:id/runs/:runId", requireScope(ScopeCronRead), s.getCronRun)

			// 服务监控（管理）
			authed.POST("/services", requireScope(ScopeServiceWrite), s.createService)
			authed.PUT("/services/:id", requireScope(ScopeServiceWrite), s.updateService)
			authed.DELETE("/services/:id", requireScope(ScopeServiceDelete), s.deleteService)

			// 文件管理（借用 server 资源 scope）
			authed.GET("/files/:serverId", requireScope(ScopeServerRead), s.listFiles)
			authed.POST("/files/:serverId/read", requireScope(ScopeServerRead), s.readFile)
			authed.POST("/files/:serverId/write", requireScope(ScopeServerWrite), s.writeFile)
			authed.POST("/files/:serverId/delete", requireScope(ScopeServerWrite), s.deleteFile)

			// 服务器过户（admin）
			authed.GET("/server-transfers", s.listTransfers)
			authed.POST("/server-transfers", s.createTransfer)
			authed.POST("/server-transfers/:id/cancel", s.cancelTransfer)

			// Agent 批量升级（admin，逐机回执）
			authed.GET("/upgrade-jobs", requireAdmin(), s.listUpgradeJobs)
			authed.POST("/upgrade-jobs", requireAdmin(), s.createUpgradeJob)

			// 会话管理
			authed.GET("/sessions", s.listSessions)
			authed.DELETE("/sessions/:id", s.revokeSession)
			authed.DELETE("/sessions", s.revokeAllSessions)

			// WAF 封禁与在线用户（admin；封禁记录与登录限流/全局 WAF 共用）
			authed.GET("/admin/online", requireAdmin(), s.listOnline)
			authed.GET("/admin/waf/bans", requireAdmin(), s.listBans)
			authed.POST("/admin/waf/ban", requireAdmin(), s.banIP)
			authed.DELETE("/admin/waf/ban/:ip", requireAdmin(), s.unbanIP)

			// Agent 配置下发（admin）
			authed.POST("/servers/:id/config", s.serverApplyConfig)

			// 批量操作（admin）
			authed.POST("/batch-delete/servers", s.batchDeleteServers)
			authed.POST("/batch-move/servers", s.batchMoveServers)
			authed.POST("/batch-config/servers", requireAdmin(), s.batchConfigServers)
			authed.POST("/batch-ddns/servers", requireAdmin(), s.batchDDNSServers)

			// 服务器过户状态机已实现（见 transfers.go），POST 原误绑已移除。

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

			// 状态页事故与维护窗口（管理端 CRUD，owner/admin 隔离 + 审计）
			authed.POST("/incidents", s.createIncident)
			authed.PUT("/incidents/:id", s.updateIncident)
			authed.DELETE("/incidents/:id", s.deleteIncident)
			authed.POST("/incidents/:id/resolve", s.resolveIncident)
			authed.POST("/maintenance-windows", s.createMaintenanceWindow)
			authed.PUT("/maintenance-windows/:id", s.updateMaintenanceWindow)
			authed.DELETE("/maintenance-windows/:id", s.deleteMaintenanceWindow)
		}
		// 仪表盘实时推送（游客可连，借鉴 komari 公开节点列表）
		api.GET("/ws", s.optionalAuthMiddleware(), s.dashboardWS)
		api.GET("/terminal/:serverId", s.authWS, s.terminalWS)
	}
	// 主题静态资源（公开，游客也可加载；安装时已白名单校验 + 响应头加固）
	r.GET("/theme-assets/*filepath", s.themeAsset)
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
