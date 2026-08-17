// 里程碑8 主题包安全测试：恶意 ZIP、路径穿越、symlink、哈希不符、回滚、损坏回退。
package theme

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeZip 构造主题 ZIP：files = rel path → content；extras 可注入特殊条目。
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// makeSymlinkZip 构造含 symlink 条目的 ZIP（Unix 创建者 + symlink 模式位）。
func makeSymlinkZip(t *testing.T, target string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{
		Name:           "evil.css",
		CreatorVersion: 3<<8 | 20, // creator system = unix
		ExternalAttrs:  0o120777 << 16,
	}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	_, _ = w.Write([]byte(target))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// validManifest 生成合法清单。
func validManifest() string {
	return `{"name":"midnight","display_name":"午夜蓝","version":"1.2.0","argus":">=0.1.0","author":"motao","entry":"css/theme.css","preview":"preview.png"}`
}

func validFiles() map[string]string {
	return map[string]string{
		"manifest.json": validManifest(),
		"css/theme.css": ":root{--bg:#000}",
		"preview.png":   "PNGDATA",
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	return New(filepath.Join(dir, "themes"))
}

func TestInstallValidTheme(t *testing.T) {
	m := newTestManager(t)
	th, err := m.Install(makeZip(t, validFiles()), "")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if th.Name != "midnight" || th.Dir != "midnight" {
		t.Fatalf("unexpected theme: %+v", th)
	}
	if m.Active() != DefaultName {
		t.Fatalf("active should default, got %q", m.Active())
	}
	if err := m.SetActive("midnight"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if m.Active() != "midnight" {
		t.Fatalf("active = %q, want midnight", m.Active())
	}
	if got := m.ActiveEntry(); got != "css/theme.css" {
		t.Fatalf("entry = %q", got)
	}
	// 静态资源可读
	data, err := m.OpenAsset("midnight", "css/theme.css")
	if err != nil || !strings.Contains(string(data), "--bg:#000") {
		t.Fatalf("open asset: %v", err)
	}
	// 白名单外资源拒绝
	if _, err := m.OpenAsset("midnight", "manifest.json"); err == nil {
		t.Fatal("manifest.json should not be servable")
	}
	if _, err := m.OpenAsset("midnight", "../outside.css"); err == nil {
		t.Fatal("traversal asset should be rejected")
	}
}

func TestRejectPathTraversal(t *testing.T) {
	m := newTestManager(t)
	cases := map[string]map[string]string{
		"../escape": {
			"manifest.json": validManifest(),
			"../evil.css":   "x",
		},
		"absolute": {
			"manifest.json":   validManifest(),
			"/etc/passwd.css": "x",
		},
		"deep-traversal": {
			"manifest.json":              validManifest(),
			"css/../../outside/evil.css": "x",
		},
		"windows-drive": {
			"manifest.json": validManifest(),
			"C:/evil.css":   "x",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := m.Install(makeZip(t, files), ""); err == nil {
				t.Fatal("expected rejection, got nil")
			}
			// 安装失败不得留下任何主题目录
			entries, _ := os.ReadDir(m.dir)
			for _, e := range entries {
				if !strings.HasPrefix(e.Name(), ".") {
					t.Fatalf("unexpected dir left behind: %s", e.Name())
				}
			}
		})
	}
}

func TestRejectSymlink(t *testing.T) {
	m := newTestManager(t)
	data := makeSymlinkZip(t, "/etc/passwd")
	if _, err := m.Install(data, ""); err == nil {
		t.Fatal("expected symlink rejection")
	} else if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink error, got: %v", err)
	}
}

func TestRejectJavascriptAndDisallowedExt(t *testing.T) {
	m := newTestManager(t)
	cases := []string{"script.js", "theme.html", "evil.php", "index.htm", "data.json"}
	for _, extra := range cases {
		files := validFiles()
		files[extra] = "boom"
		if _, err := m.Install(makeZip(t, files), ""); err == nil {
			t.Fatalf("expected rejection for %s", extra)
		}
	}
}

func TestRejectOversize(t *testing.T) {
	m := newTestManager(t)
	// 超总解压上限
	big := make([]byte, MaxUncompressedSize/2)
	for i := range big {
		big[i] = 'a'
	}
	files := validFiles()
	files["css/big.css"] = string(big)
	files["css/big2.css"] = string(big)
	if _, err := m.Install(makeZip(t, files), ""); err == nil {
		t.Fatal("expected oversize rejection")
	}
	// 超单文件上限
	single := make([]byte, MaxFileSize+1)
	for i := range single {
		single[i] = 'b'
	}
	files2 := validFiles()
	files2["css/single.css"] = string(single)
	if _, err := m.Install(makeZip(t, files2), ""); err == nil {
		t.Fatal("expected single-file oversize rejection")
	}
	// 超 ZIP 体积上限
	tooBig := append(makeZip(t, validFiles()), make([]byte, MaxZipSize)...)
	if _, err := m.Install(tooBig, ""); err == nil {
		t.Fatal("expected zip size rejection")
	}
}

func TestSha256Mismatch(t *testing.T) {
	m := newTestManager(t)
	data := makeZip(t, validFiles())
	good := sha256.Sum256(data)
	if _, err := m.Install(data, hex.EncodeToString(good[:])); err != nil {
		t.Fatalf("install with correct sha: %v", err)
	}
	// 错误哈希
	wrong := strings.Repeat("0", 64)
	if _, err := m.Install(data, wrong); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("expected sha256 mismatch, got %v", err)
	}
	// 篡改内容
	data[100] ^= 0xff
	if _, err := m.Install(data, hex.EncodeToString(good[:])); err == nil {
		t.Fatal("expected mismatch for tampered data")
	}
}

