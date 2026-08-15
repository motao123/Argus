// Package plugin 简化版插件系统（借鉴 komari 插件设计）：
// data/plugins/<name>/ 目录存放 plugin.js + manifest.json，
// goja 沙箱执行，注入 console 日志与 fetch，支持 cron 定时触发。
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// Manifest 插件清单。
type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Cron        string `json:"cron"` // cron 表达式或 @every 30s
}

// Plugin 运行中的插件。
type Plugin struct {
	Manifest
	Dir    string `json:"dir"`
	Enabled bool   `json:"enabled"`
	Logs   []string `json:"logs"` // 环形缓冲最近 50 条
	LastRun string `json:"last_run"`
}

// Manager 插件管理器。
type Manager struct {
	dir string

	mu       sync.Mutex
	plugins  map[string]*Plugin
	lastRuns map[string]time.Time
}

// New 创建管理器（dir = 插件根目录，如 ./data/plugins）。
func New(dir string) *Manager {
	return &Manager{dir: dir, plugins: make(map[string]*Plugin), lastRuns: make(map[string]time.Time)}
}

// Load 扫描目录加载插件。
func (m *Manager) Load() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if _, exists := m.plugins[name]; exists {
			continue // 已加载：保留启停/日志状态
		}
		manifestPath := filepath.Join(m.dir, name, "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // 无 manifest 不视为插件
		}
		var man Manifest
		if err := json.Unmarshal(data, &man); err != nil {
			log.Printf("plugin %s: bad manifest: %v", name, err)
			continue
		}
		if man.Name == "" {
			man.Name = name
		}
		m.plugins[name] = &Plugin{
			Manifest: man,
			Dir:      name,
			Enabled:  true,
			Logs:     make([]string, 0, 50),
		}
		log.Printf("plugin manager: loaded %s v%s (%s)", name, man.Version, man.Cron)
	}
	return nil
}

// MarketDir 市场目录（main 注入，如 ./data/market/plugins）。
var MarketDir = "./data/market/plugins"

// MarketEntry 市场插件条目。
type MarketEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
}

// ListMarket 扫描市场目录。
func (m *Manager) ListMarket() []MarketEntry {
	entries, err := os.ReadDir(MarketDir)
	if err != nil {
		return nil
	}
	out := make([]MarketEntry, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(MarketDir, e.Name(), "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var man Manifest
		_ = json.Unmarshal(data, &man)
		out = append(out, MarketEntry{
			Name:        e.Name(),
			Description: man.Description,
			Version:     man.Version,
			Installed:   m.Has(e.Name()),
		})
	}
	return out
}

// InstallFromMarket 从市场安装插件（复制目录）。
func (m *Manager) InstallFromMarket(name string) error {
	src := filepath.Join(MarketDir, name)
	if _, err := os.Stat(filepath.Join(src, "manifest.json")); err != nil {
		return fmt.Errorf("market plugin %s not found", name)
	}
	dst := filepath.Join(m.dir, name)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return copyDir(src, dst)
}

// Has 插件是否存在。
func (m *Manager) Has(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.plugins[name]
	return ok
}

// copyDir 递归复制目录。
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// List 插件列表。
func (m *Manager) List() []*Plugin {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, p)
	}
	return out
}

// SetEnabled 启停插件。
func (m *Manager) SetEnabled(name string, enabled bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	if !ok {
		return false
	}
	p.Enabled = enabled
	return true
}

// Delete 删除插件目录。
func (m *Manager) Delete(name string) bool {
	m.mu.Lock()
	_, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return false
	}
	delete(m.plugins, name)
	m.mu.Unlock()
	return os.RemoveAll(filepath.Join(m.dir, name)) == nil
}

// addLog 追加日志（环形 50 条）。
func (m *Manager) addLog(p *Plugin, line string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.Logs = append(p.Logs, time.Now().Format("15:04:05")+" "+line)
	if len(p.Logs) > 50 {
		p.Logs = p.Logs[len(p.Logs)-50:]
	}
	p.LastRun = time.Now().Format(time.RFC3339)
}

// Run 执行插件一次（注入 console/fetch）。
func (m *Manager) Run(name string) error {
	m.mu.Lock()
	p, ok := m.plugins[name]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("plugin %s not found", name)
	}
	if !p.Enabled {
		return fmt.Errorf("plugin %s disabled", name)
	}

	src, err := os.ReadFile(filepath.Join(m.dir, name, "plugin.js"))
	if err != nil {
		return err
	}

	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	// console.log/error → 日志缓冲
	console := vm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, a := range call.Arguments {
			parts = append(parts, a.String())
		}
		line := strings.Join(parts, " ")
		m.addLog(p, "LOG "+line)
		return goja.Undefined()
	})
	console.Set("error", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, a := range call.Arguments {
			parts = append(parts, a.String())
		}
		m.addLog(p, "ERR "+strings.Join(parts, " "))
		return goja.Undefined()
	})
	_ = vm.Set("console", console)

	// fetch(url) → 返回响应文本
	_ = vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		url := call.Arguments[0].String()
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			m.addLog(p, "ERR fetch "+url+": "+err.Error())
			return goja.Undefined()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return vm.ToValue(string(body))
	})

	// 超时保护（5s）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		<-ctx.Done()
		vm.Interrupt("plugin timeout")
	}()

	if _, err := vm.RunString(string(src)); err != nil {
		m.addLog(p, "ERR "+err.Error())
		return err
	}
	return nil
}

// RunScheduled 按 cron 调度（@every Ns/Nm/Nh 或标准 cron 表达式简化支持）。
func (m *Manager) RunScheduled() {
	// 简化调度：每 5s 检查 @every 表达式的插件（标准 cron 由 scheduler 扩展）
	for _, p := range m.List() {
		if !p.Enabled || !strings.HasPrefix(p.Cron, "@every") {
			continue
		}
		d, err := parseEvery(p.Cron)
		if err != nil {
			continue
		}
		// 记录上次执行时间，到期触发
		m.mu.Lock()
		last, ok := m.lastRuns[p.Name]
		now := time.Now()
		if !ok || now.Sub(last) >= d {
			m.lastRuns[p.Name] = now
		}
		m.mu.Unlock()
		if !ok || now.Sub(last) >= d {
			go func(name string) {
				if err := m.Run(name); err != nil && !strings.Contains(err.Error(), "disabled") {
					log.Printf("plugin %s run: %v", name, err)
				}
			}(p.Name)
		}
	}
}

// parseEvery 解析 @every 30s / 5m / 2h。
func parseEvery(spec string) (time.Duration, error) {
	parts := strings.Fields(spec)
	if len(parts) != 2 {
		return 0, fmt.Errorf("bad @every: %s", spec)
	}
	unit := parts[1]
	val := unit[:len(unit)-1]
	kind := unit[len(unit)-1]
	var n int
	if _, err := fmt.Sscanf(val, "%d", &n); err != nil {
		return 0, err
	}
	switch kind {
	case 's':
		return time.Duration(n) * time.Second, nil
	case 'm':
		return time.Duration(n) * time.Minute, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	}
	return 0, fmt.Errorf("unknown unit: %s", unit)
}
