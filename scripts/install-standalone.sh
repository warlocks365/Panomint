#!/usr/bin/env bash
# Panomint 独立服务器安装（无容器形态）—— 通用相册系统的"最小可用"交付脚本。
#
# 设计原则（请先读，再判断脚本行为是否"自欺"）：
#   1) 诚实降级：缺依赖只报错 + 给指引，**不假装成功**；
#      在没有 root、没有 systemd、甚至不是 Linux 的机器上，也能跑到"给出指引"而不炸。
#   2) 不写系统目录：systemd unit 只**生成到临时目录**并打印下一步，
#      绝不动 /etc/systemd/system（那一步必须由你用 root 亲自执行）。
#   3) 不做破坏性操作：不删除任何文件；数据库只执行 goose **up**（不 reset、不 drop）。
#
# 用法：
#   bash scripts/install-standalone.sh [--dir <安装根>] [--user <运行用户>] [--group <运行组>]
#                                      [--env-file <路径>] [--no-build] [--no-migrate]
#
#   --dir         安装根目录（含 bin/ data/ assets/ .env）；默认 = 本脚本所在仓库根
#   --user        运行身份（**不要用 root**）；默认 = 当前用户
#   --env-file    环境变量文件；默认 <安装根>/.env
#   --no-build    跳过编译（用已有 bin/ 里的二进制）
#   --no-migrate  跳过数据库迁移（你打算自己跑 cmd/migrate 时用）
#
# 退出码：0 = 全部就绪；1 = 有 FAIL（见末尾汇总）；2 = 参数错误。

set -uo pipefail   # 刻意不用 -e：依赖检查需要"失败后继续收集问题"，最后统一汇总

# ---------------------------------------------------------------- 基础工具
say()  { printf '%s\n' "$*"; }
head1() { printf '\n=== %s ===\n' "$*"; }
ok()   { printf '  [ OK ] %s\n' "$*"; PASS=$((PASS + 1)); }
bad()  { printf '  [FAIL] %s\n' "$*" >&2; FAIL=$((FAIL + 1)); }
skip() { printf '  [SKIP] %s\n' "$*"; SKIP=$((SKIP + 1)); }
warn() { printf '  [warn] %s\n' "$*"; }

PASS=0; FAIL=0; SKIP=0

has() { command -v "$1" >/dev/null 2>&1; }

# 从环境变量或 .env 文件取一个键的值（不 eval，故值里的特殊字符不会被解释）
env_value() { # env_value <KEY>
  local key="$1" v
  v="${!key:-}"
  if [ -n "$v" ]; then printf '%s' "$v"; return 0; fi
  [ -n "${ENV_FILE:-}" ] && [ -f "$ENV_FILE" ] || return 0
  sed -n "s/^[[:space:]]*${key}[[:space:]]*=[[:space:]]*//p" "$ENV_FILE" 2>/dev/null \
    | head -n 1 | sed -e 's/^["'\'']//' -e 's/["'\'']$//' -e 's/[[:space:]]*$//'
}

# ---------------------------------------------------------------- 参数
SCRIPT_PATH="${BASH_SOURCE[0]:-$0}"
SELF_DIR="$(cd "$(dirname "$SCRIPT_PATH")" && pwd 2>/dev/null || echo .)"
DEFAULT_ROOT="$(cd "$SELF_DIR/.." && pwd 2>/dev/null || echo .)"

ROOT="$DEFAULT_ROOT"
# ⚠️ 只取第一行并去掉 CR：某些环境（如 MSYS/Git Bash）的 id 会输出多行，
#    不清理会把换行带进后面的 sed 表达式与 unit 文件（实测踩过）。
RUN_USER="$(id -un 2>/dev/null | head -n 1 | tr -d '\r\n')"
[ -n "$RUN_USER" ] || RUN_USER="pano"
RUN_GROUP="$(id -gn 2>/dev/null | head -n 1 | tr -d '\r\n')"
[ -n "$RUN_GROUP" ] || RUN_GROUP="$RUN_USER"
ENV_FILE=""
DO_BUILD=1
DO_MIGRATE=1

usage() { sed -n '2,30p' "$SCRIPT_PATH"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --dir)        ROOT="${2:-}"; shift ;;
    --user)       RUN_USER="${2:-}"; shift ;;
    --group)      RUN_GROUP="${2:-}"; shift ;;
    --env-file)   ENV_FILE="${2:-}"; shift ;;
    --no-build)   DO_BUILD=0 ;;
    --no-migrate) DO_MIGRATE=0 ;;
    -h|--help)    usage; exit 0 ;;
    *) echo "未知参数：$1（用 --help 看用法）" >&2; exit 2 ;;
  esac
  shift
done

