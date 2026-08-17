// Package theme 主题包管理（里程碑8）：
//
// 主题包 = ZIP 归档，仅允许 CSS 变量/CSS 与受限静态资源（图片/图标/woff 字体），
// 禁止 JS/HTML 等可执行内容；必须携带 manifest.json：
//
//	{
//	  "name": "midnight",            // 小写字母数字 - _，≤32 字符，作为目录名/URL 段
//	  "display_name": "午夜蓝",       // 展示名（≤64）
//	  "version": "1.2.0",            // 语义化版本 x.y.z
//	  "argus": ">=0.1.0",            // 兼容的 Argus 版本约束（* 或 >=x.y.z）
//	  "author": "motao",             // 作者（≤64）
//	  "entry": "css/theme.css",      // 入口 CSS 相对路径（必填）
//	  "preview": "preview.png"       // 预览图相对路径（可选）
//	}
//
// 安全模型：
//   - ZIP 大小与解压总量上限（防 zip bomb），文件数与单文件大小上限
//   - Zip Slip 防护：拒绝绝对路径与 "../" 穿越，统一按 "/" 规范化
//   - 拒绝 symlink / 硬链接条目
//   - 扩展名白名单：.css .png .jpg .jpeg .gif .svg .webp .ico .woff .woff2
//   - 市场安装：HTTPS + SHA-256 校验，staging 后原子安装
//   - 原子安装：解压到 staging 目录 → 全部校验通过 → rename 切换；
//     旧版本保留为 <name>.rollback 支持回滚；失败自动还原
//   - 损坏回退：启动/读取时校验当前主题，缺失或损坏回退默认主题
//   - 删除当前主题：先切换默认主题再删除
package theme

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---- 安全限制 ----

const (
	// MaxZipSize 上传/下载的 ZIP 体积上限（4 MiB）。
	MaxZipSize = 4 << 20
	// MaxUncompressedSize 解压后总体积上限（32 MiB，防 zip bomb）。
	MaxUncompressedSize = 32 << 20
	// MaxFiles ZIP 内文件数上限。
	MaxFiles = 200
	// MaxFileSize 单个文件解压后大小上限（4 MiB）。
	MaxFileSize = 4 << 20
	// MaxPathLength 归档内路径长度上限。
	MaxPathLength = 200
	// ActiveFileName 当前启用主题标记文件名。
	ActiveFileName = ".active"
	// RollbackSuffix 回滚备份目录后缀。
	RollbackSuffix = ".rollback"
	// DefaultName 内置默认主题名。
	DefaultName = "default"
)

// allowedExts 主题包内允许的文件扩展名（白名单）。
var allowedExts = map[string]bool{
	".css": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".ico": true, ".woff": true, ".woff2": true,
}

// Manifest 主题包清单。
type Manifest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"` // 语义化版本 x.y.z
	Argus       string `json:"argus"`   // Argus 兼容约束：* 或 >=x.y.z
	Author      string `json:"author"`
	Entry       string `json:"entry"`   // 入口 CSS 相对路径
	Preview     string `json:"preview"` // 预览图相对路径（可选）
}

// Theme 已安装主题。
type Theme struct {
	Manifest
	Dir       string `json:"dir"`       // 目录名（= name）
	Active    bool   `json:"active"`    // 当前是否启用
	Rollback  bool   `json:"rollback"`  // 是否存在可回滚的旧版本
	Installed bool   `json:"installed"` // 市场条目：是否已安装
}

// MarketEntry 市场条目（远程静态索引或本地市场目录）。
type MarketEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	Installed   bool   `json:"installed"`
}

// Manager 主题管理器。
type Manager struct {
	dir string // 主题根目录，如 ./data/themes

	// MarketIndexURL 远程静态市场索引（仅 HTTPS；空 = 禁用远程市场）。
	MarketIndexURL string

	// HTTPClient 下载市场 ZIP 用（默认 30s 超时客户端）。
	HTTPClient *http.Client

	// 可注入的取数函数（测试用；默认走真实 HTTP）。
	fetchIndex func() ([]byte, error)
	fetchZip   func(downloadURL string, maxSize int64) ([]byte, error)
}

