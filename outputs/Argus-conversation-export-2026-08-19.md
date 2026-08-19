# Argus 对话导出

- 导出时间：2026-08-19 12:10（GMT+8）
- 项目目录：`D:\code\shell\Argus`
- 当前分支：`main`
- 已知基线提交：`ec9248f5c27770e8b952406279d378e898b4c472`
- 远程仓库：GitHub `origin`、CNB `cnb`

> 说明：较早轮次的逐字消息已由会话系统压缩，因此本文按当前可恢复上下文导出完整的结构化记录、关键消息、代码改动、测试结果与续接位置，不补写或虚构缺失的逐字对话。

## 1. 用户主要目标

用户要求全面审计 Argus 对以下两个参考项目的代码审计能力，并按确认的建议完善 Argus：

- `D:\code\shell\komari`
- `D:\code\shell\nezha`

审计和差距分析须覆盖：

- 监控指标
- 告警机制
- Web 页面和交互
- REST API
- Agent 通信协议
- 配置管理
- 部署、升级和备份
- 文件管理
- 服务探测
- 权限、安全和审计
- 其他业务能力

每项能力需要说明参考项目实现、Argus 当前状态、功能差距、共通与专属需求，以及实现优先级。代码修改应遵循现有架构，保留用户已有改动，不处理无关内容。

最终还需完成上线级验证、本地部署、浏览器验证、生产构建，修复发现的问题，然后创建清晰提交并将 `main` 分别推送到 GitHub `origin` 和 CNB `cnb`。禁止强推、覆盖远程历史、泄露凭据或虚构执行结果。

## 2. 已完成的总体工作

### 2.1 第一轮改进

第一轮已经完成并提交为：

```text
ec9248f5c27770e8b952406279d378e898b4c472
```

主要内容：

- WAF 指数封禁
- MCP 独立审计表
- 普通审计 CSV/JSON 导出
- 世界地图浏览器验证
- 本地部署验证
- GitHub 与 CNB 双远端推送

### 2.2 第二轮审计

第二轮完整差距审计报告：

```text
outputs/ARGUS-KOMARI-NEZHA-FULL-GAP-AUDIT-2026-08-18.md
```

共识别 35 项差距：

| 优先级 | 数量 |
|---|---:|
| P0 | 2 |
| P1 | 12 |
| P2 | 15 |
| P3 | 6 |

已完成的主要 P0/P1 能力：

- Agent `trace` 能力契约
- `auto_update` 三态配置
- 插件 Ed25519 签名校验
- 扩展历史指标与周期流量告警
- 服务多探测点
- OAuth `PUBLIC_URL`
- 节点过户 sweep/retry
- MCP 审计页面与普通审计导出 UI
- 文件流式上传
- `.argusenc` 受控恢复后端与前端入口

## 3. 关键技术设计

### 3.1 Agent 能力契约

Agent 八项能力：

- `metrics`
- `probe`
- `command`
- `terminal`
- `files`
- `upgrade`
- `nat`
- `trace`

`AutoUpdate *bool` 使用三态语义：

- `nil`：保持现状
- `true`：启用
- `false`：禁用

### 3.2 服务多探测点

兼容模型：

- 保留 `Service.ServerID` 作为默认探测点
- 新增 `ServiceProbe(ServiceID, ServerID)`
- 新 API 使用 `server_ids`
- 旧 API 的 `server_id` 继续可用

服务历史唯一桶变为：

```text
(service_id, server_id, ts)
```

### 3.3 OAuth 公开地址

- 优先使用 `ARGUS_PUBLIC_URL`
- 未配置时回退请求 Host 和 TLS
- Cookie `Secure` 同样依据公开 URL

### 3.4 文件流式上传

- 保留旧 JSON Base64 写入接口
- 新增 multipart `/upload`
- 固定 256 KiB 分块
- 首块覆盖，后续追加

### 3.5 加密备份恢复

备份格式：

- 扩展名 `.argusenc`
- AES-256-GCM
- 文件头包含 magic、版本、`key_id`、nonce
- HKDF-SHA256 派生密钥

恢复必须绑定 `BackupSchedule`，使用计划的 `KeySalt` 和当前密钥材料派生密钥，并要求显式确认：

```text
RESTORE ENCRYPTED BACKUP
```

受控恢复流程：

