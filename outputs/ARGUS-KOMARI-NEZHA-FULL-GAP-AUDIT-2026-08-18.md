# Argus 对 Komari / Nezha 全量功能差距审计

- 审计日期：2026-08-18
- Argus 基线：`ec9248f`
- 审计对象：`D:\code\shell\komari`、`D:\code\shell\nezha`、`D:\code\shell\Argus`
- 审计方法：目录与依赖、路由、数据模型、Agent 协议、运行时接线、前端路由/API 客户端、部署脚本和配置逐项交叉核验
- 审计性质：静态源码复审。本轮未重新安装或启动三个项目，也未重复上一轮已经完成的部署测试。

## 1. 结论摘要

Argus 已经覆盖两份参考项目的大部分核心能力：实时主机监控、历史趋势、服务探测、告警状态机、多渠道通知、远程命令、Cron、终端、文件管理、DDNS、HTTP/WS NAT、用户与 RBAC、PAT、WAF、MCP、插件、主题、备份、Agent 升级和公开状态页均有真实运行链路。

本轮确认 **35 项仍需处理的功能或产品差距**：

| 优先级 | 数量 | 含义 |
|---|---:|---|
| P0 | 2 | 已有协议或安全链路存在明确断点，应先修复 |
| P1 | 12 | 影响数据完整性、生产运维闭环或主要管理体验 |
| P2 | 15 | 中期产品能力增强或参考项目的重要差异能力 |
| P3 | 6 | 低频、体验增强或可明确选择不做的差异 |

最先应处理的两项是：

1. Agent 配置协议已有 `trace` 与 `auto_update`，但 REST/Web 没有贯通，管理员无法完整控制 Agent 能力。
2. 插件市场收到 Ed25519 签名却不验证，现阶段实际信任边界只有 HTTPS 与 SHA-256。

## 2. 审计边界与状态定义

- **已实现**：Argus 后端、Agent、前端或必要运行时接线完整，不列为缺口。
- **部分实现**：主体能力存在，但字段、管理入口、恢复路径或规模能力未闭环。
- **未实现**：参考项目存在完整链路，Argus 当前代码中没有对应实现。
- **设计差异**：协议或产品路线不同，但不构成实际能力损失；单独记录，不计入 31 项差距。
- Komari 的 React 源码和 Nezha 的 admin/user 前端源码不在对应主仓库，本轮对其 Web 功能采用构建产物、后端路由和外部构建配置三方取证。
- Nezha 主仓库不含生产 Agent 源码，Agent 本机执行行为只按 protobuf、Dashboard 调度与集成配置确认。

## 3. 当前已覆盖的共同核心能力

以下能力无需因为参考项目存在同类功能而重复开发：

| 类别 | Argus 当前能力 | 关键证据 |
|---|---|---|
| 实时监控 | CPU、内存、Swap、磁盘、网络速率/累计流量、Load、进程、Socket、磁盘 IO、温度、多 GPU、延迟 | `protocol/protocol.go:198-276` |
| 实时 Web | Dashboard WebSocket 每 2 秒推送，支持游客、owner 和 PAT 节点过滤 | `server/internal/api/ws.go` |
| 节点管理 | owner、分组、标签、排序、隐藏、计费、到期、流量额度、SLO、批量操作 | `server/internal/model/models.go:9-40`；`server/internal/api/router.go:112-130,269-276` |
| 告警 | 持续窗口、采样比例、确认、静默、重复、恢复、升级渠道、维护窗口排除、持久状态 | `server/internal/alert/engine.go:24-89`；`server/internal/model/models.go:111-161` |
| 通知 | Webhook、Bark、Telegram、Email、ServerChan、JavaScript、钉钉、企微、飞书、Slack、WxPusher、Matrix，持久重试队列 | `server/internal/notifier/notifier.go`；`server/internal/model/models.go:174-224` |
| 服务监控 | HTTP/TCP/Ping/Command，自定义方法、Header、Body、状态码、关键字、TLS 分段时延、证书、丢包、分位数 | `protocol/protocol.go:334-430`；`server/internal/model/models.go:317-386` |
| 运维操作 | 即时命令、Cron、PTY 终端、文件 CRUD、路由追踪、Mesh Trace、带宽测速 | `server/internal/api/router.go:115,121-124,229-247,309` |
| 网络 | DDNS、A/AAAA/dual、HTTP/WS NAT、连接配额、保留域名 | `server/internal/model/models.go:388-444` |
| 身份与权限 | 密码、OAuth2/OIDC、TOTP、admin/user/readonly、owner 隔离、PAT scope 和节点白名单、会话踢出 | `server/internal/api/router.go:66-75,97-110`；`server/internal/model/models.go:42-65,304-315,461-478` |
| 安全 | WAF、递增封禁、持久封禁、在线用户、敏感操作 2FA、MCP PAT-only、一次性文件 URL | `server/internal/api/router.go:263-267`；`server/internal/mcp/mcp.go` |
| 可用性管理 | 公开状态页、事故时间线、维护窗口、月度 SLA/SLO | `server/internal/api/router.go:90-94,298-305` |
| 扩展 | goja 插件、hooks/cron/RPC/route/KV/log；CSS 主题、市场、原子安装和回滚 | `server/internal/plugin/plugin.go`；`server/internal/theme/theme.go` |
| 数据保护 | SQLite 一致性快照、分片恢复、完整性校验、加密定时备份、恢复演练 | `server/internal/api/backup.go`；`server/internal/backup/manager.go` |
| 部署 | Docker、Compose、healthcheck、systemd、Nginx、Unix/Windows Agent 安装、Agent 多平台发布 | `deploy/`；`scripts/release-build.sh` |
| Web | 公开总览/详情与 23 个管理页面，明暗/系统主题，中英双语，响应式导航 | `web/src/main.tsx:59-129` |

