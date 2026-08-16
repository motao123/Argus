# 可迁移资产清单

> 汇总 komari / nezha-master / nezha-dash-v2 三项目的盘点结果，合并去重，
> 标注来源项目、功能用途、适配改造要点。供 Argus 整合版开发索引。
> 状态刷新：2026-08-16（第 2 轮：逐项核实并纠正此前误报；状态列统一为
> 「✅ 完整可用 / ⚠️ 部分或仅后端 / 📋 待实现」，不再以“有路由”代替“可用”）。

## 一、后端服务能力

### 1.1 监控采集与上报（核心）

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| Agent 上报协议 | komari | JSON-RPC 2.0 over WS（v2），含 register/report/事件队列 | 借鉴式实现：无 jsonrpc 信封、无 pull/ack 事件队列、无 HTTP fallback；不兼容 komari v2 | ⚠️ 部分（协议风格借鉴） |
| gRPC 上报流 | nezha | ReportSystemState/RequestTask 双向流 | 性能更强但工具链重，**不迁** | — |
| 实时状态内存区 | nezha | ServerClass 内存台账 + 权限过滤 | 已实现 store.Hub | ✅ 已实现 |
| 指标降采样 TSDB | komari | pkg/metric 分层 rollup（1m→5m→1h）+ t-digest | 单二进制场景采用 SQLite 分钟级 rollup + 聚合查询（internal/metric/rollup） | ✅ 已实现基础版 |
| 内存热降采样 | komari | raw 存内存 + 逐级物化 | 借鉴，Argus 已有 MetricBatcher | ✅ 已实现 |
| VictoriaMetrics 封装 | nezha | pkg/tsdb 客户端 + 保留期 + 磁盘水位 | 单二进制可不迁（嵌入太重） | — |
| GPU/温度采集 | nezha | State_SensorTemperature/GPU 字段 | agent hw.go 已采 GPU（nvidia-smi），温度待补 | ✅ GPU / 📋 温度 |
| Ping/TCP/HTTP 服务监控 | nezha | ServiceSentinel + 探测任务分发 | **核心差异化功能** | ✅ 已实现 |
| Ping 监控 | komari | PingTask + PingRecord（icmp/tcp/http） | 与 nezha 服务监控合并（第4项 服务统计聚合 借鉴 PingRecord 延迟/丢包统计） | ✅ 已融合 |
| 流量周期统计 | nezha | Transfer 模型 + cycle_transfer_stats | 30s 流量台账落库，详情页提供 24 小时/30 天/12 月周期图表，并接入周期流量告警 | ✅ 已实现 |

### 1.2 任务与自动化

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| 定时任务调度 | komari/nezha | robfig cron 下发命令 | ✅ 已实现 | ✅ 已实现 |
| 触发任务（报警联动） | nezha | 报警失败/恢复时执行命令 | 报警规则可关联定时任务，触发时向任务目标下发命令 | ✅ 已实现 |
| 手动触发防重放 | nezha | 一次性授权令牌 consume 语义 | cron run 已做登录/PAT 授权 | ✅ 已实现 |
| 批量命令执行 | komari | admin:exec 批量下发 | 待实现 | 📋 待实现 |
| 任务结果查询 | komari | getTaskResultsByTaskId | run 接口即时返回结果，历史未存 | ⚠️ 部分 |
| 网页终端 | komari/nezha | WS 隧道 + IOStream 中继 | 单连接复用；当前 Agent 使用 stdin/stdout pipe 启动 shell，并非 PTY，交互式 TTY、窗口尺寸和全屏程序支持有限 | ⚠️ 基础可用（非 PTY） |
| 文件管理器 | nezha | FsList/FsRead/FsWrite/FsDelete 任务 | **核心差异化** | ✅ 已实现 |
| Agent 升级下发 | nezha | TaskTypeUpgrade | 管理页支持批量选择 Agent、制品 URL、版本与 SHA-256，下发后展示逐机结果；任务记录仅保存在当前进程内存 | ⚠️ 基础可用（记录不持久化） |
| MCP 自动化接入 | nezha | /mcp JSON-RPC 端点 + server.exec/fs.* 工具 | **独特资产**，供 AI 操作服务器 | ✅ 已实现 |