// New 创建管理器。
func New(dir string) *Manager {
	return &Manager{
		dir:        dir,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ---- 名称/版本校验 ----

// validName 主题名：小写字母数字开头，仅 [a-z0-9_-]，≤32 字符（URL/目录安全）。
func validName(name string) bool {
	if len(name) == 0 || len(name) > 32 || name == DefaultName {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validVersion 语义化版本 x.y.z。
func validVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 6 {
			return false
		}
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
	}
	return true
}

// CompatArgus 判断 argus 约束是否与 serverVersion 兼容。
// 支持 "*"（任意）与 ">=x.y.z"（简单前缀比较）。
func CompatArgus(constraint, serverVersion string) bool {
	if constraint == "" || constraint == "*" {
		return true
	}
	if !strings.HasPrefix(constraint, ">=") {
		return false
	}
	need := strings.TrimSpace(strings.TrimPrefix(constraint, ">="))
	if !validVersion(need) || !validVersion(serverVersion) {
		return false
	}
	ns, ms := strings.Split(need, "."), strings.Split(serverVersion, ".")
	for i := 0; i < 3; i++ {
		a, b := 0, 0
		fmt.Sscanf(ns[i], "%d", &a)
		fmt.Sscanf(ms[i], "%d", &b)
		if b > a {
			return true
		}
		if b < a {
			return false
		}
	}
	return true
}

// ValidateManifest 校验清单字段。
func ValidateManifest(m *Manifest) error {
	if !validName(m.Name) {
		return fmt.Errorf("theme: invalid name %q (lowercase alnum/-/_ ≤32, not %q)", m.Name, DefaultName)
	}
	if !validVersion(m.Version) {
		return fmt.Errorf("theme %s: invalid version %q (want x.y.z)", m.Name, m.Version)
	}
	if len(m.DisplayName) == 0 || len(m.DisplayName) > 64 {
		return fmt.Errorf("theme %s: display_name required, ≤64", m.Name)
	}
	if len(m.Author) > 64 {
		return fmt.Errorf("theme %s: author too long", m.Name)
	}
	if len(m.Entry) == 0 || !allowedAsset(m.Entry) || path.Ext(m.Entry) != ".css" {
		return fmt.Errorf("theme %s: entry must be a .css file inside the package", m.Name)
	}
	if !validRelPath(m.Entry) {
		return fmt.Errorf("theme %s: entry %q unsafe path", m.Name, m.Entry)
	}
	if m.Preview != "" && (!allowedAsset(m.Preview) || !validRelPath(m.Preview)) {
		return fmt.Errorf("theme %s: preview %q must be an allowed image path", m.Name, m.Preview)
	}
	return nil
}

// ---- 路径安全 ----

// validRelPath 相对路径安全校验：无绝对路径、无 .. 穿越、无盘符、长度受限。
func validRelPath(p string) bool {
	if p == "" || len(p) > MaxPathLength || strings.HasPrefix(p, "/") {
		return false
	}
	clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	// Windows 盘符（C:/...）在 Linux 上不是穿越，但禁止更稳妥
	if len(clean) >= 2 && clean[1] == ':' && (clean[0] >= 'A' && clean[0] <= 'Z' || clean[0] >= 'a' && clean[0] <= 'z') {
		return false
	}
	return true
}

// allowedAsset 扩展名白名单。
func allowedAsset(name string) bool {
	return allowedExts[strings.ToLower(path.Ext(name))]
}

// ---- 目录扫描 ----

// dirPath 主题目录（name 已校验）。
func (m *Manager) dirPath(name string) string {
	return filepath.Join(m.dir, name)
}

// readManifest 读取目录内 manifest.json。
func readManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, fmt.Errorf("bad manifest: %v", err)
	}
	return &man, nil
}

// List 已安装主题（含内置默认主题）。
func (m *Manager) List() []*Theme {
	active := m.Active()
	out := []*Theme{{Manifest: Manifest{
		Name: DefaultName, DisplayName: "Default", Version: "1.0.0",
		Argus: "*", Author: "Argus", Entry: "default.css",
	}, Dir: DefaultName, Active: active == DefaultName}}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return out
	}
	seen := map[string]bool{DefaultName: true}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, RollbackSuffix) || strings.HasPrefix(name, ".") {
			continue
		}
		if !validName(name) {
			continue
		}
		man, err := readManifest(m.dirPath(name))
		if err != nil {
			continue // 无有效 manifest 不视为主题
		}
		seen[name] = true
		_, rollback := m.HasRollback(name)
		out = append(out, &Theme{
			Manifest: *man, Dir: name,
			Active:   active == name,
			Rollback: rollback,
		})
	}
	sort.Slice(out[1:], func(i, j int) bool { return out[i+1].Name < out[j+1].Name })
	return out
}