1. 管理员权限检查
2. 显式确认
3. 上传 `.argusenc`
4. 读取嵌入的 `key_id`
5. 常量时间比较密钥指纹
6. 解密到隔离 staging 目录
7. 校验 SQLite header 和 `PRAGMA integrity_check`
8. 阻止新备份任务进入
9. 使用 `VACUUM INTO` 创建当前库回滚快照
10. 关闭 SQLite pool
11. 跨平台替换数据库
12. 返回 `restart_required`

并发边界：

```go
operation sync.RWMutex
```

- 普通备份持有读锁
- 恢复持有写锁
- 恢复等待已运行备份结束，并阻止新备份启动

### 3.6 结构化审计

新增字段：

- `resource_type`
- `resource_id`
- `outcome`
- `error_code`
- `duration_ms`
- `request_id`

兼容策略：

- 保留原调用 `auditLog(c, action, detail)`
- 自动推导资源类型
- 从常见 detail 格式提取资源 ID
- 默认结果为 `success`
- Gin 中间件生成或沿用 `X-Request-ID`
- GORM `AutoMigrate` 只增列

## 4. 本轮继续开发前的准确停点

在本次继续前：

- `.argusenc` 后端三项测试已通过
- 前端恢复流程第一条测试已通过
- 第二条曾因 mock 未清空失败，已经增加 `vi.clearAllMocks()`
- 不兼容的 `Array.at` 已改为索引访问
- 上述前端修复尚未重跑
- 结构化审计模型、记录器、中间件和 CSV 字段刚完成编辑
- 结构化审计尚未格式化、编译或测试

## 5. 本轮新增和修正内容

### 5.1 后端结构化审计

涉及文件：

- `server/internal/model/models.go`
- `server/internal/api/audit.go`
- `server/internal/api/router.go`
- `server/internal/api/audit_test.go`
- `server/internal/db/migration_test.go`

已完成：

- `AuditLog` 增加结构化字段和索引
- 新增 `auditContextMiddleware`
- 沿用长度不超过 64 的 `X-Request-ID`，否则生成随机 ID
- 响应返回 `X-Request-ID`
- 记录请求开始时间和审计耗时
- 新增 `auditLogResult`
- 抽出 `newAuditEntry`
- 修复未挂中间件时 `request_id` 被写成 `"<nil>"` 的问题
- 列表支持 `resource_type` 和 `outcome` 精确筛选
- CSV 导出增加结构化列
- CSV/JSON 导出支持与页面一致的结构化筛选
- 新增结构化记录、列表筛选、CSV 导出测试
- 新增旧 `audit_logs` 表增量迁移测试

### 5.2 加密恢复审计语义

涉及文件：

- `server/internal/api/backups.go`
- `server/internal/api/backups_restore_test.go`

识别并修复的语义问题：

- 切库前直接写“成功”会在切换失败时留下假成功
- 切库后当前 GORM pool 已关闭，无法可靠补写成功日志

当前处理：

- 确认失败、格式错误、密钥不匹配、解密失败、完整性失败、切换失败等路径写入 `failure` 审计和稳定错误码
- 成功审计写入已验证的 staging 数据库
- 写入后再次执行 SQLite 完整性校验
- staging 数据库连接始终关闭，避免 Windows 文件锁
- 成功恢复后，该成功记录随恢复库一起成为新主库内容
- 后端成功恢复测试新增“恢复后数据库包含成功审计”的断言

稳定错误码包括：

- `backup.confirmation_required`
- `backup.manager_unavailable`
- `backup.schedule_not_found`
- `backup.file_required`
- `backup.file_size_invalid`
- `backup.staging_failed`
- `backup.upload_failed`
- `backup.bad_format`
- `backup.key_derivation_failed`
- `backup.key_mismatch`
- `backup.decrypt_failed`
- `backup.integrity_failed`
- `backup.audit_failed`
- `backup.restore_failed`

### 5.3 Web 结构化审计页面

涉及文件：

- `web/src/lib/api.ts`
- `web/src/pages/Audit.tsx`
- `web/src/pages/Audit.test.tsx`
- `web/src/locales/zh-CN.ts`
- `web/src/locales/en.ts`

已完成：

- 扩展 `AuditLog` TypeScript 类型
- 普通审计支持 `resource_type` 和 `outcome` 筛选
- 导出请求携带当前筛选条件
- 表格新增：
  - 资源类型与资源 ID
  - 结果
  - 错误码
  - 耗时
  - 请求 ID
