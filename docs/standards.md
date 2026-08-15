# 整合项目规范统一清单

> 依据 https://www.komari.wiki 与 https://nezha.wiki 全部文档整理。
> 本清单定义 Argus 整合版需遵循的统一设计与开发标准。

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