### 1.3 告警与通知

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| 阈值+时长状态机 | nezha | AlertSentinel 规则引擎 | ✅ 已实现基础版 | ✅ 已实现 |
| 周期流量告警 | nezha | transfer_*_cycle 规则类型 | `traffic_in_cycle` / `traffic_out_cycle` 已接入流量台账和规则管理 UI | ✅ 已实现 |
| 负载达标比例告警 | komari | LoadNotification 采样判定 | 第2项 已实现 | ✅ 已实现 |
| 离线/上线通知 | komari | notifier/offline（连接 ID 防抖） | A6 已实现（offline-notify 配置） | ✅ 已实现 |
| 通知渠道 | komari | bark/telegram/webhook/email/serverchan/javascript | 第3项 JS 渠道已补，共 6 渠道 | ✅ 已实现 |
| 通知渠道模板 | nezha | JSON/Form body + {{title}}/{{content}} | ✅ 已实现 | ✅ 已实现 |
| 通知分组扇出 | nezha | NotificationGroup 多对多 | ✅ 已实现 | ✅ 已实现 |
| 通知脱敏 | nezha | IP 打码开关 | 待实现 | 📋 待实现 |

### 1.4 服务器管理与多租户

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| 服务器 CRUD + 分组 | komari/nezha | 基础管理 | A4 分组管理已实现 | ✅ 已实现 |
| 服务器排序/搜索/过滤 | komari | orderClients + 分组/标签 | B6 标签/手动排序 + 前端 9 种排序 | ✅ 已实现 |
| 批量操作 | nezha | batch-delete/batch-move | A5 已实现 | ✅ 已实现 |
| 服务器过户转移 | nezha | transfer 状态机（跨账号） | 可发起/取消并生成目标用户新密钥；Agent 使用新密钥重连后完成验证 | ✅ 已实现 |
| 服务器计费信息 | komari | 价格/周期/自动续费字段 | B2 到期提醒 + model 字段（price/cycle_days/expire_at/auto_renew） | ✅ 已实现 |
| 服务器隐藏（guest 不可见） | nezha | HideForGuest | B6 hidden 字段 + 前端联动 | ✅ 已实现 |
| PAT 细粒度权限 | nezha | `nezha:{resource}:{verb}` + 白名单 | `argus:{resource}:{verb}` + server_ids 白名单 + 吊销 | ✅ 已实现 |
| 多用户（admin/user 两级） | nezha | owner 归属 + agent_secret 关联 | 本次补：admin 启动即生成密钥 + `GET /users/:id/secret` 管理端读取 | ✅ 已实现 |
| 在线会话管理 | nezha | online-user 踢出/封禁 | sessions 列表 + 踢出（本次新增管理页） | ✅ 已实现 |

### 1.5 第三方服务对接

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| OAuth2 登录 | komari/nezha | GitHub/Gitee/QQ/OIDC | 第5项 已实现（多 provider 可配置） | ✅ 已实现 |
| TOTP 2FA | komari | 敏感操作二次验证 | twofa.go（setup/qrcode/enable/disable） | ✅ 已实现 |
| GeoIP（HTTP provider） | nezha | 服务器国家归属 | 通过 `ARGUS_GEOIP_ENDPOINT` 配置在线 HTTP provider，无内嵌 mmdb；默认不查询。地图依赖国家码，无 provider 或无有效定位点时安全隐藏 | ⚠️ 可选能力（默认关闭） |
| GeoIP 多 provider | komari | mmdb/ipinfo/ipapi/geojs 可插拔 | 待实现 | 📋 待实现 |
| DDNS | nezha | libdns cloudflare/tencentcloud/HE/webhook | 仅 webhook/cloudflare；已补 server 归属校验 | ⚠️ 基础可用 |
| NAT 内网穿透 | nezha | 域名→服务器映射 + IOStream 隧道 | 简化的 HTTP Host 反向代理/TCP 隧道；无 TLS 终止、租约、配额或能力协商，且需单独暴露 `ARGUS_NAT_LISTEN`；已补 server 归属校验 | ⚠️ 基础可用（有限制） |

### 1.6 运维与系统能力

