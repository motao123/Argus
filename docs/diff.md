# 原始项目差异对比报告

> 基于本地独立部署的三个原始项目实测 + 全量源码盘点 + 官方文档研读。
> 最近复跑：2026-08-16（komari 源码 :25775 完整安装、nezha-master 源码 :8011、dash-v2 独立演示 :5173 + mock :8008）。

## 一、部署实测记录（2026-08-16 复跑）

| 项目 | 部署方式 | 运行结果 | 前端 UI |
|---|---|---|---|
| komari-main | go build 单二进制（komari-web npm 构建主题，嵌入 web/public/defaultTheme） | ✅ :25775 安装向导 5 步完成（admin/Argus123、站点、SQLite DSN）→ 主界面 → 管理后台全菜单（浏览器实测） | ✅ 完整（顶栏/统计/搜索/主题市场入口） |
| nezha-master | go build 单二进制（admin-frontend v2.3.3 dist + dash-v2 源码构建 user-dist；swag 生成 docs） | ✅ :8011 Dashboard（新版 listen_port 配置 schema） | ✅ dash-v2 用户端 + admin 登录 JWT |
| nezha-dash-v2 | pnpm build 生产包 + dev:mock（mock-nezha-server :8008 + vite :5173 https） | ✅ :5173 页面 + mock API 200 | ✅ 完整现代仪表盘（哪吒监控） |

**说明**：komari/nezha 的 zip 均为纯后端源码，前端需从 komari-web 仓库构建 / release dist 下载；
nezha 新版（master）配置为 `listen_port`/`listen_host` + `oauth2` map 结构，与 2.3.6 版 `httpport` 不同。

## 二、功能完整性对比

| 功能域 | komari | nezha-master | dash-v2 | 对比结论 |
|---|---|---|---|---|
| Agent 上报协议 | ✅ JSON-RPC 2.0 over WS/HTTP | ✅ gRPC 双向流 | — | 两套协议，komari 轻量免 protobuf |
| 实时监控指标 | ✅ CPU/内存/磁盘/网络/GPU | ✅ 同左 + 温度 | ✅ 展示 | 采集项基本对齐 |
| 历史指标存储 | ✅ 分层 rollup + t-digest 百分比 | ✅ TSDB（VM 嵌入，可关闭回退 SQLite） | ✅ 图表消费 | komari 存储工程更完整，nezha 依赖外部 TSDB |
| 服务监控 HTTP/TCP/Ping | ❌（仅 Ping 任务） | ✅ ServiceSentinel 完整 | ✅ 展示 + 可用率条 | **nezha 独占**（Argus 已实现 HTTP/TCP/Ping/Command） |
| 报警规则 | ✅ 离线/负载/流量三类通知 | ✅ 阈值状态机 + 周期流量 + 触发任务 | — | 两套规则引擎，nezha 更严谨 |
| 通知渠道 | ✅ bark/telegram/webhook/email/serverchan/JS 脚本 | ✅ Webhook（JSON/Form 模板） | — | **komari 渠道更多** |
| 定时任务 | ✅ cron 下发 | ✅ cron + 触发任务 + 手动授权 | — | nezha 多触发任务 |
| 网页终端 | ✅ WS 隧道 | ✅ gRPC IOStream 隧道 | — | 都支持 |
| 文件管理器 | ❌ | ✅ FsList/Read/Write/Delete | — | **nezha 独占** |
| 多用户 | ⚠️ 用户 + 会话管理 | ✅ admin/user 两级 + 过户 | — | **nezha 多租户完整** |
| PAT 细粒度权限 | ⚠️ API Key（Bearer） | ✅ scope + 白名单 + 吊销 | — | **nezha 独占** |
| OAuth2/2FA | ✅ GitHub/QQ/OIDC + TOTP 2FA | ✅ 多 provider + 会话 | — | komari 多 2FA |
| DDNS | ❌ | ✅ Cloudflare/腾讯/HE/webhook | — | **nezha 独占**（Argus 已实现四类 provider） |
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
| 总览布局 | 4 统计卡（总数/在线/离线/流量）+ 服务器卡片墙 | 统计卡 + 服务状态条 + 卡片墙 | ✅ 已对齐 |
| 分组 Tab | 胶囊滑动指示器 + 横向滚动 | 分组下拉 + 分组 Tab（前台） | ✅ 已对齐 |
| 排序 | 11 种指标 + 升降序 + 离线沉底 | 9 种排序 + 升降序 | ✅ 已对齐 |
| 搜索 | Cmd+K 命令面板（服务器跳转+主题切换） | 搜索框 + ⌘K 命令面板 | ✅ 已对齐 |
| 在线状态 | 统计卡点击联动过滤 | 统计卡筛选 + 卡片在线徽标 | ✅ 已对齐 |
| 详情页 | 系统信息/负载/GPU/温度 + Detail/Network 双 Tab | 8 宫格指标 + 图表 + 终端入口 | ⚠️ 缺 GPU/温度显示 |
| 图表 | 实时+历史拼接、Realtime/1D/7D/30D、削峰 | 1h/24h/7d + 实时拼接（B3） | ✅ 已对齐 |
| 地图 | d3-geo 全球地图 + 国家聚合 | 世界地图（国家筛选/折叠） | ✅ 已对齐 |
| 服务可用率 | 30 天色块条 + 阈值着色 | 30 天色块（B3） | ✅ 已对齐 |
| 主题 | light/dark/system 三态 + 防 FOUC | light/dark + 防 FOUC | ⚠️ 缺 system 跟随（P5 已补 system 三态） |
| 语言 | 14 语言 i18n | 中/英（EN 按钮） | ⚠️ 未全量 i18n |
| 时钟 | 实时时钟 AnimatedCount | 实时时钟 | ✅ 已对齐 |
| 空态/错误态 | 空态插画 + 后端不可达提示 | 空态文本 | ⚠️ 缺插画（后端不可达 banner 已补） |
| 移动端 | 响应式网格 | 响应式网格 | ✅ 基本对齐 |

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

> **整合进展（2026-08-16）**：上述 1-5 项已全部落地（A1-A6 / B1-B6 / 第1-7项 迭代完成）；
> 本轮复跑新增「在线会话」「站点设置」管理页与 Agent 密钥读取链路。未覆盖项见
> docs/assets.md（⚠️/📋 标记）与 docs/verification.md「已知限制」。

详见 docs/assets.md（可迁移资产清单）与 docs/standards.md（规范统一清单）。
