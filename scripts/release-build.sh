#!/usr/bin/env bash
# 产出 Server / Agent 多平台二进制与 SHA-256 校验文件。
#
# 约定：
#   - 只构建并生成校验文件，不创建 git tag / GitHub release，不推送任何远端。
#   - Agent 为纯 Go（CGO_ENABLED=0），无需任何 C 工具链即可交叉编译：
#       linux  : 386 / amd64 / arm / arm64 / riscv64 / s390x / loong64 / mips / mipsle
#       windows: 386 / amd64 / arm64（产物名带 .exe）
#       darwin : amd64 / arm64
#       freebsd: 386 / amd64 / arm / arm64
#   - 产物命名 argus-agent-<os>-<arch>（windows 加 .exe）。
#   - Server 依赖 cgo 版 SQLite（mattn/go-sqlite3），只对具备 C 交叉工具链的
#     平台产出可用二进制：
#       * linux/amd64  —— 本机构建（cgo 原生）。
#       * linux/arm64  —— 需要 aarch64-linux-gnu-gcc（或用 ARGUS_CC_AARCH64 指定），
#                         缺少时跳过并告警。
#       * 其余平台明确跳过并告警（需要 mingw / osxcross 等对应 C 交叉工具链；
#         如需尝试可显式设置 ARGUS_FORCE_CGO=1，产物可能不可用）。
#   - 逐平台记录构建成败：manifest.json 的 "results" 数组按平台列出
#     ok / skipped / failed，最终以退出码汇总（任一平台失败则非 0）。
#   - 输出目录：dist/release/<version>/，内含 checksums.txt 与 manifest.json。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${ARGUS_RELEASE_VERSION:-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)}"
OUT="$ROOT/dist/release/$VERSION"
GO="${GO:-go}"

# Agent 平台矩阵（纯 Go，全部支持）。
AGENT_TARGETS=(
  "linux 386"
  "linux amd64"
  "linux arm"
  "linux arm64"
  "linux riscv64"
  "linux s390x"
  "linux loong64"
  "linux mips"
  "linux mipsle"
  "windows 386"
  "windows amd64"
  "windows arm64"
  "darwin amd64"
  "darwin arm64"
  "freebsd 386"
  "freebsd amd64"
  "freebsd arm"
  "freebsd arm64"
)

# Server 其余平台：cgo SQLite 需要对应 C 交叉工具链，默认明确跳过并告警。
SERVER_SKIPPED=(
  "linux 386"
  "linux arm"
  "linux riscv64"
  "linux s390x"
  "linux loong64"
  "linux mips"
  "linux mipsle"
  "windows 386"
  "windows amd64"
  "windows arm64"
  "darwin amd64"
  "darwin arm64"
  "freebsd 386"
  "freebsd amd64"
  "freebsd arm"
  "freebsd arm64"
)

# 逐平台结果记录：<kind>|<os>|<arch>|<status>|<detail>
RESULTS=()

log()  { printf '\033[1;34m[release]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[release]\033[0m WARN: %s\n' "$*"; }
err()  { printf '\033[1;31m[release]\033[0m ERROR: %s\n' "$*"; }

record() { # kind os arch status [detail]
  local kind="$1" os="$2" arch="$3" status="$4" detail="${5:-}"
  RESULTS+=("$kind|$os|$arch|$status|$detail")
  case "$status" in
    ok)     log "$kind $os/$arch: 构建成功" ;;
    skipped) warn "$kind $os/$arch: 跳过（${detail:-未提供原因}）" ;;
    failed) err "$kind $os/$arch: 构建失败（${detail:-未知原因}）" ;;
  esac
}

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
  rm -f "$OUT/$name"
  local rc=0
  ( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" "$GO" build -trimpath \
      -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$name" ./cmd/argus-agent ) || rc=$?
  if [ "$rc" -eq 0 ]; then
    record agent "$os" "$arch" ok
  else
    record agent "$os" "$arch" failed "go build 退出码 $rc"
  fi
}