| 资产 | 来源 | 用途 | 改造要点 | Argus 状态 |
|---|---|---|---|---|
| 备份/恢复 | komari | 打包下载 + 上传恢复 | 直接覆盖活动 SQLite（无校验/原子切换/回滚），无 UI | ⚠️ 实验性，待重做 |
| 数据迁移向导 | komari | legacy 库迁移 + 指标库恢复 | 待实现 | 📋 待实现 |
| 运维子命令 | komari | chpasswd/disable-2fa/permitLogin | 仅 chpasswd / disable-2fa；无 permit-login 恢复流程 | ⚠️ 部分 |
| pprof 性能剖析 | komari/nezha | /api/admin/pprof | 第6项 已实现（/debug/pprof + 鉴权中间件） | ✅ 已实现 |
| 审计日志 | komari/nezha | Log/MCPAuditLog | 后端分页查询与管理页均已实现；审计覆盖关键管理操作，但不是所有读取与内部任务 | ⚠️ 基础可用（非全量） |
| 安装向导 | komari | 首启 install guide | 自动建 admin（ARGUS_ADMIN_USER/PASS） | ⚠️ 简化版 |
| 主题系统 | komari | 主题包（zip+manifest）+ 主题市场 | 内置 light/dark + 自定义代码注入点 | ⚠️ 内置主题 |
| 插件系统 | komari | goja 沙箱 JS 插件 + server 模块钩子 | 已有本地市场、安装/删除、持久启停、权限审批、手动/cron 执行与日志 UI；仍无通用路由/hook/页面扩展，沙箱不是强隔离边界，仅适合受信插件 | ⚠️ 基础可用（受信插件） |
| 分片上传 | komari | init/chunk/merge/cancel 通用上传 | 仅备份恢复 append 模式，无会话/顺序/哈希校验 | ⚠️ 简化，待重做 |
| 私有站点模式 | komari | private_site 强制登录 | 第1项 force_auth（本次新增设置页入口） | ✅ 已实现 |
| 自定义代码注入 | dash-v2/nezha | InjectContext 白标定制 | custom_code 设置项（komari 样式 head/body 注入） | ⚠️ 设置键待补全 |

## 二、前端交互能力

### 2.1 用户前台

| 资产 | 来源 | 用途 | Argus 状态 |
|---|---|---|---|
| 服务器卡片总览 | dash-v2 | ServerCard + 排序/分组/状态过滤 | ✅ 已实现 |
| 实时 WS 数据流 | dash-v2 | websocket-provider + 重连退避 | ✅ 已实现 |
| 实时+历史图表拼接 | dash-v2 | use-chart-history + ServerDetailChart | B3 已实现 | ✅ 已实现 |
| 多周期图表 | dash-v2 | realtime/1d/7d/30d + 降采样 | ✅ 已实现 1h/24h/7d |
| 全球地图 | dash-v2 | GlobalMap + ECharts + GeoJSON | ✅ 已实现；依赖 GeoIP `country_code`，无 provider/无定位结果时组件安全隐藏 |
| 服务可用率条 | dash-v2 | ServiceTracker 30 天色块 | B3 已实现（真实数据） | ✅ 已实现 |
| 周期流量统计卡 | dash-v2 | CycleTransferStats | ✅ 已实现（服务器详情 24 小时/30 天/12 月图表） |
| 命令面板 Cmd+K | dash-v2 | DashCommand 搜索跳转 + 主题切换 | ✅ 已实现（CommandPalette） |
| 实时时钟 | dash-v2 | Header AnimatedCount | ✅ 已实现 |
| 登录用户检测轮询 | dash-v2 | fetchLoginUser 30s 触发重连 | ⚠️ 简化（登录态路由守卫） |
| 错误边界 | dash-v2 | ErrorBoundary + ErrorPage | ✅ 已实现（应用根级 ErrorBoundary） |
| 后端不可达提示 | dash-v2 | BackendErrorState | 📋 待实现 |
| Mock 演示环境 | dash-v2 | mock-server + dev-mock 一键演示 | ✅ 已实现（web/scripts） |

### 2.2 管理后台

