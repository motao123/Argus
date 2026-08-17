// Package config 加载 Argus Server 运行配置。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 服务端配置。
type Config struct {
	Listen                   string   // HTTP 监听地址
	DBPath                   string   // SQLite 文件路径
	JWTSecret                string   // JWT 签名密钥（自动生成并持久化）
	AdminUser                string   // 初始管理员用户名
	AdminPass                string   // 初始管理员密码
	TrustedProxies           []string // 可信反向代理（CIDR/IP），用于 ClientIP
	NATReservedHosts         []string // dashboard/API 域名，禁止被 NAT Host 路由覆盖
	NATServerConnectionLimit int      // 每服务器并发 HTTP 隧道数
	NATUserConnectionLimit   int      // 每用户并发 HTTP 隧道数
	MCPEnabled               bool     // MCP endpoint is disabled unless explicitly enabled
	MCPRateLimit             int      // requests per token per minute
	MCPTransferMax           int64    // one-time transfer maximum bytes
	MCPTransferTTL           time.Duration
	ThemeMarketIndex         string // 远程主题市场静态索引（HTTPS；空 = 禁用）
}

// Load 从环境变量/默认值加载配置。
func Load() *Config {
	c := &Config{
		Listen:                   getenv("ARGUS_LISTEN", "0.0.0.0:8080"),
		DBPath:                   getenv("ARGUS_DB", "./data/argus.db"),
		JWTSecret:                os.Getenv("ARGUS_JWT_SECRET"),
		AdminUser:                getenv("ARGUS_ADMIN_USER", "admin"),
		AdminPass:                getenv("ARGUS_ADMIN_PASS", "argus123"),
		TrustedProxies:           splitCSV(os.Getenv("ARGUS_TRUSTED_PROXIES")),
		NATReservedHosts:         splitCSV(os.Getenv("ARGUS_NAT_RESERVED_HOSTS")),
		NATServerConnectionLimit: getenvInt("ARGUS_NAT_SERVER_CONNECTION_LIMIT", 16),
		NATUserConnectionLimit:   getenvInt("ARGUS_NAT_USER_CONNECTION_LIMIT", 32),
		MCPEnabled:               getenvBool("ARGUS_MCP_ENABLED", false),
		MCPRateLimit:             getenvInt("ARGUS_MCP_RATE_LIMIT", 60),
		MCPTransferMax:           int64(getenvInt("ARGUS_MCP_TRANSFER_MAX_MB", 64)) << 20,
		MCPTransferTTL:           time.Duration(getenvInt("ARGUS_MCP_TRANSFER_TTL_SECONDS", 300)) * time.Second,
		ThemeMarketIndex:         os.Getenv("ARGUS_THEME_MARKET_INDEX"),
	}
	if c.JWTSecret == "" {
		c.JWTSecret = loadOrGenerateJWT(c.DBPath)
	}
	return c
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenvInt(key string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func getenvBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return parsed
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadOrGenerateJWT 复用已有密钥或生成新密钥（存到 DB 同目录）。
func loadOrGenerateJWT(dbPath string) string {
	secretFile := dbPath + ".jwt"
	if b, err := os.ReadFile(secretFile); err == nil && len(b) > 0 {
		return string(b)
	}
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	secret := hex.EncodeToString(buf)
	_ = os.MkdirAll(dirOf(dbPath), 0o755)
	_ = os.WriteFile(secretFile, []byte(secret), 0o600)
	return secret
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