// healthy 主题健康检查：manifest 可读 + 入口 CSS 存在（损坏 → 回退默认的依据）。
func (m *Manager) healthy(name string) bool {
	dir := m.dirPath(name)
	man, err := readManifest(dir)
	if err != nil {
		return false
	}
	if !validRelPath(man.Entry) {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(man.Entry))); err != nil {
		return false
	}
	return true
}

// Get 获取单个主题；不存在返回 nil。
func (m *Manager) Get(name string) *Theme {
	if name == DefaultName {
		return &Theme{Manifest: Manifest{
			Name: DefaultName, DisplayName: "Default", Version: "1.0.0",
			Argus: "*", Author: "Argus", Entry: "default.css",
		}, Dir: DefaultName}
	}
	if !validName(name) {
		return nil
	}
	man, err := readManifest(m.dirPath(name))
	if err != nil {
		return nil
	}
	_, rollback := m.HasRollback(name)
	return &Theme{Manifest: *man, Dir: name, Active: m.Active() == name, Rollback: rollback}
}

// HasRollback 是否存在回滚备份。
func (m *Manager) HasRollback(name string) (string, bool) {
	rb := m.dirPath(name) + RollbackSuffix
	if _, err := os.Stat(rb); err == nil {
		return rb, true
	}
	return "", false
}

// ---- 当前主题（损坏回退默认） ----

// Active 当前启用的主题名（默认 "default"）。磁盘标记损坏/缺失时回退默认。
func (m *Manager) Active() string {
	data, err := os.ReadFile(filepath.Join(m.dir, ActiveFileName))
	if err != nil {
		return DefaultName
	}
	name := strings.TrimSpace(string(data))
	if !validName(name) {
		return DefaultName
	}
	// 主题缺失或损坏 → 回退默认（并清理标记）
	if !m.healthy(name) {
		_ = os.Remove(filepath.Join(m.dir, ActiveFileName))
		return DefaultName
	}
	return name
}

// SetActive 启用主题（不存在/损坏返回错误，不修改当前状态）。
func (m *Manager) SetActive(name string) error {
	if name == DefaultName {
		return m.writeActive(DefaultName)
	}
	if !validName(name) {
		return fmt.Errorf("theme %q not found", name)
	}
	if m.Get(name) == nil {
		return fmt.Errorf("theme %q not found", name)
	}
	return m.writeActive(name)
}

// writeActive 原子写入启用标记。
func (m *Manager) writeActive(name string) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(m.dir, ActiveFileName+".tmp")
	if err := os.WriteFile(tmp, []byte(name), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(m.dir, ActiveFileName))
}

// ValidateActive 启动时校验：当前主题损坏则回退默认。返回生效的主题名。
func (m *Manager) ValidateActive() string {
	return m.Active()
}

// ActiveEntry 当前主题的入口 CSS 相对路径（供前端注入 stylesheet）。
func (m *Manager) ActiveEntry() string {
	name := m.Active()
	if name == DefaultName {
		return ""
	}
	th := m.Get(name)
	if th == nil || th.Entry == "" {
		return ""
	}
	return th.Entry
}

// ---- ZIP 校验与原子安装 ----

// Install 安装主题包：校验 SHA-256（wantSHA 非空时）、解压、校验、原子切换。
// 同主题旧版本保留为 <name>.rollback；任何失败不触碰现有主题。
func (m *Manager) Install(data []byte, wantSHA string) (*Theme, error) {
	if len(data) == 0 || len(data) > MaxZipSize {
		return nil, fmt.Errorf("theme: zip size %d exceeds limit (%d bytes)", len(data), MaxZipSize)
	}
	if wantSHA != "" {
		got := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(got[:]), wantSHA) {
			return nil, fmt.Errorf("theme: sha256 mismatch (want %s)", wantSHA)
		}
	}
	man, files, err := extractAndValidate(data)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return nil, err
	}
	staging := filepath.Join(m.dir, fmt.Sprintf(".staging-%d", time.Now().UnixNano()))
	defer os.RemoveAll(staging)
	if err := writeExtracted(staging, files); err != nil {
		return nil, err
	}
	// 写入清单（校验通过后以归档内 manifest 为准）
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), marshalManifest(man), 0o644); err != nil {
		return nil, err
	}
	// 二次校验 staging（manifest 与 entry 存在性）
	if _, err := readManifest(staging); err != nil {
		return nil, fmt.Errorf("theme: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, man.Entry)); err != nil {
		return nil, fmt.Errorf("theme %s: entry %q missing", man.Name, man.Entry)
	}
	if man.Preview != "" {
		if _, err := os.Stat(filepath.Join(staging, man.Preview)); err != nil {
			return nil, fmt.Errorf("theme %s: preview %q missing", man.Name, man.Preview)
		}
	}
	return m.swapIn(man, staging)
}