## 4. 全量差距矩阵

### 4.1 监控指标与历史数据

#### GAP-M01：历史指标维度不完整

- **参考实现**：Komari 的指标存储保留 Swap、Load、累计流量、进程、连接和逐 GPU 维度，并提供原始与多级 rollup；Nezha TSDB 写入 17 类服务器指标。Komari 旧模型可见 `database/models/models.go:74-110`，Nezha wire 字段可见 `proto/nezha.proto:29-47`。
- **Argus 当前状态**：实时上报字段完整，但 `Metric` 只保存 CPU、内存、磁盘、网络速度、Load1、单温度、聚合 GPU、进程/Socket 和磁盘 IO；缺少 Swap、Load5/15、Uptime、累计流量、Latency、逐 GPU 显存/温度/名称等历史序列。见 `server/internal/model/models.go:273-302`、`server/internal/store/store.go:136-155`。
- **差距类型**：部分实现。
- **优先级**：**P1**。
- **建议**：扩展分钟桶和 rollup 模型；逐 GPU 与传感器采用子表或带 tag 的通用序列表，避免继续膨胀宽表；为新增字段补迁移和历史 API。

#### GAP-M02：只有 SQLite 指标存储，缺少可扩展后端

- **参考实现**：Komari 将业务库与 metric store 解耦，指标库支持 SQLite/MySQL/PostgreSQL；Nezha 可使用嵌入式 VictoriaMetrics TSDB，默认保留 30 天。
- **Argus 当前状态**：业务数据与分钟指标共用 GORM SQLite；有 1m→5m→1h rollup 和 t-digest，但没有独立指标连接、外部 TSDB 或冷热分层。
- **差距类型**：未实现。
- **优先级**：**P1**（节点量或保留期增长后成为容量瓶颈）。
- **建议**：先抽象 `MetricRepository`，保持 SQLite 默认路径；第二阶段增加 PostgreSQL/Timescale 或 VictoriaMetrics 适配，不建议一次性同时支持三种方言。

#### GAP-M03：缺少原始粒度和日级 rollup

- **参考实现**：Komari 支持原始点、1m、5m、1h、日级聚合，并按查询时间范围自动选粒度。
- **Argus 当前状态**：内存保留最新实时值，持久化从 1 分钟开始，最高只到小时级；超长时间范围需要扫描较多小时行，无法回看秒级异常尖峰。
- **差距类型**：部分实现。
- **优先级**：**P2**。
- **建议**：按容量目标选择短期原始桶（如 24 小时）和日级桶；查询层由 period 自动选择 granularity。

#### GAP-M04：缺少具名温度传感器历史

- **参考实现**：Nezha 协议上报 `repeated State_SensorTemperature`，保留传感器名称；Komari 还记录逐 GPU 温度。
- **Argus 当前状态**：只上报一个 CPU `Temperature` 和逐 GPU `Temp`，历史表只保存单一温度和聚合 GPU 利用率。
- **差距类型**：部分实现。
- **优先级**：**P3**。
- **建议**：仅在硬件监控场景有明确需求时加入带标签的传感器序列；不要为低价值传感器继续增加主指标宽表字段。

#### GAP-M05：主机静态资产信息较少

- **参考实现**：Komari 保存物理核心、虚拟化、GPU 名称；Nezha 保存 CPU/GPU 列表、虚拟化、BootTime 和 Agent version。
- **Argus 当前状态**：有 CPU 型号/核心、OS/Arch/Kernel、IP、国家码和 Agent version，但缺物理核心、虚拟化类型、BootTime、静态 GPU 清单。
- **差距类型**：部分实现。
- **优先级**：**P2**。
- **建议**：扩充 `HostInfo` 和 Server 资产详情，静态字段仅在变化时上报；BootTime 同时用于识别重启事件。

#### GAP-M06：缺少本地 GeoIP/MMDB 与管理闭环

- **参考实现**：Komari 支持 MMDB 更新、GeoIP provider 配置和测试；Nezha 在构建时下载 GeoIP 数据并接收 Agent `ReportGeoIP`。
- **Argus 当前状态**：GeoIP 只有带 24 小时缓存的 HTTP provider，返回国家码；无本地 MMDB、数据库更新、Web 配置和查询测试。见 `server/internal/geoip/geoip.go:1-118`。
- **差距类型**：部分实现。
- **优先级**：**P2**。
- **建议**：增加 MaxMind/GeoLite2 本地 provider、定时原子更新、校验和及管理页；在线 provider 保留为回退。

#### GAP-M07：缺少指标定义注册表与 tags 查询

