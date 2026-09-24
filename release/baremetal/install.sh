#!/usr/bin/env bash
# Panomint 裸机一键安装（v1.0.0，Job000106 形态一）
#
# 职责边界（诚实降级：做不到的事明说，不假装成功）：
#   做：装系统依赖（Debian 12 / Ubuntu 22.04+，PGDG 源）、建系统用户、落盘产物、
#       建库建号、写 .env、跑迁移、装 systemd 单元并启动。
#   不做：不配置反向代理（TLS/域名是你的决策，装完打印指引）；不删任何东西；
#         非 Debian/Ubuntu 系统直接诚实失败并给出依赖清单让你手工装。
#
# 用法（root）：
#   bash baremetal/install.sh [--prefix /opt/panomint] [--user pano] \
#        [--db auto|external] [--yes]
#     --db auto      自动安装并初始化本机 PostgreSQL 16 + PostGIS + pgvector（默认）
#     --db external  使用外部数据库（须交互输入或通过环境变量 PG_DSN 提供）
set -uo pipefail

VERSION="__VERSION__"   # build.sh 不替换它：安装时从产物目录名/二进制自报为准
say()  { printf '\n=== %s ===\n' "$*"; }
ok()   { printf '  [ OK ] %s\n' "$*"; }
bad()  { printf '  [FAIL] %s\n' "$*" >&2; }
die()  { printf '[install] 错误：%s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- 参数
PREFIX=/opt/panomint
RUN_USER=pano
DB_MODE=auto
ASSUME_YES=0
while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) PREFIX="${2:?}"; shift ;;
    --user)   RUN_USER="${2:?}"; shift ;;
    --db)     DB_MODE="${2:?}"; shift ;;
    --yes)    ASSUME_YES=1 ;;
    -h|--help) sed -n '2,22p' "$0"; exit 0 ;;
    *) die "未知参数 $1（--help 看用法）" ;;
  esac
  shift
done
[ "$DB_MODE" = auto ] || [ "$DB_MODE" = external ] || die "--db 只接受 auto|external"

# ---------------------------------------------------------------- 前置
say "1/8 平台预检"
[ "$(id -u)" = "0" ] || die "本脚本需要 root（装系统包与 systemd 单元）。集成包形态无需 root：见 bundle/"
[ "$(uname -s)" = "Linux" ] || die "仅支持 Linux"
[ "$(uname -m)" = "x86_64" ] || die "amd64 之外暂不承诺（arm64 见 部署方案与兼容性 §11）"
[ -d /run/systemd/system ] || die "未检测到运行中的 systemd（面板/NAS 环境请用 bundle 形态）"
. /etc/os-release
case "$ID" in
  debian|ubuntu) ok "发行版 $ID $VERSION_ID（自动装依赖适用）" ;;
  *) die "自动装依赖仅支持 Debian/Ubuntu；当前 $ID。请手工装 PG16+PostGIS+pgvector+Valkey+ffmpeg 后改用 --db external，并重跑" ;;
esac

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
SRC_ROOT="$(cd "$SELF_DIR/.." && pwd)"
for d in bin web assets; do [ -d "$SRC_ROOT/$d" ] || die "产物目录缺 $d（请在解包后的目录内运行）"; done
ok "产物目录 $SRC_ROOT"

# ---------------------------------------------------------------- 系统依赖
say "2/8 系统依赖（apt，幂等）"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
if [ "$DB_MODE" = auto ] && ! dpkg -l postgresql-16 >/dev/null 2>&1; then
  say "  配置 PGDG 源（PostgreSQL 官方 apt 仓库）"
  apt-get install -y -qq curl ca-certificates gnupg
  install -d /usr/share/postgresql-common/pgdg
  curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc
  echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] http://apt.postgresql.org/pub/repos/apt $(. /etc/os-release && echo "$VERSION_CODENAME")-pgdg main" \
    > /etc/apt/sources.list.d/pgdg.list
  apt-get update -qq
fi
PKGS="postgresql-16 postgresql-16-postgis-3 postgresql-16-pgvector valkey ffmpeg curl"
for p in $PKGS; do
  if dpkg -l "$p" >/dev/null 2>&1; then ok "$p 已装"
  else apt-get install -y -qq "$p" || die "$p 安装失败（网络/源问题，修好重跑即可，本脚本幂等）"; fi
done

# ---------------------------------------------------------------- 用户与目录
say "3/8 运行用户与安装目录"
if ! id "$RUN_USER" >/dev/null 2>&1; then
  useradd --system --home-dir "$PREFIX" --shell /usr/sbin/nologin "$RUN_USER"
  ok "创建用户 $RUN_USER"
else ok "用户 $RUN_USER 已存在"; fi
mkdir -p "$PREFIX"
# 幂等重装：先清旧的 bin/web/assets（数据目录 data/ 与 .env 永远保留）
rm -rf "$PREFIX/bin" "$PREFIX/web" "$PREFIX/assets" "$PREFIX/baremetal"
cp -a "$SRC_ROOT/bin" "$SRC_ROOT/web" "$SRC_ROOT/assets" "$PREFIX/"
cp -a "$SELF_DIR" "$PREFIX/baremetal"
mkdir -p "$PREFIX"/data/{media,uploads,hls,thumbnails} "$PREFIX"/var/log
chown -R "$RUN_USER:$RUN_USER" "$PREFIX/data" "$PREFIX/var"
ok "产物已落盘 $PREFIX（数据目录保留旧数据）"

