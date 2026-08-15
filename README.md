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
└─────────────┘   + service / fs             │  内存状态 + 降采样指标库  │
                                             │  报警状态机 + cron 任务   │
┌──────────────────────┐   REST + WS 推送    └──────────────────────────┘
│  Web (React)          │ ◄──────────────────►
│  ┌──────────────────┐ │
│  │ 前台（公开）      │ │   /           服务器总览（游客可看）
│  │ 顶栏布局 · 卡片墙  │ │   /server/:id 服务器详情（游客可看）
│  │ 服务状态条 · 统计卡 │ │
│  ├──────────────────┤ │
│  │ 后台（登录后）    │ │   /admin/*    服务器/报警/任务/服务/文件/权限
│  │ 侧边栏布局        │ │
│  └──────────────────┘ │
└──────────────────────┘
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

## 前台 / 后台

- **前台（公开，无需登录）**：服务器总览（统计卡 + 服务监控状态条 + 卡片墙）、服务器详情、实时 WS 推送——借鉴 komari 前台与 nezha dash-v2 游客模式
- **后台（登录后 /admin）**：服务器管理、报警、通知、任务、服务监控、文件管理、访问控制、网页终端

## 功能（整合 komari + nezha 生态）

**监控**
- [x] 实时监控：CPU / 内存 / 磁盘 / 网络速率 / 负载 / 在线状态
- [x] 历史指标：SQLite 分钟级降采样 + 聚合查询（1h/24h/7d）
- [x] 服务监控：HTTP / TCP / Ping 探测，今日可用率 + 30 天色块
- [x] 网页终端：xterm.js + WebSocket 隧道

**运维**
- [x] 定时任务：cron 表达式向指定服务器下发命令，手动触发
- [x] 文件管理器：远端目录浏览 / 上传 / 预览 / 删除
- [x] 远程执行：管理台直接执行命令并查看输出

**告警**
- [x] 报警规则：阈值 + 持续时长状态机，触发/恢复双向提醒
- [x] 通知渠道：webhook / bark / telegram / email / serverchan

**权限**
- [x] 多用户：admin / user 两级，用户仅见名下服务器
- [x] PAT 令牌：argus:{resource}:{verb} scope + 白名单 + 吊销

**前端**
- [x] 总览：统计卡、状态过滤、9 种排序、搜索分组
- [x] 服务器 / 报警 / 任务 / 服务监控 / 文件 / 访问控制管理页
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