- **参考实现**：Komari 暴露 metric definitions、tag 过滤和统一指标查询，插件或新 Agent 指标可按定义扩展。
- **Argus 当前状态**：指标结构、API 返回和图表字段是静态强类型；新增指标需要同时修改 protocol、模型、rollup、API 和 Web。
- **差距类型**：未实现。
- **优先级**：**P3**。
- **建议**：在插件指标或自定义采集成为明确需求前不必实现；若实施，应采用“稳定核心指标 + 扩展 tagged series”，不要完全动态化现有主链路。

### 4.2 告警、通知与服务监控

#### GAP-A01：缺少周期总流量 `in+out` 告警

- **参考实现**：Nezha 支持 `transfer_in_cycle`、`transfer_out_cycle`、`transfer_all_cycle`；Komari 支持按 sum/max/min/up/down 的流量限额。
- **Argus 当前状态**：实时基线告警有 `transfer_all`，周期告警只有 `traffic_in_cycle` 与 `traffic_out_cycle`，没有周期 `all/max`。见 `server/internal/alert/engine.go:299-323`。
- **差距类型**：部分实现。
- **优先级**：**P1**。
- **建议**：增加 `traffic_all_cycle`，并明确与 Server `TrafficAccounting=sum/in/out/max` 使用同一计算函数，避免额度与告警口径不同。

#### GAP-A02：全量覆盖规则缺少排除列表

- **参考实现**：Nezha AlertRule 用 Cover + Ignore 表达“全部节点，排除少数”或“默认忽略，只包含少数”；Komari 的负载通知也支持目标选择。
- **Argus 当前状态**：`ServerIDs` 为空表示全部，非空表示包含列表；无法直接表示“未来新增节点自动覆盖，但排除指定节点”。
- **差距类型**：部分实现。
- **优先级**：**P2**。
- **建议**：加入 `target_mode=all/include/exclude` 和目标表；迁移时空列表映射到 all、非空映射到 include。

#### GAP-A03：一个服务监控只能绑定一个探测节点

- **参考实现**：Komari Ping Task 可分发到多客户端；Nezha Service 可按覆盖规则选择多个服务器，聚合多个探测点的状态。
- **Argus 当前状态**：`Service.ServerID` 是单值，一条服务定义只能由一个 Agent 探测。见 `server/internal/model/models.go:317-326`。
- **差距类型**：部分实现。
- **优先级**：**P1**（跨区域可用性判断所需）。
- **建议**：将 Service 定义与 Probe Assignment 分离，历史按 `service_id + server_id` 入库，再提供整体/分区域聚合状态。

#### GAP-A04：告警通知策略缺少“只触发一次/持续触发”的显式模式

- **参考实现**：Nezha 规则具有单次与持续触发语义，并为失败/恢复分别联动任务；通知重复间隔独立控制。
- **Argus 当前状态**：已支持恢复、重复间隔、确认、静默和升级，实际能力更强，但没有清晰的策略枚举，`RepeatMinutes=0` 隐式代表单次。
- **差距类型**：产品表达不完整。
- **优先级**：**P3**。
- **建议**：在 UI 增加策略说明或 `notify_mode`，底层可继续映射现有字段，不必重写状态机。

### 4.3 Agent、任务、终端与文件

#### GAP-G01：`trace` 与 `auto_update` 配置契约断链

- **参考实现**：Nezha 的 Agent 配置由 Dashboard 查询/应用；Komari 的 Agent 功能由版本和连接协议管理。
- **Argus 当前状态**：协议 `Capabilities.Trace` 和 `AgentConfig.AutoUpdate` 已存在，Agent 也能处理；REST `serverApplyConfig` 请求结构没有 `AutoUpdate`，Web `CapabilitiesConfig` 没有 `trace`，因此管理员无法完整下发。证据：`protocol/protocol.go:98-121`、`server/internal/api/servers.go:564-600`。
- **差距类型**：已开发但无法完整使用。
- **优先级**：**P0**。
- **建议**：同步修改 REST DTO、批量配置 DTO、Web 类型/表单、i18n 和契约测试；`AutoUpdate` 必须使用 `*bool` 保留“不修改”语义。

#### GAP-G02：缺少 Agent HTTP pull / 离线任务队列降级通道

- **参考实现**：Komari v2 支持 WebSocket 实时派发和 HTTP pull 队列，事件有 TTL、ACK 和队列上限，WS 不可用时仍可取任务。
- **Argus 当前状态**：Agent 控制面依赖长连接 WebSocket；节点离线时即时命令、终端、文件和配置直接失败，没有可过期的排队任务。
- **差距类型**：未实现。
- **优先级**：**P2**。
- **建议**：只为幂等/可审计任务增加持久队列，终端和 NAT 不应排队；必须包含 TTL、幂等键、ACK、每节点上限和取消语义。

#### GAP-G03：Web 文件上传不是流式大文件通道

- **参考实现**：Nezha 文件管理通过双向 IOStream 中继；其 MCP 大文件通道上限 100 MiB。Komari 的归档上传采用 init/chunk/merge/cancel。
- **Argus 当前状态**：Agent 协议支持分片读取，但 Web 写入接口一次提交 `FsWriteParams.Data`；MCP 有一次性 URL，大文件体验没有复用到管理台。
- **差距类型**：部分实现。
- **优先级**：**P1**。
- **建议**：管理台上传改为分片会话或直接复用受权限保护的一次性传输 URL；加入总大小、单片哈希、最终 SHA-256、取消和过期清理。