| 资产 | 来源 | 用途 | Argus 状态 |
|---|---|---|---|
| 服务器管理（CRUD+密钥） | komari/nezha | 节点管理 | ✅ 已实现 |
| 报警规则管理 | nezha | AlertRule CRUD | ✅ 已实现 |
| 通知渠道管理 | nezha | Notification CRUD + 模板 | ✅ 已实现（报警页内嵌） |
| 定时任务管理 | komari/nezha | Cron CRUD + 手动执行 | ✅ 已实现 |
| 服务监控管理 | nezha | Service CRUD + 可用率 | ✅ 已实现 |
| 用户管理 | nezha | User CRUD + 在线会话 | ✅ 已实现（访问控制 + 本次新增在线会话页） |
| PAT 令牌管理 | nezha | scope 勾选 + 白名单 | ✅ 已实现（访问控制页） |
| 站点设置 | komari/nezha | 站点名/私有站点/终端外观 | ✅ 本次新增设置页 |
| DDNS/NAT 管理 | nezha | 配置 CRUD | ✅ 已实现管理界面；DDNS 仅 webhook/Cloudflare，NAT 为简化 HTTP Host 代理/TCP 隧道 |
| 服务器过户管理 | nezha | transfer 发起/取消/进度 | ✅ 已实现（新密钥重连验证完成过户） |
| 主题管理 | komari | 主题列表/切换/配置/市场 | 📋 待实现 |
| 插件管理 | komari | 插件启停/配置/日志 | ⚠️ 管理界面已实现市场安装、启停、审批、执行、删除与日志；无通用配置表单及路由/hook/页面扩展，仅适合受信插件 |
| 审计日志查看 | komari | Log 分页查询 | ✅ 已实现管理界面（关键管理操作审计，非全量访问日志） |
| 数据库工具/维护 | komari | 体积/VACUUM/备份恢复 | ⚠️ 管理界面已实现；备份恢复仍直接操作活动 SQLite，需维护窗口并自行保留外部备份 |
| 2FA 设置 | komari | TOTP 二维码生成 | ✅ 已实现安全设置界面（setup/qrcode/enable/disable） |
| 批量操作 | nezha | 批量删除/批量设分组 | ✅ 已实现服务器管理界面的多选、批量删除与批量移动分组 |

### 2.3 通用支撑（前端）

| 资产 | 来源 | 用途 | Argus 状态 |
|---|---|---|---|
| shadcn/ui 风格组件 | dash-v2 | 自实现基元（卡片/表单/表格/对话框） | ✅ 已实现 |
| 明暗主题系统 | dash-v2 | ThemeProvider + 防 FOUC + meta theme-color | ✅ 已实现 |
| 格式化工具 | dash-v2 | formatBytes/formatRelativeTime/cn | ✅ 已实现 |
| API 封装 | dash-v2 | fetch + 统一响应壳 + JWT | ✅ 已实现 |
| 数据适配层 | dash-v2 | formatNezhaInfo（原始→视图模型） | ✅ 已实现 |
| 错误处理约定 | dash-v2 | retry + WS 静默容错 | ✅ 已实现基础版 |
| 滚动位置保存 | dash-v2 | saveMainPageScrollPosition + 恢复 | 📋 待实现 |
| 滑动指示器动画 | dash-v2 | use-active-indicator | ✅ 已实现基础版（分组 Tab） |
| 自定义代码注入 | dash-v2 | InjectContext（白标定制） | ⚠️ 设置键待补全 |
| 国旗/OS 图标 | dash-v2 | ServerFlag + GetOsName/logo-class | ✅ 已实现基础版 |
| 轻量 i18n | dash-v2 | zh-CN / en 文案切换 | ⚠️ 基础骨架：仅少量导航/公共文案接入字典，管理页及多数业务文案仍为中文 |
| 骨架屏 | dash-v2 | ChartSkeleton/ServerDetailLoading | ✅ 已实现 |

## 三、样式设计系统

| 资产 | 来源 | 用途 | Argus 状态 |
|---|---|---|---|
| Tailwind v4 CSS-first 配置 | dash-v2 | `@import "tailwindcss"` + `@theme` 映射 | ✅ 已实现 |
| 明暗配色（CSS 变量） | dash-v2 | `--color-*` + `.dark` 变体 | ✅ 已实现 |
| 图表色板（chart-1..5） | dash-v2 | 明暗双主题图表配色 | ✅ 已实现 |
| 圆角体系（--radius） | dash-v2 | 大圆角卡片风格 | ✅ 已实现 |
| 语义化 class 前缀 | komari | `km-` / 整合版 `argus-` | ✅ 已实现 |
| 数字等宽 | dash-v2 | tabular-nums 防抖动 | ✅ 已实现 |
| 字体（Inter + 中文回退） | dash-v2 | 统一字体栈 | ✅ 已实现 |
| 胶囊控件 | dash-v2 | Tab/分组切换样式 | ✅ 已实现 |
| 淡入动画 | dash-v2 | tooltip 淡入 | ✅ 已实现 |
| 数字滚动动画 | dash-v2 | issues-count 动画 | ✅ 已实现基础版 |
| 状态着色阈值 | dash-v2 | >90 红 / >70 橙 / 反向下限 | ✅ 已实现 |