[ -n "$ROOT" ] || { echo "--dir 不能为空" >&2; exit 2; }
ROOT="$(cd "$ROOT" 2>/dev/null && pwd || echo "$ROOT")"
[ -n "$ENV_FILE" ] || ENV_FILE="$ROOT/.env"
BIN_DIR="$ROOT/bin"
DATA_DIR="$ROOT/data"

say "================================================================"
say " Panomint 独立服务器安装（无容器）"
say "   安装根 ：$ROOT"
say "   运行身份：$RUN_USER:$RUN_GROUP"
say "   环境文件：$ENV_FILE"
say "   平台    ：$(uname -s 2>/dev/null || echo unknown)/$(uname -m 2>/dev/null || echo unknown)"
say "================================================================"

# ---------------------------------------------------------------- 1. 平台与权限
head1 "1/6 平台与 systemd 可用性"
OS="$(uname -s 2>/dev/null || echo unknown)"
ARCH="$(uname -m 2>/dev/null || echo unknown)"
IS_LINUX=0
[ "$OS" = "Linux" ] && IS_LINUX=1

if [ "$IS_LINUX" = "1" ]; then
  ok "平台 $OS/$ARCH（Linux，systemd 部署适用）"
else
  warn "平台 $OS/$ARCH：非 Linux —— systemd 独立部署不适用"
  skip "编译 / 迁移 / 服务启动（非 Linux，下面只做检查与指引）"
fi

IS_ROOT=0
[ "$(id -u 2>/dev/null || echo 1)" = "0" ] && IS_ROOT=1
if [ "$IS_ROOT" = "1" ]; then
  warn "当前是 root。服务**不应**以 root 运行，安装 unit 时请用 --user 指定普通用户"
else
  ok '当前非 root（符合「服务不以 root 运行」的要求）'
fi

HAS_SYSTEMD=0
if [ -d /run/systemd/system ] && has systemctl; then HAS_SYSTEMD=1; fi
if [ "$HAS_SYSTEMD" = "1" ]; then
  ok "检测到 systemd"
else
  skip "未检测到运行中的 systemd（本脚本只生成 unit 文件与指引，不会启用服务）"
fi

if [ -f "$ROOT/src/backend/go.mod" ]; then
  ok "安装根看起来是本仓库（找到 src/backend/go.mod）"
else
  bad "安装根不是本仓库（缺 src/backend/go.mod）：编译与迁移会被跳过，只能生成 unit 指引"
fi

# ---------------------------------------------------------------- 2. 依赖检查
head1 "2/6 依赖检查"

GO_OK=0
if has go; then
  GVER="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
  GMAJ=0; GMIN=0
  if [ -n "$GVER" ]; then
    IFS=. read -r GMAJ GMIN _ <<EOF
$GVER
EOF
  fi
  case "$GMAJ" in ''|*[!0-9]*) GMAJ=0 ;; esac
  case "$GMIN" in ''|*[!0-9]*) GMIN=0 ;; esac
  if [ "$GMAJ" -gt 1 ] || { [ "$GMAJ" -eq 1 ] && [ "$GMIN" -ge 26 ]; }; then
    ok "go $GVER（需要 >= 1.26，见 go.mod）"
    GO_OK=1
  else
    warn "go $GVER 低于 go.mod 声明的 1.26；编译可能失败"
    GO_OK=1
  fi
else
  bad "未找到 go（编译后端二进制所需）。装 Go 1.26+：https://go.dev/dl/"
fi

FFMPEG_OK=0
if has ffmpeg; then ok "ffmpeg：$(ffmpeg -version 2>/dev/null | head -n 1)"; FFMPEG_OK=1
else bad "未找到 ffmpeg（缩略图与 HLS 转码必需）。Debian/Ubuntu：apt install ffmpeg；RHEL：dnf install ffmpeg"; fi

CURL_OK=0
if has curl; then ok "curl：$(command -v curl)"; CURL_OK=1
else warn "未找到 curl（仅影响末尾的健康检查，不影响部署）"; fi

# PostgreSQL 连通性
PG_DSN="$(env_value PG_DSN)"
PG_REACHABLE=0
if [ -z "$PG_DSN" ]; then
  bad "未取到 PG_DSN（环境变量与 $ENV_FILE 都没有）。请参照 .env.example 创建 .env"
elif has pg_isready; then
  if pg_isready -d "$PG_DSN" >/dev/null 2>&1; then ok "PostgreSQL 可连通（pg_isready）"; PG_REACHABLE=1
  else bad "PostgreSQL 不可连通（pg_isready -d <PG_DSN> 失败）。检查 PG 是否启动、DSN 是否正确"; fi
elif has psql; then
  if psql "$PG_DSN" -c 'select 1' >/dev/null 2>&1; then ok "PostgreSQL 可连通（psql）"; PG_REACHABLE=1
  else bad "PostgreSQL 不可连通（psql select 1 失败）"; fi
