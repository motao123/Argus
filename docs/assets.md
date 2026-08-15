# 可迁移资产清单

> 汇总 komari / nezha-master / nezha-dash-v2 三项目的盘点结果，合并去重，
> 标注来源项目、功能用途、适配改造要点。供 Argus 整合版开发索引。

## 一、后端服务能力

### 1.1 监控采集与上报（核心）

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| Agent 上报协议 | komari | JSON-RPC 2.0 over WS（v2），含 register/report/事件队列 | 直接复用设计 | ✅ 已实现（protocol/rpc） |
| gRPC 上报流 | nezha | ReportSystemState/RequestTask 双向流 | 性能更强但工具链重，**不迁** | — |
| 实时状态内存区 | nezha | ServerClass 内存台账 + 权限过滤 | 已实现 store.Hub | ✅ 已实现 |
| 指标降采样 TSDB | komari | pkg/metric 分层 rollup（1m→5m→1h）+ t-digest | **核心资产**，当前 Argus 仅分钟级，需升级 | ⚠️ 已实现基础版，待增强 |
| 内存热降采样 | komari | raw 存内存 10min + 逐级物化 | 借鉴，Argus 已有 MetricBatcher | ⚠️ 已实现基础版 |
| VictoriaMetrics 封装 | nezha | pkg/tsdb 客户端 + 保留期 + 磁盘水位 | 单二进制可不迁（嵌入太重） | — |
| GPU/温度采集 | nezha | State_SensorTemperature/GPU 字段 | agent 采集端扩展 | 📋 待实现 |
| Ping/TCP/HTTP 服务监控 | nezha | ServiceSentinel + 探测任务分发 | **核心差异化功能**，需实现 | 📋 待实现 |
| Ping 监控 | komari | PingTask + PingRecord（icmp/tcp/http） | 与 nezha 服务监控合并 | 📋 待实现 |
| 流量周期统计 | nezha | Transfer 模型 + cycle_transfer_stats | 待实现 | 📋 待实现 |

### 1.2 任务与自动化

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| 定时任务调度 | komari/nezha | robfig cron 下发命令 | ✅ 已实现 | ✅ 已实现 |
| 触发任务（报警联动） | nezha | 报警失败/恢复时执行命令 | 状态机扩展 | 📋 待实现 |
| 手动触发防重放 | nezha | 一次性授权令牌 consume 语义 | 待实现 | 📋 待实现 |
| 批量命令执行 | komari | admin:exec 批量下发 | 待实现 | 📋 待实现 |
| 任务结果查询 | komari | getTaskResultsByTaskId | 待实现 | 📋 待实现 |
| 网页终端 | komari/nezha | WS 隧道 + IOStream 中继 | ✅ 已实现 | ✅ 已实现 |
| 文件管理器 | nezha | FsList/FsRead/FsWrite/FsDelete 任务 | **核心差异化**，需实现 | 📋 待实现 |
| Agent 升级下发 | nezha | TaskTypeUpgrade | 待实现 | 📋 待实现 |
| MCP 自动化接入 | nezha | /mcp JSON-RPC 端点 + server.exec/fs.* 工具 | **独特资产**，供 AI 操作服务器 | 📋 待实现（可选） |

### 1.3 告警与通知

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| 阈值+时长状态机 | nezha | AlertSentinel 规则引擎 | ✅ 已实现基础版 | ✅ 已实现 |
| 周期流量告警 | nezha | transfer_*_cycle 规则类型 | 依赖流量统计 | 📋 待实现 |
| 负载达标比例告警 | komari | LoadNotification 采样判定 | 待实现 | 📋 待实现 |
| 离线/上线通知 | komari | notifier/offline（连接 ID 防抖） | 待实现 | 📋 待实现 |
| 通知渠道 | komari | bark/telegram/webhook/email/serverchan/javascript | **多渠道**，当前仅 webhook | ⚠️ 已实现 webhook，待扩展 |
| 通知渠道模板 | nezha | JSON/Form body + {{title}}/{{content}} | ✅ 已实现 | ✅ 已实现 |
| 通知分组扇出 | nezha | NotificationGroup 多对多 | 待实现 | 📋 待实现 |
| 通知脱敏 | nezha | IP 打码开关 | 待实现 | 📋 待实现 |