## 四、合并去重结论

### 4.1 共用功能（两项目都有，选优融合）

| 功能 | 优选来源 | 理由 | Argus 处理 |
|---|---|---|---|
| Agent 上报 | komari | JSON-RPC 2.0 轻量免 protobuf，浏览器可直调 | ✅ 已选 komari 方案 |
| 指标存储 | komari | 分层 rollup + t-digest 百分比，工程更完整 | ✅ 采用简化版（分钟级 rollup） |
| 实时推送 | nezha | singleflight 合并投影 + 周期推送 | ✅ 已选 nezha 方案 |
| 网页终端 | nezha | IOStream 隧道复用同一条连接 | ✅ 已选单连接复用方案 |
| 报警规则 | nezha | 状态机更严谨（持续时长+周期+联动） | ✅ 已选 nezha 方案 |
| 定时任务 | nezha | cron + 手动执行授权 | ✅ 已选 nezha 方案 |
| 通知渠道 | komari | 渠道更多（bark/telegram/email/js） | ✅ 已扩展 6 渠道 |
| 前端框架 | dash-v2 | React 19 + Tailwind v4 + Recharts | ✅ 已选 dash-v2 方案 |
| 认证 | nezha | JWT + PAT scope + OAuth2，权限更细 | ✅ 已实现 JWT + PAT + OAuth2 + 2FA |

### 4.2 差异功能（仅一项目有，全量保留）

| 功能 | 来源 | Argus 处理 |
|---|---|---|
| 服务监控（HTTP/TCP/Ping） | nezha | ✅ 已实现（核心差异化） |
| 文件管理器 | nezha | ✅ 已实现 |
| DDNS | nezha | ✅ API 与管理界面已实现（仅 webhook/Cloudflare） |
| NAT 内网穿透 | nezha | ⚠️ API 与管理界面已实现；仅简化 HTTP Host 代理/TCP 隧道 |
| 服务器过户 | nezha | ✅ API 与管理界面已实现（新密钥重连验证） |
| PAT 细粒度权限 | nezha | ✅ 已实现 |
| 多用户 | nezha | ✅ 已实现（本次补密钥读取链路） |
| MCP 自动化 | nezha | ✅ 已实现 |
| 全球地图 | dash-v2 | ✅ 已实现（依赖 GeoIP；无 provider/无定位结果时隐藏） |
| 服务可用率条 | dash-v2 | ✅ 已实现 |
| 命令面板 | dash-v2 | ✅ 已实现 |
| GeoIP | nezha | ⚠️ 可选 HTTP provider，默认关闭；地图无定位数据时安全隐藏 |
| 插件系统 | komari | ⚠️ 简化版（goja + 本地市场/管理 UI/权限审批），仅适合受信插件且无完整宿主扩展点 |
| 主题市场 | komari | 📋 待实现（内置主题 + 自定义代码） |
| 计费/续费 | komari | ✅ 字段 + 到期提醒（界面待补） |
| 多 GeoIP provider | komari | 📋 待实现 |

### 4.3 需要融合改造的重复模块

| 模块 | 融合方案 | 状态 |
|---|---|---|
| 报警规则 | nezha 状态机骨架 + komari 渠道/离线通知/流量告警，统一为 Argus 报警引擎 | ✅ 已融合 |
| 通知渠道 | komari 多渠道实现 + nezha 分组/模板，统一为 Argus 通知中心 | ✅ 已融合 |
| 服务监控 | nezha ServiceSentinel（HTTP/TCP/Ping）+ komari PingTask，统一为 Argus 服务监控 | ✅ 已融合 |
| 服务器管理 | nezha 多用户/过户/隐藏 + komari 计费字段，统一为 Argus 服务器台账 | ✅ 已融合 |
| 前端总览 | dash-v2 卡片/地图/服务条 + komari 节点隐藏，统一为 Argus 用户前台 | ✅ 已融合 |
