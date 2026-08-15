# 原始项目差异对比报告

> 基于本地独立部署的三个原始项目实测 + 全量源码盘点 + 官方文档研读。
> 部署环境：komari :25774（安装向导）、nezha-master :8008（Dashboard）、
> nezha-dash-v2 :8010（生产包 + Mock API，30 台模拟服务器，浏览器实测）。

## 一、部署实测记录

| 项目 | 部署方式 | 运行结果 | 前端 UI |
|---|---|---|---|
| komari-main | go build 单二进制 | ✅ :25774 安装向导可达 | ⚠️ zip 不含前端产物（主题 dist 需从 komari-web 仓库构建），页面为占位 |
| nezha-master | go build 单二进制 | ✅ :8008 Dashboard 启动 | ⚠️ zip 不含前端产物（user-dist/admin-dist 需从 release 下载，GitHub 直连不可达），页面为占位 |
| nezha-dash-v2 | pnpm build 生产包 + Mock API 同源托管 | ✅ :8010 完整 UI 实测通过 | ✅ 完整现代仪表盘 |

**关键结论**：两个 Go 项目的 zip 均为**纯后端源码**，前端产物依赖外部仓库/release（当前网络无法获取），因此：
- 后端功能对比：以源码盘点 + 官方文档为准（完整可靠）
- 前端视觉/交互对比：以 dash-v2 为唯一完整基准（浏览器实测）

## 二、功能完整性对比

| 功能域 | komari | nezha-master | dash-v2 | 对比结论 |
|---|---|---|---|---|
| Agent 上报协议 | ✅ JSON-RPC 2.0 over WS/HTTP | ✅ gRPC 双向流 | — | 两套协议，komari 轻量免 protobuf |
| 实时监控指标 | ✅ CPU/内存/磁盘/网络/GPU | ✅ 同左 + 温度 | ✅ 展示 | 采集项基本对齐 |
| 历史指标存储 | ✅ 分层 rollup + t-digest 百分比 | ✅ TSDB（VM 嵌入，可关闭回退 SQLite） | ✅ 图表消费 | komari 存储工程更完整，nezha 依赖外部 TSDB |
| 服务监控 HTTP/TCP/Ping | ❌（仅 Ping 任务） | ✅ ServiceSentinel 完整 | ✅ 展示 + 可用率条 | **nezha 独占** |
| 报警规则 | ✅ 离线/负载/流量三类通知 | ✅ 阈值状态机 + 周期流量 + 触发任务 | — | 两套规则引擎，nezha 更严谨 |
| 通知渠道 | ✅ bark/telegram/webhook/email/serverchan/JS 脚本 | ✅ Webhook（JSON/Form 模板） | — | **komari 渠道更多** |
| 定时任务 | ✅ cron 下发 | ✅ cron + 触发任务 + 手动授权 | — | nezha 多触发任务 |
| 网页终端 | ✅ WS 隧道 | ✅ gRPC IOStream 隧道 | — | 都支持 |
| 文件管理器 | ❌ | ✅ FsList/Read/Write/Delete | — | **nezha 独占** |
| 多用户 | ⚠️ 用户 + 会话管理 | ✅ admin/user 两级 + 过户 | — | **nezha 多租户完整** |
| PAT 细粒度权限 | ⚠️ API Key（Bearer） | ✅ scope + 白名单 + 吊销 | — | **nezha 独占** |
| OAuth2/2FA | ✅ GitHub/QQ/OIDC + TOTP 2FA | ✅ 多 provider + 会话 | — | komari 多 2FA |
| DDNS | ❌ | ✅ Cloudflare/腾讯/HE/webhook | — | **nezha 独占** |
| NAT 内网穿透 | ❌ | ✅ 域名→内网隧道 | — | **nezha 独占** |
| GeoIP | ✅ mmdb + 3 在线 provider | ✅ 内嵌 mmdb | ✅ 地图聚合 | komari provider 可插拔 |
| 插件系统 | ✅ goja JS 沙箱 + 市场 | ❌ | — | **komari 独占** |
| 主题系统 | ✅ 主题包 + 市场 | ⚠️ 模板覆盖 | ✅ 明暗主题 | komari 生态完整 |
| MCP/AI 接入 | ❌ | ✅ /mcp + server.exec/fs 工具 | — | **nezha 独占** |
| 服务器计费 | ✅ 价格/周期/续费 | ❌ | ✅ 展示 | komari 独占 |
| 剪贴板 | ✅ CloudClipboard | ❌ | — | komari 独占 |
| 数据库工具 | ✅ VACUUM/受限 SQL/跨库迁移 | ❌ | — | komari 独占 |
| 备份/恢复 | ✅ 打包下载/上传恢复/迁移向导 | ⚠️ 无备份接口 | — | komari 完整 |
| 审计日志 | ✅ 分页查询 | ✅ MCP 审计 | — | 都有 |

## 三、UI 交互逻辑对比（dash-v2 实测基准）