### 1.4 服务器管理与多租户

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| 服务器 CRUD + 分组 | komari/nezha | 基础管理 | ✅ 已实现 | ✅ 已实现 |
| 服务器排序/搜索/过滤 | komari | orderClients + 分组/标签 | 前端已实现，后端排序待实现 | ⚠️ 前端已实现 |
| 批量操作 | nezha | batch-delete/batch-move | 待实现 | 📋 待实现 |
| 服务器过户转移 | nezha | transfer 状态机（跨账号） | **独特资产** | 📋 待实现 |
| 服务器计费信息 | komari | 价格/周期/自动续费字段 | 待实现 | 📋 待实现 |
| 服务器隐藏（guest 不可见） | nezha | HideForGuest | 待实现 | 📋 待实现 |
| PAT 细粒度权限 | nezha | `nezha:{resource}:{verb}` + 白名单 | **核心安全资产**，需实现 | 📋 待实现 |
| 多用户（admin/user 两级） | nezha | owner 归属 + agent_secret 关联 | **核心多租户资产**，需实现 | 📋 待实现 |
| 在线会话管理 | nezha | online-user 踢出/封禁 | 待实现 | 📋 待实现 |

### 1.5 第三方服务对接

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| OAuth2 登录 | komari/nezha | GitHub/Gitee/QQ/OIDC | **核心资产**，需实现 | 📋 待实现 |
| TOTP 2FA | komari | 敏感操作二次验证 | 待实现 | 📋 待实现 |
| GeoIP（mmdb 内嵌） | nezha | pkg/geoip 国家归属 | **差异化**，需实现 | 📋 待实现 |
| GeoIP 多 provider | komari | mmdb/ipinfo/ipapi/geojs 可插拔 | 待实现 | 📋 待实现 |
| DDNS | nezha | libdns cloudflare/tencentcloud/HE/webhook | **独特资产**，需实现 | 📋 待实现 |
| NAT 内网穿透 | nezha | 域名→服务器映射 + IOStream 隧道 | **独特资产**，需实现 | 📋 待实现 |

### 1.6 运维与系统能力

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| 备份/恢复 | komari | 打包下载 + 上传恢复 | 待实现 | 📋 待实现 |
| 数据迁移向导 | komari | legacy 库迁移 + 指标库恢复 | 待实现 | 📋 待实现 |
| 运维子命令 | komari | chpasswd/disable-2fa/permitPasswordLogin | **逃生门**，需实现 | 📋 待实现 |
| pprof 性能剖析 | komari/nezha | /api/admin/pprof | 待实现 | 📋 待实现 |
| 审计日志 | komari/nezha | Log/MCPAuditLog | 待实现 | 📋 待实现 |
| 安装向导 | komari | 首启 install guide | 当前自动建 admin，可保持 | ⚠️ 已实现简化版 |
| 主题系统 | komari | 主题包（zip+manifest）+ 主题市场 | **差异化**，当前内置单主题 | ⚠️ 已实现内置主题 |
| 插件系统 | komari | goja 沙箱 JS 插件 + server 模块钩子 | **核心差异化**，大工程 | 📋 待实现 |
| 分片上传 | komari | init/chunk/merge/cancel 通用上传 | 待实现 | 📋 待实现 |

## 二、前端交互能力

### 2.1 用户前台

| 资产 | 来源 | 用途 | Argus 状态 |
|---|---|---|---|
| 服务器卡片总览 | dash-v2 | ServerCard + 排序/分组/状态过滤 | ✅ 已实现基础版 |
| 实时 WS 数据流 | dash-v2 | websocket-provider + 30 条历史缓冲 + 重连 | ✅ 已实现基础版 |
| 实时+历史图表拼接 | dash-v2 | use-chart-history + ServerDetailChart | ✅ 已实现基础版 |
| 多周期图表 | dash-v2 | realtime/1d/7d/30d + 降采样 | ✅ 已实现 1h/24h/7d |
| 全球地图 | dash-v2 | GlobalMap + d3-geo + GeoJSON 内嵌 | **差异化**，待实现 📋 |
| 服务可用率条 | dash-v2 | ServiceTracker 30 天色块 | **差异化**，待实现 📋 |
| 周期流量统计卡 | dash-v2 | CycleTransferStats | 待实现 📋 |
| 命令面板 Cmd+K | dash-v2 | DashCommand 搜索跳转 + 主题切换 | 待实现 📋 |
| 实时时钟 | dash-v2 | Header AnimatedCount | 待实现 📋 |
| 登录用户检测轮询 | dash-v2 | fetchLoginUser 30s 触发重连 | 待实现 📋 |
| 错误边界 | dash-v2 | ErrorBoundary + ErrorPage | 待实现 📋 |
| 后端不可达提示 | dash-v2 | BackendErrorState | 待实现 📋 |
| Mock 演示环境 | dash-v2 | mock-server + dev-mock 一键演示 | ✅ 已实现基础版 |