#### GAP-G04：终端管理体验少于 Komari

- **参考实现**：Komari 终端集成搜索、命令剪贴板、复制粘贴、自定义字体/配色/滚动缓冲，并记录会话时长。
- **Argus 当前状态**：PTY、resize、关闭、主题和 2FA 已实现；独立剪贴板页面存在，但未形成终端内命令面板、搜索和会话历史入口。
- **差距类型**：体验差距。
- **优先级**：**P3**。
- **建议**：先补终端审计会话时长和搜索，再考虑命令片段面板；不要默认记录终端内容。

#### GAP-G05：过户状态机缺少后台 GC 与重试闭环

- **参考实现**：Nezha 过户具有 Pending/Verified/Failed/Timeout/Cancelled、retry、独立握手/回滚密钥和 WebSocket 状态流。
- **Argus 当前状态**：有 pending/verified/cancelled/failed 和 30 分钟回滚，但 `sweepTransfers()` 只在管理员调用列表接口时执行；没有 retry 路由和实时状态流。见 `server/internal/api/transfers.go:14-157`。
- **差距类型**：部分实现，包含实际生命周期缺陷。
- **优先级**：**P1**。
- **建议**：在 Server 启动时运行定时 GC；增加 retry，重新生成一次性密钥；前端轮询即可，暂不必新增专用 WS。

### 4.4 Web 界面、公开站点与可访问性

#### GAP-W01：MCP 没有管理页面

- **参考实现**：Nezha 已将 MCP 作为正式运维接口，PAT/scope、工具和详细调用审计均是产品能力；Komari 的 RPC2 也有清晰的管理入口体系。
- **Argus 当前状态**：MCP 后端、工具、scope、限流、文件 URL 和审计表已实现，但 Web 无启停、连接说明、工具清单、调用状态或配置入口。
- **差距类型**：仅后端。
- **优先级**：**P1**。
- **建议**：设置页增加 MCP 状态与 URL；Access 页提供最小 scope PAT 指引；新增 MCP 工具与健康状态页，不在前端展示 PAT 明文。

#### GAP-W02：MCP 详细审计和普通审计导出没有 Web 入口

- **参考实现**：Nezha MCP 审计作为独立资源查询；Komari 管理端有日志页面。
- **Argus 当前状态**：后端已有 `/api/v1/admin/logs/mcp` 和 `/api/v1/admin/logs/export`，Audit 页面只显示普通分页日志，没有 MCP Tab、过滤器和 CSV/JSON 下载。见 `server/internal/api/router.go:185-188`、`web/src/pages/Audit.tsx:7-69`。
- **差距类型**：已开发但无法从 Web 使用。
- **优先级**：**P1**。
- **建议**：Audit 页面增加“管理审计/MCP 调用”Tab、时间/用户/action/outcome 过滤和导出按钮。

#### GAP-W03：缺少 PWA 安装与离线壳

- **参考实现**：Komari 和 Nezha 用户前端均包含 manifest、Service Worker 和安装图标；Komari 为 standalone PWA。
- **Argus 当前状态**：`web/index.html` 只有 SPA 入口、主题和语言首屏脚本，没有 Web App Manifest 或 Service Worker。
- **差距类型**：未实现。
- **优先级**：**P2**。
- **建议**：仅缓存静态壳和只读公开请求，不缓存管理 API、会话和实时数据；升级时明确 SW 版本与失效策略。

#### GAP-W04：国际化覆盖范围较小

- **参考实现**：Komari 构建产物有 5 套完整语言资源；Nezha 后端内嵌约 15 种 gotext 语言，前端也采用 i18n。
- **Argus 当前状态**：完整支持 `zh-CN` 与 `en`，有 key/占位符检查脚本。
- **差距类型**：产品范围差距。
- **优先级**：**P2**。
- **建议**：按用户来源优先增加繁中、日语，不建议一次扩充到 15 种；翻译必须纳入 CI key 检查。

#### GAP-W05：i18n 完整检查未进入 CI

- **参考实现**：参考项目构建流程会绑定外部前端和语言资源；缺失资源会在构建阶段暴露。
- **Argus 当前状态**：`web/package.json` 有 `check:i18n`，但当前 `.cnb.yml`/构建脚本没有显式执行；TypeScript 只能覆盖部分结构问题。
- **差距类型**：质量门禁缺口。
- **优先级**：**P2**。
- **建议**：在 Web test/build 前执行 `pnpm check:i18n`。

#### GAP-W06：公开节点详情缺少可持久化布局模板

- **参考实现**：Komari 节点详情支持指标选择、拖拽图表布局和全局模板。
- **Argus 当前状态**：已有历史图表、指标详情、对比页和世界地图，但布局固定，用户不能保存个人/全局看板布局。
- **差距类型**：体验差距。
- **优先级**：**P3**。
- **建议**：只有在看板定制成为高频需求后再做；优先采用用户级 JSON 布局，不开放任意 HTML。

