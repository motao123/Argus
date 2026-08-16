# Argus 整合版部署验证报告

> 最近验证日期：2026-08-16（第2轮全量验证 + 阶段0 安全整改复验）
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

## 九、生产部署与 CNB 流水线复核（2026-08-16）

- 新增 `/healthz` 无状态探活端点（HTTP 200 `{status:"ok"}`）
- 修复 deploy/docker-compose.yml：改为根目录 build context + 生产 Dockerfile，不再运行时挂载错误目录现场编译；管理员密码必须由 deploy/.env 提供
- 补齐 systemd：deploy/argus-server.service + deploy/argus.env.example
- 生产 Dockerfile 修复：复制 pnpm-workspace.yaml/.npmrc，CI 模式安装，基础镜像使用完整 docker.io 路径
- 本地 Podman 构建已通过 web 阶段（2273 modules），进入 Go 阶段时因本机 Docker Hub 直连超时未拉到 golang:1.26-alpine；代码自身 go test/vet/web build 均已独立通过
- 新增 .cnb.yml：main push 顺序执行 Go test/vet、Web build、生产镜像构建并推送 `docker.cnb.cool/<repo>:<commit>` 与 latest
- **边界说明**：CNB 仓库/制品流水线可由 .cnb.yml 完成；CNB 不自动提供公网生产运行实例。当前已完成双远端代码同步与 CNB 发布流水线定义，公网生产 URL 仍需目标服务器/Kubernetes/云平台凭据后才能执行并验证。

## 十、未完成项续做（2026-08-16）

- 新增后台「网络服务」页 `/admin/network`，统一管理 DDNS 与 NAT：列表、创建、编辑、删除；DDNS 支持 Cloudflare/Webhook 与立即测试。
- 浏览器黑盒验证：页面渲染正常；临时 NAT `nat-test.local → 127.0.0.1:3000` 创建后即时出现在列表，随后经 API 删除并确认列表为空；未调用任何真实外部 DDNS。
- 下一批优先级：周期流量统计卡、报警联动任务表单、备份/数据库维护、2FA、插件与审计日志管理页。

## 十一、阶段 0：安全基线整改与复验（2026-08-16）

**修复的安全/数据完整性缺陷（均有回归测试）**
1. 统一授权模型：`authorizeServer` / `authorizePublicServer` / `canViewService`，覆盖 REST、WebSocket、MCP
2. MCP 全部工具（server.get/exec、fs.*）校验 scope、owner、PAT 白名单；admin 才可越过 scope
3. 服务器 update/delete/exec/config/terminal 增加 owner 校验；修复 updateServer 单字段更新清空 name/group/note 的缺陷（列名 group_name）
4. 公开 metrics/traffic 对隐藏服务器按 owner/hidden 过滤；Dashboard WS 按 guest-hidden、user-owner、PAT 白名单过滤
5. Service 新增 hidden；list/history/stats 统一可见性；Service/DDNS/NAT 创建与改绑校验 server 归属；DDNS test 校验 profile owner
6. 删除误绑的「过户」POST 路由（曾调用流量查询）；流量接口统一 `/servers/:id/traffic`
7. Agent 注册：禁止空密钥；用户 AgentSecret 注册创建 owner server；服务端密钥仅重连；存量重复 AgentSecret 去重回填
8. WAF 改为真实时间窗口（可注入时钟测试），过期清理；修复「累计 301 次永久封禁」缺陷
9. 新增 `ARGUS_TRUSTED_PROXIES`，不再无条件信任 X-Forwarded-For；部署样例与 nginx 注释同步
10. 通知 HTTP 客户端恢复 TLS 证书校验（移除 InsecureSkipVerify）
11. 插件与通知渠道接口收紧为 admin

**新增测试**
- api/authz_test.go：跨租户 update/delete/exec、隐藏服务器 metrics、Service/DDNS/NAT server 归属、服务可见性
- api/waf_test.go：时间窗口重置、封禁、解封（可控时钟）
- mcp/mcp_test.go：工具级 scope/owner/白名单、server.list 过滤

**本地双用户端到端复验（全部通过）**
- alice 创建服务器（owner=alice）→ 真实 Agent 注册上线（Deepin 实时数据）
- bob 对 alice 服务器 update/exec/terminal → 403；bob 服务器列表为空
- 隐藏服务器：guest 404、非 owner 404、owner 200
- 单字段 PATCH 不清空 name；MCP 只读 PAT：get 放行、exec 拒绝、不存在 404
- 空密钥 Agent：客户端与服务端双重拒绝

## 十二、阶段 1：认证闭环与前端可靠性（2026-08-16）

**后端**
- OAuth 不再把 JWT 放入 URL：改为一次性短期 code 交换（`POST /auth/oauth/consume`），新增公开 `GET /auth/oauth/providers` 与 `GET /auth/me`
- 2FA 全生命周期 API：setup/qrcode/enable/disable + 登录强制校验（me 返回 two_fa_enabled）
- 公开设置补充 favicon 字段