# ---------------------------------------------------------------- 数据库
say "4/8 数据库"
DB_PASSWORD=""
if [ "$DB_MODE" = auto ]; then
  systemctl enable --now postgresql >/dev/null 2>&1 || die "postgresql 启动失败"
  DB_PASSWORD="$(openssl rand -hex 16)"
  sudo -u postgres psql -v ON_ERROR_STOP=1 <<SQL || die "建库建号失败"
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'pano') THEN
    CREATE ROLE pano LOGIN PASSWORD '$DB_PASSWORD';
  ELSE
    ALTER ROLE pano PASSWORD '$DB_PASSWORD';
  END IF;
END
\$\$;
SQL
  if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='pano_album'" | grep -q 1; then
    sudo -u postgres createdb -O pano pano_album || die "createdb 失败"
  fi
  sudo -u postgres psql -d pano_album -v ON_ERROR_STOP=1 -c "CREATE EXTENSION IF NOT EXISTS postgis; CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS pg_trgm;" \
    || die "扩展安装失败（postgis/pgvector）"
  PG_DSN="postgres://pano:$DB_PASSWORD@127.0.0.1:5432/pano_album"
  ok "本机库就绪：pano_album（postgis+vector+pg_trgm）"
else
  if [ -n "${PG_DSN:-}" ]; then ok "使用环境变量 PG_DSN"
  else printf '请输入外部 PostgreSQL DSN（postgres://user:pass@host:5432/dbname）：'; read -r PG_DSN; fi
  [ -n "$PG_DSN" ] || die "external 模式必须提供 PG_DSN"
fi

# ---------------------------------------------------------------- .env
say "5/8 生成 $PREFIX/.env"
if [ -f "$PREFIX/.env" ]; then
  ok ".env 已存在（保留旧配置；如需重生成请删除后重跑）"
else
  JWT_SECRET="$(openssl rand -hex 32)"
  CIPHER_KEY="$(openssl rand -hex 32)"
  cat > "$PREFIX/.env" <<EOF
APP_ENV=prod
API_PORT=8080
PG_DSN=$PG_DSN
VALKEY_ADDR=127.0.0.1:6379
VALKEY_PASSWORD=
JWT_SECRET=$JWT_SECRET
UPLOAD_DIR=$PREFIX/data/media
UPLOAD_TMP=$PREFIX/data/uploads
HLS_DIR=$PREFIX/data/hls
MEDIA_ROOT=$PREFIX/data/media
THUMB_DIR=$PREFIX/data/thumbnails
# 允许的跨域源：默认同源部署无需任何跨域；前后端分离调试时再填
CORS_ORIGINS=
# 可信反代：装了本机 nginx 反代保持默认即可
TRUSTED_PROXIES=127.0.0.1
# 挂载凭据加密密钥（32 字节 hex；不使用网络挂载可留空）
STORAGE_CIPHER_KEY=$CIPHER_KEY
# AI 运行时（资产已随包）
EMBED_LIB=$PREFIX/assets/lib/onnxruntime-linux-x64-1.29.0/lib/libonnxruntime.so
EMBED_MODEL_DIR=$PREFIX/assets/models/chinese-clip
EMBED_FAMILY=chinese-clip
EMBED_DEVICE=auto
FACE_MODEL_DIR=$PREFIX/assets/models/faces
# 高德 Key（空=地图搜索降级；Web 服务类型 Key，详见部署指南）
AMAP_KEY=
AMAP_SECRET=
# 自动迁移：默认开。迁移幂等；off 仅供排障
AUTO_MIGRATE=true
EOF
  chmod 600 "$PREFIX/.env"
  chown "$RUN_USER:$RUN_USER" "$PREFIX/.env"
  ok ".env 已生成（600）"
fi

# ---------------------------------------------------------------- 迁移
say "6/8 数据库迁移（幂等）"
runuser -u "$RUN_USER" -- sh -c "cd '$PREFIX' && bin/pano-migrate up" || die "迁移失败（DSN 问题？修 .env 后重跑）"
ok "迁移完成"

# ---------------------------------------------------------------- systemd
say "7/8 systemd 单元"
for u in pano-api.service pano-worker@.service pano-ai@.service; do
  sed -e "s|@INSTALL_DIR@|$PREFIX|g" -e "s|@RUN_USER@|$RUN_USER|g" -e "s|@RUN_GROUP@|$RUN_USER|g" \
      "$PREFIX/baremetal/$u" > "/etc/systemd/system/$u"
  grep -q '@INSTALL_DIR@' "/etc/systemd/system/$u" && die "单元 $u 占位符未替换净"
done
systemctl daemon-reload
for s in pano-api pano-worker@index pano-worker@transcode pano-ai@embed pano-ai@tag pano-ai@faces pano-ai@phash; do
  systemctl enable --now "$s" >/dev/null 2>&1 || bad "$s 启动失败（看 journalctl -u $s）"
done
ok "单元已安装并启动"

# ---------------------------------------------------------------- 验证
say "8/8 健康验证"
for i in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:8080/ready >/dev/null 2>&1; then ok "GET /ready 通过"; break; fi
  [ "$i" = 30 ] && bad "/ready 30s 未通（服务可能仍在启动，稍后再验）"
  sleep 1
done
V="$(curl -fsS http://127.0.0.1:8080/version 2>/dev/null || echo '{}')"
echo "$V" | grep -q '"version"' && echo "  /version → $V"

say "安装完成"
cat <<EOF

  访问入口 : http://<本机IP>:8080  （首次访问自动进入初始化向导，创建管理员）
  数据目录 : $PREFIX/data（备份它 = 备份全部媒体）
  日志     : journalctl -u pano-api -f
  反代/TLS : 请按 文档/独立部署指南_v1.0.md 的 6 条规则配置 nginx/Caddy
             （注意：新增 /setup、/version 两个 API 前缀必须进反代正则组）

EOF
exit 0
