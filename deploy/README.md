# deploy/ 部署说明

本目录提供 Argus Server 的部署样例：

| 文件 | 用途 |
|---|---|
| `Dockerfile` / `docker-compose.yml` / `.env.example` | Docker Compose 部署 |
| `argus-server.service` | systemd 单元（二进制方式部署 Server） |
| `argus.env.example` | Server 环境变量模板（systemd/OpenRC 二进制部署共用） |
| `nginx.conf` | 反向代理参考配置 |

> Agent 端的一键安装见后台「服务器」页生成的安装命令（`install.sh` / `install.ps1`），
> 与 Server 部署方式无关。

## 二进制方式部署 Server

### 目录规划

| 路径 | 用途 |
|---|---|
| `/usr/local/bin/argus-server` | Server 二进制 |
| `/etc/argus/argus.env` | 运行环境变量（权限 600） |
| `/opt/argus` | 工作目录（WorkingDirectory） |
| `/var/lib/argus` | 数据目录（SQLite 数据库 `argus.db` + `.jwt` 密钥文件） |

1. 下载 Server 二进制（linux/amd64 或 linux/arm64）到 `/usr/local/bin/argus-server`，
   执行权限 `0755`（与 Agent 相同，可从 GitHub Releases 或国内镜像获取）。
2. 创建运行用户与目录：

   ```bash
   useradd -r -s /usr/sbin/nologin argus
   install -d -o argus -g argus -m 0750 /opt/argus /var/lib/argus /etc/argus
   ```

3. 配置环境变量：复制 `deploy/argus.env.example` 为 `/etc/argus/argus.env`，
   必须修改 `ARGUS_ADMIN_PASS`，并将数据库放到数据目录：

   ```bash
   ARGUS_LISTEN=127.0.0.1:8080        # 反代场景绑回环；直连公网用 0.0.0.0:8080
   ARGUS_DB=/var/lib/argus/argus.db   # 必须位于 /var/lib/argus 下（见下方 systemd 说明）
   ```

   `chmod 600 /etc/argus/argus.env`；环境文件缺失时 systemd 应直接报错，禁止静默回退到示例凭据。

### systemd（推荐）

样例单元见 `deploy/argus-server.service`，关键点：

- `Restart=always`：数据库恢复完成后 Server 正常关闭 HTTP 服务，systemd 将自动拉起新进程；显式 `systemctl stop` 不会触发重启。
- `ProtectSystem=strict` + `ReadWritePaths=/var/lib/argus`：进程只能写数据目录，
  **因此 `ARGUS_DB` 必须指向 `/var/lib/argus` 下**（默认 `./data/argus.db` 会被拒绝写入）。
- `EnvironmentFile=/etc/argus/argus.env`：加载全部 `ARGUS_*` 变量；文件缺失会阻止服务启动。

安装步骤：

```bash
install -m 0644 deploy/argus-server.service /etc/systemd/system/argus-server.service
systemctl daemon-reload
systemctl enable --now argus-server
systemctl status argus-server
# 健康检查：curl http://127.0.0.1:8080/healthz
```

### OpenRC（Alpine / Gentoo 等）

创建 `/etc/init.d/argus-server`：

```sh
#!/sbin/openrc-run
name="argus-server"
description="Argus monitoring server"
command="/usr/local/bin/argus-server"
command_user="argus:argus"
directory="/opt/argus"
pidfile="/run/${RC_SVCNAME}.pid"

depend() {
    need net
    after firewall
}

start_pre() {
    # 载入 /etc/argus/argus.env（忽略注释与空行）
    if [ -f /etc/argus/argus.env ]; then
        set -a
        . /etc/argus/argus.env
        set +a
    fi
    checkpath --directory --owner argus:argus --mode 0750 /var/lib/argus
    checkpath --directory --owner argus:argus --mode 0750 /opt/argus
}
```

```bash
chmod +x /etc/init.d/argus-server
rc-update add argus-server default
rc-service argus-server start
```

## 常见问题（FAQ）

### 1. trusted proxies（反代后的真实客户端 IP）

