#!/usr/bin/env bash
# ============================================================================
# Panomint docker-compose 交互式生成器（Job000130 配套工具）
#
# 用途：通过交互式问答收集必填/可选变量，自动生成
#   1) 通用部署编排  panomint-compose/docker-compose.yml + .env
#   2) 群晖 NAS 专用编排  panomint-compose/docker-compose.synology.yml（bind 路径版）
#
# 用法：
#   bash scripts/generate-compose.sh            # 交互式
#   bash scripts/generate-compose.sh --help     # 帮助
#
# 输出目录：当前目录下 panomint-compose/（不覆盖已有文件，除非加 --force）
# 生成后：cd panomint-compose && docker compose up -d
# ============================================================================
set -euo pipefail

FORCE=0
[[ "${1:-}" == "--force" ]] && FORCE=1
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  sed -n '2,14p' "$0"
  exit 0
fi

C_ASK="\033[1;36m"; C_OK="\033[1;32m"; C_WARN="\033[1;33m"; C_ERR="\033[1;31m"; C_END="\033[0m"
ask() { printf "${C_ASK}%s${C_END}" "$1"; }
ok()  { printf "${C_OK}%s${C_END}\n" "$1"; }
warn(){ printf "${C_WARN}%s${C_END}\n" "$1"; }
die() { printf "${C_ERR}错误：%s${C_END}\n" "$1" >&2; exit 1; }

OUTDIR="panomint-compose"
[[ -d "$OUTDIR" && $FORCE -eq 0 ]] && die "目录 ./$OUTDIR 已存在（加 --force 覆盖）"
mkdir -p "$OUTDIR"

echo "=============================================="
echo " Panomint docker-compose 配置生成器"
echo " 必填项会强制校验；可选项直接回车用默认值"
echo "=============================================="

