#!/bin/sh
# Argus Agent 一键安装脚本（哪吒风格）。
#
# 用法（由后台「安装命令」生成）：
#   curl -fsSL http://<server>/install.sh | sh -s -- -s ws://<server>/ws/agent -k <secret>
#
# 可选参数（与旧版保持兼容）：
#   -s <url>       Server WebSocket 地址（必填，如 ws://127.0.0.1:8080/ws/agent）
#   -k <secret>    注册密钥（服务器密钥或用户 Agent 密钥，必填）
#   -m <base>      国内镜像下载根 URL（如 AtomGit/Gitee Release 镜像，优先级最高）
#   -u <base>      二进制下载根 URL（默认 GitHub Releases latest）
#   -v <version>   Agent 版本（默认 latest）
#   -c <dir>       Agent 配置目录（默认 /etc/argus-agent）
#   -p <prefix>    安装前缀（默认 /usr/local，二进制在 $prefix/bin）
#
# 下载源优先级：-m/--mirror > ARGUS_AGENT_MIRROR > -u > ARGUS_AGENT_BASE_URL >
# 默认 GitHub Releases latest。镜像根 URL 结构须与 GitHub Release 产物一致：
# <base>/argus-agent-<os>-<arch> 与 <base>/checksums.txt。
#
# 平台支持：linux/darwin/freebsd，架构与 release 实际产物严格对齐——
#   linux: 386 amd64 arm arm64 riscv64 s390x loong64 mips mipsle
#   darwin: amd64 arm64
#   freebsd: 386 amd64 arm arm64
# Windows 请使用 install.ps1（PowerShell 一键安装脚本）。
#
# 特性：架构/系统自动识别、强制 SHA-256 校验（checksums.txt 下载失败或其中
#       找不到当前文件条目时立即中止，绝不跳过校验）、systemd/OpenRC 服务、
#       幂等重装。
set -eu

SERVER_URL=""
SECRET=""
MIRROR="${ARGUS_AGENT_MIRROR:-}"
BASE_URL="${ARGUS_AGENT_BASE_URL:-https://github.com/motao123/Argus/releases/latest/download}"
VERSION="latest"
CONF_DIR="/etc/argus-agent"
PREFIX="/usr/local"

usage() {
    echo "用法: $0 -s <server-url> -k <secret> [-m <mirror>] [-u <download-base>] [-v <version>] [-c <conf-dir>] [-p <prefix>]"
    exit 1
}

while [ $# -gt 0 ]; do
    case "$1" in
        -s) SERVER_URL="$2"; shift 2 ;;
        -k) SECRET="$2"; shift 2 ;;
        -m|--mirror) MIRROR="$2"; shift 2 ;;
        -u) BASE_URL="$2"; shift 2 ;;
        -v) VERSION="$2"; shift 2 ;;
        -c) CONF_DIR="$2"; shift 2 ;;
        -p) PREFIX="$2"; shift 2 ;;
        *) usage ;;
    esac
done

[ -n "$SERVER_URL" ] || usage
[ -n "$SECRET" ] || usage

# 下载源优先级：-m/--mirror > ARGUS_AGENT_MIRROR > -u > ARGUS_AGENT_BASE_URL > 默认 GitHub。
[ -n "$MIRROR" ] && BASE_URL="$MIRROR"

# ---- 系统与架构识别（与 release 实际产物严格对齐）----
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
MACH="$(uname -m)"
case "$OS" in
    *mingw*|*msys*|*cygwin*)
        echo "检测到 Windows 环境：请改用 PowerShell 一键安装脚本 install.ps1。" >&2
        echo "  下载: curl.exe -fsSL http://<server>/install.ps1 -o install.ps1" >&2
        echo "  执行: powershell -ExecutionPolicy Bypass -File install.ps1 -ServerUrl $SERVER_URL -Secret <secret>" >&2
        exit 1 ;;
    linux|darwin|freebsd) ;;
    *) echo "不支持的平台: $OS（仅支持 linux/darwin/freebsd；Windows 请使用 install.ps1）" >&2; exit 1 ;;
esac
case "$MACH" in
    x86_64|amd64)      ARCH="amd64" ;;
    aarch64|arm64)     ARCH="arm64" ;;
    i686|i386|x86)     ARCH="386" ;;
    armv7l|armv6l|armv5tel|armhf) ARCH="arm" ;;
    riscv64)           ARCH="riscv64" ;;
    s390x)             ARCH="s390x" ;;
    loongarch64)       ARCH="loong64" ;;
    mips)              ARCH="mips" ;;
    mipsel|mipsle)     ARCH="mipsle" ;;
    *)
        echo "不支持的架构: $MACH（支持的架构见脚本头部说明）" >&2
        exit 1 ;;
