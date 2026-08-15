# Argus 架构设计

> 轻量自托管服务器监控系统。取 komari / nezha-master / nezha-dash-v2 三家精华，
> 单二进制部署，一条 WebSocket 长连接完成采集上报与任务下发。

## 1. 总体架构

```
┌──────────────┐   WebSocket (JSON-RPC 2.0)   ┌──────────────────────────┐
│   Agent ×N   │ ◄──────────────────────────► │        Server            │
│  (Go 采集端) │      report / exec /         │  (Go + Gin + SQLite)     │
└──────────────┘      terminal / ping         │                          │
                                              │  ┌────────────────────┐  │
┌──────────────┐    REST + WebSocket          │  │ 内存状态区(实时)    │  │
│  Web (React) │ ◄──────────────────────────► │  │ metrics 降采样存储  │  │
│  仪表盘      │      /api/v1/*               │  └────────────────────┘  │
└──────────────┘                              │  alert 状态机 / cron    │
                                             │  webhook 通知           │
                                             └──────────────────────────┘
```

## 2. 仓库结构（monorepo）

```
argus/
├── server/                  # Go 服务端（单二进制，内嵌前端）
│   ├── cmd/argus-server/    # 入口：生命周期启动
│   ├── internal/
│   │   ├── config/          # 配置加载（koanf 风格简化）
│   │   ├── db/              # GORM + SQLite 初始化、迁移
│   │   ├── model/           # 数据模型（Server/User/Alert/Cron/Notification）
│   │   ├── api/             # REST handler + WS 推送
│   │   ├── agent/           # Agent 连接管理、JSON-RPC 2.0 分发
│   │   ├── metric/          # 指标接收、降采样、历史查询
│   │   ├── alert/           # 报警状态机
│   │   └── scheduler/       # cron 任务调度
│   └── embed/               # go:embed 前端产物（构建时注入）
├── agent/                   # Go Agent 采集端
│   ├── cmd/argus-agent/
│   └── internal/collector/  # CPU/内存/磁盘/网络采集
├── web/                     # React 19 前端
│   ├── src/
│   │   ├── pages/           # 总览 / 详情 / 服务器管理 / 报警 / 任务
│   │   ├── components/      # ServerCard、图表、主题
│   │   ├── lib/             # API 客户端、WS、格式化
│   │   └── context/         # WS、主题、排序
│   └── scripts/             # mock 演示环境
├── docs/                    # 设计文档、对比报告
├── deploy/                  # docker-compose、systemd 样例
└── README.md
```

## 3. 通信协议（Agent ↔ Server）

JSON-RPC 2.0 over WebSocket（借鉴 komari，免 protobuf 工具链）：

| Method | 方向 | 说明 |
|---|---|---|
| `agent.register` | Agent → Server | 首次注册，返回 agent id + token |
| `agent.report` | Agent → Server | 周期状态上报（默认 2s） |
| `agent.exec` | Server → Agent | 下发远程命令 |
| `agent.terminal` | 双向 | 网页终端字节隧道 |
| `agent.ping` | Server → Agent | 在线探测（可省略，靠 report 心跳） |

认证：Agent 连接时带 `Authorization: Bearer <secret>`，secret 在 Server 端生成。

## 4. 数据模型（SQLite + GORM）

- `servers`：名称、secret、分组、备注、最后在线时间
- `metrics`：降采样指标（server_id, ts, cpu, mem, disk, net_in, net_out, load）
- `users`：管理员账号（bcrypt + JWT）
- `alerts`：报警规则（metric 类型、阈值 min/max、持续秒数、通知渠道）
- `crons`：定时任务（cron 表达式、命令、目标服务器）
- `notifications`：通知渠道（webhook URL/模板）

## 5. 实时推送（Server → Web）

- `GET /api/v1/ws`：每 2s 推送全部服务器快照（合并序列化，借鉴 nezha singleflight 思路）
- 快照含：CPU%、内存%、磁盘%、网络速率、负载、在线状态

## 6. 报警状态机（借鉴 nezha 简化）

- 每 3s 轮询规则 × 服务器
- 指标持续超过阈值达 `duration` 秒 → 触发 → 发送通知（webhook）
- 恢复后发送恢复通知；单次触发模式避免重复轰炸

## 7. 前端页面

| 路由 | 页面 | 内容 |
|---|---|---|
| `/` | 总览 | 服务器卡片网格、在线率、排序/搜索/分组 |
| `/server/:id` | 详情 | 实时曲线（Recharts）+ 历史图表 + 终端入口 |
| `/terminal/:id` | 终端 | xterm.js WebSocket 终端 |
| `/servers` | 管理 | 新增/编辑/删除服务器、查看密钥 |
| `/alerts` | 报警 | 规则 CRUD + 通知渠道配置 |
| `/crons` | 任务 | 定时任务 CRUD |

## 8. 部署形态

1. **开发**：`make dev` 一键起 mock（无后端演示）
2. **生产单机**：`make build` → 单二进制（内嵌前端+agent 静态编译）
3. **Docker**：`deploy/docker-compose.yml`（server + agent 容器）

## 9. 技术选型清单

| 层 | 选型 | 借鉴来源 |
|---|---|---|
| 后端 | Go 1.26 + Gin + GORM + gorilla/websocket + robfig/cron | komari / nezha |
| 存储 | SQLite（单文件，metrics 表内降采样） | komari |
| 前端 | React 19 + Vite + TS + Tailwind v4 + Recharts + TanStack Query | nezha-dash-v2 |
| Agent | Go 1.26，gopsutil 采集 | nezha agent 思路 |
| 终端 | xterm.js + WS 隧道 | komari / nezha |
