# 参考项目本地部署对比报告

> 部署日期：2026-08-15，本地三项目同时运行验证。

## 一、部署结果

| 项目 | 本地运行方式 | 验证结果 |
|---|---|---|
| nezha-master（哪吒监控服务端） | `go build` 单二进制，HTTP+gRPC 同端口 :8008 | ✅ 200，Dashboard 正常启动 |
| komari（轻量监控面板） | `go build` 单二进制，:25774 | ✅ 307 → `/install` 首次安装向导正常 |
| nezha-dash-v2（React 仪表盘） | `pnpm dev` + 独立 Node Mock 后端 :8010，前端 https://localhost:5173 | ✅ 200，Mock 返回 50 台模拟服务器数据 |

## 二、技术栈与架构对比

| 维度 | nezha-master | komari | nezha-dash-v2 |
|---|---|---|---|
| 定位 | 完整自托管监控服务端 | 轻量现代重写版监控服务端 | 纯前端仪表盘 |
| 后端语言 | Go 1.26 | Go 1.25 | — |
| 前端 | 内置双 dist（管理端+用户端） | 主题系统，默认主题内嵌 | React 19 + Vite 8 |
| Agent 通信 | gRPC 双向流（protobuf） | WebSocket + JSON-RPC 2.0 | — |
| 持久化 | SQLite（可切 VictoriaMetrics TSDB） | 双库：业务 SQLite + 独立指标库 | — |
| 实时推送 | WebSocket 聚合广播（singleflight） | WebSocket | WebSocket |
| UI 方案 | 老式管理后台 | 主题市场 | shadcn/ui + Tailwind v4 + Recharts |
| 认证 | JWT + PAT + OAuth2 | 密码 + 2FA + OAuth | Cookie 会话 + CSRF |

## 三、功能清单对比

| 功能 | nezha-master | komari | nezha-dash-v2 |
|---|---|---|---|
| 实时系统监控（CPU/内存/磁盘/网络） | ✅ gRPC 流 | ✅ WS 上报 | ✅ 展示 |
| 历史指标存储 | ✅ TSDB 降采样 | ✅ 多级 rollup + t-digest | ✅ Recharts 图表 |
| 网页终端 | ✅ gRPC IOStream 隧道 | ✅ WS 隧道 | — |
| 定时任务下发 | ✅ cron + 触发任务 | ✅ 任务系统 | — |
| 报警规则 | ✅ 阈值+时长+周期流量状态机 | ✅ 离线/负载/流量告警 | — |
| 通知渠道 | ✅ Webhook 模板 | ✅ Bark/Telegram/webhook/邮件/ServerChan | — |
| 服务监控（HTTP/TCP/Ping） | ✅ | — | ✅ 展示 |
| 插件系统 | — | ✅ goja 沙箱 JS 插件 + 市场 | — |
| 主题系统 | 固定 | ✅ 主题市场 | ✅ light/dark 三态 |
| 地图视图 | — | — | ✅ d3-geo 全球地图 |
| 命令面板 | — | — | ✅ Cmd+K |
| Mock 演示环境 | — | — | ✅ Node Mock 生成 3000 台服务器 |

## 四、核心差异结论

1. **通信协议**：nezha 用 gRPC 双向流（性能强、工具链重），komari 用 WebSocket + JSON-RPC 2.0（轻量、易调试、浏览器友好）。二者均支持"一条长连接复用：状态上报 + 任务下发 + 终端隧道"。
2. **存储架构**：nezha 内存优先（实时状态全在内存，DB 只做持久化）；komari 双库隔离（业务数据与监控指标物理分离）。
3. **前端代差**：nezha-dash-v2 是现代化 React 19 + shadcn/ui 方案，组件化程度、图表体验、可定制性（window 注入配置）远超两个 Go 项目内置的旧前端。
4. **工程化**：dash-v2 提供零后端 Mock 演示环境；komari 有显式生命周期 + 声明式 RPC 注册；nezha 有报警状态机与 WebSocket 合并推送优化。

## 五、Argus 取精华决策

| 借鉴来源 | 采纳点 | Argus 中的落地 |
|---|---|---|
| komari | JSON-RPC 2.0 上报协议 | Agent ↔ Server 走 WebSocket + JSON-RPC 2.0 |
| komari | 单二进制 + 内嵌前端 | `go:embed` 打包 web 构建产物 |
| komari | 显式生命周期启动 | server 启动阶段化、fail-fast |
| nezha-master | 内存优先架构 | Server 实时状态驻留内存，DB 仅持久化 |
| nezha-master | 报警状态机（阈值+时长+周期） | 精简版：阈值+持续时长+Webhook 通知 |
| nezha-master | 一条连接复用任务/终端 | agent 单 WS 连接承载 report/exec/terminal |
| nezha-master | cron 任务下发 | robfig/cron 定时向 agent 下发命令 |
| nezha-dash-v2 | React 19 + Vite + shadcn/ui | Argus Web 技术栈 |
| nezha-dash-v2 | Recharts 实时图表 + 主题 | 总览/详情页实时曲线，light/dark |
| nezha-dash-v2 | Mock 演示环境 | 内置 mock 模式，无后端可演示 |