`ARGUS_TRUSTED_PROXIES` 为空时，Server **不采信**任何 `X-Forwarded-For` /
`X-Real-IP` 等转发头（避免伪造 IP 绕过限流/审计）。仅在可信反向代理后部署时填写：

```bash
# nginx/caddy 与 Server 同机，反代走回环：
ARGUS_TRUSTED_PROXIES=127.0.0.1
# 或上游代理网段：
ARGUS_TRUSTED_PROXIES=10.0.0.0/8
```

填写后转发头仅从这些来源采信。直连部署请留空。

### 2. GeoIP 地图不显示国家/地区

`ARGUS_GEOIP_ENDPOINT` 为空（默认）时不查询 GeoIP，地图安全隐藏而不是报错。
如需地图定位，配置 HTTP GeoIP provider 基础 URL：

```bash
ARGUS_GEOIP_ENDPOINT=https://your-geoip-provider.example.com
```

Server 请求 `<endpoint>/<ip>`，响应 JSON 需包含 `country_code` 或 `countryCode`。
注意：GeoIP 服务需要能访问公网，且内网 IP（如 NAT 隧道源地址）通常无结果，
属于正常现象。

### 3. NAT 端口（`ARGUS_NAT_LISTEN`）

NAT HTTP 隧道默认监听 `:9090`（所有网卡）。Agent 通过 WebSocket 上报后，
NAT 目标服务的客户端访问 `http://<server-ip>:9090/` 并按 HTTP `Host` 路由到
对应服务器——**因此 9090 端口必须对客户端可达**：

- 云服务器：安全组/防火墙放行 9090（HTTP，80/443 的 TLS 场景需自行接反代）。
- 反代场景：将 `ARGUS_NAT_LISTEN` 绑到回环（如 `127.0.0.1:9090`），再在反代中
  把目标域名/路径转发到 9090。
- 只服务本机时可用 `ARGUS_NAT_LISTEN=127.0.0.1:9090` 收紧暴露面。
- 保留域名（如仪表盘域名）用 `ARGUS_NAT_RESERVED_HOSTS` 声明，命中返回 421，
  防止 NAT Host 路由覆盖管理入口。

### 4. 备份目录 / 数据备份

所有持久化数据都在 `ARGUS_DB`（示例为 `/var/lib/argus/argus.db`）这一个 SQLite
文件及其伴生文件：

| 文件 | 说明 |
|---|---|
| `argus.db` | 全部业务数据（服务器、报警、任务、用户、指标） |
| `argus.db.jwt` | JWT 签名密钥（自动生成）。**备份加密备份的口令兜底也用它**，必须与 DB 一起备份 |
| `.backup-work/` | 定时备份的临时工作目录（`/var/lib/argus/.backup-work`，可清空） |

推荐做法：

- **在线备份**：后台「备份」页下载加密快照（`VACUUM INTO` 一致性快照，WAL 安全）
  或配置定时备份任务。备份用 `ARGUS_BACKUP_KEY`（或
  `ARGUS_BACKUP_KEY_FILE` 指向的口令文件）加密；未设置时兜底用 `argus.db.jwt`。
  恢复时使用同一密钥，**换机/新实例恢复前先备份旧 `.jwt` 或用固定的
  `ARGUS_BACKUP_KEY`**，否则旧备份无法解密。
- **冷备份**：`systemctl stop argus-server` 后直接复制
  `/var/lib/argus/argus.db`（连同 `.jwt`），再启动。
- **不要**只备份 `argus.db` 而丢失 `argus.db.jwt` 与备份口令——解密与登录
  签名均依赖它们。

### 5. 其他

- 修改 `/etc/argus/argus.env` 后需重启服务：`systemctl restart argus-server`
  （OpenRC：`rc-service argus-server restart`）。
- `ARGUS_JWT_SECRET` 留空时自动生成并持久化到 `argus.db.jwt`；多实例部署
  必须共享同一高强度 `ARGUS_JWT_SECRET`（且不要提交真实密钥）。
