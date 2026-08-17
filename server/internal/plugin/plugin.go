// Package plugin 插件宿主（goja 沙箱，借鉴 komari 插件设计）：
// data/plugins/<name>/ 目录存放 plugin.js + manifest.json，
// 附带 state.json（启停/批准/运行状态）、logs.json（持久化日志）、kv.json（每插件命名空间 KV）。
// 调度支持标准 5/6 段 cron 表达式（秒可选）与 @every/@daily 等描述符；
// 事件 hook：onSchedule / onAlert / onServerOnline / onServerOffline（异步隔离、超时、禁止并发重入）。
// 宿主 API：console / argus.log / argus.getServers（脱敏只读）/ argus.notify / argus.kv / fetch。
// fetch 需声明 allow_fetch + fetch_domains 域名白名单 + 管理员批准，并做 SSRF 防护：
// DNS 解析后阻断回环/私网/链路本地（含云元数据 169.254.169.254）/组播/未指定地址，重定向目标复查。
package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/robfig/cron/v3"
)

// 运行与限制参数。
const (
	logMemoryLimit    = 50              // 内存环形日志条数
	logFileLimit      = 200             // logs.json 保留条数
	kvMaxKeys         = 64              // 每插件 KV 键数上限
	kvMaxKeyLen       = 128             // 键长度上限
	kvMaxValueLen     = 4096            // 单值长度上限（4KiB）
	kvMaxTotalBytes   = 256 * 1024      // 每插件 KV 总大小上限（256KiB）
	fetchMaxBody      = 1 << 20         // fetch 响应体上限（1MiB）
	fetchMaxRedirect  = 5               // fetch 最大重定向次数
	defaultRunTimeout = 5 * time.Second // 单次执行/事件 hook 超时
)

// Manifest 插件清单。
type Manifest struct {
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description"`
	Cron        string      `json:"cron"`   // 标准 5/6 段 cron 或 @every 30s / @daily 等
	Events      []string    `json:"events"` // 声明的事件 hook：onSchedule/onAlert/onServerOnline/onServerOffline
	Permissions Permissions `json:"permissions"`
}

// Permissions 插件权限声明（不声明即不授予；借鉴 komari manifest 权限模型）。
type Permissions struct {
	AllowFetch   bool     `json:"allow_fetch"`   // 允许网络请求（还需 fetch_domains 白名单 + 批准）
	FetchDomains []string `json:"fetch_domains"` // fetch 域名白名单（空 = 不授予网络访问）
	AllowNotify  bool     `json:"allow_notify"`  // 允许通过 argus.notify 发送通知（需批准）
	AllowExec    bool     `json:"allow_exec"`    // 已删除的虚假权限：声明 true 将拒绝加载
	Approved     bool     `json:"approved"`      // 管理员批准（运行时状态，state.json 持久化）
}

// Plugin 运行中的插件。
type Plugin struct {
	Manifest
	Dir     string   `json:"dir"`
	Enabled bool     `json:"enabled"`
	Logs    []string `json:"logs"` // 环形缓冲最近 50 条
	LastRun string   `json:"last_run"`
	// 权限摘要（JSON 展示用，兼容旧 UI）
	PermissionsAllowFetch bool `json:"permissions_allow_fetch"`
	// 运行状态
	LastStatus string `json:"last_status"` // ok / error
	LastError  string `json:"last_error"`
	RunCount   int64  `json:"run_count"`
	Running    bool   `json:"running"` // 当前是否有执行在进行（禁止并发重入）
	// 每插件命名空间 KV（kv.json 持久化，不随 JSON 序列化暴露给 UI）
	KV map[string]string `json:"-"`
}

