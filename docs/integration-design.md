# Argus 整合版架构设计（v2）

> 依据 docs/standards.md（规范）、docs/assets.md（资产）、docs/diff.md（差异）设计。
> 目标：单仓库整合 komari + nezha 全部核心能力，前端对齐 dash-v2 视觉基准。

## 1. 总体架构

```
┌─────────────────────────────────────────────────────────────┐
│  Web (React 19 + Vite + Tailwind v4 + Recharts)             │
│  ┌─────────────────────┐  ┌──────────────────────────────┐  │
│  │ 用户前台             │  │ 管理后台                      │  │
│  │ 总览/详情/终端/服务监控│  │ 服务器/用户/PAT/报警/通知/    │  │
│  │ 地图/可用率/命令面板   │  │ 任务/服务/文件/DDNS/NAT/设置  │  │
│  └─────────┬───────────┘  └──────────────┬───────────────┘  │
│            └─────────── REST /api/v1 + WS ────┘              │
└───────────────────────────┬─────────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────────┐
│  Server (Go + Gin + GORM + SQLite)                          │
│  ┌────────────┐ ┌───────────┐ ┌──────────┐ ┌──────────────┐ │
│  │ API 层      │ │ 权限层     │ │ 业务服务  │ │ Agent 接入    │ │
│  │ REST 统一壳 │ │ JWT+PAT   │ │ 监控/报警 │ │ WS JSON-RPC  │ │
│  │ 分页/错误码 │ │ admin/user │ │ 任务/服务 │ │ 上报/exec/    │ │
│  │ WS 推送     │ │ scope 校验 │ │ 文件/通知 │ │ terminal/fs  │ │
│  └────────────┘ └───────────┘ └──────────┘ └──────────────┘ │
│  存储: SQLite(业务) + metrics 分钟级降采样 + 服务历史         │
└───────────────────────────┬─────────────────────────────────┘
                            │ WS (JSON-RPC 2.0)
                    ┌───────▼────────┐
                    │  Agent (Go)    │
                    │ 采集+exec+term │
                    │ +fs+service    │
                    └────────────────┘
```

## 2. 分层设计

### 2.1 API 层（nezha 风格统一）
- 统一响应壳：`{"success":true,"data":{},"pagination":{}}` / `{"success":false,"error":""}`
- 错误码：400/401/403（scope 不足）/404/409/500
- 路由：
  - `/api/v1/auth/*` 登录
  - `/api/v1/servers|alerts|crons|notifications|services|files|users|tokens|settings`
  - `/api/v1/ws`（仪表盘推送）、`/api/v1/terminal/:id`、`/api/v1/file/:id`
  - `/ws/agent`（Agent 接入）

### 2.2 权限层（nezha PAT + komari 角色）
- 认证：JWT（30 天）+ PAT（`argus_{resource}:{verb}` scope + server_ids 白名单 + 吊销）
- 角色：admin（全部）/ user（仅 owner 资源）/ guest（只读公开）
- 中间件链：`auth → scope → owner-filter`

### 2.3 业务服务（融合）
| 服务 | 来源 | 说明 |
|---|---|---|
| ServerRegistry | nezha ServerClass | 内存台账 + owner 归属 + 隐藏过滤 |
| AlertEngine | nezha AlertSentinel | 阈值+时长状态机 + 触发任务 |
| CronScheduler | nezha CronClass | cron + 手动授权执行 |
| ServiceSentinel | nezha | HTTP/TCP/Ping 探测聚合 + 可用率 |
| FsService | nezha | 文件列表/读/写/删（走 agent 任务） |
| Notifier | komari 渠道 + nezha 模板 | webhook/bark/telegram/email/serverchan |
| GeoIP | komari provider 化 | mmdb + 在线回退 |

### 2.4 Agent 协议扩展（protocol/rpc）
- 现有：register/report/exec/terminal
- 新增：`agent.service`（服务探测执行）、`agent.fs.list|read|write|delete`（文件操作）
- 保持 JSON-RPC 2.0 over WS 单连接复用

## 3. 分阶段实施计划

| 阶段 | 内容 | 验证方式 |
|---|---|---|
| A1 | 统一响应壳 + 分页 + 错误码重构 | API 回归测试 |
| A2 | PAT 权限体系 | curl 测试 scope 收窄 |
| A3 | 多用户 + owner 归属过滤 | 双账号 API 验证 |
| B1 | 服务监控（HTTP/TCP/Ping） | agent 真实探测 |
| B2 | 文件管理器 | agent 文件操作往返 |
| C1 | 前端升级（统计卡/排序/命令面板/详情） | 浏览器实测 |
| C2 | 通知渠道扩展 + GeoIP | webhook/bark 实测 |
| D | 全量端到端测试 + 修复 | 回归清单 |
| E | 推送 GitHub + cnb | 线上验证 |

## 4. 数据模型新增

```go
// 多用户
User{ID, Username, PasswordHash, Role(admin/user), AgentSecret}
// PAT
APIToken{ID, UserID, Name, TokenHash, Scopes(JSON), ServerIDs(JSON), ExpiresAt, Revoked}
// 服务监控
Service{ID, OwnerID, Name, Type(http/tcp/ping), Target, Interval, ...}
ServiceHistory{ID, ServiceID, Ts, Delay, Up}
// 文件会话
// 复用 terminal 隧道，purpose 区分
// 服务器归属
Server{ID, Name, Secret, Group, Note, OwnerID}
```

## 5. 前端页面规划（对齐 dash-v2 + 后台补全）

用户前台：总览（统计卡+分组Tab+排序+搜索+地图+可用率）、详情（系统信息+Detail/Network Tab）、终端、服务监控页
管理后台：服务器、用户、PAT 令牌、报警、通知、任务、服务、文件管理、设置
全局：Cmd+K 命令面板、主题 system 跟随、i18n 预留
