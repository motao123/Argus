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
deploy/     docker-compose / systemd / OpenRC 部署样例（见 deploy/README.md）
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
| `ARGUS_NAT_LISTEN` | `:9090` | NAT HTTP 隧道监听地址；Compose 中保持为 `0.0.0.0:9090`。仅按 HTTP `Host` 路由、承载 HTTP/1.x 与 WebSocket Upgrade（不暴露通用 TCP/UDP 入口），含端到端背压（缓冲满不丢包）、连接配额与 reserved host 保护。 |
| `ARGUS_NAT_SERVER_CONNECTION_LIMIT` | `16` | 每台服务器并发 HTTP 隧道上限。 |
| `ARGUS_NAT_USER_CONNECTION_LIMIT` | `32` | 每用户（owner）并发 HTTP 隧道上限。 |
| `ARGUS_NAT_RESERVED_HOSTS` | 空 | 逗号分隔的保留域名（如 dashboard 域名），禁止被 NAT Host 路由覆盖；命中返回 421。 |
| `ARGUS_MCP_ENABLED` | `false`（默认关闭） | 是否启用 MCP 端点（`POST /mcp`）。仅接受 `Authorization: Bearer argus_*` PAT，不支持 JWT。 |
| `ARGUS_MCP_RATE_LIMIT` | `60` | 每个 PAT 每分钟允许的 MCP 请求数（超限返回 429 与 `Retry-After`）。 |
| `ARGUS_MCP_TRANSFER_MAX_MB` | `64` | MCP `fs.download_url` / `fs.upload_url` 一次性传输的大小上限（MB）。 |
| `ARGUS_MCP_TRANSFER_TTL_SECONDS` | `300` | 一次性传输 URL 的有效期（秒），过期或使用后立即失效。 |
| `ARGUS_TRUSTED_PROXIES` | 空（不信任代理头） | 逗号分隔的代理 IP/CIDR。仅在可信反向代理后部署时填写，否则不得采信客户端传入的转发头。 |
| `ARGUS_JWT_SECRET` | 自动生成并持久化到数据库旁的 `.jwt` 文件 | 可选固定 JWT 签名密钥；生产环境设置时请使用高强度随机值，并在多实例间保持一致。不要提交真实密钥。 |

Agent 必须连接 `/ws/agent`，例如 `wss://your-domain/ws/agent`。

### 方式二：一键安装命令（哪吒风格）

后台「服务器」页每行点击下载图标（或「访问控制 → 我的安装命令」），会生成一条命令：

```bash
curl -fsSL http://your-domain/install.sh | sh -s -- -s ws://your-domain/ws/agent -k <密钥>
```

SSH 登录目标服务器（Linux/macOS/FreeBSD，需 root）后执行即可：脚本自动识别系统与架构、
从发布源下载 Agent 二进制、校验 SHA-256、安装为 systemd/OpenRC 服务并注册上线，幂等可重装。

**Windows**：以管理员 PowerShell 执行（脚本由同一后台端点下发，用法与 install.sh 对齐）：

```powershell
# 1) 下载脚本（PowerShell 5.1 内置 curl.exe 别名，需显式调用；也可用 Invoke-WebRequest）
curl.exe -fsSL http://your-domain/install.ps1 -o install.ps1
# 2) 以管理员身份执行（管理员 PowerShell 中 .\install.ps1 即可）
powershell -ExecutionPolicy Bypass -File install.ps1 -ServerUrl ws://your-domain/ws/agent -Secret <密钥>
```

- 服务器模式：使用该服务器专属密钥（补装/重连同一节点）。
- 用户模式：使用你的 Agent 注册密钥，一条命令可批量部署任意新机器（自动创建服务器）。
- 二进制下载源默认 GitHub Releases latest（`argus-agent-<os>-<arch>` + `checksums.txt`）；
  下载源优先级：`-m <mirror>` > `ARGUS_AGENT_MIRROR` > `-u <base>` > `ARGUS_AGENT_BASE_URL`
  > 默认 GitHub。国内网络可用镜像根 URL（如 AtomGit/Gitee 的 Release 镜像，目录结构须与
  GitHub Release 一致，含 `checksums.txt`）：

  ```bash
  # 方式一：-m/--mirror 参数（优先级最高）
  curl -fsSL http://your-domain/install.sh | sh -s -- -s ws://your-domain/ws/agent -k <密钥> \
      -m https://gitee.com/mirrors/Argus/releases/latest/download

  # 方式二：环境变量 ARGUS_AGENT_MIRROR（同样适用于 install.ps1 的 -BaseUrl 参数）
  ARGUS_AGENT_MIRROR=https://atomgit.com/Argus/releases/latest/download \
      curl -fsSL http://your-domain/install.sh | sh -s -- -s ws://your-domain/ws/agent -k <密钥>
  ```
- HTTPS 反代场景请在「设置 → 安装命令基础 URL」填写公网地址，命令会自动推导为 `wss://`。

### 方式三：本地构建

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

### 方式四：前端演示（无需后端）

```bash
cd web && pnpm install && pnpm dev:mock
```

### 发布构建（多平台二进制 + SHA-256）

```bash
make release          # 先构建前端并内嵌，再产出 dist/release/<version>/ 下全部二进制
bash scripts/release-build.sh   # 仅构建（需已存在 webdist）
```

- Agent（纯 Go，`CGO_ENABLED=0`，无需任何 C 工具链）：
  - linux：386 / amd64 / arm / arm64 / riscv64 / s390x / loong64 / mips / mipsle
  - windows：386 / amd64 / arm64（产物带 `.exe`）
  - darwin：amd64 / arm64
  - freebsd：386 / amd64 / arm / arm64
  - 产物命名 `argus-agent-<os>-<arch>`（windows 加 `.exe`）。
