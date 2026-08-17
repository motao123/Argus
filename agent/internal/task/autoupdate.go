package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/protocol/rpc"
)

const (
	// defaultAutoUpdateMinInterval / defaultAutoUpdateMaxInterval 默认检查周期：
	// 对标 Nezha，每次重连后随机取 30–90 分钟中的一个值。
	defaultAutoUpdateMinInterval = 30 * time.Minute
	defaultAutoUpdateMaxInterval = 90 * time.Minute
	// checkUpdateTimeout 单次 check_update 请求超时。
	checkUpdateTimeout = 30 * time.Second
)

// AutoUpdater 周期向服务端请求最新版本信息，发现新版本时走既有升级流程
// （下载 → SHA-256 校验 → 原子替换 → 重启）。任何一步失败只记日志，
// 进入下一个检查周期，绝不中断上报循环。
// 依赖全部可注入（Call/Upgrade/RandFloat/Logf），便于测试。
type AutoUpdater struct {
	// Call 向服务端发起 RPC 请求（默认 peer.Call）。
	Call func(method string, params any, timeout time.Duration) (*protocol.Response, error)
	// CurrentVersion 当前 Agent 版本；与最新版本相同则跳过升级。
	CurrentVersion string
	// Enabled 是否启用自动检查（false 时 Run 直接返回）。
	Enabled bool
	// MinInterval / MaxInterval 检查周期随机区间（默认 30–90 分钟）。
	MinInterval time.Duration
	MaxInterval time.Duration
	// RandFloat 返回 [0,1) 随机数（可注入固定值）。
	RandFloat func() float64
	// Upgrade 执行升级（默认 upgradeSelf；失败仅记日志）。
	Upgrade func(p *protocol.UpgradeParams) (string, error)
	// Logf 日志输出（默认 log.Printf）。
	Logf func(format string, args ...any)
}

// NewAutoUpdater 创建默认配置的自动更新检查器。
func NewAutoUpdater(peer *rpc.Peer, currentVersion string) *AutoUpdater {
	return &AutoUpdater{
		Call:           peer.Call,
		CurrentVersion: currentVersion,
		Enabled:        true,
		MinInterval:    defaultAutoUpdateMinInterval,
		MaxInterval:    defaultAutoUpdateMaxInterval,
		RandFloat:      rand.Float64,
		Upgrade:        upgradeSelf,
		Logf:           log.Printf,
	}
}

// Run 按随机周期循环检查，直到 ctx 取消或连接关闭。禁用时直接返回。
func (u *AutoUpdater) Run(ctx context.Context) {
	if u == nil || !u.Enabled {
		return
	}
	for {
		if !u.sleep(ctx, u.nextInterval()) {
			return
		}
		if err := u.checkAndUpgrade(); err != nil {
			u.logf("auto update check failed: %v", err)
		}
	}
}

// nextInterval 在 [MinInterval, MaxInterval) 内取随机周期；每次调用都重新随机，
// 等价于每次重连/每周期重置抖动。
func (u *AutoUpdater) nextInterval() time.Duration {
	min, max := u.MinInterval, u.MaxInterval
	if min <= 0 {
		min = defaultAutoUpdateMinInterval
	}
	if max <= 0 {
		max = defaultAutoUpdateMaxInterval
	}
	if max <= min {
		max = min + time.Minute
	}
	r := u.randf()
	if r != r || r < 0 || r >= 1 {
		r = 0 // NaN / 越界兜底
	}
	return min + time.Duration(r*float64(max-min))
}

// checkAndUpgrade 单次检查：请求最新版本 → 有更新且版本不同 → 执行升级。
// 返回 nil 表示无需升级或升级已触发；错误由调用方记日志后进入下一周期。
func (u *AutoUpdater) checkAndUpgrade() error {
	resp, err := u.Call(protocol.MethodCheckUpdate, protocol.CheckUpdateParams{Version: u.CurrentVersion}, checkUpdateTimeout)
	if err != nil {
		return fmt.Errorf("check_update rpc: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("check_update: %s", resp.Error.Message)
	}
	var info protocol.CheckUpdateResult
	raw, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(raw, &info); err != nil {
		return fmt.Errorf("bad check_update result: %w", err)
	}
	version := strings.TrimSpace(info.Version)
	if version == "" || version == u.CurrentVersion {
		return nil // 服务端未配置（空结果）或版本相同：跳过
	}
	note, err := u.Upgrade(&protocol.UpgradeParams{
		URL:     strings.TrimSpace(info.URL),
		SHA256:  strings.TrimSpace(info.SHA256),
		Version: version,
	})
	if err != nil {
		return fmt.Errorf("upgrade to %s: %w", version, err)
	}
	u.logf("auto update: %s", note)
	return nil
}

func (u *AutoUpdater) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (u *AutoUpdater) randf() float64 {
	if u.RandFloat == nil {
		return rand.Float64()
	}
	return u.RandFloat()
}

func (u *AutoUpdater) logf(format string, args ...any) {
	if u.Logf != nil {
		u.Logf(format, args...)
	}
}
