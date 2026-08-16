# 整合项目规范统一清单

> 依据 https://www.komari.wiki 与 https://nezha.wiki 全部文档整理。
> 本清单定义 Argus 整合版需遵循的统一设计与开发标准。
> 研读日期：2026-08-16（komari.wiki 全站 37 个中文页面 + nezha.wiki 全站 55 个中文页面，逐页抓取整理；研读要点见文末附录）。

## 一、后端接口标准

### 1. 路由分层（统一为 /api/v1）
| 层 | 路径前缀 | 认证 | 说明 |
|---|---|---|---|
| 读接口 | `/api/v1/servers\|services\|metrics\|history` | optionalAuth | 游客可读公开视图（借鉴 nezha optionalAuth） |
| 写接口 | `/api/v1/*`（POST/PUT/DELETE） | JWT/PAT 强制 | 游客一律 401 |
| 登录 | `/api/v1/auth/login` | 无 | JWT 登录 |
| Agent | `/ws/agent` | Secret | Agent 注册/上报/exec/terminal/service/fs |
| 实时 | `/api/v1/ws` | optionalAuth | 游客可连（公开快照，借鉴 komari 公开节点列表） |
| 终端 | `/api/v1/terminal/:id` | JWT 强制 | 敏感操作需登录 |

### 2. 响应格式（统一 JSON 信封）
```json
// 成功
{ "success": true, "data": { ... }, "pagination": { "offset": 0, "limit": 50, "total": 100 } }
// 失败
{ "success": false, "error": "错误描述" }
```
- 分页参数：`offset` + `limit`（默认 50，上限 500）
- 错误码：400 参数错误 / 401 未认证或 Token 失效 / 403 权限不足（缺 scope 或不在白名单）/ 404 不存在 / 409 冲突（如服务器离线）

### 3. 认证与授权（融合两套体系）
- **JWT 会话**：`Authorization: Bearer <token>`，30 天过期，密钥自动生成持久化（借鉴 komari 的 jwt 落盘 + nezha 的 jwt_secret_key）
- **PAT（个人访问令牌）**：明文 `argus_` 前缀 + 哈希存储，创建时仅显示一次，scope 格式 `argus:{resource}:{verb}`（资源：server/service/alert/cron/notification/admin/inventory；动作：read/write/delete/exec），支持 server_ids 白名单（借鉴 nezha PAT 设计）
- **用户角色**：
  - **admin**：全部资源 + 设置修改 + 用户创建
  - **user**：仅自己名下服务器（通过 agent secret 关联）+ 自己的 PAT
  - **guest**：只读公开数据，受 `force_auth` 与「对游客隐藏」限制
- **敏感字段脱敏**：通知渠道不回显 url/headers/body，Agent secret 仅创建时返回一次（借鉴 nezha）

### 4. 定时任务规范
- cron 表达式 5 段（分 时 日 月 周）
- 触发任务：报警失败/恢复联动（借鉴 nezha）
- 执行结果截断保存（≤2KB），记录最近执行时间

## 二、前端开发指引

### 1. 页面路由（前台公开 + 后台登录）
```
/                     前台总览（公开）：顶栏布局 + 统计卡 + 服务状态条 + 卡片墙
/server/:id           前台详情（公开）：实时指标 + 历史图表（终端入口按登录态显示）
/login                登录
/admin/overview       后台总览（登录后）
/admin/servers|alerts|crons|services|files|access   后台管理
/admin/terminal/:id   网页终端（登录后）
```
- 前台布局：顶栏式（Logo/实时时钟/在线徽标/主题/登录按钮/页脚），借鉴 komari 前台
- 后台布局：侧边栏式（导航 + 在线统计 + 主题 + 前台入口），登录保护
- SPA 路由回退：服务端未匹配路径返回 index.html（借鉴 komari 主题 SPA 规范）
- `/admin` 与 `/terminal` 保留路径，不与主题路由冲突