- Server（cgo SQLite）：linux/amd64 原生构建；linux/arm64 需要
  `aarch64-linux-gnu-gcc`（或 `ARGUS_CC_AARCH64`），缺少时跳过并告警；其余平台
  明确跳过并告警（可用 `ARGUS_FORCE_CGO=1` 尝试交叉构建 windows/darwin，产物需自行验证）。
- 每个二进制配套 SHA-256：`checksums.txt`（`sha256sum -c` 可校验）+
  `manifest.json`（含逐平台构建结果 `results`：ok / skipped / failed，以及产物 `artifacts`）。
- 脚本与 GitHub Actions（`release.yml`，手动触发）都**不创建 tag / release、不推送远端**。

## 前台 / 后台

- **前台（公开，无需登录）**：服务器总览（统计卡 + 服务监控状态条 + 卡片墙）、服务器详情、实时 WS 推送——借鉴 komari 前台与 nezha dash-v2 游客模式
- **后台（登录后 /admin）**：服务器管理、报警、通知、任务、服务监控、文件管理、访问控制、网页终端

## 功能（整合 komari + nezha 生态）

**监控**
- [x] 实时监控：CPU / 内存 / 磁盘 / 网络速率 / 负载 / 在线状态
- [x] 历史指标：SQLite 分钟级降采样 + 聚合查询（1h/24h/7d）
- [x] 服务监控：HTTP / TCP / Ping / Command 探测，今日可用率 + 30 天色块
- [x] 网页终端：xterm.js + WebSocket 隧道（PTY 会话，支持窗口尺寸调整）
- [x] 网络测试：路由追踪（ICMP/TCP/UDP）、多源 mesh 追踪、Agent 间带宽测速

**运维**
- [x] 定时任务：cron 表达式向指定服务器下发命令，手动触发
- [x] 文件管理器：远端目录浏览 / 上传 / 预览 / 删除
- [x] 远程执行：管理台直接执行命令并查看输出（启用 2FA 时需二次验证）
- [x] Agent 批量升级：持久化 Job + 受控并发 + 逐机回执 + SHA-256 强制校验 + 失败回滚；Server 重启后自动恢复未完成任务
- [x] Agent 能力开关与网卡/挂载过滤：按服务器开关 metrics / probe / command / terminal / files / upgrade / nat / trace 采集能力，并按网卡名与挂载点 include/exclude 过滤上报（UI 入口：服务器 → 新建/编辑配置）
- [x] 生命周期：服务器过户（密钥轮换 + 状态机 + 取消回滚）

**MCP（默认关闭，`ARGUS_MCP_ENABLED=true` 启用）**
- [x] JSON-RPC 2.0 over Streamable HTTP：`initialize` / `notifications/initialized` / `ping` / `tools/list` / `tools/call` / 会话终止
- [x] 仅 PAT 认证（`argus_*`），按令牌限流，全部调用写入审计
- [x] 工具：`server.list/get/exec`、`fs.list/read/write/delete`、`meta.whoami`
- [x] 一次性传输：`fs.download_url` / `fs.upload_url`，带大小上限、有效期、SHA-256 与 `if-match` 条件覆盖

**告警**
- [x] 报警规则：阈值 + 持续时长状态机，触发/恢复双向提醒；指标覆盖 CPU/内存/交换/磁盘/负载/连接数/进程数/温度/GPU/延迟/速率/累计流量/周期流量/离线
- [x] 周期流量：锚点 + 单位（小时/天/周/月/年）步进，可自定义周期
- [x] 通知渠道：webhook / bark / telegram / email / serverchan / javascript / dingtalk / wecom / feishu / slack / wxpusher / matrix

**权限**
- [x] 多用户：admin / user / readonly 三级；readonly 只读角色仅可查看公开视图与名下服务器状态、调用只读 MCP 工具（UI 入口：访问控制 → 用户角色）
- [x] PAT 令牌：argus:{resource}:{verb} scope + 白名单 + 吊销
- [x] TOTP 2FA：登录与敏感操作（远程执行/网页终端）二次验证，PAT 豁免
- [x] 私有站点：强制登录 + 临时分享密钥（?temp_key=），通知 IP 打码

**DDNS / NAT**
- [x] DDNS：Webhook / Cloudflare / 腾讯 DNSPod / Hurricane Electric 四类 provider，A/AAAA 双记录
- [x] NAT 内网穿透：HTTP Host 隧道 + 连接配额 + 保留域名保护

**前端**
- [x] 总览：统计卡、状态过滤、9 种排序、搜索分组、滚动位置恢复
- [x] 服务器 / 报警 / 任务 / 服务监控 / 文件 / 访问控制 / 网络测试管理页
- [x] 主题：light / dark / 跟随系统，主题市场

**插件（goja 沙箱 + 宿主 API）**
- [x] 本地市场安装 / 启停 / 权限审批 / 手动与 cron 执行 / 日志
- [x] 宿主 API：`argus.registerRPC`（暴露 HTTP 可调用的 RPC 方法）、`argus.callRPC`（插件间调用）、`argus.route`（注册 HTTP 路由）、`argus.cron`（JS 定时任务）、`argus.config`（声明式配置 + 管理端覆盖）、`argus.getServers` / `argus.notify` / `argus.kv` / `fetch`（SSRF 防护）
- [x] 页面注入：插件声明的 html_head / html_body 注入所有页面（启用 + 批准生效）

**指标**
- [x] CPU 分位数：分钟级 t-digest 采集、降采样无损合并，查询输出 cpu_p50/p95/p99（历史数据自动兼容）

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