// swapIn 原子切换：旧版 → <name>.rollback，staging → <name>；失败自动还原。
func (m *Manager) swapIn(man *Manifest, staging string) (*Theme, error) {
	name := man.Name
	dst := m.dirPath(name)
	rb := dst + RollbackSuffix
	// 清理旧回滚
	_ = os.RemoveAll(rb)
	// 先把现有主题挪为回滚备份
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, rb); err != nil {
			return nil, err
		}
	}
	// staging → 正式目录
	if err := os.Rename(staging, dst); err != nil {
		_ = os.Rename(rb, dst) // 还原旧版本
		return nil, err
	}
	_, hasRB := m.HasRollback(name)
	return &Theme{Manifest: *man, Dir: name, Active: m.Active() == name, Rollback: hasRB}, nil
}

// extractAndValidate 解压并全量校验（内存路径安全 + 限额 + 白名单 + 清单）。
// 返回清单与提取的文件映射（clean rel path → 内容）。
func extractAndValidate(data []byte) (*Manifest, map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("theme: invalid zip: %v", err)
	}
	if len(zr.File) > MaxFiles {
		return nil, nil, fmt.Errorf("theme: too many files (%d > %d)", len(zr.File), MaxFiles)
	}
	var total uint64
	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		// 拒绝 symlink / 非普通文件
		if f.Mode()&os.ModeSymlink != 0 || f.Mode()&os.ModeType != 0 && f.Mode()&os.ModeDir == 0 {
			return nil, nil, fmt.Errorf("theme: %q: symlink/device entries are not allowed", f.Name)
		}
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if strings.HasSuffix(f.Name, "/") {
			continue // 目录条目
		}
		if !validRelPath(name) {
			return nil, nil, fmt.Errorf("theme: %q: unsafe path (absolute or traversal)", f.Name)
		}
		if name == ".." || strings.HasPrefix(name, "../") {
			return nil, nil, fmt.Errorf("theme: %q: path traversal rejected", f.Name)
		}
		// 扩展名白名单（manifest.json 仅允许根目录一个）
		if name != "manifest.json" && !allowedAsset(name) {
			return nil, nil, fmt.Errorf("theme: %q: extension not allowed (css/images/fonts only, no JS)", f.Name)
		}
		if f.UncompressedSize64 > MaxFileSize {
			return nil, nil, fmt.Errorf("theme: %q: file too large (%d bytes)", f.Name, f.UncompressedSize64)
		}
		total += f.UncompressedSize64
		if total > MaxUncompressedSize {
			return nil, nil, fmt.Errorf("theme: uncompressed size %d exceeds limit (%d)", total, MaxUncompressedSize)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, fmt.Errorf("theme: %q: %v", f.Name, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, MaxFileSize+1))
		rc.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("theme: %q: read: %v", f.Name, err)
		}
		if len(content) > MaxFileSize {
			return nil, nil, fmt.Errorf("theme: %q: file too large", f.Name)
		}
		if _, dup := files[name]; dup {
			return nil, nil, fmt.Errorf("theme: duplicate entry %q", name)
		}
		files[name] = content
	}
	raw, ok := files["manifest.json"]
	if !ok {
		return nil, nil, fmt.Errorf("theme: manifest.json missing")
	}
	var man Manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return nil, nil, fmt.Errorf("theme: bad manifest: %v", err)
	}
	if err := ValidateManifest(&man); err != nil {
		return nil, nil, err
	}
	if man.Entry != "" {
		if _, ok := files[man.Entry]; !ok {
			return nil, nil, fmt.Errorf("theme %s: entry %q missing", man.Name, man.Entry)
		}
	}
	if man.Preview != "" {
		if _, ok := files[man.Preview]; !ok {
			return nil, nil, fmt.Errorf("theme %s: preview %q missing", man.Name, man.Preview)
		}
	}
	return &man, files, nil
}

// writeExtracted 把校验后的文件写入目录。
func writeExtracted(dir string, files map[string][]byte) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}

