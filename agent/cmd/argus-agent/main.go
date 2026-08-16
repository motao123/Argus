package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"github.com/motao123/Argus/agent/internal/collector"
	"github.com/motao123/Argus/agent/internal/task"
	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
)

const version = "0.1.0"

func main() {
	var (
		serverURL = flag.String("s", "ws://127.0.0.1:8080/ws/agent", "server WebSocket 地址")
		secret    = flag.String("k", "", "服务器密钥（空则首次注册后自动保存）")
		interval  = flag.Duration("i", 2*time.Second, "上报间隔")
		configDir = flag.String("c", ".", "配置目录（用于保存注册密钥）")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfgFile := filepath.Join(*configDir, "argus-agent.json")
	task.ApplyConfigPath = cfgFile
	// 读取已下发的配置（重启生效）
	if data, err := os.ReadFile(cfgFile); err == nil {
		var applied map[string]any
		if json.Unmarshal(data, &applied) == nil {
			if u, ok := applied["server_url"].(string); ok && u != "" {
				*serverURL = u
			}
			if iv, ok := applied["interval"].(float64); ok && iv > 0 {
				*interval = time.Duration(iv) * time.Second
			}
		}
	}
	if *secret == "" {
		if s, err := loadSecret(cfgFile); err == nil && s != "" {
			*secret = s
		}
	}

	col := collector.New(version)
	for {
		// 每次重连都重新从文件加载密钥：
		// run() 首次注册后会保存新密钥，重连必须复用而非再次注册新服务器
		if *secret == "" {
			if s, err := loadSecret(cfgFile); err == nil && s != "" {
				*secret = s
			}
		}
		log.Printf("connecting to %s ...", *serverURL)
		if err := run(ctx, *serverURL, *secret, *interval, col, cfgFile); err != nil {
			log.Printf("connection error: %v, retrying in 5s", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func run(ctx context.Context, serverURL, secret string, interval time.Duration, col *collector.Collector, cfgFile string) error {
	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	handler := task.NewHandler(conn)
	peer := rpc.New(conn, handler)
	handler.SetPeer(peer)

	// 必须先启动读循环：应答与任务下发都靠它（未调用的函数会被链接器剪除）
	go peer.ReadLoop()

	// 注册（每次连接都执行：首次无密钥由服务端生成，之后用已保存密钥重新鉴权）
	resp, err := peer.Call(protocol.MethodRegister, protocol.RegisterParams{Secret: secret}, 10*time.Second)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("register failed: %s", resp.Error.Message)
	}
	var reg protocol.RegisterResult
	raw, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(raw, &reg); err != nil {
		return fmt.Errorf("bad register result: %w", err)
	}
	if reg.Secret != "" && reg.Secret != secret {
		secret = reg.Secret
		if err := saveSecret(cfgFile, reg.ServerID, secret); err != nil {
			log.Printf("warn: save secret: %v", err)
		}
		log.Printf("registered as server #%d", reg.ServerID)
	} else {
		log.Printf("authenticated as server #%d", reg.ServerID)
	}

	// 上报循环
	lastHost := protocol.HostInfo{}
	go func() {
		_ = col.Run(ctx, interval, func(r *protocol.ReportParams) {
			h := col.HostInfo()
			if h != lastHost {
				r.Host = h
				lastHost = h
			}
			_ = peer.Notify(protocol.MethodReport, r)
		})
	}()

	// 连接断开（读循环退出）或收到退出信号时返回，外层负责重连
	select {
	case <-ctx.Done():
		return nil
	case <-peer.Closed():
		return errors.New("connection closed")
	}
}

func loadSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// 注意：server_id 是数字，只能按结构体解析，避免 map[string]string 类型错误
	var m struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	return m.Secret, nil
}

func saveSecret(path string, serverID int64, secret string) error {
	m := map[string]any{"server_id": serverID, "secret": secret}
	data, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(path, data, 0600)
}
