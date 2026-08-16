# Argus 整合版部署验证报告

> 最近验证日期：2026-08-16（第2轮全量验证，随「7 步整合任务」复跑）
> 本地环境：Deepin 25 / Go 1.26.6 / Node 22 / pnpm 11 / SQLite（WAL）
> 验证方式：单测 + go vet + API 冒烟 + 真实 Agent 端到端 + 浏览器可视化实测

## 一、部署形态（2026-08-16）

| 组件 | 部署方式 | 地址 | 状态 |
|---|---|---|---|
| Argus Server（单二进制内嵌前端） | go build → 直接运行 | http://127.0.0.1:8080 | ✅ 运行中 |
| Argus Agent | go build → 直接运行 | ws://127.0.0.1:8080/ws/agent | ✅ 已注册 #1（本地测试机） |
| 数据库 | SQLite（WAL） | run/data | ✅ |

## 二、后端回归（2026-08-16）

| 检查项 | 结果 |
|---|---|
| `go test ./...`（alert/api/store 含单测包） | ✅ 全绿（含新增 TestGetUserSecret） |
| `go vet ./...` | ✅ 干净 |
| `gofmt -l` | ✅ 0 文件待格式化（本轮统一格式化） |
| 登录 admin/argus123 → JWT | ✅ |
| GET /api/v1/sessions（会话列表） | ✅ 返回 JSON |
| GET /api/v1/public/settings（游客） | ✅ 200 |
| GET /api/v1/servers（游客，force_auth=off） | ✅ 200 空列表 |
| POST /api/v1/servers（游客写） | ✅ 401 |
| GET /api/v1/alerts（游客） | ✅ 401 |
| WS /api/v1/ws（游客升级） | ✅ 101 |
| 设置保存/读取往返（site_desc/term_font_size） | ✅ |
| 会话踢出（DELETE /sessions/:id） | ✅ JTI 即时吊销 |

## 三、真实 Agent 端到端（2026-08-16）

| 链路 | 结果 |
|---|---|
| 创建服务器 → 返回一次性密钥 | ✅ |
| Agent `-k <密钥>` 注册 | ✅ authenticated as server #1 |
| 实时上报（浏览器前台卡片） | ✅ 在线 1/1，CPU/内存/磁盘/网络/负载实时刷新 |
| 远程执行 `echo hello-argus; uname -s` | ✅ 输出 `hello-argus\nLinux`，exit 0 |
| 会话追踪 | ✅ 浏览器会话 + curl 会话均在列 |

## 四、本轮修复与新增（2026-08-16）

1. **修复 Agent 密钥链路缺陷**：
   - 初始 admin 用户此前创建时无 AgentSecret，且无接口可读取已有用户密钥
   - 修复：db.Init 引导 admin 即生成密钥 + 存量用户空密钥自动回填 + 新增 `GET /api/v1/users/:id/secret`（仅 admin）
   - 前端访问控制页新增「查看 Agent 密钥」按钮（含复制），单测 TestGetUserSecret 覆盖
2. **新增「在线会话」管理页**（/admin/sessions）：会话列表（用户/IP/设备/登录/过期）+ 踢出，10s 自动刷新
3. **新增「站点设置」管理页**（/admin/settings）：站点名/描述/favicon、私有站点模式（force_auth）、终端字号/主题（xterm.js 外观）
4. **全仓库 gofmt 统一格式化**

## 五、浏览器实测（2026-08-16，截图存 docs/screenshots/）

| 页面 | 验证内容 | 结果 |
|---|---|---|
| 前台总览 | 实时时钟/在线徽标/统计卡/筛选/9 种排序/搜索/实时卡片（本地测试机在线） | ✅ |
| 后台登录 | admin 登录 → 会话建立 | ✅ |
| 后台总览 | 侧边栏 9 项导航 + 统计卡 + 实时数据 | ✅ |
| 访问控制 | PAT 列表 / 用户管理 / 新「查看 Agent 密钥」展示与复制 | ✅ |
| 在线会话 | 会话表格 + 踢出按钮 | ✅ |
| 站点设置 | 6 项设置表单 → 保存 → 前台顶栏/页脚站点名即时生效 | ✅ |

## 六、参考项目部署对照（2026-08-16）

| 项目 | 形态 | 验证 |
|---|---|---|
| komari（源码构建） | :25775，安装向导 5 步完成（admin/站点/DSN）→ 主界面 → 管理后台全菜单 | ✅ 浏览器实测 |
| nezha-master（源码构建） | :8011（listen_port 8011，新版配置 schema），admin/admin 登录 JWT | ✅ 登录 API 实测 |
| nezha-dash-v2 | 内嵌于 nezha user-dist + 独立 :5173（https）演示（mock :8008） | ✅ 页面/标题实测 |

## 七、已知限制（后续迭代）

1. **管理界面覆盖度**：DDNS/NAT/插件/备份/审计/数据库工具/过户/批量操作等后端 API 已完备，管理界面待补（详见 docs/assets.md「⚠️ API 完整」条目）
2. **周期流量统计卡 / 周期流量告警 / 触发任务（报警联动）**：待实现
3. **主题市场 / 多 GeoIP provider / i18n 完整多语言**：后续迭代
4. **Agent 终端无 PTY**：简化实现（管道），复杂交互（vim/top）体验受限

## 八、结论

整合项目覆盖 komari + nezha 生态核心能力：实时监控、历史指标、服务监控、
终端、文件管理、任务、多渠道告警、多用户、PAT/OAuth2/2FA 权限、插件、
DDNS/NAT/过户/备份/审计、现代化仪表盘。本轮修复 1 个真实缺陷（Agent 密钥链路）、
新增 2 个管理页面，后端测试/静态检查全绿，真实 Agent 端到端与浏览器实测全部通过。