# ---------- 必填 1：版本 ----------
ask "1/6 镜像版本号 [默认 1.7.1]（格式 X.Y.Z）："
read -r VER
VER="${VER:-1.7.1}"
[[ "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "版本号须为 X.Y.Z 格式，得到：$VER"
ok "  版本 = $VER"

# ---------- 必填 2：数据库口令 ----------
DEF_PW=$(openssl rand -hex 16 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' | cut -c1-32)
ask "2/6 数据库口令 [回车自动生成强随机]："
read -r PW
PW="${PW:-$DEF_PW}"
[[ ${#PW} -ge 12 ]] || die "数据库口令至少 12 位（当前 ${#PW} 位）。已取消，未生成任何文件。"
[[ "$PW" =~ ^[A-Za-z0-9_@#%^+\-]+$ ]] || warn "  口令含特殊字符，已自动改为强随机口令以避免连接串转义问题"
[[ "$PW" =~ ^[A-Za-z0-9_@#%^+\-]+$ ]] || PW="$DEF_PW"
ok "  数据库口令已设置（长度 ${#PW}）"

# ---------- 必填 3：JWT 密钥 ----------
ask "3/6 JWT 签名密钥 [回车自动生成]："
read -r JWT
JWT="${JWT:-$(openssl rand -hex 32 2>/dev/null || head -c 64 /dev/urandom | od -An -tx1 | tr -d ' \n')}"
[[ ${#JWT} -ge 32 ]] || die "JWT 密钥至少 32 字符。已取消，未生成任何文件。"
ok "  JWT 密钥已设置（长度 ${#JWT}）"

# ---------- 可选 4：HTTP 端口 ----------
ask "4/6 Web 访问端口 [默认 8088]："
read -r PORT
PORT="${PORT:-8088}"
[[ "$PORT" =~ ^[0-9]+$ ]] && (( PORT >= 1 && PORT <= 65535 )) || die "端口须为 1-65535 的数字，得到：$PORT"
ok "  端口 = $PORT"

# ---------- 可选 5：高德 Key（地名逆地理编码；留空则降级为无地名） ----------
ask "5/6 高德 Web 服务 Key [可选，回车跳过]："
read -r AMAP_KEY
AMAP_SECRET=""
if [[ -n "$AMAP_KEY" ]]; then
  ask "    高德 Secret [可选]："
  read -r AMAP_SECRET
  ok "  高德 Key 已设置"
else
  ok "  跳过（地图仍可用，地点名为空）"
fi

# ---------- 模式选择：通用 / 群晖 ----------
ask "6/6 生成模式：1) 通用 Docker  2) 群晖 DSM（bind 路径）  3) 两者都生成 [默认 1]："
read -r MODE
MODE="${MODE:-1}"
[[ "$MODE" =~ ^[123]$ ]] || die "模式须为 1/2/3"

MEDIA_DIR="/volume1/photo"
if [[ "$MODE" == "2" || "$MODE" == "3" ]]; then
  ask "   群晖照片目录 [默认 /volume1/photo]："
  read -r MEDIA_DIR
  MEDIA_DIR="${MEDIA_DIR:-/volume1/photo}"
  [[ "$MEDIA_DIR" == /* ]] || die "媒体目录须为绝对路径（以 / 开头）"
  MEDIA_DIR="${MEDIA_DIR%/}"
  [[ -d "$MEDIA_DIR" ]] || warn "  目录 $MEDIA_DIR 当前不存在（NAS 上需真实存在）"
  ok "  照片目录 = $MEDIA_DIR"
fi

# ---------- 生成通用 compose ----------
gen_generic() {
  cat > "$OUTDIR/docker-compose.yml" <<YAML
# Panomint 通用部署编排（由 scripts/generate-compose.sh 生成于 $(date '+%F %T')）
# 版本 $VER ｜ 访问端口 $PORT
# 启动：docker compose up -d   首次访问 http://<主机IP>:$PORT 进入初始化向导
name: panomint

services:
  db:
    image: warlocks/panomint-db:$VER
    container_name: panomint-db
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      POSTGRES_USER: pano
      POSTGRES_PASSWORD: "$PW"
      POSTGRES_DB: pano_album
      PGDATA: /var/lib/postgresql/data/pgdata
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U pano -d pano_album"]
      interval: 5s
      timeout: 3s
      retries: 12

  valkey:
    image: valkey/valkey:8.0-alpine
    container_name: panomint-valkey
    restart: unless-stopped
    command: ["valkey-server", "--appendonly", "yes", "--maxmemory-policy", "noeviction"]
    volumes:
      - valkeydata:/data
    healthcheck:
      test: ["CMD", "valkey-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 12

  api:
    image: warlocks/panomint-app:$VER
    container_name: panomint-api
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      API_PORT: "8080"
      APP_ENV: prod
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      VALKEY_ADDR: valkey:6379
      JWT_SECRET: "$JWT"
      MEDIA_ROOT: /data/media
      UPLOAD_DIR: /data/media
      UPLOAD_TMP: /data/uploads
      HLS_DIR: /data/hls
      THUMB_DIR: /data/thumbnails
      TRUSTED_PROXIES: 172.16.0.0/12
      AMAP_KEY: "$AMAP_KEY"
      AMAP_SECRET: "$AMAP_SECRET"
    volumes:
      - appdata:/data
    depends_on:
      db:
        condition: service_healthy
      valkey:
        condition: service_healthy

  embed-worker:
    image: warlocks/panomint-app:$VER
    container_name: panomint-embed-worker
    restart: unless-stopped
    entrypoint: ["/usr/local/bin/embedgen", "-mode", "watch", "-interval", "30"]
    environment:
      TZ: Asia/Shanghai
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      THUMB_DIR: /data/thumbnails
      EMBED_FAMILY: chinese-clip
      EMBED_MODEL_DIR: /opt/models/chinese-clip
      EMBED_DEVICE: auto
    volumes:
      - appdata:/data
    depends_on:
      db:
        condition: service_healthy

  tag-worker:
    image: warlocks/panomint-app:$VER
    container_name: panomint-tag-worker
    restart: unless-stopped
    entrypoint: ["/usr/local/bin/taggen", "-mode", "watch", "-interval", "60"]
    environment:
      TZ: Asia/Shanghai
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      THUMB_DIR: /data/thumbnails
      EMBED_FAMILY: chinese-clip
      EMBED_MODEL_DIR: /opt/models/chinese-clip
      EMBED_DEVICE: auto
    volumes:
      - appdata:/data
    depends_on:
      db:
        condition: service_healthy

  phash-worker:
    image: warlocks/panomint-app:$VER
    container_name: panomint-phash-worker
    restart: unless-stopped
    entrypoint: ["/usr/local/bin/phashgen", "-mode", "watch", "-interval", "30"]
    environment:
      TZ: Asia/Shanghai
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      THUMB_DIR: /data/thumbnails
    volumes:
      - appdata:/data
    depends_on:
      db:
        condition: service_healthy

  faces-worker:
    image: warlocks/panomint-app:$VER
    container_name: panomint-faces-worker
    restart: unless-stopped
    entrypoint: ["/usr/local/bin/facesgen", "-mode", "watch", "-interval", "30"]
    environment:
      TZ: Asia/Shanghai
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      MEDIA_ROOT: /data/media
      FACE_MODEL_DIR: /opt/models/faces
      FACE_DEVICE: ""
      EMBED_DEVICE: auto
      FACE_MERGE_SIM: "0.40"
    volumes:
      - appdata:/data
    depends_on:
      db:
        condition: service_healthy

  index-worker:
    image: warlocks/panomint-worker:$VER
    container_name: panomint-index-worker
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      WORKER_KIND: index
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      VALKEY_ADDR: valkey:6379
      FFMPEG_PATH: /usr/bin
      THUMB_DIR: /data/thumbnails
      MEDIA_ROOT: /data/media
      HLS_DIR: /data/hls
    volumes:
      - appdata:/data
    depends_on:
      db:
        condition: service_healthy
      valkey:
        condition: service_healthy

  transcode-worker:
    image: warlocks/panomint-worker:$VER
    container_name: panomint-transcode-worker
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      WORKER_KIND: transcode
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      VALKEY_ADDR: valkey:6379
      FFMPEG_PATH: /usr/bin
      THUMB_DIR: /data/thumbnails
      MEDIA_ROOT: /data/media
      HLS_DIR: /data/hls
    volumes:
      - appdata:/data
    depends_on:
      db:
        condition: service_healthy
      valkey:
        condition: service_healthy

  web:
    image: warlocks/panomint-web:$VER
    container_name: panomint-web
    restart: unless-stopped
    ports:
      - "$PORT:80"
    depends_on:
      - api

volumes:
  pgdata:
  valkeydata:
  appdata:
YAML
}

# ---------- 生成群晖 compose ----------
gen_synology() {
  cat > "$OUTDIR/docker-compose.synology.yml" <<YAML
# Panomint 群晖 DSM Container Manager 编排（由 scripts/generate-compose.sh 生成于 $(date '+%F %T')）
# 版本 $VER ｜ 访问端口 $PORT ｜ 照片目录 $MEDIA_DIR
# 用法：Container Manager → 项目 → 新增 → 项目路径 /volume1/docker/panomint → 粘贴本文件
# 注意：db/valkey/appdata 数据持久化在 /volume1/docker/panomint 下（可按存储池全局替换）
name: panomint

services:
  db:
    image: warlocks/panomint-db:$VER
    container_name: panomint-db
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      POSTGRES_USER: pano
      POSTGRES_PASSWORD: "$PW"
      POSTGRES_DB: pano_album
      PGDATA: /var/lib/postgresql/data/pgdata
    volumes:
      - /volume1/docker/panomint/db:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U pano -d pano_album"]
      interval: 5s
      timeout: 3s
      retries: 12

  valkey:
    image: valkey/valkey:8.0-alpine
    container_name: panomint-valkey
    restart: unless-stopped
    command: ["valkey-server", "--appendonly", "yes", "--maxmemory-policy", "noeviction"]
    environment:
      TZ: Asia/Shanghai
    volumes:
      - /volume1/docker/panomint/valkey:/data
    healthcheck:
      test: ["CMD", "valkey-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 12

  api:
    image: warlocks/panomint-app:$VER
    container_name: panomint-api
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      API_PORT: "8080"
      APP_ENV: prod
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      VALKEY_ADDR: valkey:6379
      JWT_SECRET: "$JWT"
      MEDIA_ROOT: /data/media
      UPLOAD_DIR: /data/media
      UPLOAD_TMP: /data/uploads
      HLS_DIR: /data/hls
      THUMB_DIR: /data/thumbnails
      TRUSTED_PROXIES: 172.16.0.0/12
      AMAP_KEY: "$AMAP_KEY"
      AMAP_SECRET: "$AMAP_SECRET"
    volumes:
      - /volume1/docker/panomint/data:/data
      - $MEDIA_DIR:/data/media
      - /volume1/docker/panomint/tile-cache:/tile-cache
    depends_on:
      db:
        condition: service_healthy
      valkey:
        condition: service_healthy

  embed-worker:
    image: warlocks/panomint-app:$VER
    container_name: panomint-embed-worker
    restart: unless-stopped
    entrypoint: ["/usr/local/bin/embedgen", "-mode", "watch", "-interval", "30"]
    environment:
      TZ: Asia/Shanghai
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      THUMB_DIR: /data/thumbnails
      EMBED_FAMILY: chinese-clip
      EMBED_MODEL_DIR: /opt/models/chinese-clip
      EMBED_DEVICE: auto
    volumes:
      - /volume1/docker/panomint/data:/data
      - $MEDIA_DIR:/data/media
    depends_on:
      db:
        condition: service_healthy

  tag-worker:
    image: warlocks/panomint-app:$VER
    container_name: panomint-tag-worker
    restart: unless-stopped
    entrypoint: ["/usr/local/bin/taggen", "-mode", "watch", "-interval", "60"]
    environment:
      TZ: Asia/Shanghai
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      THUMB_DIR: /data/thumbnails
      EMBED_FAMILY: chinese-clip
      EMBED_MODEL_DIR: /opt/models/chinese-clip
      EMBED_DEVICE: auto
    volumes:
      - /volume1/docker/panomint/data:/data
      - $MEDIA_DIR:/data/media
    depends_on:
      db:
        condition: service_healthy

  phash-worker:
    image: warlocks/panomint-app:$VER
    container_name: panomint-phash-worker
    restart: unless-stopped
    entrypoint: ["/usr/local/bin/phashgen", "-mode", "watch", "-interval", "30"]
    environment:
      TZ: Asia/Shanghai
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      THUMB_DIR: /data/thumbnails
    volumes:
      - /volume1/docker/panomint/data:/data
      - $MEDIA_DIR:/data/media
    depends_on:
      db:
        condition: service_healthy

  faces-worker:
    image: warlocks/panomint-app:$VER
    container_name: panomint-faces-worker
    restart: unless-stopped
    entrypoint: ["/usr/local/bin/facesgen", "-mode", "watch", "-interval", "30"]
    environment:
      TZ: Asia/Shanghai
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      MEDIA_ROOT: /data/media
      FACE_MODEL_DIR: /opt/models/faces
      FACE_DEVICE: ""
      EMBED_DEVICE: auto
      FACE_MERGE_SIM: "0.40"
    volumes:
      - /volume1/docker/panomint/data:/data
      - $MEDIA_DIR:/data/media
    depends_on:
      db:
        condition: service_healthy

  index-worker:
    image: warlocks/panomint-worker:$VER
    container_name: panomint-index-worker
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      WORKER_KIND: index
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      VALKEY_ADDR: valkey:6379
      FFMPEG_PATH: /usr/bin
      THUMB_DIR: /data/thumbnails
      MEDIA_ROOT: /data/media
      HLS_DIR: /data/hls
    volumes:
      - /volume1/docker/panomint/data:/data
      - $MEDIA_DIR:/data/media
    depends_on:
      db:
        condition: service_healthy
      valkey:
        condition: service_healthy

  transcode-worker:
    image: warlocks/panomint-worker:$VER
    container_name: panomint-transcode-worker
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      WORKER_KIND: transcode
      PG_DSN: postgres://pano:$PW@db:5432/pano_album
      VALKEY_ADDR: valkey:6379
      FFMPEG_PATH: /usr/bin
      THUMB_DIR: /data/thumbnails
      MEDIA_ROOT: /data/media
      HLS_DIR: /data/hls
    volumes:
      - /volume1/docker/panomint/data:/data
      - $MEDIA_DIR:/data/media
    depends_on:
      db:
        condition: service_healthy
      valkey:
        condition: service_healthy

  web:
    image: warlocks/panomint-web:$VER
    container_name: panomint-web
    restart: unless-stopped
    ports:
      - "$PORT:80"
    depends_on:
      - api
YAML
}

if [[ "$MODE" == "1" || "$MODE" == "3" ]]; then gen_generic;  ok "已生成 $OUTDIR/docker-compose.yml"; fi
if [[ "$MODE" == "2" || "$MODE" == "3" ]]; then gen_synology; ok "已生成 $OUTDIR/docker-compose.synology.yml"; fi

cat > "$OUTDIR/README.md" <<MD
# Panomint 部署编排（由 generate-compose.sh 生成）

- 镜像版本：$VER
- 访问端口：$PORT（首次访问进入初始化向导）
- 模型资产：先在仓库根执行 \`bash scripts/fetch-all-assets.sh\`（离线机见该脚本说明）

## 启动

\`\`\`bash
cd $(basename "$(pwd)")
docker compose -f docker-compose.yml up -d          # 通用 Docker
# 群晖：Container Manager 导入 docker-compose.synology.yml
\`\`\`

## 安全提示

本目录含数据库口令与 JWT 密钥（明文），请：
- 不要提交到任何版本库；
- 权限收紧：\`chmod 600 .env docker-compose*.yml\`；
- 泄露后立即轮换（JWT 轮换会强制全部重新登录）。
MD
ok "已生成 $OUTDIR/README.md（含安全提示）"

echo ""
ok "全部完成。下一步：cd $OUTDIR && docker compose up -d"
echo "     AI 模型资产：仓库根 bash scripts/fetch-all-assets.sh"