#### GAP-W07：插件不能提供公开用户页面

- **参考实现**：Komari 插件 manifest 可声明 public 页面，公开路由以 iframe 提供；插件还可注入管理和用户 HTML。
- **Argus 当前状态**：插件支持后台 RPC/route/hooks，但插件路由全部位于鉴权管理 API，Web 只有管理页，没有公开插件页面挂载点。
- **差距类型**：部分实现。
- **优先级**：**P2**。
- **建议**：若实现，仅提供严格 CSP 的 sandbox iframe、独立 origin 或不可读主站凭据的隔离；不允许直接注入主 React DOM。

### 4.5 API、协议与集成

#### GAP-I01：缺少 OpenAPI/Swagger 规范

- **参考实现**：Nezha debug 模式挂载 Swagger UI，并在 release 流程生成 docs；Komari 通过声明式 RPC2 暴露统一方法表。
- **Argus 当前状态**：REST/WS 路由完整，但没有 OpenAPI 文档、schema 生成或契约发布；前端 API 类型为手写。
- **差距类型**：未实现。
- **优先级**：**P2**。
- **建议**：以 OpenAPI 3.1 描述 REST；WebSocket/Agent JSON-RPC 单独维护 AsyncAPI 或协议文档；优先从服务端 DTO 生成客户端类型，降低字段断链风险。

#### GAP-I02：OAuth 账户绑定/解绑与登录方式管理缺失

- **参考实现**：Komari 支持 OAuth 登录、绑定、解绑并记录 Session 登录方式；Nezha 提供 OAuth2 unbind 和 profile 自我管理。
- **Argus 当前状态**：可配置 GitHub/Gitee/自定义 OAuth provider 并登录/自动注册，但 User 没有 OAuthBind 模型，用户不能查看或解绑登录方式。
- **差距类型**：部分实现。
- **优先级**：**P2**。
- **建议**：新增 `(provider, subject)` 唯一绑定表；解绑前必须保证仍有密码或其他绑定，防止锁死账号。

#### GAP-I03：反向代理下 OAuth 回调 URL 推导不稳健

- **参考实现**：两份参考项目均把外部访问配置作为部署配置的一部分。
- **Argus 当前状态**：OAuth 回调 scheme 主要依赖 `Request.TLS`；TLS 在 Nginx/Ingress 终止时可能生成 `http` 回调，缺固定 public URL 或受信任代理头策略。
- **差距类型**：现有功能部署缺陷。
- **优先级**：**P1**。
- **建议**：优先增加明确的 `PUBLIC_URL`；仅在配置可信代理网段后读取 `Forwarded`/`X-Forwarded-Proto`，避免任意客户端伪造。

#### GAP-I04：普通审计日志缺少结果、耗时与资源字段

- **参考实现**：Nezha MCP 审计记录 tool、server、参数哈希、outcome、错误、耗时和 IP；Komari 日志覆盖终端/管理操作。
- **Argus 当前状态**：MCP 审计结构完整，但普通 `AuditLog` 只有 user/action/detail/IP/time，很多操作只在成功路径记录，不能统一查询失败率或耗时。见 `server/internal/model/models.go:587-613`。
- **差距类型**：部分实现。
- **优先级**：**P1**。
- **建议**：扩展 `resource_type/resource_id/outcome/error_code/duration_ms/request_id`；敏感输入继续仅保存哈希和长度；用中间件/操作包装器保证失败路径也落审计。

### 4.6 配置、备份与生命周期

#### GAP-C01：定时加密备份不能直接受控恢复主库

- **参考实现**：Komari 有上传恢复、独立恢复引导页和数据库迁移流程。
- **Argus 当前状态**：普通 SQLite 备份和旧版 `.argusenc` 均保留兼容；旧版加密备份已支持显式确认、隔离解密、完整性检查、回滚快照和 `restart_required`。新增完整实例包另提供 `/admin/backup-schedules/:id/instance` 下载和 `/instance/restore` 受控恢复入口。
- **差距类型**：已实现核心闭环；独立恢复引导和进程管理器自动重启仍未实现。
- **优先级**：**P1**。
- **验证**：CGO 开启后 `TestEncryptedRestore*`、归档安全测试、Server 全量测试通过；前端 Backups 定向测试通过。
- **剩余工作**：资产切换失败时保留回滚目录，成功后由外部进程管理器立即重启。

#### GAP-C02：备份范围不包含完整实例资产

- **参考实现**：Komari 归档备份设计覆盖数据库和受控数据目录，包括主题、插件等实例资产（当前参考代码的 `plguin-data` 拼写缺陷应避免照搬）。
- **Argus 当前状态**：已新增 `argus-instance-backup` manifest v1。归档通过 `VACUUM INTO` 收集一致性 DB 快照，并按白名单纳入 `themes/**`、`plugins/**` 和实际 `ARGUS_DATA_DIR/scripts/**`；manifest 记录逐项大小、SHA-256、权限、必选项和凭据策略，外层继续 AES-256-GCM。恢复前拒绝版本、路径穿越、符号链接、重复、未知条目、大小或哈希错误，解包到隔离 staging 后按 themes/plugins/scripts → DB 顺序切换并保留回滚。
- **差距类型**：核心能力已实现；市场缓存、部署文件和外部 secrets 仍按显式排除策略处理。
- **优先级**：**P1**。
- **验证**：实例归档 round-trip、symlink 拒绝、manifest 篡改拒绝、DB/审计/恢复重点测试通过；前端提供完整实例下载和恢复入口。
- **剩余工作**：市场供应链资产和部署层密钥需由运维单独管理，不写入普通 manifest。

