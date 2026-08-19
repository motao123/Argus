package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// saveMarketDir 保存并切换 MarketDir（测试后恢复）。
func saveMarketDir(t *testing.T) {
	t.Helper()
	old := MarketDir
	oldKeys := marketTrustedKeys
	t.Cleanup(func() {
		MarketDir = old
		marketTrustedKeys = oldKeys
	})
}

// writeMarketPlugin 在市场目录写入插件。
func writeMarketPlugin(t *testing.T, name string, files map[string]string) {
	t.Helper()
	pdir := filepath.Join(MarketDir, name)
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		path := filepath.Join(pdir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// writeMarketIndex 写入带条目 sha256 的 index.json。
func writeMarketIndex(t *testing.T, entries []MarketIndexItem) {
	t.Helper()
	data, err := json.Marshal(MarketIndex{Version: 1, Plugins: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(MarketDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func signedMarketItem(t *testing.T, name, version, hash, keyID string, privateKey ed25519.PrivateKey) MarketIndexItem {
	t.Helper()
	item := MarketIndexItem{Name: name, Version: version, SHA256: hash, KeyID: keyID}
	item.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, marketSignaturePayload(item)))
	return item
}

func configureTestMarketKey(t *testing.T, keyID string) ed25519.PrivateKey {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetMarketTrustedKeys(keyID + "=" + base64.StdEncoding.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	return privateKey
}

// TestMarketInstallChecksum 市场安装：SHA-256 校验通过才能安装；篡改后拒绝。
func TestMarketInstallChecksum(t *testing.T) {
	dir := t.TempDir()
	market := t.TempDir()
	saveMarketDir(t)
	MarketDir = market

	writeMarketPlugin(t, "demo", map[string]string{
		"manifest.json": `{"name":"demo","version":"1.0.0","description":"d","permissions":{}}`,
		"plugin.js":     `console.log("demo")`,
	})
	hash, err := dirSHA256(filepath.Join(market, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	privateKey := configureTestMarketKey(t, "release-2026")
	writeMarketIndex(t, []MarketIndexItem{signedMarketItem(t, "demo", "1.0.0", hash, "release-2026", privateKey)})

	m := New(dir)
	if err := m.InstallFromMarket("demo"); err != nil {
		t.Fatalf("install with matching sha256 should succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo", "plugin.js")); err != nil {
		t.Fatalf("installed files missing: %v", err)
	}
	// 与主流程一致：安装后 Load 注册
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if !m.Has("demo") {
		t.Fatal("installed plugin should be registered after Load")
	}

	// 篡改市场源文件后安装被拒
	if err := os.WriteFile(filepath.Join(market, "demo", "plugin.js"), []byte(`console.log("tampered")`), 0o644); err != nil {
		t.Fatal(err)
	}
	m2 := New(filepath.Join(t.TempDir()))
	if err := m2.InstallFromMarket("demo"); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("tampered plugin must be refused, got: %v", err)
	}

	// 条目不在索引中：拒绝
	writeMarketPlugin(t, "ghost", map[string]string{
		"manifest.json": `{"name":"ghost","version":"1.0.0","description":"d","permissions":{}}`,
		"plugin.js":     `console.log("ghost")`,
	})
	m3 := New(filepath.Join(t.TempDir()))
	if err := m3.InstallFromMarket("ghost"); err == nil || !strings.Contains(err.Error(), "not in index") {
		t.Fatalf("unindexed plugin must be refused, got: %v", err)
	}
}

// TestMarketInstallRequiresIndex 无 index.json：安装一律拒绝。
func TestMarketInstallSignaturePolicy(t *testing.T) {
	market := t.TempDir()
	saveMarketDir(t)
	MarketDir = market
	writeMarketPlugin(t, "demo", map[string]string{
		"manifest.json": `{"name":"demo","version":"1.0.0","permissions":{}}`,
		"plugin.js":     `console.log("demo")`,
	})
	hash, err := dirSHA256(filepath.Join(market, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	privateKey := configureTestMarketKey(t, "trusted")
	m := New(t.TempDir())

	writeMarketIndex(t, []MarketIndexItem{{Name: "demo", Version: "1.0.0", SHA256: hash}})
	if err := m.InstallFromMarket("demo"); err == nil || !strings.Contains(err.Error(), "unsigned") {
		t.Fatalf("unsigned entry must be refused, got: %v", err)
	}

	unknown := signedMarketItem(t, "demo", "1.0.0", hash, "unknown", privateKey)
	writeMarketIndex(t, []MarketIndexItem{unknown})
	if err := m.InstallFromMarket("demo"); err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("unknown signing key must be refused, got: %v", err)
	}

	invalid := signedMarketItem(t, "demo", "1.0.0", hash, "trusted", privateKey)
	invalid.Version = "1.0.1"
	writeMarketIndex(t, []MarketIndexItem{invalid})
	if err := m.InstallFromMarket("demo"); err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("metadata tampering must be refused, got: %v", err)
	}
}

func TestSetMarketTrustedKeysValidation(t *testing.T) {
	saveMarketDir(t)
	if err := SetMarketTrustedKeys("missing-separator"); err == nil {
		t.Fatal("invalid trusted key syntax must fail")
	}
	if err := SetMarketTrustedKeys("release=Zm9v"); err == nil || !strings.Contains(err.Error(), "want 32") {
		t.Fatalf("invalid Ed25519 key size must fail, got: %v", err)
	}
}

func TestMarketInstallRequiresIndex(t *testing.T) {
	dir := t.TempDir()
	market := t.TempDir()
	saveMarketDir(t)
	MarketDir = market
	writeMarketPlugin(t, "legacy", map[string]string{
		"manifest.json": `{"name":"legacy","version":"1.0.0","description":"d","permissions":{}}`,
		"plugin.js":     `console.log("legacy")`,
	})
	m := New(dir)
	if err := m.InstallFromMarket("legacy"); err == nil || !strings.Contains(err.Error(), "index.json missing") {
		t.Fatalf("install without index must be refused, got: %v", err)
	}
}

// TestMarketListAndAllowExecFilter 市场列表：索引模式展示 sha256；allow_exec 插件不展示。
func TestMarketListAndAllowExecFilter(t *testing.T) {
	dir := t.TempDir()
	market := t.TempDir()
	saveMarketDir(t)
	MarketDir = market

	writeMarketPlugin(t, "good", map[string]string{
		"manifest.json": `{"name":"good","version":"2.0.0","description":"nice","permissions":{}}`,
		"plugin.js":     `console.log("good")`,
	})
	hash, err := dirSHA256(filepath.Join(market, "good"))
	if err != nil {
		t.Fatal(err)
	}
	writeMarketIndex(t, []MarketIndexItem{{Name: "good", Version: "2.0.0", SHA256: hash}})
	m := New(dir)

	entries := m.ListMarket()
	if len(entries) != 1 || entries[0].Name != "good" || entries[0].SHA256 != hash {
		t.Fatalf("index-driven market list wrong: %+v", entries)
	}

	// 无索引回退目录扫描：allow_exec 插件被过滤
	MarketDir = filepath.Join(t.TempDir())
	writeMarketPlugin(t, "good2", map[string]string{
		"manifest.json": `{"name":"good2","version":"1.0.0","description":"d","permissions":{}}`,
		"plugin.js":     `console.log("good2")`,
	})
	writeMarketPlugin(t, "evil2", map[string]string{
		"manifest.json": `{"name":"evil2","version":"1.0.0","description":"d","permissions":{"allow_exec":true}}`,
		"plugin.js":     `console.log("evil2")`,
	})
	entries = m.ListMarket()
	if len(entries) != 1 || entries[0].Name != "good2" {
		t.Fatalf("legacy market scan should filter allow_exec, got: %+v", entries)
	}
}
