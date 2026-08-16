package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/alert"
	"github.com/motao123/Argus/server/internal/api"
	"github.com/motao123/Argus/server/internal/config"
	"github.com/motao123/Argus/server/internal/db"
	"github.com/motao123/Argus/server/internal/geoip"
	"github.com/motao123/Argus/server/internal/mcp"
	"github.com/motao123/Argus/server/internal/metric"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/nat"
	"github.com/motao123/Argus/server/internal/notifier"
	"github.com/motao123/Argus/server/internal/oauth"
	"github.com/motao123/Argus/server/internal/plugin"
	"github.com/motao123/Argus/server/internal/scheduler"
	"github.com/motao123/Argus/server/internal/sentinel"
	"github.com/motao123/Argus/server/internal/store"
)

func main() {
	// 运维子命令：argus-server <server|chpasswd|disable-2fa>
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "chpasswd", "disable-2fa":
			// 重排参数：把子命令放到 flag.Args()[0]
			os.Args = append([]string{os.Args[0], os.Args[1]}, os.Args[2:]...)
			flag.Parse()
			runOps()
			return
		}
	}

	listen := flag.String("l", "", "HTTP 监听地址（默认取环境变量 ARGUS_LISTEN）")
	dbPath := flag.String("d", "", "SQLite 数据库路径（默认取环境变量 ARGUS_DB）")
	flag.Parse()

	cfg := config.Load()
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}

	// 1. 数据库
	gdb, err := db.Init(cfg.DBPath, cfg.AdminUser, cfg.AdminPass)
	if err != nil {
		log.Fatalf("db init: %v", err)
	}

	// 2. 内存状态区 + 指标批处理
	st := store.NewHub()
	batcher := store.NewMetricBatcher(gdb)
	go batcher.Run()
	defer batcher.Stop()

	// 流量账本（reset-aware，重启恢复）
	ledger := store.NewTrafficLedger(gdb)
	st.Ledger = ledger
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ledger.Flush()
		}
	}()

	// 3. Agent 连接中心
	agents := agent.NewHub(gdb, st, batcher)

	// 预加载 DB 服务器到内存 Hub（前台 WS 快照依赖内存态，
	// 否则重启后未连接 agent 的服务器会从前台消失）
	var allServers []model.Server
	if err := gdb.Find(&allServers).Error; err == nil {
		for i := range allServers {
			st.Upsert(&allServers[i])
		}
		log.Printf("preloaded %d servers into memory hub", len(allServers))
	}

	// 4. 定时调度器 + 报警引擎（触发任务联动）
	sched := scheduler.New(gdb, agents)
	sched.Start()
	defer sched.Stop()

	engine := alert.NewEngine(gdb, st)
	engine.Trigger = func(cron *model.Cron, serverID int64) {
		// 只对目标服务器执行（借鉴 nezha 触发任务按服务器分发）
		old := cron.ServerIDs
		cron.ServerIDs = fmt.Sprintf("%d", serverID)
		sched.RunOnce(cron)
		cron.ServerIDs = old
	}
	go engine.Run()
	defer engine.Stop()

	// 指标 rollup 聚合与保留清理（借鉴 komari 分层 rollup）
	rollup := metric.New(gdb)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rollup.Aggregate5m()
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			rollup.AggregateHour()
			rollup.Cleanup()
		}
	}()

	// 离线/上线通知哨兵（借鉴 komari notifier/offline）
	offlineSentinel := alert.NewOfflineSentinel(gdb, st)
	go offlineSentinel.Run()
	defer offlineSentinel.Stop()

	// 服务监控哨兵
	svcSentinel := sentinel.New(gdb)
	svcSentinel.NotifyCb = func(svc *model.Service, up bool) {
		var n model.Notification
		if err := gdb.First(&n, svc.NotifyWebhookID).Error; err != nil {
			return
		}
		kind := "恢复"
		if !up {
			kind = "故障"
		}
		title := fmt.Sprintf("[Argus] 服务%s %s", kind, svc.Name)
		content := fmt.Sprintf("%s (%s) %s", svc.Name, svc.Type, svc.Target)
		go notifier.Send(&n, title, content)
	}
	go svcSentinel.Run(agents.Peers)
	defer svcSentinel.Stop()

	// NAT 内网穿透反向代理（默认 :9090）
	natProxy := nat.New(gdb, agents.Peers)
	agents.NATDataCb = natProxy.DataSink
	go func() {
		if err := natProxy.Start(os.Getenv("ARGUS_NAT_LISTEN")); err != nil && err != http.ErrServerClosed {
			log.Printf("nat proxy: %v", err)
		}
	}()
	defer natProxy.Close()

	// 插件管理器（data/plugins 目录）
	plugin.MarketDir = filepath.Join(filepath.Dir(cfg.DBPath), "market", "plugins")
	plugins := plugin.New(filepath.Join(filepath.Dir(cfg.DBPath), "plugins"))
	_ = plugins.Load()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			_ = plugins.Load() // 增量扫描新插件
			plugins.RunScheduled()
		}
	}()

	// 5. API 路由
	geoipSvc := geoip.New()
	if ep := os.Getenv("ARGUS_GEOIP_ENDPOINT"); ep != "" {
		geoipSvc.SetProvider(&geoip.HTTPProvider{Endpoint: ep})
		log.Printf("GeoIP provider: %s", ep)
	}
	srv := &api.Server{
		DB:        gdb,
		Cfg:       cfg,
		Store:     st,
		Agents:    agents,
		Scheduler: sched,
		OAuth:     oauth.NewClient(),
		GeoIP:     geoipSvc,
		Plugins:   plugins,
	}
	srv.ReloadOAuthConfigs()

	// 每日任务：流量报告 + 到期提醒（借鉴 komari 流量报告/renewal）
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		lastDay := -1
		for range ticker.C {
			if time.Now().Day() == lastDay {
				continue
			}
			lastDay = time.Now().Day()
			srv.RunTrafficReport()
			srv.RunExpireCheck()
		}
	}()

	// 周期流量落库改为 TrafficLedger 的 30s flush（见上文），此处移除旧小时 ticker。

	agents.TermDataCb = srv.HandleAgentTermData
	// 过户验证：Agent 用新密钥重连即完成
	agents.TransferCb = srv.VerifyTransfer
	// DDNS：服务器 IP 变化时更新解析记录
	agents.IPChangeCb = srv.HandleServerIPChange
	router := api.New(srv)
	// 可信代理：只有来自这些代理的请求才采信 X-Forwarded-For（默认空 = 直连模式）
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("invalid trusted proxies: %v", err)
	}

	// 自定义代码注入（设置键 custom_css / custom_js / custom_footer，热更新）
	injectHTML = func(html string) string {
		css := srv.GetSetting(api.SettingCustomCSS, "")
		js := srv.GetSetting(api.SettingCustomJS, "")
		footer := srv.GetSetting(api.SettingCustomFooter, "")
		if css != "" {
			html = strings.Replace(html, "</head>", "<style>\n"+css+"\n</style>\n</head>", 1)
		}
		if js != "" {
			html = strings.Replace(html, "</body>", "<script>\n"+js+"\n</script>\n</body>", 1)
		}
		if footer != "" {
			html = strings.Replace(html, "Powered by Argus", footer+"\nPowered by Argus", 1)
		}
		return html
	}

	// 容器编排/反向代理探活端点：不访问数据库，不泄露运行数据。
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 6. 静态资源（内嵌前端，构建时注入；目录不存在则跳过）
	// pprof 性能剖析（借鉴 komari，受 admin 路由保护）
	router.GET("/debug/pprof/*pprof", srv.AuthMiddlewareForPProf(), srv.PProfHandler)

	router.NoRoute(func(c *gin.Context) {
		// gin 对未匹配路由默认 404，静态资源需显式改回 200
		c.Status(http.StatusOK)
		serveEmbedded(c.Writer, c.Request)
	})

	// 7. Agent WebSocket 端点（不经过 JWT，走 secret 鉴权）
	// MCP 端点（PAT 认证）
	mcpServer := &mcp.Server{DB: gdb, Peers: agents.Peers, IdentifyPAT: srv.IdentifyPATToken}
	router.Any("/mcp", func(c *gin.Context) {
		mcpServer.Handler().ServeHTTP(c.Writer, c.Request)
	})

	router.GET("/ws/agent", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		agents.Serve(conn)
	})

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 优雅停机
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down ...")
		_ = httpServer.Close()
	}()

	log.Printf("Argus server listening on %s (admin: %s / %s)", cfg.Listen, cfg.AdminUser, cfg.AdminPass)
	log.Printf("agent endpoint: ws://%s/ws/agent", displayAddr(cfg.Listen))
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
	log.Println("bye")
}

var upgrader = websocket.Upgrader{
	// 校验 Origin/Referer 与 Host 一致（防跨站 WS 劫持）
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Header.Get("Referer")
		}
		if origin == "" {
			return true // 非浏览器客户端（agent/脚本）
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

func displayAddr(l string) string {
	if len(l) > 0 && (l[0] == ':' || l[0] == '0') {
		return "localhost" + l
	}
	return l
}