#### GAP-C03：缺少首次安装/恢复/迁移 Web 引导

- **参考实现**：Komari 将 install、database-recovery、database-migration 作为独立受限 Router，普通业务接口不会进入引导状态。
- **Argus 当前状态**：主要依赖命令行环境变量、初始化账号和登录后的维护页面，没有隔离的首装/灾备引导服务。
- **差距类型**：未实现。
- **优先级**：**P2**。
- **建议**：若面向非技术用户，增加一次性 setup token 的引导模式；生产实例已初始化后永久关闭该路由。

#### GAP-C04：缺少 Dashboard/Server 自升级与回滚

- **参考实现**：Komari 安装脚本支持 stable/snapshot 安装、升级和失败回滚；Nezha release 提供多平台 Dashboard 制品并由外部安装脚本管理。
- **Argus 当前状态**：Agent 有手工批量升级和自动更新逻辑；Argus Server 自身只能由 Docker/systemd/手工替换升级，没有内置升级状态、版本检查或回滚工具。
- **差距类型**：未实现。
- **优先级**：**P2**。
- **建议**：Docker 部署优先交给镜像标签和编排器；二进制部署提供独立 CLI `argus-server update`，不要由正在运行的 Web 进程自覆盖。

#### GAP-C05：Server 发布平台覆盖不足

- **参考实现**：Komari 发布 Linux/Windows 等平台；Nezha Dashboard 覆盖 Linux amd64/arm64/s390x 和 Windows amd64，Docker 覆盖多架构。
- **Argus 当前状态**：Agent 发布矩阵较完整；Server 使用 CGO SQLite，默认主要构建 Linux amd64，arm64 依赖额外交叉工具链，Windows 没有标准发行闭环。
- **差距类型**：部分实现。
- **优先级**：**P2**。
- **建议**：至少稳定支持 Linux amd64/arm64 和多架构镜像；Windows Server 仅在有真实部署需求时加入。

#### GAP-C06：DDNS 高级选项不完整

- **参考实现**：Nezha 支持自定义 DNS、IDN、1-10 次重试和服务器级域名覆盖；Komari/Nezha 均有明确 provider 管理。
- **Argus 当前状态**：Cloudflare、腾讯 DNSPod、HE、Webhook、A/AAAA/dual 和持久重试状态已实现，但没有用户可配置的 DNS resolver、IDN 显式规范化和重试次数策略。
- **差距类型**：部分实现。
- **优先级**：**P2**。
- **建议**：统一域名 punycode 规范化；resolver 与 retry policy 放在 profile 高级设置，默认保持当前安全值。

### 4.7 插件、主题和安全边界

#### GAP-S01：插件市场签名未验证

- **参考实现**：Komari 对危险插件权限按 manifest hash 审批，权限变化要求重新批准；插件市场和加载失败处理形成完整信任链。
- **Argus 当前状态**：已有权限审批、SHA-256、SSRF 防护和运行时限制，但市场 `Signature` 明确标注“预留，当前不校验”，即使索引携带 Ed25519 签名也只记录日志。见 `server/internal/plugin/plugin.go:1494,1503-1509,1617-1618`。
- **差距类型**：已设计但未实现的供应链安全功能。
- **优先级**：**P0**。
- **建议**：配置可信公钥集合，验证“规范化 manifest + artifact SHA-256 + version”；签名存在但失败必须拒绝，未签名包按显式策略禁用或强警告。

#### GAP-S02：插件市场源管理不如 Komari 完整

- **参考实现**：Komari 可 CRUD 多个主题/插件市场源，按来源拉取 catalog。
- **Argus 当前状态**：Manager 主要使用单一 MarketIndexURL；Web 有市场安装，但没有多源优先级、禁用、健康状态和来源级信任策略。
- **差距类型**：部分实现。
- **优先级**：**P3**。
- **建议**：在签名验证完成前不要优先扩展多源；之后增加来源公钥、启停和最后同步状态。

## 5. 明确不应按“缺失”处理的设计差异

这些差异已经审计，但不建议为了表面对齐而开发：

