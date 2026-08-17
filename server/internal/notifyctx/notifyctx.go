// Package notifyctx 统一通知上下文与模板占位符渲染。
//
// 对标 Nezha #SERVER.*# 与 Komari {{event}}/{{client}}：告警/离线/服务监控/
// 流量额度/报告/测试等所有发送点构造同一份上下文（事件、服务器信息、规则、
// 指标、阈值、时间、消息正文），模板渲染统一入口：
//   - 渠道级 Body 模板（webhook）与告警规则自定义模板共用 notifyctx.Render；
//   - {{title}} / {{content}} 保持兼容（默认格式渲染后的标题/正文）；
//   - 新增 {{event}}、{{server.name}}、{{server.id}}、{{server.ip}}、
//     {{server.ipv4}}、{{server.ipv6}}、{{server.platform}}、{{rule}}、
//     {{metric}}、{{value}}、{{threshold}}、{{time}}；
//   - 未提供的变量渲染为空字符串。
package notifyctx

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/motao123/Argus/server/internal/store"
)

// TimeFormat 事件时间默认格式（服务器本地时区）。
const TimeFormat = "2006-01-02 15:04:05"

// maskIPEnabled 通知中是否隐藏服务器 IP（借鉴 nezha EnablePlainIPInNotification）。
// 默认关闭；由启动/设置保存时调用 SetMaskIP 配置，全局生效（所有发送点走 FromState）。
var maskIPEnabled bool

// SetMaskIP 配置通知 IP 打码开关（线程安全）。
func SetMaskIP(enabled bool) {
	maskIPMutex.Lock()
	maskIPEnabled = enabled
	maskIPMutex.Unlock()
}

// MaskIPEnabled 查询当前打码开关。
func MaskIPEnabled() bool {
	maskIPMutex.Lock()
	defer maskIPMutex.Unlock()
	return maskIPEnabled
}

var maskIPMutex sync.RWMutex

// maskIP 把 IP 打码为固定掩码（IPv4/IPv6 统一），避免通知内容泄露主机地址；
// 空串（无上报 IP）原样返回，不生成误导性的打码值。
func maskIP(ip string) string {
	if ip == "" {
		return ""
	}
	return "xxx.xxx.xxx.xxx"
}

// Ctx 统一通知上下文。字段缺省（空串/零值）时对应占位符渲染为空字符串。
type Ctx struct {
	// Event 事件类型：triggered / recovered / repeat / escalated / offline / online /
	// traffic_quota / failure / certificate_changed / certificate_expiring /
	// report / expire_check / test …
	Event string
	// Title/Content 默认格式渲染的消息标题/正文（{{title}}/{{content}}）。
	Title   string
	Content string
	// Rule 规则名（告警规则名 / 服务名）。
	Rule string
	// Metric 指标名（cpu/mem/offline/traffic_quota/服务探测类型…）。
	Metric string
	// Value 指标值（已格式化文本）。
	Value string
	// Threshold 触发阈值（已格式化文本；如 "80.00"、"90%"）。
	Threshold string
	// Time 事件时间（已格式化文本）。
	Time string

	// 服务器信息。
	ServerName     string
	ServerID       int64
	ServerIP       string // 主 IPv4（旧客户端兼容）
	ServerIPv4     string
	ServerIPv6     string
	ServerPlatform string
	ServerOnline   string // online / offline

	// Extras 扩展变量（模板键直接使用，如 {{detail}}）。
	Extras map[string]string
}

// FromState 从服务器运行时状态填充 server.* 变量（st 可为 nil/无 Server）。
// 若已开启 IP 打码，server.ip/ipv4/ipv6 统一替换为打码值。
func (c *Ctx) FromState(st *store.State) *Ctx {
	if st == nil || st.Server == nil {
		return c
	}
	c.ServerName = st.Server.Name
	c.ServerID = st.Server.ID
	c.ServerOnline = OnlineStr(st.Online)
	c.ServerIP = st.Host.IP
	c.ServerIPv4 = st.Host.IPv4
	c.ServerIPv6 = st.Host.IPv6
	c.ServerPlatform = st.Host.Platform
	if MaskIPEnabled() {
		c.ServerIP = maskIP(c.ServerIP)
		c.ServerIPv4 = maskIP(c.ServerIPv4)
		c.ServerIPv6 = maskIP(c.ServerIPv6)
	}
	return c
}

// OnlineStr 在线状态文本。
func OnlineStr(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}

// FormatTime 统一事件时间格式（服务器本地时区）。
func FormatTime(t time.Time) string {
	return t.Format(TimeFormat)
}

// Flat 展开为占位符 → 值的平面表。可选字段为空时省略对应键，
// 缺失键在 Render 中渲染为空字符串。
func (c *Ctx) Flat() map[string]string {
	m := make(map[string]string, 16+len(c.Extras))
	m["event"] = c.Event
	m["title"] = c.Title
	m["content"] = c.Content
	if c.Rule != "" {
		m["rule"] = c.Rule
	}
	if c.Metric != "" {
		m["metric"] = c.Metric
	}
	if c.Value != "" {
		m["value"] = c.Value
	}
	if c.Threshold != "" {
		m["threshold"] = c.Threshold
	}
	if c.Time != "" {
		m["time"] = c.Time
	}
	if c.ServerName != "" {
		m["server.name"] = c.ServerName
	}
	if c.ServerID != 0 {
		m["server.id"] = strconv.FormatInt(c.ServerID, 10)
	}
	if c.ServerIP != "" {
		m["server.ip"] = c.ServerIP
	}
	if c.ServerIPv4 != "" {
		m["server.ipv4"] = c.ServerIPv4
	}
	if c.ServerIPv6 != "" {
		m["server.ipv6"] = c.ServerIPv6
	}
	if c.ServerPlatform != "" {
		m["server.platform"] = c.ServerPlatform
	}
	if c.ServerOnline != "" {
		m["server.online"] = c.ServerOnline
	}
	for k, v := range c.Extras {
		m[k] = v
	}
	return m
}

// Render 用上下文渲染模板：替换所有 {{key}} 占位符。
// 缺失/未提供的变量渲染为空字符串；单遍扫描，值中出现的 {{ 不再二次替换。
func (c *Ctx) Render(tmpl string) string {
	return Render(tmpl, c.Flat())
}

// Encode 将上下文编码为 JSON（供送达记录持久化，渠道发送时还原）。
func (c *Ctx) Encode() string {
	return EncodeMap(c.Flat())
}

// EncodeMap 将变量表编码为 JSON；空表返回空串。
func EncodeMap(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}
	b, err := json.Marshal(vars)
	if err != nil {
		return ""
	}
	return string(b)
}

// Decode 从 JSON 还原变量表；空串/非法返回 nil。
func Decode(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var vars map[string]string
	if err := json.Unmarshal([]byte(raw), &vars); err != nil {
		return nil
	}
	return vars
}

// Render 渲染模板：{{key}} 占位符替换为 vars[key]，缺失键渲染为空字符串。
// 兼容 {{title}}/{{content}}（由 vars 提供，默认格式渲染后的正文）。
func Render(tmpl string, vars map[string]string) string {
	if !strings.Contains(tmpl, "{{") {
		return tmpl
	}
	var b strings.Builder
	rest := tmpl
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			b.WriteString(rest)
			break
		}
		j := strings.Index(rest[i+2:], "}}")
		if j < 0 {
			// 未闭合的 {{ 原样保留
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		key := rest[i+2 : i+2+j]
		if v, ok := vars[key]; ok {
			b.WriteString(v)
		}
		rest = rest[i+2+j+2:]
	}
	return b.String()
}