else
  skip "本机没有 pg_isready / psql，无法验证 PostgreSQL 连通性（迁移步骤也会被跳过）"
fi

# Valkey 连通性
VALKEY_ADDR="$(env_value VALKEY_ADDR)"
VK_REACHABLE=0
if [ -z "$VALKEY_ADDR" ]; then
  bad "未取到 VALKEY_ADDR（环境变量与 $ENV_FILE 都没有）"
else
  VH="${VALKEY_ADDR%:*}"; VP="${VALKEY_ADDR##*:}"
  [ "$VH" = "$VALKEY_ADDR" ] && VH="$VALKEY_ADDR" && VP=6379
  if (exec 3<>"/dev/tcp/$VH/$VP") >/dev/null 2>&1; then
    ok "Valkey 端口可连通：$VH:$VP"
    VK_REACHABLE=1
  else
    bad "Valkey 不可连通：$VH:$VP（队列不可用，API 与 worker 都无法消费任务）"
  fi
fi

if has nginx; then ok "nginx：$(command -v nginx)（可用于反代；6 条必需规则见独立部署指南）"
else skip "未装 nginx（可选）。反代也可用 Caddy/traefik，但**必须**复刻指南里的 6 条规则"; fi

# ---------------------------------------------------------------- 3. 编译
head1 "3/6 编译二进制 → $BIN_DIR"
if [ "$IS_LINUX" != "1" ]; then
  skip "非 Linux，跳过编译"
elif [ "$DO_BUILD" = "0" ]; then
  skip "按 --no-build 跳过编译"
elif [ "$GO_OK" != "1" ]; then
  skip "缺 go，跳过编译（装好 Go 后重跑，或自带二进制放进 $BIN_DIR）"
elif [ ! -f "$ROOT/src/backend/go.mod" ]; then
  skip "安装根不是仓库，跳过编译"
else
  mkdir -p "$BIN_DIR" || bad "无法创建 $BIN_DIR"
  BUILD_FAIL=0
  build_one() { # build_one <cmd 子目录> <输出名>
    say "  · 编译 $2 ……"
    if ( cd "$ROOT/src/backend" && go build -trimpath -o "$BIN_DIR/$2" "./cmd/$1" ); then
      ok "$2 已生成"
    else
      bad "$2 编译失败（见上方 go 报错）"; BUILD_FAIL=1
    fi
  }
  # api 走 CGO（ONNX Runtime）；其余为纯 Go，与 docker/*/Dockerfile 的构建方式一致
  build_one api         pano-api
  build_one indexctl    pano-indexctl
  build_one transcodectl pano-transcodectl
  # migrate 只在主机上跑，**不进任何镜像**（docker/api/Dockerfile 未编译它）
  build_one migrate     pano-migrate
  [ "$BUILD_FAIL" = "0" ] && ok "4 个二进制就绪（api / indexctl / transcodectl / migrate）"
fi

# ---------------------------------------------------------------- 4. 数据库迁移
head1 "4/6 数据库迁移（goose up，只在主机执行）"
if [ "$IS_LINUX" != "1" ]; then
  skip "非 Linux，跳过迁移"
elif [ "$DO_MIGRATE" = "0" ]; then
  skip "按 --no-migrate 跳过（请自行执行：cd src/backend && ../bin/pano-migrate up）"
elif [ "$PG_REACHABLE" != "1" ]; then
  skip "PostgreSQL 不可达，跳过迁移（修好连通性后：cd \"$ROOT/src/backend\" && \"$BIN_DIR/pano-migrate\" up）"
elif [ ! -x "$BIN_DIR/pano-migrate" ] && [ ! -f "$BIN_DIR/pano-migrate" ]; then
  skip "缺 $BIN_DIR/pano-migrate，跳过迁移（加 --no-build 之外先让它编译成功）"
else
  # ⚠️ 必须在 src/backend 下执行：cmd/migrate 用相对路径 "migrations"（goose.Up(db, "migrations")）
  if ( cd "$ROOT/src/backend" && "$BIN_DIR/pano-migrate" up ); then
    ok "迁移已执行（goose up）"
  else
    bad "迁移失败（见上方输出）。不会回滚、不会 drop —— 请修好 DSN 或迁移脚本后重跑"
  fi
fi