| 差异 | 参考项目 | Argus 决策建议 |
|---|---|---|
| gRPC/protobuf | Nezha 使用 gRPC 双向流 | Argus 已明确采用 WebSocket + JSON-RPC 2.0，并具备完整 capability 与错误码；除非需要 Nezha Agent 兼容，不迁移协议。 |
| 浏览器 RPC2 batch | Komari 使用 JSON-RPC 直连和 batch | Argus REST + WS 已满足 Web 与外部集成；通过 OpenAPI补契约即可，不必再加第二套浏览器 API。 |
| 主题执行 JS/Raw HTML/redirect | Komari 支持 raw/redirect，Nezha支持外部完整前端模板 | Argus 主题只允许 CSS/字体/图片是有意的安全边界。不要为了“主题能力”放开任意 JS；完整 UI 替换应作为独立签名应用包设计。 |
| WAF `count^4` | Nezha 使用四次方封禁 | Argus 当前 `base × count²`、上限 72h 是策略差异，不是功能缺失；可配置化优于硬对齐。 |
| MCP GET/SSE | Nezha 与 Argus 都采用 POST-only Streamable HTTP | 不构成参考差距；只有客户端兼容数据证明需要时再加入会话/SSE。 |
| NAT 通用 TCP/UDP | 两份参考实现核心同样是 HTTP/字节隧道 | Argus 明确限制为 HTTP/WS，不能误报为落后于参考项目。 |
| 任意数据库 SQL 控制台 | Komari 注册了 `dbQuery/dbExec/dbTables` 后端能力 | 该能力扩大破坏面，Argus 不应提供通用 Web SQL 执行；维护页保留 size/vacuum/备份即可。 |

## 6. 建议实施顺序

### 阶段 A：契约与供应链安全

1. GAP-G01：补齐 `trace`、`auto_update` REST/Web 配置链路。
2. GAP-S01：实施插件 Ed25519 签名验证。
3. GAP-I03：引入 `PUBLIC_URL` 和可信代理配置。
4. GAP-G05：过户后台 GC 与 retry。

### 阶段 B：数据与可观测性完整性

1. GAP-M01：补全历史指标字段及逐设备模型。
2. GAP-A01：增加周期总流量口径。
3. GAP-A03：服务多探测点模型。
4. GAP-I04：统一普通审计 outcome/耗时/资源字段。
5. GAP-M02：抽象指标存储接口，再决定外部后端。

### 阶段 C：管理闭环

1. GAP-W01/W02：MCP 管理、MCP 审计、普通审计导出 UI。
2. GAP-C01/C02：加密备份恢复与完整实例备份。
3. GAP-G03：管理台流式文件传输。
4. GAP-I01：OpenAPI 与客户端类型生成。

### 阶段 D：产品增强

按实际用户需求选择 PWA、更多语言、本地 MMDB、安装向导、Server 多平台发布、OAuth 绑定、多市场源、看板布局和终端体验，不建议无差别照搬。

## 7. 验收建议

每个阶段至少执行以下验证：

- Go：`go test ./...`，Windows 下 SQLite 临时文件锁包可单独重试，但必须记录首次失败原因。
- Web：单元测试、TypeScript build、`pnpm check:i18n`。
- 契约：对 AgentConfig、Capabilities、OpenAPI/TypeScript 类型增加字段一致性测试。
- 数据迁移：用旧版 SQLite 副本验证 AutoMigrate、rollup 和查询结果。
- 浏览器：桌面与移动 viewport 检查 Overview、Server Detail、Audit、Files、Terminal、Settings。
- 安全：插件签名的合法、错误、未知公钥、降级、重放场景；备份恢复的篡改、错误密钥、路径穿越和中断恢复场景。
- 生产闭环：所有新增管理能力必须有 API、权限、审计、Web 入口和失败状态，避免再次出现“后端已实现但不可操作”。

## 8. 关键源码索引

### Komari

- 路由全景：`D:\code\shell\komari\web\router\router.go`
- 主模型与监控字段：`D:\code\shell\komari\database\models\models.go`
- 指标存储：`D:\code\shell\komari\internal\metricstore\`
- Agent v2 队列：`D:\code\shell\komari\web\agent\v2_events.go`
- 配置中心：`D:\code\shell\komari\internal\config\config.go`
- 插件与主题：`D:\code\shell\komari\internal\plugin\`、`web/api/admin/theme*`
- 构建前端来源：`D:\code\shell\komari\.github\actions\build-frontend\action.yml`

### Nezha

- API 全景：`D:\code\shell\nezha\cmd\dashboard\controller\controller.go`
- Agent protobuf：`D:\code\shell\nezha\proto\nezha.proto`
- 告警指标：`D:\code\shell\nezha\model\rule.go`
- 服务监控：`D:\code\shell\nezha\service\singleton\servicesentinel.go`
- 告警调度：`D:\code\shell\nezha\service\singleton\alertsentinel.go`
- TSDB：`D:\code\shell\nezha\pkg\tsdb\`
- 前端来源：`D:\code\shell\nezha\service\singleton\frontend-templates.yaml`

### Argus

- Agent 协议：`D:\code\shell\Argus\protocol\protocol.go`
- REST/WS 路由：`D:\code\shell\Argus\server\internal\api\router.go`
- 数据模型：`D:\code\shell\Argus\server\internal\model\models.go`
- 实时与分钟聚合：`D:\code\shell\Argus\server\internal\store\store.go`
- 多级 rollup：`D:\code\shell\Argus\server\internal\metric\rollup.go`
- 告警引擎：`D:\code\shell\Argus\server\internal\alert\engine.go`
- MCP：`D:\code\shell\Argus\server\internal\mcp\mcp.go`
- 插件：`D:\code\shell\Argus\server\internal\plugin\plugin.go`
- Web 路由：`D:\code\shell\Argus\web\src\main.tsx`
- Web API 客户端：`D:\code\shell\Argus\web\src\lib\api.ts`

## 9. 最终判断

Argus 当前不是“缺大量基础模块”，而是进入了 **能力收口与生产化补齐** 阶段。继续按页面数量堆功能的收益较低，优先应把已经存在的协议字段、MCP、审计、备份和插件信任链做成完整闭环，再扩展指标存储和多探测点模型。

本报告所列 P0/P1 项处理完后，Argus 在核心监控、告警、远程运维和安全审计方面可达到或超过两份参考项目的共同能力；P2/P3 应依据目标用户和部署规模选做，不建议机械复制 Komari/Nezha 的全部产品决策。

## 10. 2026-08-19 本地部署复核

- **Komari**：使用仓库已有 `komari.exe`，隔离数据库 `D:\code\shell\komari\.argus-audit-runtime\komari.db` 和端口 `25775` 启动成功。首次未初始化时访问根路由得到 `307 /install`，说明 HTTP 服务和安装引导可用；该版本在安装引导状态下未返回 README 所述匿名 `/ping`，因此不能把 `/ping` 作为本次运行的唯一成功标准。源码/README 仍确认正常初始化后探针为 `GET /ping`。
- **Nezha**：使用仓库 MinGW、`CGO_ENABLED=1` 和 `go_json` 标签构建 `cmd/dashboard` 成功，产物约 96 MiB。使用隔离 `config.yaml`/`sqlite.db` 启动成功，访问 `http://127.0.0.1:8008/` 返回 HTTP 200 和嵌入前端 HTML；该项目没有专用匿名 `/healthz`/`/ping`，根页面 200 与启动日志作为健康证据。
- **环境限制**：本机无 Docker，因此没有执行两项目 Docker Compose/容器部署；两项目的容器命令、卷路径、端口和源码入口已按 Dockerfile 记录。参考项目隔离运行目录和构建产物不纳入 Argus 提交。

