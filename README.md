# Argus

轻量自托管服务器监控系统 —— 取 komari / nezha-master / nezha-dash-v2 三家精华。

- **单二进制部署**：Server 内嵌前端，一条 WebSocket 长连接承载 Agent 采集上报、任务下发与终端隧道
- **秒级实时监控**：CPU / 内存 / 磁盘 / 网络 / 负载，内存优先架构 + SQLite 降采样历史
- **现代仪表盘**：React 19 + Vite + Tailwind v4 + Recharts，light/dark 主题
- **报警与任务**：阈值 + 持续时长状态机报警、Webhook 通知、cron 定时任务
- **网页终端**：xterm.js WebSocket 隧道直连 Agent
- **零后端演示**：内置 Mock 模式，无 Server 也能完整体验前端

## 架构

```
┌─────────────┐   WebSocket (JSON-RPC 2.0)   ┌──────────────────────────┐
│  Agent ×N   │ ◄──────────────────────────► │         Server           │
│ (Go 采集端) │   report / exec / terminal   │  Go + Gin + SQLite       │
└─────────────┘                              │  内存状态 + 降采样指标库  │
                                             │  报警状态机 + cron 任务   │
┌─────────────┐   REST + WebSocket 推送      └──────────────────────────┘
│  Web(React) │ ◄──────────────────────────►
└─────────────┘
```

## 目录结构

```
server/     Go 服务端（单二进制，go:embed 内嵌前端）
agent/      Go Agent 采集端
web/        React 19 前端
docs/       设计文档与参考项目对比报告
deploy/     docker-compose / systemd 部署样例
```

## 快速开始

### 方式一：Docker Compose

```bash
docker compose -f deploy/docker-compose.yml up -d
# 打开 http://localhost:8080
```

### 方式二：本地构建

```bash
# 1. 构建前端
cd web && pnpm install && pnpm build

# 2. 构建服务端（内嵌前端）
cd ../server && go build -o argus-server ./cmd/argus-server

# 3. 启动
./argus-server -l 0.0.0.0:8080

# 4. 部署 Agent（在任意被监控机器上）
cd ../agent && go build -o argus-agent ./cmd/argus-agent
./argus-agent -s ws://server-ip:8080/ws -k <server密钥>
```

### 方式三：前端演示（无需后端）

```bash
cd web && pnpm install && pnpm dev:mock
```

## 功能

- [x] 实时监控：CPU / 内存 / 磁盘 / 网络速率 / 负载 / 在线状态
- [x] 历史指标：SQLite 分钟级降采样，Recharts 曲线
- [x] 报警规则：阈值 + 持续时长，Webhook 通知，失败/恢复双向提醒
- [x] 定时任务：cron 表达式向指定服务器下发命令
- [x] 网页终端：xterm.js + WebSocket 隧道
- [x] 服务器管理：注册密钥、分组、备注
- [x] 主题：light / dark

## 协议

Agent 与 Server 之间使用 WebSocket + JSON-RPC 2.0：

| Method | 方向 | 说明 |
|---|---|---|
| `agent.register` | Agent → Server | 注册并获取密钥 |
| `agent.report` | Agent → Server | 周期状态上报（默认 2s） |
| `agent.exec` | Server → Agent | 下发远程命令 |
| `agent.terminal` | 双向 | 终端字节隧道 |

详见 [docs/design.md](docs/design.md) 与 [docs/comparison.md](docs/comparison.md)（三参考项目部署对比）。

## License

MIT
