#!/usr/bin/env bash
# 产出 Server / Agent 多平台二进制与 SHA-256 校验文件。
#
# 约定：
#   - 只构建并生成校验文件，不创建 git tag / GitHub release，不推送任何远端。
#   - Agent 为纯 Go：CGO_ENABLED=0，覆盖 linux/amd64、linux/arm64、
#     windows/amd64、darwin/amd64、darwin/arm64 五平台。
#   - Server 依赖 cgo 版 SQLite（mattn/go-sqlite3），只能为具备 C 交叉
#     工具链的平台产出可用二进制：
#       * linux/amd64  —— 本机构建（cgo 原生）。
#       * linux/arm64  —— 需要 aarch64-linux-gnu-gcc（或用 ARGUS_CC_AARCH64 指定），
#                         缺少时跳过并告警。
#       * windows/darwin —— 需要 mingw / osxcross 工具链；缺省跳过，
#                          如需尝试可显式设置 ARGUS_FORCE_CGO=1（产物可能不可用）。
#   - 输出目录：dist/release/<version>/，内含 checksums.txt 与 manifest.json。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${ARGUS_RELEASE_VERSION:-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)}"
OUT="$ROOT/dist/release/$VERSION"
GO="${GO:-go}"

AGENT_TARGETS=(
  "linux amd64"
  "linux arm64"
  "windows amd64"
  "darwin amd64"
  "darwin arm64"
)

log()  { printf '\033[1;34m[release]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[release]\033[0m WARN: %s\n' "$*"; }

log "版本: $VERSION"
log "输出目录: $OUT"
mkdir -p "$OUT"
rm -f "$OUT"/checksums.txt "$OUT"/manifest.json

# Server 内嵌前端：webdist 缺失或落后于 web/src 时提示先执行 make web。
if [ ! -f "$ROOT/server/cmd/argus-server/webdist/index.html" ]; then
  warn "server/cmd/argus-server/webdist 缺失；请先执行 'make web' 再构建 Server。"
fi

build_agent() {
  local os="$1" arch="$2"
  local name="argus-agent-$os-$arch"
  [ "$os" = windows ] && name="$name.exe"
  log "agent $os/$arch"
  ( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" "$GO" build -trimpath \
      -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$name" ./cmd/argus-agent )
}

build_server() {
  local os="$1" arch="$2" cc="$3"
  local name="argus-server-$os-$arch"
  log "server $os/$arch (cc=${cc:-native})"
  if [ -n "$cc" ]; then
    ( cd "$ROOT/server" && CGO_ENABLED=1 CC="$cc" GOOS="$os" GOARCH="$arch" "$GO" build -trimpath \
        -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$name" ./cmd/argus-server )
  else
    ( cd "$ROOT/server" && CGO_ENABLED=1 GOOS="$os" GOARCH="$arch" "$GO" build -trimpath \
        -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$name" ./cmd/argus-server )
  fi
}

# ---- Agent：纯 Go，全部平台 ----
for t in "${AGENT_TARGETS[@]}"; do
  set -- $t
  build_agent "$1" "$2"
done

# ---- Server：需要 cgo 工具链的平台 ----
build_server linux amd64 ""

CC_AARCH64="${ARGUS_CC_AARCH64:-}"
if [ -z "$CC_AARCH64" ] && command -v aarch64-linux-gnu-gcc >/dev/null 2>&1; then
  CC_AARCH64="aarch64-linux-gnu-gcc"
fi
if [ -n "$CC_AARCH64" ]; then
  build_server linux arm64 "$CC_AARCH64"
else
  warn "未找到 aarch64-linux-gnu-gcc（或 ARGUS_CC_AARCH64），跳过 server linux/arm64。"
fi

if [ "${ARGUS_FORCE_CGO:-0}" = "1" ]; then
  warn "ARGUS_FORCE_CGO=1：尝试交叉构建 windows/darwin Server（需要对应 C 工具链，产物需自行验证）。"
  build_server windows amd64 "${CC_WINDOWS:-x86_64-w64-mingw32-gcc}"
  build_server darwin arm64  "${CC_DARWIN:-o64-clang}"
  build_server darwin amd64  "${CC_DARWIN_AMD64:-o64-clang}"
else
  warn "跳过 windows/darwin Server：SQLite 需要 cgo 交叉工具链（设置 ARGUS_FORCE_CGO=1 可尝试）。"
fi

# ---- SHA-256 与清单 ----
log "生成 checksums.txt / manifest.json"
(
  cd "$OUT"
  find . -maxdepth 1 -type f ! -name checksums.txt ! -name manifest.json -print0 \
    | sort -z | xargs -0 -r sha256sum | sed 's|  \./|  |' > checksums.txt
)
cat > "$OUT/manifest.json" <<EOF
{
  "version": "$VERSION",
  "built_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "go": "$("$GO" version)",
  "artifacts": [
$(for f in "$OUT"/*; do
  [ -f "$f" ] || continue
  base="$(basename "$f")"
  case "$base" in checksums.txt|manifest.json) continue ;; esac
  printf '    {"file": "%s", "sha256": "%s"}' "$base" "$(awk -v n="$base" '$2==n {print $1}' "$OUT/checksums.txt")"
  printf ','
  printf '\n'
done | sed '$s/,$//')
  ]
}
EOF

log "完成：$OUT"
ls -lh "$OUT"