| 维度 | dash-v2（实测） | Argus 现状 | 差异 |
|---|---|---|---|
| 总览布局 | 4 统计卡（总数/在线/离线/流量）+ 服务器卡片墙 | 无统计卡，直接卡片网格 | 缺统计卡 |
| 分组 Tab | 胶囊滑动指示器 + 横向滚动 | 下拉选择分组 | Tab 交互更佳 |
| 排序 | 11 种指标 + 升降序 + 离线沉底 | 无排序 | 缺 |
| 搜索 | Cmd+K 命令面板（服务器跳转+主题切换） | 顶部搜索框 | 命令面板更高级 |
| 在线状态 | 统计卡点击联动过滤 | 卡片圆点 | 交互联动不足 |
| 详情页 | 系统信息/负载/GPU/温度 + Detail/Network 双 Tab | 8 宫格指标 + 4 图表 | 缺 GPU/温度/Tab 结构 |
| 图表 | 实时+历史拼接、Realtime/1D/7D/30D、削峰 | 1h/24h/7d 静态拉取 | 缺实时拼接与削峰 |
| 地图 | d3-geo 全球地图 + 国家聚合 | 无 | 缺 |
| 服务可用率 | 30 天色块条 + 阈值着色 | 无 | 缺 |
| 主题 | light/dark/system 三态 + 防 FOUC | light/dark | 缺 system 跟随 |
| 语言 | 14 语言 i18n | 仅中文 | 缺 i18n |
| 时钟 | 实时时钟 AnimatedCount | 无 | 缺 |
| 空态/错误态 | 空态插画 + 后端不可达提示 | 简单文本 | 缺插画 |
| 移动端 | 响应式网格 | 响应式网格 | 基本对齐 |

## 四、技术栈架构对比

| 维度 | komari | nezha-master | dash-v2 | Argus 选型 |
|---|---|---|---|---|
| 后端语言 | Go 1.25 | Go 1.26 | — | Go 1.26 |
| Web 框架 | Gin | Gin + gRPC 同端口复用 | — | Gin |
| Agent 协议 | JSON-RPC 2.0（WS/HTTP） | gRPC 双向流 | — | JSON-RPC 2.0（komari 方案） |
| ORM | GORM | GORM | — | GORM |
| 主存储 | SQLite（GORM） | SQLite（koanf 配置） | — | SQLite |
| 指标存储 | 独立指标库（SQLite/MySQL/PG 三方言） | VictoriaMetrics 嵌入（可选） | — | SQLite 分钟级（待升级 komari rollup） |
| API 风格 | REST + RPC2 声明式桥 | REST + Swagger | REST 消费 | REST（nezha 风格 /api/v1） |
| 前端 | 内嵌主题（外部仓库） | 内嵌双 dist（外部 release） | React 19 + Vite 8 | React 19 + Vite 6（dash-v2 方案） |
| 前端 UI 库 | — | — | shadcn/ui + Tailwind v4 | shadcn 风格（自实现） |
| 图表 | — | — | Recharts 3 | Recharts 2 |
| 状态管理 | — | — | React Query + Context | React Query + Context |
| 实时推送 | WS + 事件拉取 | WS singleflight 合并 | WS 消费 | WS（nezha 方案） |
| 构建 | zig 交叉编译 | goreleaser-cross | pnpm + vite | make + pnpm + go build |

## 五、数据流向对比

```
komari:  Agent ──WS/HTTP JSON-RPC──▶ web/api(client) ──▶ metricstore(批处理) ──▶ 指标库(SQLite/MySQL/PG)
          │                                                          │
          └──▶ 事件队列(v2_events) ◀── admin:exec/terminal ◀── 管理后台
  前台:   浏览器 ──WS /api/clients──▶ 实时节点数据 ◀── 内存 presence + 环形缓存

nezha:   Agent ──gRPC 双向流──▶ service/rpc(NezhaService) ──▶ ServerClass(内存) + TSDB(可选)
          │                                                          │
          └──▶ RequestTask 流 ◀── CronClass/AlertSentinel/NAT ◀── 管理后台
  前台:   浏览器 ──WS /api/v1/ws/server──▶ singleflight 合并投影 ──▶ 卡片墙
  用户端: 浏览器 ──REST /api/v1──▶ filter() 权限过滤（owner/PAT 白名单）

dash-v2: 浏览器 ──WS /api/v1/ws/server──▶ websocket-provider(30 条缓冲) ──▶ UI
         浏览器 ──REST /api/v1──▶ TanStack Query ──▶ 图表（历史+实时拼接）
```

**差异结论**：
1. komari 数据流：上报直接写双库（业务/指标分离），前端靠 WS 事件拉取
2. nezha 数据流：上报写内存（DB 只持久化），推送靠 singleflight 合并；任务/终端/NAT 全部复用 gRPC 流
3. dash-v2 数据流：纯消费端，WS 缓冲 + Query 缓存双通道，无状态管理库

## 六、整合改造依据

1. **协议层**：采用 komari 的 JSON-RPC 2.0（免 protobuf、浏览器友好），保留 nezha 的单连接复用思想（report/exec/terminal 同一条 WS）
2. **存储层**：主库 SQLite（komari 双库思路）+ 指标分钟级降采样（已实现，待升级 komari rollup 阶梯）
3. **API 层**：nezha 的 /api/v1 REST 风格 + 统一响应壳 + 分页；权限借鉴 nezha PAT scope + komari 角色中间件
4. **前端**：以 dash-v2 为视觉基准补齐统计卡/排序/命令面板/地图/可用率条/详情 Tab；后台补齐 komari/nezha 管理能力
5. **差异功能全量保留**：服务监控/文件管理/DDNS/NAT/多用户/PAT/过户（nezha）+ 通知渠道/2FA/插件/主题/计费（komari）

详见 docs/assets.md（可迁移资产清单）与 docs/standards.md（规范统一清单）。