func marshalManifest(m *Manifest) []byte {
	data, _ := json.MarshalIndent(m, "", "  ")
	return data
}

// Rollback 回滚主题到上一版本（<name> ↔ <name>.rollback 交换）。
func (m *Manager) Rollback(name string) error {
	if !validName(name) {
		return fmt.Errorf("theme %q not found", name)
	}
	dst := m.dirPath(name)
	rb, ok := m.HasRollback(name)
	if !ok {
		return fmt.Errorf("theme %s: no rollback available", name)
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.Rename(rb, dst); err != nil {
		return err
	}
	return nil
}

// Delete 删除主题；当前启用主题先切换默认再删除。
func (m *Manager) Delete(name string) error {
	if !validName(name) {
		return fmt.Errorf("theme %q not found", name)
	}
	if m.Get(name) == nil {
		return fmt.Errorf("theme %q not found", name)
	}
	if m.Active() == name {
		if err := m.SetActive(DefaultName); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(m.dirPath(name)); err != nil {
		return err
	}
	_ = os.RemoveAll(m.dirPath(name) + RollbackSuffix)
	return nil
}

// ---- 静态资源服务（已安装主题） ----

// OpenAsset 读取主题内静态资源（白名单 + 路径安全校验）。返回内容与 MIME 提示。
func (m *Manager) OpenAsset(name, rel string) ([]byte, error) {
	if name == DefaultName || !validName(name) || !validRelPath(rel) || !allowedAsset(rel) {
		return nil, os.ErrNotExist
	}
	if _, err := readManifest(m.dirPath(name)); err != nil {
		return nil, os.ErrNotExist
	}
	full := filepath.Join(m.dirPath(name), filepath.FromSlash(rel))
	if !strings.HasPrefix(full, m.dirPath(name)+string(filepath.Separator)) {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(full)
}

// ---- 远程市场（HTTPS + SHA-256 + staging 原子安装） ----

// marketIndex 远程静态索引结构。
type marketIndex struct {
	Themes []MarketEntry `json:"themes"`
}

// ListMarket 市场主题列表（远程 HTTPS 索引；禁用/失败时为空）。
func (m *Manager) ListMarket() []MarketEntry {
	if m.MarketIndexURL == "" {
		return nil
	}
	data, err := m.getIndex()
	if err != nil {
		return nil
	}
	var idx marketIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil
	}
	installed := m.installedSet()
	for i := range idx.Themes {
		t := &idx.Themes[i]
		t.Installed = installed[t.Name]
	}
	return idx.Themes
}

func (m *Manager) getIndex() ([]byte, error) {
	if m.fetchIndex != nil {
		return m.fetchIndex()
	}
	u, err := url.Parse(m.MarketIndexURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("theme market index must be https")
	}
	resp, err := m.HTTPClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("market index: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, MaxZipSize))
}

// marketEntry 按名称查市场条目。
func (m *Manager) marketEntry(name string) (*MarketEntry, error) {
	for _, e := range m.ListMarket() {
		if e.Name == name {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("market theme %q not found", name)
}

func (m *Manager) installedSet() map[string]bool {
	set := make(map[string]bool)
	for _, th := range m.List() {
		set[th.Name] = true
	}
	return set
}

// InstallFromMarket 从市场安装：下载（限尺寸）→ SHA-256 校验 → 原子安装。
func (m *Manager) InstallFromMarket(name string) error {
	entry, err := m.marketEntry(name)
	if err != nil {
		return err
	}
	// 强制 HTTPS 下载地址
	u, err := url.Parse(entry.DownloadURL)
	if entry.DownloadURL == "" || err != nil || u.Scheme != "https" {
		return fmt.Errorf("market theme %s: download url must be https", name)
	}
	data, err := m.getZip(entry.DownloadURL)
	if err != nil {
		return fmt.Errorf("market theme %s: download: %v", name, err)
	}
	if _, err := m.Install(data, entry.SHA256); err != nil {
		return fmt.Errorf("market theme %s: %v", name, err)
	}
	return nil
}

func (m *Manager) getZip(downloadURL string) ([]byte, error) {
	if m.fetchZip != nil {
		return m.fetchZip(downloadURL, MaxZipSize)
	}
	u, err := url.Parse(downloadURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("only https downloads allowed")
	}
	resp, err := m.HTTPClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxZipSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxZipSize {
		return nil, fmt.Errorf("zip exceeds %d bytes", MaxZipSize)
	}
	return data, nil
}
