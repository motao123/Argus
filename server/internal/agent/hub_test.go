package agent

import (
	"encoding/json"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
	"github.com/motao123/Argus/server/internal/store"
)

const testSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// :memory: 库每个连接都是独立数据库，必须单连接才能跨 goroutine 可见。
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	return NewHub(gdb, store.NewHub(), store.NewMetricBatcher(gdb))
}

func setSetting(t *testing.T, h *Hub, key, value string) {
	t.Helper()
	if value == "" {
		return
	}
	if err := h.db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
		t.Fatal(err)
	}
}

// checkUpdate 走 connHandler.Handle 的完整 RPC 分发路径。
func checkUpdate(t *testing.T, h *Hub) protocol.CheckUpdateResult {
	t.Helper()
	ch := &connHandler{hub: h}
	result, rpcErr := ch.Handle(protocol.MethodCheckUpdate, json.RawMessage(`{}`))
	if rpcErr != nil {
		t.Fatalf("check_update rpc error: %+v", rpcErr)
	}
	raw, _ := json.Marshal(result)
	var out protocol.CheckUpdateResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("bad result: %v", err)
	}
	return out
}

func TestCheckUpdateEmptyConfig(t *testing.T) {
	h := newTestHub(t)
	got := checkUpdate(t, h)
	if got.Version != "" || got.URL != "" || got.SHA256 != "" {
		t.Fatalf("expected empty result (no update), got %+v", got)
	}
}

func TestCheckUpdatePartialConfig(t *testing.T) {
	h := newTestHub(t)
	setSetting(t, h, SettingUpgradeLatestVersion, "0.2.0")
	setSetting(t, h, SettingUpgradeLatestURL, "https://example.com/agent")
	// 缺少 sha256 → 视为未配置完成，返回空（避免 agent 拿半截配置下载）
	got := checkUpdate(t, h)
	if got.Version != "" || got.URL != "" || got.SHA256 != "" {
		t.Fatalf("expected empty result for incomplete config, got %+v", got)
	}
}

func TestCheckUpdateInvalidSHA256(t *testing.T) {
	h := newTestHub(t)
	setSetting(t, h, SettingUpgradeLatestVersion, "0.2.0")
	setSetting(t, h, SettingUpgradeLatestURL, "https://example.com/agent")
	setSetting(t, h, SettingUpgradeLatestSHA256, "not-a-sha")
	got := checkUpdate(t, h)
	if got.Version != "" || got.URL != "" || got.SHA256 != "" {
		t.Fatalf("expected empty result for invalid sha256, got %+v", got)
	}
}

func TestCheckUpdateCompleteConfig(t *testing.T) {
	h := newTestHub(t)
	setSetting(t, h, SettingUpgradeLatestVersion, "0.2.0")
	setSetting(t, h, SettingUpgradeLatestURL, "https://example.com/agent-linux-amd64")
	setSetting(t, h, SettingUpgradeLatestSHA256, testSHA256)
	got := checkUpdate(t, h)
	if got.Version != "0.2.0" || got.URL != "https://example.com/agent-linux-amd64" || got.SHA256 != testSHA256 {
		t.Fatalf("unexpected result: %+v", got)
	}
}