- 保留原有动作、详情、用户、时间和 IP
- 增加中英文文案
- 前端测试覆盖普通结构化审计、MCP 筛选和带筛选导出

## 6. 测试与验证结果

### 6.1 已通过

Server API 仅编译检查：

```text
ok github.com/motao123/Argus/server/internal/api [no tests to run]
```

Web 定向测试：

```text
Test Files  2 passed (2)
Tests       5 passed (5)
```

覆盖：

- `Audit.test.tsx`：3 条通过
- `Backups.test.tsx`：2 条通过

这也确认了此前修复后尚未重跑的“取消恢复不调用 API”测试已经通过。

TypeScript：

```text
通过，无错误
```

国际化键一致性：

```text
zh-CN 1088 个 key
en 1088 个 key
src 中使用 1065 个 key
```

检查通过，保留 23 个既有未使用 key 警告。

补丁格式：

```text
git diff --check 通过
```

### 6.2 当前环境阻塞

后端 SQLite 运行测试当前无法执行，原因不是测试断言失败，而是本机 Go 环境为：

```text
CGO_ENABLED=0
```

`go-sqlite3` 因此只加载 stub，错误为：

```text
Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work.
```

检查了常见 MinGW、MSYS2、WinLibs、LLVM 路径，尚未找到可用 `gcc.exe`。项目源码已通过 Server API 编译检查，但以下新增 SQLite 运行测试仍待 CGO 工具链后执行：

- 结构化审计记录测试
- 结构化列表和 CSV 导出测试
- 旧审计表迁移测试
- 新增强断言后的加密恢复测试

## 7. 期间遇到的其他错误及处理

### 7.1 多 Go module

根目录运行 Go 测试失败：

```text
go: cannot find main module
```

原因：仓库包含独立 `server`、`agent`、`protocol` module。应使用：

```text
go -C server
go -C agent
go -C protocol
```

### 7.2 Web 工作目录

根目录运行 npm 失败，因为 `package.json` 位于 `web`。

首次直接调用 Vitest 又从仓库根启动，未加载 jsdom，出现：

```text
ReferenceError: localStorage is not defined
```

修正为显式使用：

- Web root：`D:\code\shell\Argus\web`
- 配置：`web/vite.config.ts`

之后 5 条定向测试全部通过。

### 7.3 Go 工具链校验

`server/go.mod` 要求 Go `1.26.6`，本机自动下载工具链时受全局 `GOSUMDB=off` 影响。没有修改项目或全局设置，而是单次命令覆盖：

```text
GOSUMDB=sum.golang.org
```

### 7.4 Go 变量遮蔽

新增恢复审计时局部变量 `ok` 遮蔽了响应函数 `ok(...)`，导致编译失败。已改名为 `auditReady`，编译检查通过。

### 7.5 Windows SQLite 文件锁

恢复测试曾因测试数据库连接未关闭导致临时目录清理失败。测试已注册底层 `sql.DB.Close()`。

恢复切换实现也已使用：

- `VACUUM INTO` 创建 WAL 安全回滚快照
- 关闭 SQLite pool
- 先移动旧库，再安装 staging 库
- 安装失败时尝试恢复旧库

## 8. 当前工作树状态

当前为脏工作树，既包含用户已有改动，也包含本轮改动。已明确不回退用户修改。

主要变更涉及：

- Agent probe
- 告警引擎与测试
- 审计模型、API、页面和测试
- 加密备份恢复
- 文件上传
- OAuth
- 服务多探测点
- 节点过户
- 指标 rollup
- 插件签名
- Web 多个管理页面和测试

未跟踪内容包括：

- `node_modules/`
- `outputs/`
- 若干新增测试文件

最终提交前必须排除 `node_modules/`，并核对 `outputs/` 是否应纳入提交。

本轮尚未创建新提交，也尚未推送。

## 9. 尚未完成事项

### 9.1 结构化审计收尾

- 在可用 CGO/MinGW 环境运行新增后端 SQLite 测试
- 根据运行结果修复潜在问题
- 评估是否将更多高风险失败路径接入 `auditLogResult`

### 9.2 加密恢复