esac
# 各 OS 实际发布组合校验（避免下载不存在的产物）：darwin 仅 amd64/arm64。
if [ "$OS" = "darwin" ] && [ "$ARCH" != "amd64" ] && [ "$ARCH" != "arm64" ]; then
    echo "macOS 仅发布 amd64/arm64（当前 $MACH）" >&2
    exit 1
fi

# ---- 下载与校验工具 ----
download() {
    # $1=url  $2=输出文件；失败返回非零
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" -o "$2"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$2" "$1"
    else
        echo "需要 curl 或 wget" >&2
        return 1
    fi
}

sha256_of() {
    # $1=文件 -> 输出十六进制 SHA-256
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    elif command -v sha256 >/dev/null 2>&1; then
        sha256 -q "$1"
    else
        echo "缺少 SHA-256 校验工具（sha256sum/shasum/sha256）" >&2
        exit 1
    fi
}

# ---- 下载与强制校验 ----
FILE="argus-agent-$OS-$ARCH"
BIN_DIR="$PREFIX/bin"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "==> 下载 Argus Agent ($OS/$ARCH, version=$VERSION)"
BASE_URL="${BASE_URL%/}"
if [ "$VERSION" != "latest" ]; then
    BASE_URL="$(echo "$BASE_URL" | sed "s#/latest/download#/download/$VERSION#")"
fi
download "$BASE_URL/$FILE" "$TMP_DIR/$FILE" || { echo "下载失败: $BASE_URL/$FILE" >&2; exit 1; }

# 供应链强化：checksums.txt 必须可下载且包含当前文件条目，否则中止安装。
download "$BASE_URL/checksums.txt" "$TMP_DIR/checksums.txt" || {
    echo "下载 checksums.txt 失败: $BASE_URL/checksums.txt（供应链强化：拒绝无校验安装）" >&2
    exit 1
}
EXPECTED="$(awk -v n="$FILE" '$2==n {print $1}' "$TMP_DIR/checksums.txt")"
if [ -z "$EXPECTED" ]; then
    echo "checksums.txt 中未找到 $FILE 条目（供应链强化：拒绝无校验安装）" >&2
    exit 1
fi
ACTUAL="$(sha256_of "$TMP_DIR/$FILE")"
if [ "$ACTUAL" != "$EXPECTED" ]; then
    echo "SHA-256 校验失败（期望 $EXPECTED，实际 $ACTUAL），已中止" >&2
    exit 1
fi
echo "==> SHA-256 校验通过"

# ---- 安装 ----
echo "==> 安装到 $BIN_DIR"
mkdir -p "$BIN_DIR" "$CONF_DIR"
install -m 0755 "$TMP_DIR/$FILE" "$BIN_DIR/argus-agent"

# 服务单元（systemd 优先，其次 OpenRC）
UNIT_DIR="/etc/systemd/system"
if [ -d "$UNIT_DIR" ] && command -v systemctl >/dev/null 2>&1; then
    cat > "$UNIT_DIR/argus-agent.service" <<EOF
[Unit]
Description=Argus Monitor Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$BIN_DIR/argus-agent -s $SERVER_URL -k $SECRET -i 2s -c $CONF_DIR
Restart=always
RestartSec=5
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable argus-agent >/dev/null 2>&1 || true
    systemctl restart argus-agent
    echo "==> 已通过 systemd 启动 (argus-agent)"
elif [ -d /etc/init.d ] && command -v rc-service >/dev/null 2>&1; then
    cat > /etc/init.d/argus-agent <<EOF
#!/sbin/openrc-run
command="$BIN_DIR/argus-agent"
command_args="-s $SERVER_URL -k $SECRET -i 2s -c $CONF_DIR"
command_background=true
pidfile="/run/\${RC_SVCNAME}.pid"
EOF
    chmod +x /etc/init.d/argus-agent
    rc-update add argus-agent default >/dev/null 2>&1 || true
    rc-service argus-agent start
    echo "==> 已通过 OpenRC 启动 (argus-agent)"
else
    nohup "$BIN_DIR/argus-agent" -s "$SERVER_URL" -k "$SECRET" -i 2s -c "$CONF_DIR" >/var/log/argus-agent.log 2>&1 &
    echo "==> 无 systemd/OpenRC，已用 nohup 后台启动（日志 /var/log/argus-agent.log）"
fi

echo "==> 安装完成。可在 Argus 后台查看服务器上线状态。"
