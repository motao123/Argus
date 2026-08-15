package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/motao123/Argus/server/internal/agent"
	"github.com/motao123/Argus/server/internal/alert"
	"github.com/motao123/Argus/server/internal/api"
	"github.com/motao123/Argus/server/internal/config"
	"github.com/motao123/Argus/server/internal/db"
	"github.com/motao123/Argus/server/internal/scheduler"
	"github.com/motao123/Argus/server/internal/store"
)

func main() {
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

	// 3. Agent 连接中心
	agents := agent.NewHub(gdb, st, batcher)

	// 4. 报警引擎 + 定时调度器
	engine := alert.NewEngine(gdb, st)
	go engine.Run()
	defer engine.Stop()

	sched := scheduler.New(gdb, agents)
	sched.Start()
	defer sched.Stop()

	// 5. API 路由
	srv := &api.Server{
		DB:        gdb,
		Cfg:       cfg,
		Store:     st,
		Agents:    agents,
		Scheduler: sched,
	}
	agents.TermDataCb = srv.HandleAgentTermData
	router := api.New(srv)

	// 6. 静态资源（内嵌前端，构建时注入；目录不存在则跳过）
	router.NoRoute(func(c *gin.Context) {
		// gin 对未匹配路由默认 404，静态资源需显式改回 200
		c.Status(http.StatusOK)
		serveEmbedded(c.Writer, c.Request)
	})

	// 7. Agent WebSocket 端点（不经过 JWT，走 secret 鉴权）
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
	CheckOrigin: func(r *http.Request) bool { return true },
}

func displayAddr(l string) string {
	if len(l) > 0 && (l[0] == ':' || l[0] == '0') {
		return "localhost" + l
	}
	return l
}