func TestRollback(t *testing.T) {
	m := newTestManager(t)
	v1 := validFiles()
	if _, err := m.Install(makeZip(t, v1), ""); err != nil {
		t.Fatalf("install v1: %v", err)
	}
	v2 := validFiles()
	v2["manifest.json"] = strings.Replace(validManifest(), "1.2.0", "1.3.0", 1)
	if _, err := m.Install(makeZip(t, v2), ""); err != nil {
		t.Fatalf("install v2: %v", err)
	}
	th := m.Get("midnight")
	if th == nil || th.Version != "1.3.0" || !th.Rollback {
		t.Fatalf("expected v1.3.0 with rollback, got %+v", th)
	}
	if err := m.Rollback("midnight"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	th = m.Get("midnight")
	if th == nil || th.Version != "1.2.0" {
		t.Fatalf("rollback should restore v1.2.0, got %+v", th)
	}
	// 无回滚时报错
	if err := m.Rollback("midnight"); err == nil {
		t.Fatal("expected error when no rollback")
	}
}

func TestInstallFailureKeepsOldVersion(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Install(makeZip(t, validFiles()), ""); err != nil {
		t.Fatalf("install v1: %v", err)
	}
	// 坏包覆盖安装
	bad := validFiles()
	bad["script.js"] = "boom"
	if _, err := m.Install(makeZip(t, bad), ""); err == nil {
		t.Fatal("expected failure")
	}
	th := m.Get("midnight")
	if th == nil || th.Version != "1.2.0" {
		t.Fatalf("old version must survive, got %+v", th)
	}
	if _, err := m.OpenAsset("midnight", "css/theme.css"); err != nil {
		t.Fatalf("old asset must survive: %v", err)
	}
}

func TestDeleteActiveThemeSwitchesToDefault(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Install(makeZip(t, validFiles()), ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	_ = m.SetActive("midnight")
	if err := m.Delete("midnight"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if m.Active() != DefaultName {
		t.Fatalf("active after delete = %q, want default", m.Active())
	}
	if m.Get("midnight") != nil {
		t.Fatal("theme should be gone")
	}
}

func TestCorruptedActiveFallsBackToDefault(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Install(makeZip(t, validFiles()), ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	_ = m.SetActive("midnight")
	// 模拟损坏：删掉入口 CSS
	if err := os.Remove(filepath.Join(m.dir, "midnight", "css", "theme.css")); err != nil {
		t.Fatalf("remove entry: %v", err)
	}
	// 损坏主题（entry 缺失）→ 回退默认
	if m.Active() != DefaultName {
		t.Fatalf("active = %q, want fallback to default", m.Active())
	}
	// 删掉整个目录同样回退
	_ = m.SetActive("midnight")
	if err := os.RemoveAll(filepath.Join(m.dir, "midnight")); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	if m.Active() != DefaultName {
		t.Fatalf("active = %q after dir removal, want default", m.Active())
	}
}

func TestValidateManifest(t *testing.T) {
	cases := []string{
		`{"name":"BadName","version":"1.0.0","entry":"a.css"}`,
		`{"name":"ok","version":"1.0","entry":"a.css"}`,
		`{"name":"ok","version":"1.0.0","entry":"../a.css"}`,
		`{"name":"ok","version":"1.0.0","entry":"script.js"}`,
		`{"name":"default","version":"1.0.0","entry":"a.css"}`,
	}
	for i, c := range cases {
		var man Manifest
		if err := json.Unmarshal([]byte(c), &man); err != nil {
			t.Fatalf("case %d unmarshal: %v", i, err)
		}
		if err := ValidateManifest(&man); err == nil {
			t.Fatalf("case %d should fail: %s", i, c)
		}
	}
	// 合法
	var man Manifest
	if err := json.Unmarshal([]byte(validManifest()), &man); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := ValidateManifest(&man); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
}

func TestCompatArgus(t *testing.T) {
	cases := []struct {
		constraint, ver string
		want            bool
	}{
		{"*", "0.1.0", true},
		{"", "0.1.0", true},
		{">=0.1.0", "0.1.0", true},
		{">=0.1.0", "0.2.0", true},
		{">=1.0.0", "0.9.9", false},
		{">=0.1.0", "0.0.9", false},
		{"<=1.0.0", "1.0.0", false}, // 不支持的约束一律拒绝
	}
	for _, c := range cases {
		if got := CompatArgus(c.constraint, c.ver); got != c.want {
			t.Errorf("CompatArgus(%q, %q) = %v, want %v", c.constraint, c.ver, got, c.want)
		}
	}
}

func TestMarketInstallWithSha(t *testing.T) {
	m := newTestManager(t)
	m.MarketIndexURL = "https://example.com/index.json"
	data := makeZip(t, validFiles())
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	m.fetchIndex = func() ([]byte, error) {
		return []byte(fmt.Sprintf(`{"themes":[{"name":"midnight","display_name":"午夜蓝","version":"1.2.0","author":"motao","description":"d","download_url":"https://example.com/midnight.zip","sha256":"%s","size":%d}]}`, sha, len(data))), nil
	}
	m.fetchZip = func(u string, max int64) ([]byte, error) {
		if u != "https://example.com/midnight.zip" {
			return nil, fmt.Errorf("unexpected url %s", u)
		}
		return data, nil
	}
	entries := m.ListMarket()
	if len(entries) != 1 || entries[0].Name != "midnight" || entries[0].Installed {
		t.Fatalf("market list: %+v", entries)
	}
	if err := m.InstallFromMarket("midnight"); err != nil {
		t.Fatalf("install from market: %v", err)
	}
	th := m.Get("midnight")
	if th == nil || th.Version != "1.2.0" {
		t.Fatalf("market install failed: %+v", th)
	}
	if entries = m.ListMarket(); len(entries) != 1 || !entries[0].Installed {
		t.Fatalf("market installed flag: %+v", entries)
	}
}

func TestMarketRejectsWrongHashAndNonHTTPS(t *testing.T) {
	m := newTestManager(t)
	m.MarketIndexURL = "https://example.com/index.json"
	data := makeZip(t, validFiles())
	m.fetchIndex = func() ([]byte, error) {
		return []byte(`{"themes":[{"name":"midnight","download_url":"http://insecure.example/x.zip","sha256":"` + strings.Repeat("0", 64) + `"}]}`), nil
	}
	m.fetchZip = func(u string, max int64) ([]byte, error) { return data, nil }
	if err := m.InstallFromMarket("midnight"); err == nil {
		t.Fatal("non-https download url must be rejected")
	}
	// https 但哈希不符
	m.fetchIndex = func() ([]byte, error) {
		return []byte(`{"themes":[{"name":"midnight","download_url":"https://example.com/x.zip","sha256":"` + strings.Repeat("0", 64) + `"}]}`), nil
	}
	if err := m.InstallFromMarket("midnight"); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("expected sha256 mismatch, got %v", err)
	}
	if m.Get("midnight") != nil {
		t.Fatal("failed market install must not leave theme")
	}
}

func TestUpgradePreservesActiveState(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.Install(makeZip(t, validFiles()), ""); err != nil {
		t.Fatalf("install v1: %v", err)
	}
	_ = m.SetActive("midnight")
	v2 := validFiles()
	v2["manifest.json"] = strings.Replace(validManifest(), "1.2.0", "2.0.0", 1)
	if _, err := m.Install(makeZip(t, v2), ""); err != nil {
		t.Fatalf("install v2: %v", err)
	}
	if m.Active() != "midnight" {
		t.Fatalf("active should persist across upgrade, got %q", m.Active())
	}
	// v1 内容进入回滚目录
	th := m.Get("midnight")
	if th == nil || th.Version != "2.0.0" || !th.Rollback {
		t.Fatalf("unexpected theme: %+v", th)
	}
}
