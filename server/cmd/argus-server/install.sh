#!/bin/sh
# Argus Agent 一键安装脚本（哪吒风格）。
#
# 用法（由后台「安装命令」生成）：
#   curl -fsSL http://<server>/install.sh | sh -s -- -s ws://<server>/ws/agent -k <secret>
#
# 可选参数：
#   -s <url>       Server WebSocket 地址（必填，如 ws://127.0.0.1:8080/ws/agent）
#   -k <secret>    注册密钥（服务器密钥或用户 Agent 密钥，必填）
#   -u <base>      二进制下载根 URL（默认 GitHub Releases latest）
#   -v <version>   Agent 版本（默认 latest）
#   -c <dir>       Agent 配置目录（默认 /etc/argus-agent）
#   -p <prefix>    安装前缀（默认 /usr/local，二进制在 $prefix/bin）
#
# 特性：架构/系统自动识别、SHA-256 校验、systemd/OpenRC 服务、幂等重装。
set -eu

SERVER_URL=""
SECRET=""
BASE_URL="${ARGUS_AGENT_BASE_URL:-https://github.com/motao123/Argus/releases/latest/download}"
VERSION="latest"
CONF_DIR="/etc/argus-agent"
PREFIX="/usr/local"

usage() {
    echo "用法: $0 -s <server-url> -k <secret> [-u <download-base>] [-v <version>] [-c <conf-dir>] [-p <prefix>]"
    exit 1
}

while [ $# -gt 0 ]; do
    case "$1" in
        -s) SERVER_URL="$2"; shift 2 ;;
        -k) SECRET="$2"; shift 2 ;;
        -u) BASE_URL="$2"; shift 2 ;;
        -v) VERSION="$2"; shift 2 ;;
        -c) CONF_DIR="$2"; shift 2 ;;
        -p) PREFIX="$2"; shift 2 ;;
        *) usage ;;
    esac
done

[ -n "$SERVER_URL" ] || usage
[ -n "$SECRET" ] || usage

# ---- 系统与架构识别 ----
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
MACH="$(uname -m)"
case "$OS" in
    linux|darwin|freebsd) ;;
    *) echo "不支持的平台: $OS（请手动安装或使用 Windows 的 install.ps1）" >&2; exit 1 ;;
esac
case "$MACH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    armv7l|armv6l|armhf) ARCH="arm" ;;
    i686|i386)     ARCH="386" ;;
    riscv64)       ARCH="riscv64" ;;
    s390x)         ARCH="s390x" ;;
    mips)          ARCH="mips" ;;
    mipsle)        ARCH="mipsle" ;;
    *) echo "未知架构: $MACH" >&2; exit 1 ;;
esac

# ---- 下载与校验 ----
FILE="argus-agent-$OS-$ARCH"
if command -v curl >/dev/null 2>&1; then
    FETCH="curl -fsSL"
else
    FETCH="wget -qO-"
fi
BIN_DIR="$PREFIX/bin"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "==> 下载 Argus Agent ($OS/$ARCH, version=$VERSION)"
BASE_URL="${BASE_URL%/}"
if [ "$VERSION" != "latest" ]; then
    BASE_URL="$(echo "$BASE_URL" | sed "s#/latest/download#/download/$VERSION#")"
fi
$FETCH "$BASE_URL/$FILE" -o "$TMP_DIR/$FILE" 2>/dev/null || { echo "下载失败: $BASE_URL/$FILE" >&2; exit 1; }
$FETCH "$BASE_URL/checksums.txt" -o "$TMP_DIR/checksums.txt" 2>/dev/null || true

if [ -f "$TMP_DIR/checksums.txt" ]; then
    EXPECTED="$(awk -v n="$FILE" '$2==n {print $1}' "$TMP_DIR/checksums.txt")"
    if [ -n "$EXPECTED" ]; then
        ACTUAL="$(sha256sum "$TMP_DIR/$FILE" | awk '{print $1}')"
        if [ "$ACTUAL" != "$EXPECTED" ]; then
            echo "SHA-256 校验失败（期望 $EXPECTED，实际 $ACTUAL），已中止" >&2
            exit 1
        fi
        echo "==> SHA-256 校验通过"
    else
        echo "==> 警告: checksums.txt 中未找到 $FILE，跳过校验"
    fi
fi

# ---- 安装 ----
echo "==> 安装到 $BIN_DIR"
mkdir -p "$BIN_DIR" "$CONF_DIR"
install -m 0755 "$TMP_DIR/$FILE" "$BIN_DIR/argus-agent"
chmod 0644 /dev/null 2>/dev/null || true

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