### 2.2 管理后台

| 资产 | 来源 | 用途 | Argus 状态 |
|---|---|---|---|
| 服务器管理（CRUD+密钥） | komari/nezha | 节点管理 | ✅ 已实现 |
| 报警规则管理 | nezha | AlertRule CRUD | ✅ 已实现 |
| 通知渠道管理 | nezha | Notification CRUD + 模板 | ✅ 已实现 |
| 定时任务管理 | komari/nezha | Cron CRUD + 手动执行 | ✅ 已实现 |
| 服务监控管理 | nezha | Service CRUD + 探测任务 | **待实现** 📋 |
| 用户管理 | nezha | User CRUD + 在线会话 | **待实现** 📋 |
| PAT 令牌管理 | nezha | scope 勾选 + 白名单 | **待实现** 📋 |
| DDNS/NAT 管理 | nezha | 配置 CRUD | **待实现** 📋 |
| 服务器过户管理 | nezha | transfer 发起/取消/进度 | **待实现** 📋 |
| 主题管理 | komari | 主题列表/切换/配置/市场 | **待实现** 📋 |
| 插件管理 | komari | 插件启停/配置/日志 | **待实现** 📋 |
| 审计日志查看 | komari | Log 分页查询 | **待实现** 📋 |
| 数据库工具 | komari | 体积/VACUUM/受限 SQL | **待实现** 📋 |
| 2FA 设置 | komari | TOTP 二维码生成 | **待实现** 📋 |
| 系统设置 | komari/nezha | 站点名/主题/安全/性能 | **待实现** 📋 |

### 2.3 通用支撑（前端）

| 资产 | 来源 | 用途 | Argus 状态 |
|---|---|---|---|
| shadcn/ui 组件库 | dash-v2 | 20 个基元组件（button/card/dialog/chart 等） | ✅ 已实现基础版（含 chart） |
| 明暗主题系统 | dash-v2 | ThemeProvider + 防 FOUC + meta theme-color | ✅ 已实现 |
| 格式化工具 | dash-v2 | formatBytes/formatRelativeTime/cn | ✅ 已实现基础版 |
| API 封装 | dash-v2 | fetch + CSRF + refresh-token | ✅ 已实现基础版 |
| 数据适配层 | dash-v2 | formatNezhaInfo（原始→视图模型） | ✅ 已实现基础版 |
| 错误处理约定 | dash-v2 | retry:false + toast + WS 静默容错 | ✅ 已实现基础版 |
| 滚动位置保存 | dash-v2 | saveMainPageScrollPosition + 恢复 | 待实现 📋 |
| 滑动指示器动画 | dash-v2 | use-active-indicator（ResizeObserver） | 待实现 📋 |
| 自定义代码注入 | dash-v2 | InjectContext（白标定制） | 待实现 📋 |
| 国旗/OS 图标 | dash-v2 | ServerFlag + GetOsName/logo-class | 待实现 📋 |
| 骨架屏 | dash-v2 | ChartSkeleton/ServerDetailLoading | 待实现 📋 |

## 三、样式设计系统