**前端**
- 登录页：OAuth provider 按钮、2FA 验证码字段（登录失败含 2fa 时自动出现）、`oauth_code` 消费
- 新增「账户安全」页（/admin/security）：TOTP 开启/关闭（二维码+密钥+验证码）、OAuth provider CRUD（admin）
- 全局 ErrorBoundary；显式 403/404 页面（替换通配跳首页）
- WS 连接状态提示（重连横幅）；修复「离线沉底」排序（在线优先）
- 服务器详情/管理列表新增网页终端入口；favicon 从公开设置应用
- 后台响应式：<1024px 顶部栏+抽屉导航，表格与内容适配移动端

**验证**
- API：TOTP 生成→启用→无码登录 401→带码 200→错码 401→关闭，全链路通过；OAuth code 单次/过期/无效测试通过（oauth_test.go）
- 浏览器：登录页 GitHub 按钮、安全页 2FA 二维码/密钥/验证码、OAuth provider 表格、404 页、390x844 移动端抽屉导航，全部通过

## 十三、阶段 2：数据正确性与安全维护（2026-08-16）

- 流量账本重写（TrafficLedger）：每次上报 reset-aware 增量累加至小时桶，幂等 upsert，
  持久化计数基线（traffic_baselines）支持重启恢复，断档>5min 不产生虚假尖峰；
  旧实现「只在跨小时记一次差值、多数增量丢失」已修复
- /servers/:id/traffic 改为自然日历桶（24 小时整点 / 30 自然日 / 12 自然月）
- rollup 只聚合已完成桶（进行中桶跳过，等待补齐），幂等覆盖不再固化不完整桶；
  Metric/批处理/rollup/API 全链路补充 temperature/gpu_util 历史字段
- 备份重做：VACUUM INTO 一致性快照 + SHA-256/大小响应头；恢复走 staging 分片
  （upload_id/offset 顺序校验）→ SQLite 头 + integrity_check + 总哈希校验 →
  备份当前库后原子切换，无效/乱序文件拒绝
- 新增「备份维护」页（/admin/maintenance）：下载备份、分片恢复（浏览器分片+进度）、DB 体积、VACUUM
- 验证：备份 SHA 与下载文件一致；恢复→重启→数据完整；无效文件拒绝；pre-restore 回滚点生成；
  重启无流量虚假尖峰；store 新增 4 个账本单测

## 十四、阶段 3：监控/告警/服务器运维完整化（2026-08-16）

- 服务器管理页：REST 列表（含离线）、搜索、多选、批量移动/删除、分组管理、完整编辑
  （价格/周期/到期/自动续费/标签/排序/隐藏）、Agent 配置下发、终端/远程执行入口
- 服务器详情：温度/GPU（无数据显「不可用」）、周期流量卡（24h/30d/12m 双序列柱状图）
- 告警表单：触发任务（Cron）、采样达标比例、通知分组；指标新增 temperature/gpu
- 通知渠道：类型选择（webhook/bark/telegram/email/serverchan）、chat_id、测试发送
- 浏览器验证：服务器列表/标签/分组管理/周期流量/终端入口全部通过

## 十五、阶段 4：Agent 升级与服务器过户状态机（2026-08-16）

- Agent 自升级：新增 `agent.upgrade` RPC（下载 → SHA-256 校验 → 备份 → 原子替换 → 后台重启）；
  agent 端 platform 分离 detach 实现；服务端 `POST /upgrade-jobs` 批量升级 + 逐机回执
  （success/failure/offline），仅 http(s) URL
- 服务器过户状态机：`ServerTransfer` 模型（pending/verified/cancelled/failed）；
  发起即轮换服务器密钥为一次性握手密钥、断开旧连接；新 owner 的 Agent 用新密钥重连即
  `VerifyTransfer` 验证过户；取消回滚原密钥；30 分钟未验证自动回滚（sweep）
- 端到端验证：alice 拥有服务器 → admin 过户给 bob → bob Agent 用新密钥重连 → owner 变 bob、
  transfer=verified、bob 可见/alice 不可见；取消路径回滚原密钥通过；单测 TestTransferLifecycle/AdminOnly
- Agent 升级端到端：v1(0.1.0) → HTTP 源提供 v2(0.2.0) → 触发升级 → 二进制替换 + v2 重连上报

## 十六、阶段 5：插件安全化与自定义注入（2026-08-16）

- 插件系统安全化：manifest 权限声明（allow_fetch/allow_exec/approved，默认全禁）；
  fetch 仅 http(s) 且需批准（防 SSRF）；启停/批准状态持久化（state.json）重启保留；
  新增 `POST /plugins/:name/approve`
- 新增「插件」管理页（/admin/plugins）：已安装/市场 Tab、启停、批准、立即运行、日志、删除、安装
- 自定义代码注入：设置新增 custom_css/custom_js/custom_footer，serve 时注入
  （<head> CSS、</body> 前 JS、前台 Powered by 前页脚），热更新
- 端到端验证：CSS/JS/页脚注入生效；未批准插件 fetch 拒绝、批准后成功（httptest）；
  插件状态重启持久化（单测）
- 新增 plugin_test.go（TestPluginPermissionDeniedThenApproved）
