#!/usr/bin/env bash
# Panomint 集成包首次配置（v1.0.0，Job000106 形态二）
#
# 与裸机形态的区别：不装系统包、不写系统目录、不需要 root。
# 它只做三件事：生成 .env、初始化数据目录、跑数据库迁移。
# 外部依赖（PostgreSQL/Valkey/ffmpeg）由你自带 —— 可先 ./bundle/panoctl env-check 自检。
#
# 用法：
#   ./bundle/install.sh                 交互式（逐项提问，回车取默认）
#   ./bundle/install.sh --non-interactive \
#        --pg-dsn 'postgres://user:pass@host:5432/db' [--jwt-secret <hex>] [--port 8080]
set -uo pipefail

BASE="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/.." && pwd)"
cd "$BASE" || exit 9
ENV_FILE="$BASE/.env"

say() { printf '%s\n' "$*"; }
die() { printf '[install] 错误：%s\n' "$*" >&2; exit 1; }

PG_DSN_IN=""; PORT_IN=""; JWT_IN=""
NON_INTERACTIVE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --non-interactive) NON_INTERACTIVE=1 ;;
    --pg-dsn)     PG_DSN_IN="${2:?}"; shift ;;
    --port)       PORT_IN="${2:?}"; shift ;;
    --jwt-secret) JWT_IN="${2:?}"; shift ;;
    -h|--help)    sed -n '2,16p' "$0"; exit 0 ;;
    *) die "未知参数 $1" ;;
  esac
  shift
done

[ -x "$BASE/bin/pano-api" ] || die "bin/pano-api 不存在（请在解包后的目录内运行）"

if [ -f "$ENV_FILE" ]; then
  say "[skip] .env 已存在（保留旧配置；重配请删除后重跑）"
else
  # ---------------- 采集 ----------------
  if [ "$NON_INTERACTIVE" = 1 ]; then
    [ -n "$PG_DSN_IN" ] || die "--non-interactive 必须给 --pg-dsn"
    PG_DSN="$PG_DSN_IN"
  else
    printf 'PostgreSQL DSN（postgres://user:pass@host:5432/dbname）：'
    read -r PG_DSN
    [ -n "$PG_DSN" ] || die "PG_DSN 不能为空"
  fi
  API_PORT="${PORT_IN:-8080}"
  JWT_SECRET="$JWT_IN"
  [ -n "$JWT_SECRET" ] || JWT_SECRET="$(openssl rand -hex 32 2>/dev/null || python3 -c 'import secrets;print(secrets.token_hex(32))')"
  CIPHER_KEY="$(openssl rand -hex 32 2>/dev/null || python3 -c 'import secrets;print(secrets.token_hex(32))')"

  cat > "$ENV_FILE" <<EOF
APP_ENV=prod
API_PORT=$API_PORT
PG_DSN=$PG_DSN
VALKEY_ADDR=127.0.0.1:6379
VALKEY_PASSWORD=
JWT_SECRET=$JWT_SECRET
UPLOAD_DIR=$BASE/data/media
UPLOAD_TMP=$BASE/data/uploads
HLS_DIR=$BASE/data/hls
MEDIA_ROOT=$BASE/data/media
THUMB_DIR=$BASE/data/thumbnails
CORS_ORIGINS=
TRUSTED_PROXIES=127.0.0.1
STORAGE_CIPHER_KEY=$CIPHER_KEY
EMBED_LIB=$BASE/assets/lib/onnxruntime-linux-x64-1.29.0/lib/libonnxruntime.so
EMBED_MODEL_DIR=$BASE/assets/models/chinese-clip
EMBED_FAMILY=chinese-clip
EMBED_DEVICE=auto
FACE_MODEL_DIR=$BASE/assets/models/faces
AMAP_KEY=
AMAP_SECRET=
AUTO_MIGRATE=true
EOF
  chmod 600 "$ENV_FILE"
  say "[ OK ] .env 已生成（600）"
fi

mkdir -p "$BASE"/data/{media,uploads,hls,thumbnails} "$BASE"/var/{log,pids}
say "[ OK ] 数据目录就位"

# ---------------- 迁移（幂等；迁移源已编译进二进制，无需 SQL 目录随行） ----------------
if ./bin/pano-migrate up; then
  say "[ OK ] 数据库迁移完成"
else
  die "迁移失败（检查 PG_DSN 与网络；修 .env 后重跑本脚本）"
fi

say ""
say "配置完成。下一步："
say "  1) ./bundle/panoctl env-check     环境自检"
say "  2) ./bundle/panoctl start         启动全部进程"
say "  3) 浏览器打开 http://<本机IP>:${PORT_IN:-8080} —— 首次访问自动进入初始化向导"
exit 0