// ServerView 暴露给插件的服务器摘要（脱敏只读：不含密钥/计费/备注/所有者）。
type ServerView struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Group        string `json:"group,omitempty"`
	Online       bool   `json:"online"`
	Hostname     string `json:"hostname,omitempty"`
	IP           string `json:"ip,omitempty"`
	OS           string `json:"os,omitempty"`
	Arch         string `json:"arch,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	Tags         string `json:"tags,omitempty"`
}

// LookupIPResolver DNS 解析器（默认 net.DefaultResolver；测试可注入 fake 以覆盖 SSRF 场景）。
type LookupIPResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Manager 插件管理器。
type Manager struct {
	dir string

	mu      sync.Mutex
	plugins map[string]*Plugin
	running map[string]bool // 禁止并发重入
	asyncWG sync.WaitGroup
	stopped bool

	cron *cron.Cron // 宿主调度器（标准 5/6 段 + 描述符）
	ids  map[string]cron.EntryID

	// 宿主能力（main 注入；nil 表示不可用）
	ServerSource func() []ServerView                         // 脱敏只读服务器列表
	NotifyFunc   func(id int64, title, content string) error // 按 notificationId 发送通知

	// 测试/运维注入
	Resolver LookupIPResolver
	// Dial 自定义拨号器（默认 m.dialContext：dial 前做 DNS 后 SSRF 地址校验）。
	// 测试注入可把任何目标重定向到本地测试服务器。
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)

	RunTimeout time.Duration // 单次执行/事件 hook 超时（0 = 默认 5s）
}

// New 创建管理器（dir = 插件根目录，如 ./data/plugins）。
func New(dir string) *Manager {
	return &Manager{
		dir:     dir,
		plugins: make(map[string]*Plugin),
		running: make(map[string]bool),
		cron:    cron.New(cron.WithParser(pluginCronParser)),
		ids:     make(map[string]cron.EntryID),
	}
}

// pluginCronParser 标准 5/6 段 cron（秒可选）+ 描述符（@every/@daily/@hourly 等）。
var pluginCronParser = cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// validateCron 校验 cron 表达式（空 = 不调度）。
func validateCron(spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	_, err := pluginCronParser.Parse(spec)
	return err
}

// syncPerm 运行时同步权限摘要（JSON 展示用）。
func (p *Plugin) syncPerm() { p.PermissionsAllowFetch = p.Permissions.AllowFetch }

// clonePlugin 返回可脱离 Manager 锁安全使用的深拷贝。
func clonePlugin(p *Plugin) *Plugin {
	if p == nil {
		return nil
	}
	out := *p
	out.Events = append([]string(nil), p.Events...)
	out.Permissions.FetchDomains = append([]string(nil), p.Permissions.FetchDomains...)
	out.Logs = append([]string(nil), p.Logs...)
	if p.KV != nil {
		out.KV = make(map[string]string, len(p.KV))
		for k, v := range p.KV {
			out.KV[k] = v
		}
	}
	return &out
}

// Load 增量扫描插件目录；已加载插件保留启停/日志/运行状态。
func (m *Manager) Load() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var fresh []string
	m.mu.Lock()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if _, exists := m.plugins[name]; exists {
			continue // 已加载：保留启停/日志/运行状态
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
		// 明确拒绝已删除的虚假 allow_exec 权限
		if man.Permissions.AllowExec {
			log.Printf("plugin %s: allow_exec is not supported and has been removed; plugin refused", name)
			continue
		}
		if err := validateCron(man.Cron); err != nil {
			log.Printf("plugin %s: bad cron %q: %v (loaded without schedule)", name, man.Cron, err)
		}
		p := &Plugin{
			Manifest: man,
			Dir:      name,
			Enabled:  true,
			Logs:     make([]string, 0, logMemoryLimit),
			KV:       make(map[string]string),
		}
		m.loadState(p)
		p.syncPerm()
		m.loadKV(p)
		m.loadLogs(p)
		m.plugins[name] = p
		fresh = append(fresh, name)
		log.Printf("plugin manager: loaded %s v%s (cron=%q events=%v)", name, man.Version, man.Cron, man.Events)
	}
	m.mu.Unlock()
	for _, name := range fresh {
		m.schedule(name)
	}
	return nil
}

// ---- 启停 / 批准 / 删除 ----

// SetEnabled 启停插件（持久化并同步 cron 调度）。
func (m *Manager) SetEnabled(name string, enabled bool) bool {
	m.mu.Lock()
	p, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return false
	}
	p.Enabled = enabled
	p.syncPerm()
	m.persistStateLocked(p)
	m.mu.Unlock()
	m.schedule(name)
	return true
}

// SetApproved 批准/撤销插件高危权限（管理员操作，持久化；撤销即时生效——权限在每次调用时检查）。
func (m *Manager) SetApproved(name string, approved bool) bool {
	m.mu.Lock()
	p, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return false
	}
	p.Permissions.Approved = approved
	p.syncPerm()
	m.persistStateLocked(p)
	m.mu.Unlock()
	m.schedule(name)
	return true
}

// Has 插件是否存在。
func (m *Manager) Has(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.plugins[name]
	return ok
}

// Get 返回插件的深拷贝快照，调用方可在 Manager 锁外安全读取。
func (m *Manager) Get(name string) (*Plugin, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	return clonePlugin(p), ok
}

// List 返回插件深拷贝快照列表。
func (m *Manager) List() []*Plugin {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, clonePlugin(p))
	}
	return out
}

// Delete 删除插件目录并停止其调度。
func (m *Manager) Delete(name string) bool {
	m.mu.Lock()
	_, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return false
	}
	delete(m.plugins, name)
	m.mu.Unlock()
	if eid, ok := m.ids[name]; ok {
		m.cron.Remove(eid)
		delete(m.ids, name)
	}
	return os.RemoveAll(filepath.Join(m.dir, name)) == nil
}

// ---- 调度 ----

// Start 启动 cron 调度器。
func (m *Manager) Start() {
	m.mu.Lock()
	m.stopped = false
	m.mu.Unlock()
	m.cron.Start()
}

// Stop 停止新任务并等待已启动的 cron/hook 异步执行结束。
func (m *Manager) Stop() {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
	<-m.cron.Stop().Done()
	m.asyncWG.Wait()
}

// schedule 依据启停/清单状态注册或移除 cron 任务（onSchedule 事件）。
func (m *Manager) schedule(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	if !ok {
		return
	}
	if eid, ok := m.ids[name]; ok {
		m.cron.Remove(eid)
		delete(m.ids, name)
	}
	if !p.Enabled {
		return
	}
	spec := strings.TrimSpace(p.Cron)
	if spec == "" {
		return
	}
	if _, err := pluginCronParser.Parse(spec); err != nil {
		log.Printf("plugin %s: bad cron %q: %v", p.Name, spec, err)
		return
	}
	eid, err := m.cron.AddFunc(spec, func() {
		m.runAsync(name, "onSchedule", map[string]any{"time": time.Now().Format(time.RFC3339)})
	})
	if err != nil {
		log.Printf("plugin %s: schedule %q: %v", name, spec, err)
		return
	}
	m.ids[name] = eid
}

// ---- 事件 hook ----

// FireEvent 事件分发：对每个声明了该事件且已启用的插件异步执行 hook。
// 异步隔离（各自独立 goroutine/VM）+ 超时 + 禁止并发重入（runAsync 保证）。
func (m *Manager) FireEvent(event string, payload any) {
	for _, p := range m.List() {
		if !p.Enabled || !hasEvent(p.Events, event) {
			continue
		}
		m.runAsync(p.Name, event, payload)
	}
}

func hasEvent(events []string, name string) bool {
	for _, e := range events {
		if e == name {
			return true
		}
	}
	return false
}

// runAsync 异步执行插件（cron/hook 触发）：reentry 防护 + panic 恢复 + 状态落盘。
func (m *Manager) runAsync(name, hook string, payload any) {
	m.mu.Lock()
	p, ok := m.plugins[name]
	if !ok || !p.Enabled || m.stopped {
		m.mu.Unlock()
		return
	}
	if m.running[name] {
		m.mu.Unlock()
		m.addLog(name, "SKIP "+hook+": previous run still active (concurrent reentry blocked)")
		return
	}
	m.running[name] = true
	p.Running = true
	run := clonePlugin(p)
	m.asyncWG.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.asyncWG.Done()
		defer func() {
			m.mu.Lock()
			delete(m.running, name)
			if current, exists := m.plugins[name]; exists {
				current.Running = false
			}
			m.mu.Unlock()
		}()
		err := m.runPlugin(run, hook, payload)
		m.finishRun(name, err)
		if err != nil {
			m.addLog(name, "ERR "+hook+": "+err.Error())
		}
	}()
}

// Run 手动执行插件一次（同步；admin「立即运行」）。仅执行 plugin.js 顶层脚本，不触发 onSchedule。
func (m *Manager) Run(name string) error {
	m.mu.Lock()
	p, ok := m.plugins[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s not found", name)
	}
	if !p.Enabled {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s disabled", name)
	}
	if m.running[name] {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s already running", name)
	}
	m.running[name] = true
	p.Running = true
	run := clonePlugin(p)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.running, name)
		if current, exists := m.plugins[name]; exists {
			current.Running = false
		}
		m.mu.Unlock()
	}()

	err := m.runPlugin(run, "", nil)
	m.finishRun(name, err)
	if err != nil {
		m.addLog(name, "ERR "+err.Error())
	}
	return err
}

// runPlugin 在独立 VM 中执行插件：顶层脚本 + （可选）事件 hook 函数。
// 超时中断、panic 恢复；任何错误都不影响宿主进程。
func (m *Manager) runPlugin(p *Plugin, hook string, payload any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %s panicked: %v", p.Name, r)
		}
	}()

	src, err := os.ReadFile(filepath.Join(m.dir, p.Name, "plugin.js"))
	if err != nil {
		return err
	}

	timeout := m.RunTimeout
	if timeout <= 0 {
		timeout = defaultRunTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	m.injectAPI(vm, p.Name, ctx)

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt("plugin timeout")
		case <-done:
		}
	}()

	if _, err := vm.RunString(string(src)); err != nil {
		return err
	}

	if hook != "" {
		if fnVal := vm.Get(hook); fnVal != nil && !goja.IsUndefined(fnVal) && !goja.IsNull(fnVal) {
			if fn, ok := goja.AssertFunction(fnVal); ok {
				if _, err := fn(goja.Undefined(), vm.ToValue(payload)); err != nil {
					return fmt.Errorf("%s hook: %v", hook, err)
				}
			}
		}
	}
	close(done)
	return nil
}

// finishRun 记录运行状态并持久化。
func (m *Manager) finishRun(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	if !ok {
		return
	}
	p.LastRun = time.Now().Format(time.RFC3339)
	p.RunCount++
	if err != nil {
		p.LastStatus = "error"
		p.LastError = err.Error()
	} else {
		p.LastStatus = "ok"
		p.LastError = ""
	}
	m.persistStateLocked(p)
}

// ---- 宿主 API ----

// injectAPI 注入 console / argus / fetch。
func (m *Manager) injectAPI(vm *goja.Runtime, name string, ctx context.Context) {
	console := vm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		m.logJS(name, "LOG", call)
		return goja.Undefined()
	})
	console.Set("error", func(call goja.FunctionCall) goja.Value {
		m.logJS(name, "ERR", call)
		return goja.Undefined()
	})
	_ = vm.Set("console", console)

	argus := vm.NewObject()

	// argus.log(...) → 插件日志
	argus.Set("log", func(call goja.FunctionCall) goja.Value {
		m.logJS(name, "LOG", call)
		return goja.Undefined()
	})

	// argus.getServers() → 脱敏只读服务器列表
	argus.Set("getServers", func(goja.FunctionCall) goja.Value {
		defer m.recoverToLog(name, "argus.getServers")
		if m.ServerSource == nil {
			return vm.ToValue([]ServerView{})
		}
		return vm.ToValue(m.ServerSource())
	})

	// argus.notify(notificationId, title, content) → 需声明 allow_notify 且已批准
	argus.Set("notify", func(call goja.FunctionCall) goja.Value {
		permissions, ok := m.currentPermissions(name)
		if !ok || !permissions.AllowNotify || !permissions.Approved {
			m.addLog(name, "ERR argus.notify denied: plugin lacks allow_notify/approved permission")
			return vm.ToValue(false)
		}
		if len(call.Arguments) < 3 {
			return vm.ToValue(false)
		}
		id := int64(call.Arguments[0].ToInteger())
		title := call.Arguments[1].String()
		content := call.Arguments[2].String()
		if m.NotifyFunc == nil {
			m.addLog(name, "ERR argus.notify: host notify unavailable")
			return vm.ToValue(false)
		}
		var nerr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					nerr = fmt.Errorf("notify panicked: %v", r)
				}
			}()
			nerr = m.NotifyFunc(id, title, content)
		}()
		if nerr != nil {
			m.addLog(name, fmt.Sprintf("ERR argus.notify %d: %v", id, nerr))
			return vm.ToValue(false)
		}
		return vm.ToValue(true)
	})

	// argus.kv.get/set → 每插件命名空间（kv.json 持久化，大小限制）
	kv := vm.NewObject()
	kv.Set("get", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}
		key := call.Arguments[0].String()
		v, ok := m.kvGet(name, key)
		if !ok {
			return goja.Undefined()
		}
		return vm.ToValue(v)
	})
	kv.Set("set", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue(false)
		}
		key := call.Arguments[0].String()
		value := call.Arguments[1].String()
		if err := m.kvSet(name, key, value); err != nil {
			m.addLog(name, "ERR argus.kv.set "+key+": "+err.Error())
			return vm.ToValue(false)
		}
		return vm.ToValue(true)
	})
	argus.Set("kv", kv)
	_ = vm.Set("argus", argus)

	// fetch(url) → 响应文本；需 allow_fetch + 批准 + 域名白名单 + SSRF 防护
	_ = vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
		permissions, ok := m.currentPermissions(name)
		if !ok || !permissions.AllowFetch || !permissions.Approved {
			m.addLog(name, "ERR fetch denied: plugin lacks allow_fetch/approved permission")
			return goja.Undefined()
		}
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		raw := call.Arguments[0].String()
		text, err := m.doFetch(ctx, permissions.FetchDomains, raw)
		if err != nil {
			m.addLog(name, "ERR fetch "+raw+": "+err.Error())
			return goja.Undefined()
		}
		return vm.ToValue(text)
	})
}

func (m *Manager) currentPermissions(name string) (Permissions, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	if !ok {
		return Permissions{}, false
	}
	permissions := p.Permissions
	permissions.FetchDomains = append([]string(nil), p.Permissions.FetchDomains...)
	return permissions, true
}

func (m *Manager) logJS(name, prefix string, call goja.FunctionCall) {
	parts := make([]string, 0, len(call.Arguments))
	for _, a := range call.Arguments {
		parts = append(parts, a.String())
	}
	m.addLog(name, prefix+" "+strings.Join(parts, " "))
}

func (m *Manager) recoverToLog(name, api string) {
	if r := recover(); r != nil {
		m.addLog(name, "ERR "+api+" panicked: "+fmt.Sprint(r))
	}
}

// ---- KV（每插件命名空间 + 大小限制）----

func (m *Manager) kvGet(name, key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	if !ok || p.KV == nil {
		return "", false
	}
	v, ok := p.KV[key]
	return v, ok
}

func (m *Manager) kvSet(name, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %s not found", name)
	}
	if key == "" || len(key) > kvMaxKeyLen {
		return fmt.Errorf("key length out of range (1-%d)", kvMaxKeyLen)
	}
	if len(value) > kvMaxValueLen {
		return fmt.Errorf("value exceeds %d bytes", kvMaxValueLen)
	}
	if p.KV == nil {
		p.KV = make(map[string]string)
	}
	if _, exists := p.KV[key]; !exists && len(p.KV) >= kvMaxKeys {
		return fmt.Errorf("kv namespace full (%d keys)", kvMaxKeys)
	}
	total := 0
	for k, v := range p.KV {
		total += len(k) + len(v)
	}
	total += len(key) + len(value)
	if total > kvMaxTotalBytes {
		return fmt.Errorf("kv namespace exceeds %d bytes", kvMaxTotalBytes)
	}
	p.KV[key] = value
	data, _ := json.Marshal(p.KV)
	return os.WriteFile(filepath.Join(m.dir, p.Dir, "kv.json"), data, 0o600)
}

// ---- fetch SSRF 防护 ----

func (m *Manager) resolver() LookupIPResolver {
	if m.Resolver != nil {
		return m.Resolver
	}
	return net.DefaultResolver
}

// blockedIP 判定内网/保留地址：回环、私网、链路本地（含 169.254.169.254 云元数据）、
// 链路本地组播、组播、未指定。
func blockedIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// validateHost 校验主机可达性：IP 字面量直接判定；域名解析后任一地址为内网/保留即拒绝
// （DNS 重绑定防护：连接前在 dial 时再查一次，见 dialContext）。
func (m *Manager) validateHost(ctx context.Context, host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return errors.New("empty host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return fmt.Errorf("address %s is blocked (loopback/private/link-local/multicast/metadata)", host)
		}
		return nil
	}
	ips, err := m.resolver().LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve %s: %v", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve %s: no addresses", host)
	}
	for _, a := range ips {
		if blockedIP(a.IP) {
			return fmt.Errorf("host %s resolves to blocked address %s", host, a.IP)
		}
	}
	return nil
}

// dialContext 自定义拨号器：dial 前再次做 DNS 后地址校验（防 DNS 重绑定）。
func (m *Manager) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if err := m.validateHost(ctx, host); err != nil {
		return nil, err
	}
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
}

// hostAllowed 域名白名单匹配（精确或子域）。
func hostAllowed(host string, domains []string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if h == "" {
		return false
	}
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
		if d == "" {
			continue
		}
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

// checkFetchURL 校验 fetch 目标：scheme、无 userinfo、域名白名单、SSRF 地址检查。
func (m *Manager) checkFetchURL(ctx context.Context, raw string, domains []string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("only http(s) allowed")
	}
	if u.User != nil {
		return nil, errors.New("userinfo in url not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("empty host")
	}
	if !hostAllowed(host, domains) {
		return nil, fmt.Errorf("host %s not in fetch_domains allowlist", host)
	}
	if err := m.validateHost(ctx, host); err != nil {
		return nil, err
	}
	return u, nil
}

// doFetch 执行 SSRF 防护的网络请求：响应体大小限制、超时、重定向复查。
func (m *Manager) doFetch(ctx context.Context, domains []string, raw string) (string, error) {
	u, err := m.checkFetchURL(ctx, raw, domains)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if m.Dial != nil {
					return m.Dial(ctx, network, addr)
				}
				return m.dialContext(ctx, network, addr)
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > fetchMaxRedirect {
				return errors.New("too many redirects")
			}
			// 重定向目标重新做白名单 + SSRF 检查（dial 时还会再校验一次地址）
			if _, err := m.checkFetchURL(ctx, req.URL.String(), domains); err != nil {
				return fmt.Errorf("redirect blocked: %v", err)
			}
			return nil
		},
	}
	resp, err := client.Get(u.String())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBody))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ---- 持久化 ----

// persistStateLocked 持久化启停/批准/运行状态到插件目录 state.json（调用方持有 m.mu）。
func (m *Manager) persistStateLocked(p *Plugin) {
	data, _ := json.Marshal(map[string]any{
		"enabled":     p.Enabled,
		"approved":    p.Permissions.Approved,
		"last_run":    p.LastRun,
		"last_status": p.LastStatus,
		"last_error":  p.LastError,
		"run_count":   p.RunCount,
	})
	_ = os.WriteFile(filepath.Join(m.dir, p.Dir, "state.json"), data, 0o600)
}

// loadState 启动加载 state.json（覆盖 manifest 默认启停/批准与运行状态）。
func (m *Manager) loadState(p *Plugin) {
	data, err := os.ReadFile(filepath.Join(m.dir, p.Dir, "state.json"))
	if err != nil {
		return
	}
	var st struct {
		Enabled    *bool  `json:"enabled"`
		Approved   *bool  `json:"approved"`
		LastRun    string `json:"last_run"`
		LastStatus string `json:"last_status"`
		LastError  string `json:"last_error"`
		RunCount   int64  `json:"run_count"`
	}
	if json.Unmarshal(data, &st) == nil {
		if st.Enabled != nil {
			p.Enabled = *st.Enabled
		}
		if st.Approved != nil {
			p.Permissions.Approved = *st.Approved
		}
		p.LastRun = st.LastRun
		p.LastStatus = st.LastStatus
		p.LastError = st.LastError
		p.RunCount = st.RunCount
	}
}

// loadKV 启动加载 kv.json。
func (m *Manager) loadKV(p *Plugin) {
	data, err := os.ReadFile(filepath.Join(m.dir, p.Dir, "kv.json"))
	if err != nil {
		return
	}
	var kv map[string]string
	if json.Unmarshal(data, &kv) == nil {
		p.KV = kv
	}
}

// addLog 追加日志：内存环形 50 条 + logs.json 持久化 200 条（重启保留）。
func (m *Manager) addLog(name, line string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[name]
	if !ok {
		return
	}
	ts := time.Now().Format("15:04:05")
	entry := ts + " " + line
	p.Logs = append(p.Logs, entry)
	if len(p.Logs) > logMemoryLimit {
		p.Logs = p.Logs[len(p.Logs)-logMemoryLimit:]
	}
	path := filepath.Join(m.dir, p.Dir, "logs.json")
	var old []string
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &old)
	}
	old = append(old, entry)
	if len(old) > logFileLimit {
		old = old[len(old)-logFileLimit:]
	}
	if data, err := json.Marshal(old); err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}

// loadLogs 启动加载 logs.json（重启保留日志）。
func (m *Manager) loadLogs(p *Plugin) {
	data, err := os.ReadFile(filepath.Join(m.dir, p.Dir, "logs.json"))
	if err != nil {
		return
	}
	var logs []string
	if json.Unmarshal(data, &logs) == nil && len(logs) > 0 {
		if len(logs) > logMemoryLimit {
			logs = logs[len(logs)-logMemoryLimit:]
		}
		p.Logs = logs
	}
}

// ---- 市场 ----

// MarketDir 市场目录（main 注入，如 ./data/market/plugins）。
var MarketDir = "./data/market/plugins"

// MarketEntry 市场插件条目。
type MarketEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	SHA256      string `json:"sha256,omitempty"`    // 目录确定性哈希（安装校验）
	Signature   string `json:"signature,omitempty"` // 预留：Ed25519 签名（base64），当前不校验
}

// MarketIndex 远程市场索引（index.json）。
type MarketIndex struct {
	Version int               `json:"version"`
	Plugins []MarketIndexItem `json:"plugins"`
}

// MarketIndexItem 索引条目。Signature 预留 Ed25519 签名（base64），
// 当前仅记录与展示，待后续版本实现签名验证。
type MarketIndexItem struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature,omitempty"`
}