### 2. UI 设计系统（统一 token）
| 类别 | 规范 |
|---|---|
| 色彩 | light/dark 双主题，CSS 变量驱动：`--bg` `--panel` `--border-c` `--fg` `--muted` `--accent`(主色 #6366f1) `--ok`/`--warn`/`--err` 三态色 |
| 字体 | Inter + 中文回退（PingFang SC / Microsoft YaHei），数字用 `tabular-nums` 等宽防抖动 |
| 组件 | 语义化 class 前缀 `argus-`（借鉴 komari 的 `km-` 规范），布局组件 `argus-layout`/`argus-main`，页面级 `argus-page-<route>` |
| 图标 | lucide-react 统一图标库 |
| 图表 | Recharts（线/面积图），颜色取 CSS 变量随主题变化，数值格式化统一走 `lib/format.ts` |
| 动效 | 进度条/数字滚动过渡 ≤700ms，禁用不必要的循环动画 |
| 主题切换 | localStorage `argus-theme`，挂载前内联脚本应用防 FOUC（借鉴 dash-v2） |
| 语言 | 预留 i18n 接入点，默认 zh-CN，本地存储字段 `language`（借鉴 komari） |

### 3. 实时数据约定
- WS 推送周期 2s（可配置），消息格式 `{"type":"snapshot","servers":[...]}`
- 前端重连策略：指数退避（1s → 2s → 4s → … 上限 30s）
- 图表：实时曲线用 WS 历史缓冲 + 历史数据拼接（借鉴 dash-v2 use-chart-history）

## 三、权限管控体系（打通两项目）

| 资源 | admin | user | guest | PAT scope |
|---|---|---|---|---|
| 服务器（inventory） | 全部 CRUD | 自己名下 | 只读（受隐藏限制） | `argus:server:*` |
| 服务器运行态（exec/terminal） | 全部 | 自己名下 | 禁止 | `argus:server:exec` |
| 服务监控 | 全部 | 自己名下 | 只读 | `argus:service:*` |
| 报警规则 | 全部 | 自己名下 | 禁止 | `argus:alert:*` |
| 定时任务 | 全部 | 自己名下 | 禁止 | `argus:cron:*` |
| 通知渠道 | 全部 | 禁止 | 禁止 | `argus:notification:*` |
| 设置/用户 | 仅 admin | 禁止 | 禁止 | `argus:admin:*`（仅 admin 可签发） |

- 删除用户 → 其 agent secret 立即失效，关联服务器联动删除（借鉴 nezha）
- 管理员代配 Agent 需通过管理接口读取对应用户 agent_secret（借鉴 nezha）

## 四、文档编写规范

- README：项目定位 + 架构图 + 三种部署方式 + 功能清单 + 协议表
- docs/：设计文档（design.md）、资产清单（assets.md）、差异对比（diff.md）、本清单（standards.md）
- 提交信息：`feat|fix|chore|docs|refactor: 中文描述`，单次提交聚焦一个主题
- API 变更同步更新 docs/standards.md

## 附录：两站点研读要点（2026-08-16 全站抓取）

### A. komari.wiki 研读要点（37 页，含 dev/ 开发指南全目录）

**UI 设计规范**
- 主题 = ZIP 包（根目录 `komari-theme.json` + `dist/` 构建产物）；`short` 为唯一标识（字母/数字/下划线/连字符，禁止 `default`）
- 明暗主题 localStorage 字段 `appearance`（light/dark/system），i18n 字段 `language`（如 zh-CN）
- 语义化类名 `km-` 前缀：页面根 `km-page-<路由>`、布局 `km-layout/km-main/km-navbar/km-footer/km-admin-layout`、组件 `km-<组件名>`、UI 基件 `km-ui-button/km-ui-input/km-ui-table`（稳定 DOM 钩子，供注入脚本使用）
- `dist/index.html` 必须含占位符 `<title>Komari Monitor</title>` 与 description meta，服务端替换注入（`</head>` 前注入 head、`</body>` 前注入 body）
- SPA 回退：服务端未匹配路径返回 index.html
- `/admin` 与 `/terminal` 为系统内置界面禁区，主题不得占用；页脚必须保留 "Powered by Komari Monitor."
- 主题动态配置三类型：`managed`（自动生成表单）/ `raw`（HTML iframe）/ `redirect`（站内跳转，拒绝绝对 URL 与 `..`）；`theme_settings` 公开可读，禁止放密钥
- 配置项类型：title/textbox/string/number/select/switch/richtext/nodes/pingtasks

**前端开发指引**
- komari-web 为 React 仓库；后端 go-embed 默认主题；构建：`npm install && npm run build` 后拷入 `web/public/defaultTheme/dist`
- 实时数据：WebSocket `/api/clients`，发送 `"get"` / `"get <uuid>"`；RPC2 端点 `/api/rpc2`（JSON-RPC 2.0，官方推荐优先）
- **结构约定**：实时/WS 数据为嵌套对象（`cpu.usage`），历史记录接口为扁平结构（`cpu`）
- Agent 通道：WS `GET /api/clients/v2/rpc?token=`（首选）→ POST 回退，ws_connecting/ws_active/post_fallback/recovering_ws 状态机
- 时间一律 RFC3339 带时区；单位一律字节；日报/周报/cron 按服务器操作系统时区
- 上报频率：基础信息 5–30 分钟，实时监控 5–8 秒；POST 保活 11 秒

**后端接口标准**
- REST 统一 `{status: "success", message, data}` 三层；优先 RPC2
- 认证：Cookie `session_token`（HttpOnly/SameSite=Lax）+ API Key（Bearer）；`/api/admin` 前缀需认证
- RPC2 方法命名空间：`rpc.`（保留）/ `common:` / `public:` / `admin:` / `client:` / `plugin:`
- 兼容性纪律：1.3.0/1.4.0 分阶段移除旧接口（v1 Agent 协议、`/api/records/*`、`/api/clients` 等）

**权限管控体系**
- principal 四类：agent/user/api_key/anonymous；`server.call` 以管理员身份执行
- 脱敏：`GET /api/nodes` 固定清空 token/ipv4/ipv6/remark/version；私有备注对游客不可见
- `private_site` 私有站点模式；`disable_password_login` 强制 SSO；运维逃生门 `chpasswd` / `disable-2fa` / `permit-login`
- 插件权限声明：不声明即不授予（allowSystemRPC/allowRoutes/allowHooks/allowHTMLInject 等触发批准流程）
- 安装包校验：≤10,000 文件、单文件 ≤128 MiB、总量 ≤512 MiB、拒绝路径穿越；市场源禁内网 IP + SHA-256 校验

### B. nezha.wiki 研读要点（55 页，含 guide/ configuration/ developer/ 全目录）

**UI 设计规范**
- 主题模板化：`user_template` / `admin_template` 配置项指定内置主题
- 自定义代码两处注入点：`custom_code`（用户前端）、`custom_code_dashboard`（管理前端），作用域严格分离
- 用户前端全局变量 `window.` 前缀约定（ForceShowServices/ForceShowMap/ForceCardInline/FixedTopServerName/ForceUseSvgFlag/DisableAnimatedMan/CustomIllustration/CustomBackgroundImage/CustomLogo/CustomLinks/ShowNetTransfer/ForcePeakCutEnabled 等）
- 布局：顶部统计（总数/在线/离线/总流量/实时速率）+ 可筛选卡片 + 地图 + 服务监控面板；详情页 Detail/Network 双 Tab
- 排序 11 种指标 + 升降序 + 在线优先；分组 Tab 存浏览器会话
- i18n：YAML 下划线（zh_CN）↔ 界面连字符（zh-CN）；通过 Hosted Weblate 协作
- 终端 Ctrl+Shift+V 粘贴；并发流上限（同用户 20 条 IOStream、同服务器 40 条）

**前端开发指引**
- 两个独立前端构建产物：`dashboard/admin-dist`（管理端）+ `dashboard/user-dist`（用户端）
- WS 路径：`/api/v1/ws/server`（实时状态流）、`/api/v1/ws/terminal/{id}`、`/api/v1/ws/file/{id}`、`/api/v1/ws/transfer`；Bearer 认证；反代需配置 Upgrade/Connection 头
- 历史数据依赖 TSDB；未启用时游客仅可看实时 + 1d；7d/30d 需登录
- 开发工具链：`script/bootstrap.sh` 生成 Swagger；`debug: true` 挂载 Swagger UI
- 用户偏好存 localStorage；公开备注缓存 sessionStorage

**后端接口标准**
- REST `/api/v1`（v1 是路由版本）；成功 `{success: true, data: {...}}`，失败 `{success: false, error: "..."}`；分页 `data.pagination: {offset, limit, total}`
- 认证：JWT（`POST /api/v1/login`）+ PAT（明文 `nzp_` 前缀，只存哈希）；统一 `Authorization: Bearer`
- 错误码：401（无效/过期 Token）、403（缺 scope/白名单外）、405、429（MCP 限流）
- 批量操作：`batch-delete/{resource}`；部分更新 PATCH；动作 `/{id}/action`（如 `cron/{id}/manual`）
- 历史查询：`period=1d|7d|30d`；`metric` 必填（cpu/memory/swap/disk/net_in_speed/.../gpu）
- MCP 入口：`POST /mcp`（仅 PAT），JSON-RPC 2.0，8 MiB 上限，限流 10 次/秒
- 脱敏：通知 url/request_header/request_body、DDNS access_secret/webhook_headers 读取不回显
- 版本概念独立：产品代际 V2 / Dashboard 版本 / Agent 版本 / API 路径版本互不绑定

**权限管控体系**
- 两级角色：管理员（全部 + 设置）/ 普通用户（仅名下资源）
- 每用户独立 Agent 连接密钥；删除用户 → 密钥立即失效 + 关联服务器联动删除；Agent 唯一 UUID
- PAT：`nezha:{resource}:{verb}`，资源 server/service/alertrule/cron/ddns/nat/notification/notification-group/transfer/admin/inventory；动作 read/write/delete/exec；通配 `nezha:admin:*`、`nezha:*`（仅管理员可签发）
- PAT 只收窄不放大；白名单 server_ids 建议必填；`inventory` 与 `server` 资源域分离
- 个人账户接口禁用 PAT（refresh-token/profile/oauth2 unbind/api-tokens）
- OAuth2 为本地账号绑定第三方（非独立账号体系），回调 `/api/v1/oauth2/callback`
- `force_auth` 游客访问控制；服务器/服务监控各自有"对游客隐藏"开关
- WAF：按资源区分封禁标识（用户 ID/连接密钥爆破/API Token 爆破/登录不存在用户/手动封禁），计数越多封禁越长
- IP 默认脱敏（`1.**.1`）；`enable_plain_ip_in_notification` 默认 false
- JWT 密钥生产用环境变量注入；jwt_timeout 默认 1 小时；密码 bcrypt + 最低 8 位
- Agent 能力裁剪：disable_command_execute / disable_force_update / disable_nat / disable_send_query / disable_auto_update
- 最小权限原则：能读不给写，能写不给 exec；自动化账号与管理员分离

**文档编写规范**
- nezha.wiki 四章节：guide/（使用指南）、configuration/（配置指南）、case/（社区项目）、developer/（开发手册）
- 写作风格：TIP/WARNING/DANGER/INFO 提示框、页尾"相关文档"交叉链接、配置项条目化（说明+默认值+示例）、代码块标注语言
- 旧版本页面明确标注"不适用于当前 V2"；对比类页面只用官方资料
- komari.wiki 结构：install/（安装）、dev/（开发指南，含 plugin/ 子目录）、faq/、community/；中文为根语言，英文 `/en/` 镜像；文件命名 kebab-case
- komari 写作风格：TLDR 先行 + 步骤化教程 + 字段表（类型/必填/默认值/说明）+ ASCII 架构图