## 11. 本轮验证摘要

- Server：`CGO_ENABLED=1 CC=gcc` 使用仓库 `.tools/mingw`，全量测试全部通过；Protocol、Agent 全量测试此前已通过；三模块 build/vet 已通过。
- Web：TypeScript、i18n 检查、Vite 生产构建和 Vitest 全量通过；Node 25 的无效 `--localstorage-file` 会导致 jsdom storage 失效，已在测试 setup 增加仅针对缺失 Storage 方法的 fallback。最终 Vitest 为 17 个文件、78/78 用例通过；备份页面定向测试 2/2 通过。构建仍有既有的大 bundle 约 2.5 MiB 警告，i18n 保留 23 个既有未使用 key 提示。
- 完整实例备份：manifest v1、归档安全测试、加密恢复/审计重点测试和前端入口已通过；完整实例恢复仍要求外部进程重启，市场缓存/外部密钥由部署层单独管理。

## 12. 上线阻断项收口（2026-08-19）

按上线风险排序完成以下收口：

1. **P0 - 安全首次启动**：裸二进制默认监听收紧为 `127.0.0.1:8080`。生产模式必须提供至少 12 字符、非示例的 `ARGUS_ADMIN_PASS`；显式 `ARGUS_DEV_MODE=true` 才允许本地示例凭据。启动日志不再输出密码。`ARGUS_JWT_SECRET` 若显式设置，必须至少 32 字符；JWT 自动生成移到命令行 `-d` 最终数据库路径确定之后。Compose 强制管理员密码与 JWT secret，systemd 环境文件缺失会使服务失败而非静默回退。
2. **P0 - 恢复生命周期**：明文、加密和完整实例恢复成功后均先返回响应，再标记 `restart_pending` 并调用主进程重启协调器。`/healthz` 对 restart pending、数据库 pool 不可用或 SQLite Ping 失败返回 HTTP 503；主进程关闭 HTTP 服务并以退出码 75 退出，供 systemd/Docker 重启。
3. **P1 - 明文恢复收敛**：分片 `.db` 恢复增加显式确认、失败与成功结构化审计、staging 审计写入、backup restore lock、回滚路径和重启调度，不再绕过加密恢复的安全语义。
4. **P1 - 高风险追责和二次验证**：敏感 PAT 被明确拒绝；用户密钥读取、管理员用户操作、插件副作用与 OAuth 配置路由要求管理员二次验证。用户更新、2FA setup/enable/disable、OAuth 配置和插件 RPC/route/config/启停/批准/运行/删除已接入结构化成功或关键失败审计，审计 detail 不记录密码、验证码、OAuth secret、Agent secret、插件参数或请求体。
5. **P2 - 提交卫生**：`.tools/`、根 `node_modules/` 与 `outputs/web-p0-build/` 已加入忽略规则，防止本机 MinGW、依赖与验证产物进入源代码提交。

本轮验证：Server 全量 test/build/vet、Agent/Protocol 全量 test/build/vet、Web 17 个 Vitest 文件共 78 个用例、i18n 检查与 Vite 生产构建均通过。隔离 Argus Server 证实缺失管理员密码时以明确配置错误退出；提供强密码/JWT 后 `/healthz` 返回 200，浏览器首页渲染正常。Docker/Podman 在本机不可用，因此 Compose 和 systemd 的真实编排重启仍是部署环境验证项，不应表述为本机已通过。