| 资产 | 来源 | 用途 | Argus 状态 |
|---|---|---|---|
| Tailwind v4 CSS-first 配置 | dash-v2 | `@import "tailwindcss"` + `@theme` 映射 | ✅ 已实现 |
| 明暗配色（CSS 变量） | dash-v2 | `--color-*` + `.dark` 变体 | ✅ 已实现 |
| 图表色板（chart-1..5） | dash-v2 | 明暗双主题图表配色 | ✅ 已实现基础版 |
| 圆角体系（--radius 1rem） | dash-v2 | 大圆角卡片风格 | ✅ 已实现基础版 |
| 语义化 class 前缀 | komari | `km-` / 整合版 `argus-` | ⚠️ 已实现基础版 |
| 数字等宽 | dash-v2 | tabular-nums 防抖动 | ✅ 已实现 |
| 字体（Inter + 中文回退） | dash-v2 | 统一字体栈 | ✅ 已实现 |
| 胶囊控件（rounded-[50px]） | dash-v2 | Tab/分组切换样式 | ✅ 已实现基础版 |
| 淡入动画（tooltip-animate） | dash-v2 | blur 淡入 | 待实现 📋 |
| 数字滚动动画 | dash-v2 | data-issues-count-animation | 待实现 📋 |
| 状态着色阈值 | dash-v2 | >90 红 / >70 橙 / 反向下限 | ✅ 已实现基础版 |

## 四、合并去重结论

### 4.1 共用功能（两项目都有，选优融合）

| 功能 | 优选来源 | 理由 | Argus 处理 |
|---|---|---|---|
| Agent 上报 | komari | JSON-RPC 2.0 轻量免 protobuf，浏览器可直调 | ✅ 已选 komari 方案 |
| 指标存储 | komari | 分层 rollup + t-digest 百分比，工程更完整 | ⚠️ 已采用简化版，待升级 |
| 实时推送 | nezha | singleflight 合并投影 + 2s 周期 | ✅ 已选 nezha 方案 |
| 网页终端 | nezha | IOStream 隧道复用同一条连接 | ✅ 已选单连接复用方案 |
| 报警规则 | nezha | 状态机更严谨（持续时长+周期+联动） | ✅ 已选 nezha 方案 |
| 定时任务 | nezha | cron + 触发任务 + 手动执行授权 | ✅ 已选 nezha 方案 |
| 通知渠道 | komari | 渠道更多（bark/telegram/email/js） | ⚠️ 已用 webhook，待扩展 komari 渠道 |
| 前端框架 | dash-v2 | React 19 + Tailwind v4 + shadcn/ui + Recharts | ✅ 已选 dash-v2 方案 |
| 认证 | nezha | JWT + PAT scope + OAuth2，权限更细 | ⚠️ 已用 JWT，待扩展 PAT+OAuth |

### 4.2 差异功能（仅一项目有，全量保留）

| 功能 | 来源 | Argus 处理 |
|---|---|---|
| 服务监控（HTTP/TCP/Ping） | nezha | **核心差异化**，高优先级实现 |
| 文件管理器 | nezha | 高优先级实现 |
| DDNS | nezha | 中优先级实现 |
| NAT 内网穿透 | nezha | 中优先级实现 |
| 服务器过户 | nezha | 中优先级实现 |
| PAT 细粒度权限 | nezha | 高优先级实现 |
| 多用户 | nezha | 高优先级实现 |
| MCP 自动化 | nezha | 低优先级（可选） |
| 全球地图 | dash-v2 | 高优先级实现 |
| 服务可用率条 | dash-v2 | 高优先级实现 |
| 命令面板 | dash-v2 | 中优先级实现 |
| GeoIP | nezha | 中优先级实现 |
| 插件系统 | komari | 低优先级（大工程，可后置） |
| 主题市场 | komari | 低优先级 |
| 计费/续费 | komari | 低优先级（VPS 售卖场景专用） |
| 多 GeoIP provider | komari | 低优先级 |

### 4.3 需要融合改造的重复模块

| 模块 | 融合方案 |
|---|---|
| 报警规则 | nezha 状态机骨架 + komari 渠道/离线通知/流量告警，统一为 Argus 报警引擎 |
| 通知渠道 | komari 多渠道实现 + nezha 分组/模板，统一为 Argus 通知中心 |
| 服务监控 | nezha ServiceSentinel（HTTP/TCP/Ping）+ komari PingTask，统一为 Argus 服务监控 |
| 服务器管理 | nezha 多用户/过户/隐藏 + komari 计费字段，统一为 Argus 服务器台账 |
| 前端总览 | dash-v2 卡片/地图/服务条 + komari 节点隐藏，统一为 Argus 用户前台 |