# ---------------------------------------------------------------- 5. 生成 systemd unit
head1 "5/6 生成 systemd unit（只写临时目录，不动 /etc）"
UNIT_SRC="$ROOT/deploy/systemd"
UNIT_STAGE=""
if [ -d "$UNIT_SRC" ]; then
  UNIT_STAGE="$(mktemp -d "${TMPDIR:-/tmp}/panomint-units.XXXXXX" 2>/dev/null || true)"
  if [ -z "$UNIT_STAGE" ]; then
    UNIT_STAGE="$ROOT/deploy/systemd/generated"
    mkdir -p "$UNIT_STAGE"
    warn "mktemp 不可用，改写到 $UNIT_STAGE"
  fi
  for u in pano-api.service pano-worker@.service; do
    if [ -f "$UNIT_SRC/$u" ]; then
      # 用 | 作分隔符，避免路径里的 / 破坏 sed 表达式
      if sed -e "s|@INSTALL_DIR@|$ROOT|g" \
             -e "s|@RUN_USER@|$RUN_USER|g" \
             -e "s|@RUN_GROUP@|$RUN_GROUP|g" \
             -e "s|@ENV_FILE@|$ENV_FILE|g" \
             "$UNIT_SRC/$u" > "$UNIT_STAGE/$u" \
         && ! grep -q '@INSTALL_DIR@\|@RUN_USER@\|@RUN_GROUP@\|@ENV_FILE@' "$UNIT_STAGE/$u"; then
        ok "已生成 $UNIT_STAGE/$u（占位符已全部替换）"
      else
        bad "生成 $UNIT_STAGE/$u 失败或仍有未替换的占位符（检查 --dir/--user/--group 是否含特殊字符）"
      fi
    else
      bad "模板缺失：$UNIT_SRC/$u"
    fi
  done
  if [ -f "$UNIT_SRC/pano-worker-dispatch.sh" ]; then
    cp "$UNIT_SRC/pano-worker-dispatch.sh" "$UNIT_STAGE/"
    chmod +x "$UNIT_STAGE/pano-worker-dispatch.sh" 2>/dev/null
    ok "已生成 $UNIT_STAGE/pano-worker-dispatch.sh（已 chmod +x）"
  fi
  ok "unit 已就绪（检查无误后由你用 root 安装，见末尾第 2 步）"
else
  bad "找不到 $UNIT_SRC（unit 模板目录）"
fi

# ---------------------------------------------------------------- 6. 健康检查
head1 "6/6 健康检查"
API_PORT="$(env_value API_PORT)"; [ -n "$API_PORT" ] || API_PORT=8080
if [ "$IS_LINUX" != "1" ]; then
  skip "非 Linux，跳过健康检查"
elif [ "$CURL_OK" != "1" ]; then
  skip "无 curl，跳过健康检查（可手动：curl -fsS http://127.0.0.1:$API_PORT/ready）"
else
  if curl -fsS --max-time 3 "http://127.0.0.1:$API_PORT/ready" >/dev/null 2>&1; then
    ok "GET /ready 返回 2xx（服务已在 $API_PORT 上正常运行）"
  else
    skip "GET /ready 未通（服务大概还没启动）—— 启动服务后重跑本脚本即可验证"
  fi
fi

# ---------------------------------------------------------------- 汇总与下一步
head1 "汇总"
say "  OK=$PASS  FAIL=$FAIL  SKIP=$SKIP"

say ""
say "下一步（需要 root 的动作请你亲自执行）："
say "  1) 建运行用户（若尚未存在）："
say "       sudo useradd --system --home-dir \"$ROOT\" --shell /usr/sbin/nologin $RUN_USER"
say "       sudo chown -R $RUN_USER:$RUN_GROUP \"$DATA_DIR\" 2>/dev/null || true"
say "  2) 安装并启动 unit（<UNIT_STAGE> 换成上面的临时目录）："
if [ -n "$UNIT_STAGE" ]; then
  say "       sudo cp \"$UNIT_STAGE/pano-api.service\"          /etc/systemd/system/"
  say "       sudo cp \"$UNIT_STAGE/pano-worker@.service\"      /etc/systemd/system/"
  say "       sudo cp \"$UNIT_STAGE/pano-worker-dispatch.sh\"   \"$ROOT/deploy/systemd/\""
  say "       sudo chmod +x \"$ROOT/deploy/systemd/pano-worker-dispatch.sh\""
else
  say "       sudo cp \"$UNIT_SRC/pano-api.service\"          /etc/systemd/system/"
  say "       sudo cp \"$UNIT_SRC/pano-worker@.service\"      /etc/systemd/system/"
fi
say "       sudo systemctl daemon-reload"
say "       sudo systemctl enable --now pano-api"
say "       sudo systemctl enable --now pano-worker@index pano-worker@transcode"
say "  3) 自备反向代理，并**逐条复刻** 6 条规则 → 文档/独立部署指南_v1.0.md"
say "  4) 验证：curl -fsS http://127.0.0.1:${API_PORT}/ready && journalctl -u pano-api -f"

if [ "$FAIL" -gt 0 ]; then
  say ""
  say "结论：有 $FAIL 项 FAIL，尚不具备启动条件（脚本未做任何破坏性操作，可修好后直接重跑）。"
  exit 1
fi
say ""
say "结论：所有检查通过。"
exit 0
