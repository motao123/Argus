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
cp deploy/.env.example deploy/.env
# 修改 deploy/.env 中的 ARGUS_ADMIN_PASS 后启动
cd deploy && docker compose up -d
# 健康检查：http://localhost:8080/healthz
# 打开：http://localhost:8080
```

生产镜像也可直接构建：

```bash
docker build -f deploy/Dockerfile -t argus:local .
```

常用部署变量（Compose 可在 `deploy/.env` 中设置，systemd 可在 `/etc/argus/argus.env` 中设置）：

| 变量 | 默认行为 | 说明 |
|---|---|---|
| `ARGUS_GEOIP_ENDPOINT` | 空（不查询 GeoIP） | 可选 HTTP GeoIP 基础 URL；服务端请求 `<endpoint>/<ip>`，响应需包含 `country_code` 或 `countryCode`。地图依赖 GeoIP 国家码；未配置 provider 或查询无结果时，地图会安全隐藏，而非显示错误数据。 |
| `ARGUS_NAT_LISTEN` | `:9090` | NAT Host 反向代理监听地址；Compose 中保持为 `0.0.0.0:9090`。NAT 仅提供基于 HTTP `Host` 的简化 TCP 隧道，不含 TLS 终止、租约、配额或能力协商。 |
| `ARGUS_TRUSTED_PROXIES` | 空（不信任代理头） | 逗号分隔的代理 IP/CIDR。仅在可信反向代理后部署时填写，否则不得采信客户端传入的转发头。 |
| `ARGUS_JWT_SECRET` | 自动生成并持久化到数据库旁的 `.jwt` 文件 | 可选固定 JWT 签名密钥；生产环境设置时请使用高强度随机值，并在多实例间保持一致。不要提交真实密钥。 |

Agent 必须连接 `/ws/agent`，例如 `wss://your-domain/ws/agent`。

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
./argus-agent -s ws://server-ip:8080/ws/agent -k <server密钥>
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