build_server() {
  local os="$1" arch="$2" cc="$3"
  local name="argus-server-$os-$arch"
  log "server $os/$arch (cc=${cc:-native})"
  rm -f "$OUT/$name"
  local rc=0
  if [ -n "$cc" ]; then
    ( cd "$ROOT/server" && CGO_ENABLED=1 CC="$cc" GOOS="$os" GOARCH="$arch" "$GO" build -trimpath \
        -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$name" ./cmd/argus-server ) || rc=$?
  else
    ( cd "$ROOT/server" && CGO_ENABLED=1 GOOS="$os" GOARCH="$arch" "$GO" build -trimpath \
        -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$name" ./cmd/argus-server ) || rc=$?
  fi
  if [ "$rc" -eq 0 ]; then
    record server "$os" "$arch" ok
  else
    record server "$os" "$arch" failed "go build 退出码 $rc"
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
  record server linux arm64 skipped "缺少 aarch64-linux-gnu-gcc / ARGUS_CC_AARCH64"
fi

if [ "${ARGUS_FORCE_CGO:-0}" = "1" ]; then
  warn "ARGUS_FORCE_CGO=1：尝试交叉构建 windows/darwin Server（需要对应 C 工具链，产物需自行验证）。"
  build_server windows amd64 "${CC_WINDOWS:-x86_64-w64-mingw32-gcc}"
  build_server darwin arm64  "${CC_DARWIN:-o64-clang}"
  build_server darwin amd64  "${CC_DARWIN_AMD64:-o64-clang}"
  # 其余平台仍明确跳过并告警。
  for t in "${SERVER_SKIPPED[@]}"; do
    set -- $t
    if { [ "$1" = windows ] && [ "$2" = amd64 ]; } \
      || { [ "$1" = darwin ] && { [ "$2" = amd64 ] || [ "$2" = arm64 ]; }; }; then
      continue
    fi
    record server "$1" "$2" skipped "SQLite 需要 cgo 交叉工具链（未提供）"
  done
else
  warn "跳过其余 Server 平台：SQLite 需要 cgo 交叉工具链（设置 ARGUS_FORCE_CGO=1 可尝试）。"
  for t in "${SERVER_SKIPPED[@]}"; do
    set -- $t
    record server "$1" "$2" skipped "SQLite 需要 cgo 交叉工具链（未提供）"
  done
fi

# ---- SHA-256 与清单 ----
log "生成 checksums.txt / manifest.json"
(
  cd "$OUT"
  find . -maxdepth 1 -type f ! -name checksums.txt ! -name manifest.json -print0 \
    | sort -z | xargs -0 -r sha256sum | sed 's|  \./|  |' > checksums.txt
)

RESULTS_JSON="$(
  for r in "${RESULTS[@]}"; do
    IFS='|' read -r kind os arch status detail <<<"$r"
    printf '    {"kind": "%s", "os": "%s", "arch": "%s", "status": "%s"' "$kind" "$os" "$arch" "$status"
    [ -n "$detail" ] && printf ', "detail": "%s"' "$detail"
    printf '},\n'
  done | sed '$s/,$//'
)"

cat > "$OUT/manifest.json" <<EOF
{
  "version": "$VERSION",
  "built_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "go": "$("$GO" version)",
  "results": [
$RESULTS_JSON
  ],
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

# ---- 汇总 ----
log "构建结果汇总："
for r in "${RESULTS[@]}"; do
  IFS='|' read -r kind os arch status detail <<<"$r"
  printf '  %-6s %-10s %-8s %-8s %s\n' "$kind" "$os" "$arch" "$status" "$detail"
done

FAILED=0
for r in "${RESULTS[@]}"; do
  IFS='|' read -r _ _ _ status _ <<<"$r"
  [ "$status" = failed ] && FAILED=1
done

log "完成：$OUT"
ls -lh "$OUT"
if [ "$FAILED" = "1" ]; then
  err "存在失败的平台构建，请检查上方输出。"
  exit 1
fi