func loadMarketIndex() (*MarketIndex, error) {
	data, err := os.ReadFile(filepath.Join(MarketDir, "index.json"))
	if err != nil {
		return nil, err
	}
	var idx MarketIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// readMarketManifest 读取市场插件清单。
func readMarketManifest(name string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(MarketDir, name, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, err
	}
	if man.Name == "" {
		man.Name = name
	}
	return &man, nil
}

// ListMarket 市场插件列表。存在 index.json 时以索引为准（含 sha256/签名）；
// 否则退回旧式目录扫描（仅展示，安装将被拒绝）。
func (m *Manager) ListMarket() []MarketEntry {
	if idx, err := loadMarketIndex(); err == nil {
		out := make([]MarketEntry, 0, len(idx.Plugins))
		for _, it := range idx.Plugins {
			desc := ""
			if man, err := readMarketManifest(it.Name); err == nil {
				desc = man.Description
			}
			out = append(out, MarketEntry{
				Name:        it.Name,
				Version:     it.Version,
				Description: desc,
				SHA256:      it.SHA256,
				Signature:   it.Signature,
				Installed:   m.Has(it.Name),
			})
		}
		return out
	}
	entries, err := os.ReadDir(MarketDir)
	if err != nil {
		return nil
	}
	out := make([]MarketEntry, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		man, err := readMarketManifest(e.Name())
		if err != nil {
			continue
		}
		if man.Permissions.AllowExec {
			continue // 拒绝展示带已删除权限的插件
		}
		out = append(out, MarketEntry{
			Name:        e.Name(),
			Description: man.Description,
			Version:     man.Version,
			Installed:   m.Has(e.Name()),
		})
	}
	return out
}

// InstallFromMarket 从市场安装插件：必须存在 index.json 且 SHA-256 校验通过。
func (m *Manager) InstallFromMarket(name string) error {
	idx, err := loadMarketIndex()
	if err != nil {
		return fmt.Errorf("market index.json missing (%v); install requires a verified index with sha256", err)
	}
	var item *MarketIndexItem
	for i := range idx.Plugins {
		if idx.Plugins[i].Name == name {
			item = &idx.Plugins[i]
			break
		}
	}
	if item == nil {
		return fmt.Errorf("market plugin %s not in index", name)
	}
	if item.SHA256 == "" {
		return fmt.Errorf("market index entry %s has no sha256; install refused", name)
	}
	src := filepath.Join(MarketDir, name)
	if _, err := os.Stat(filepath.Join(src, "manifest.json")); err != nil {
		return fmt.Errorf("market plugin %s not found", name)
	}
	got, err := dirSHA256(src)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, item.SHA256) {
		return fmt.Errorf("market plugin %s sha256 mismatch (got %s, index %s); install refused", name, got, item.SHA256)
	}
	if item.Signature != "" {
		log.Printf("market %s: Ed25519 signature present; verification reserved for a future release (not enforced)", name)
	}
	dst := filepath.Join(m.dir, name)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return copyDir(src, dst)
}

// dirSHA256 对插件目录做确定性哈希：按相对路径排序，
// 逐文件「相对路径:文件sha256」行拼接后再整体 sha256。
func dirSHA256(dir string) (string, error) {
	var rels []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(rels)
	h := sha256.New()
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		fileHash := sha256.Sum256(data)
		fmt.Fprintf(h, "%s:%s\n", rel, hex.EncodeToString(fileHash[:]))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