- 在 CGO 环境重跑新增强断言的三条恢复测试
- 增加切换失败回滚测试
- 增加损坏密文或完整性失败测试
- 评估成功恢复后其他 goroutine 使用已关闭数据库的行为
- 明确部署层必须立即重启进程

### 9.3 完整实例备份

尚未实现：

- 版本化 manifest
- 数据库
- themes
- plugins
- 插件数据
- 必要非敏感配置
- 文件哈希
- 版本兼容检查
- 恢复顺序
- 凭据排除策略

### 9.4 其他集成测试

- 文件流式上传真实 HTTP/fake Agent 测试
- Sentinel 多探测点 fake `rpc.Peer` 调度测试

### 9.5 全量验证

仍需执行：

- Protocol 全量测试
- Agent 全量测试
- Server 串行全量测试
- Web 全量 Vitest
- TypeScript
- i18n
- Vite 生产构建

### 9.6 本地部署和浏览器验证

仍需覆盖：

- Server 和 Agent 启动
- WebSocket 注册
- 实时指标
- 服务多探测点
- 告警
- OAuth
- 文件上传
- 审计导出
- 过户重试
- 加密恢复演练
- 页面交互、表单、空状态和错误状态
- 响应式布局和视觉样式
- 浏览器控制台错误

### 9.7 提交和推送

验证全部通过后：

1. 清理提交范围
2. 创建清晰提交
3. 推送 `main` 到 GitHub `origin`
4. 推送 `main` 到 CNB `cnb`
5. 不得强推或覆盖远程历史
6. 汇总提交编号和两个远端的真实推送结果

## 10. 关键文件索引

| 文件 | 作用 |
|---|---|
| `outputs/ARGUS-KOMARI-NEZHA-FULL-GAP-AUDIT-2026-08-18.md` | 第二轮完整差距审计报告 |
| `server/internal/model/models.go` | `AuditLog` 结构化字段、服务与备份模型 |
| `server/internal/api/audit.go` | 审计中间件、记录器、筛选和导出 |
| `server/internal/api/audit_test.go` | 结构化审计 API 测试 |
| `server/internal/db/migration_test.go` | 旧表增量迁移测试 |
| `server/internal/api/backups.go` | 加密恢复、失败审计、staging 成功审计 |
| `server/internal/api/backup.go` | 明文分片恢复与数据库安全切换 |
| `server/internal/api/backups_restore_test.go` | 加密恢复真实 SQLite/密文测试 |
| `server/internal/backup/manager.go` | 备份与恢复读写锁 |
| `web/src/lib/api.ts` | Web API 类型和结构化审计筛选参数 |
| `web/src/pages/Audit.tsx` | 普通与 MCP 审计页面 |
| `web/src/pages/Audit.test.tsx` | 审计页面测试 |
| `web/src/pages/Backups.tsx` | 加密恢复入口 |
| `web/src/pages/Backups.test.tsx` | 加密恢复前端测试 |

## 11. 用户消息清单（当前上下文可恢复部分）

1. 要求针对 Argus 对 Komari 和 Nezha 的代码审计能力缺口开展分析与完善。
2. “继续”。
3. 询问任务完成情况。
4. 要求按建议依次完善，必须本地部署且全部功能运行成功后才能提交并推送 GitHub 和 CNB。
5. 多次要求生成详细、高度结构化的上下文总结。
6. “你好”。
7. 再次询问上述审计完善任务完成情况。
8. 要求再次全面审计两个参考项目的所有功能模块、核心特性和实现细节，并与 Argus 逐项对比。
9. 要求按建议完成代码修改、本地部署、完整验证、修复问题、提交并双远端推送。
10. 多次要求按九部分结构整理上下文。
11. 多次要求根据压缩后的上下文继续对话，并保持原有详细程度。
12. 当前请求：“对话导出为markdown文档到该目录”。

## 12. 推荐续接顺序

1. 准备或定位 Windows MinGW/CGO 工具链。
2. 运行新增审计、迁移和恢复后端测试。
3. 修复任何真实测试失败。
4. 完成完整实例备份 manifest 和恢复顺序。
5. 执行 Protocol、Agent、Server、Web 全量验证。
6. 本地部署 Server、Agent 和 Web。
7. 使用浏览器完成主要流程与响应式验证。
8. 清理提交范围，排除 `node_modules/`。
9. 创建提交并非强制推送到 `origin/main` 和 `cnb/main`。
10. 输出最终上线验证与推送报告。
