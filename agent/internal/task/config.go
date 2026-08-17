package task

import (
	"encoding/json"
	"os"

	"github.com/motao123/Argus/protocol"
)

// ApplyConfigPath agent 配置文件路径（main 注入）。
var ApplyConfigPath = "argus-agent.json"

// handleApplyConfig 应用服务端下发的配置（保存到本地文件，重启生效）。
func (h *Handler) handleApplyConfig(params json.RawMessage) (any, *protocol.RPCError) {
	var cfg protocol.AgentConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, protocol.NewError(protocol.ErrParams, err.Error())
	}
	// 读取现有配置并合并
	existing := map[string]any{}
	if data, err := os.ReadFile(ApplyConfigPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if cfg.ServerURL != "" {
		existing["server_url"] = cfg.ServerURL
	}
	if cfg.Interval > 0 {
		existing["interval"] = cfg.Interval
	}
	if cfg.Secret != "" {
		existing["secret"] = cfg.Secret
	}
	if cfg.Capabilities != nil {
		existing["capabilities"] = cfg.Capabilities
	}
	if cfg.AutoUpdate != nil {
		existing["auto_update"] = *cfg.AutoUpdate
	}
	if cfg.InterfaceInclude != nil {
		existing["interface_include"] = cfg.InterfaceInclude
	}
	if cfg.InterfaceExclude != nil {
		existing["interface_exclude"] = cfg.InterfaceExclude
	}
	if cfg.MountInclude != nil {
		existing["mount_include"] = cfg.MountInclude
	}
	if cfg.MountExclude != nil {
		existing["mount_exclude"] = cfg.MountExclude
	}
	existing["pending_restart"] = true
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(ApplyConfigPath, data, 0600); err != nil {
		return nil, protocol.NewError(protocol.ErrInternal, err.Error())
	}
	return map[string]any{"ok": true, "note": "配置已保存，重启 agent 生效"}, nil
}
