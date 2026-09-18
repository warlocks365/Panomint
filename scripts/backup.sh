#!/usr/bin/env bash
# 全景相册备份（Phase 6 T6.3 运维可观测性）
#
# 覆盖对象：
#   ① PostgreSQL    —— pg_dump -Fc 一致性逻辑快照 + --schema-only DDL（永久保留语义）
#   ② mediadata 卷  —— 媒体原文件 + 缩略图 + HLS（**红线要求，必须纳入备份**）
#   ③ 配置与证书    —— .env / docker-compose.yml / docker/（含 caddy 泛域名证书与私钥）
#
# 用法：
#   scripts/backup.sh                     # 直接执行（cron 友好）
#
# 可覆盖的环境变量（均有默认值）：
#   COMPOSE_DIR            compose 项目目录        默认 /home/warlocks/pano-album
#   BACKUP_DIR             备份根目录              默认 /home/backups/pano
#                          ⚠️ 只要目标分区可用空间 ≥ MIN_FREE_GB 即可（示例环境把它放在 /home）；
#                             落在根分区（单分区/数据即根分区的机器）只告警，不阻塞。
#   KEEP_DAILY/WEEKLY/MONTHLY  保留份数            默认 14 / 8 / 6
#   MIN_FREE_GB            执行前最小可用空间(GB)  默认 10
#   BACKUP_GPG_RECIPIENT   gpg 收件人；设置后整体加密为 <stamp>.tar.gz.gpg
#
# ① 幂等可重入：以 flock 串行化，重复触发（如 cron 重叠）会安全跳过。
# ② 红线：本脚本**只读**业务数据；绝不执行 `compose down`、`down -v`、卷 prune
#        或任何删除卷/容器的操作。清理仅作用于 $BACKUP_DIR 下的历史备份目录。

set -euo pipefail

COMPOSE_DIR="${COMPOSE_DIR:-/home/warlocks/pano-album}"
BACKUP_DIR="${BACKUP_DIR:-/home/backups/pano}"
KEEP_DAILY="${KEEP_DAILY:-14}"
KEEP_WEEKLY="${KEEP_WEEKLY:-8}"
KEEP_MONTHLY="${KEEP_MONTHLY:-6}"
MIN_FREE_GB="${MIN_FREE_GB:-10}"
PROJECT="${COMPOSE_PROJECT:-pano-album}"
MEDIA_VOLUME="${MEDIA_VOLUME:-${PROJECT}_mediadata}"

STAMP="$(date +%Y%m%d-%H%M%S)"
DOW="$(date +%u)"   # 1..7（7 = 周日）
DOM="$(date +%d)"

log() { printf '%s %s\n' "$(date -Is)" "$*"; }
die() { printf '%s ERROR %s\n' "$(date -Is)" "$*" >&2; exit 1; }

[ -d "$COMPOSE_DIR" ] || die "compose 目录不存在：$COMPOSE_DIR"
cd "$COMPOSE_DIR"

# ---- 0) 串行锁 + 空间门禁 + 目标分区守卫 ----------------------------------
mkdir -p "$BACKUP_DIR"
exec 9>"$BACKUP_DIR/.backup.lock"
if ! flock -n 9; then
  log "已有备份实例在运行，本次跳过（幂等）"
  exit 0
fi

DEST_FS="$(df -P "$BACKUP_DIR" | tail -n1 | awk '{print $6}')"
# 备份目录"与数据同盘"不算错误：单分区机器（数据即根分区）只有 / 可用，禁用备份反而更糟。
# 故这里只告警，**不阻塞**；真正的门禁是下面的可用空间检查。
if [ "$DEST_FS" = "/" ]; then
  log "警告：备份目录落在根分区 $BACKUP_DIR —— 备份与业务数据同盘，磁盘/机器故障时两者会一起丢；建议放到独立磁盘或 NAS（BACKUP_DIR=<独立盘路径>）"
fi

AVAIL_GB="$(df -BG --output=avail "$BACKUP_DIR" | tail -n1 | tr -dc '0-9')"
[ -n "$AVAIL_GB" ] || die "无法读取 $BACKUP_DIR 可用空间"
[ "$AVAIL_GB" -ge "$MIN_FREE_GB" ] || die "磁盘余量不足：${AVAIL_GB}G < ${MIN_FREE_GB}G，中止备份"

envget() { [ -f "$COMPOSE_DIR/.env" ] && sed -nE "s/^$1=(.*)$/\1/p" "$COMPOSE_DIR/.env" | head -n1 || true; }
PG_USER="$(envget POSTGRES_USER)"; PG_USER="${PG_USER:-pano}"
PG_DB="$(envget POSTGRES_DB)";     PG_DB="${PG_DB:-pano_album}"

O="$BACKUP_DIR/$STAMP"
mkdir -p "$O"
umask 077

log "开始备份 stamp=$STAMP dest=$O（源：PG=$PG_DB / 卷=$MEDIA_VOLUME）"

# ---- 1) PostgreSQL 逻辑快照 + DDL -----------------------------------------
docker compose ps --status running --services 2>/dev/null | grep -qx db \
  || die "db 容器未运行。请先执行 docker compose start db（严禁 down -v）"

docker compose exec -T db pg_dump -U "$PG_USER" -d "$PG_DB" -Fc --no-owner > "$O/pano_album.dump"
log "PG 逻辑备份完成：$(du -h "$O/pano_album.dump" | cut -f1)"

docker compose exec -T db pg_dump -U "$PG_USER" -d "$PG_DB" --schema-only --no-owner > "$O/schema.sql"
log "DDL schema 导出完成：$(du -h "$O/schema.sql" | cut -f1)"

# ---- 2) mediadata 卷（原文件 + 缩略图 + HLS）------------------------------
# 优先借用运行中的 api 容器（免拉镜像）；api 未运行时退化为一次性容器（--no-deps，不触碰卷）。
# 媒体原文件为写入后不变（write-once），缩略图/HLS 可能被 worker 并发写入，
# 故对 tar 的 "file changed as we read it" 警告放行，改由后面的 tar -tzf 做完整性判定。
MEDIA_TAR_RC=0
if docker compose ps --status running --services 2>/dev/null | grep -qx api; then
  log "打包 mediadata 卷（via 运行中的 api 容器）"
  docker compose exec -T api tar -czf - -C /data . --warning=no-file-changed > "$O/mediadata.tar.gz" || MEDIA_TAR_RC=$?
else
  log "api 未运行，改用一次性容器读取卷（只读，不触碰卷）"
  docker compose run --rm --no-deps --entrypoint tar api \
    -czf - -C /data . --warning=no-file-changed > "$O/mediadata.tar.gz" || MEDIA_TAR_RC=$?
fi
[ "$MEDIA_TAR_RC" -le 1 ] || die "mediadata 打包失败（tar rc=$MEDIA_TAR_RC）"
[ "$MEDIA_TAR_RC" -eq 1 ] && log "警告：tar 返回 1（并发写入导致文件变化），将以 tar -tzf 复核完整性"
log "mediadata 打包完成：$(du -h "$O/mediadata.tar.gz" | cut -f1)"

# ---- 3) 配置与证书（含密钥 → 必须 600）------------------------------------
CFG_INPUTS=()
[ -f "$COMPOSE_DIR/.env" ] && CFG_INPUTS+=(".env")
[ -f "$COMPOSE_DIR/docker-compose.yml" ] && CFG_INPUTS+=("docker-compose.yml")
[ -d "$COMPOSE_DIR/docker" ] && CFG_INPUTS+=("docker")
[ "${#CFG_INPUTS[@]}" -gt 0 ] || die "未找到任何配置项（.env / docker-compose.yml / docker/）"
tar -czf "$O/config.tar.gz" -C "$COMPOSE_DIR" "${CFG_INPUTS[@]}"
log "配置与证书打包完成（含 .env 与 docker/caddy/certs，权限 600）"

# ---- 4) 完整性校验 ---------------------------------------------------------
docker compose exec -T db pg_restore --list < "$O/pano_album.dump" > /dev/null \
  || die "pg_restore --list 无法解析 dump，备份不可用"
tar -tzf "$O/mediadata.tar.gz" > /dev/null \
  || die "mediadata.tar.gz 无法读取，备份不可用"

MEDIA_TAR_ENTRIES="$(tar -tzf "$O/mediadata.tar.gz" | grep -cv '/$' || true)"
MEDIA_DB_COUNT="$(docker compose exec -T db psql -U "$PG_USER" -d "$PG_DB" -tAc 'SELECT count(*) FROM media' | tr -d '[:space:]')"
log "校验：卷内文件条目=$MEDIA_TAR_ENTRIES（含原文件/缩略图/HLS），DB media 记录=$MEDIA_DB_COUNT"

# ---- 5) 可选加密（gpg）-----------------------------------------------------
if [ -n "${BACKUP_GPG_RECIPIENT:-}" ]; then
  command -v gpg >/dev/null || die "设置了 BACKUP_GPG_RECIPIENT 但未安装 gpg"
  ( cd "$O" && tar -czf ../"$STAMP".tar.gz . && gpg --batch --yes --encrypt \
      --recipient "$BACKUP_GPG_RECIPIENT" -o ../"$STAMP".tar.gz.gpg ../"$STAMP".tar.gz )
  mv "$BACKUP_DIR/$STAMP.tar.gz.gpg" "$O/backup.tar.gz.gpg"
  rm -f "$BACKUP_DIR/$STAMP.tar.gz" "$O/pano_album.dump" "$O/schema.sql" \
        "$O/mediadata.tar.gz" "$O/config.tar.gz"
  log "已整包 gpg 加密：$O/backup.tar.gz.gpg"
fi

( cd "$O" && sha256sum ./* > SHA256SUMS )
( cd "$O" && sha256sum -c SHA256SUMS > /dev/null ) || die "SHA256SUMS 自校验失败"
log "SHA256SUMS 生成并自校验通过"

# ---- 6) 分级保留（硬链接晋升，不额外占空间）--------------------------------
if [ "$DOW" = "7" ]; then mkdir -p "$BACKUP_DIR/weekly" && cp -al "$O" "$BACKUP_DIR/weekly/$STAMP"; log "已晋升为周备份"; fi
if [ "$DOM" = "01" ]; then mkdir -p "$BACKUP_DIR/monthly" && cp -al "$O" "$BACKUP_DIR/monthly/$STAMP"; log "已晋升为月备份"; fi

prune() {
  local dir="$1" keep="$2"
  [ -d "$dir" ] || return 0
  ls -1dt "$dir"/*/ 2>/dev/null | tail -n +$((keep + 1)) | xargs -r rm -rf
}
prune "$BACKUP_DIR"         "$KEEP_DAILY"
prune "$BACKUP_DIR/weekly"  "$KEEP_WEEKLY"
prune "$BACKUP_DIR/monthly" "$KEEP_MONTHLY"

# ---- 7) 成功标记（供 ops_check.sh 判定新鲜度）------------------------------
date +%s > "$BACKUP_DIR/LAST_SUCCESS"
printf '%s OK %s size=%s\n' "$(date -Is)" "$STAMP" "$(du -sh "$O" | cut -f1)" >> "$BACKUP_DIR/backup.log"

log "备份完成：$O（$(du -sh "$O" | cut -f1)）"
log "保留策略：日 $KEEP_DAILY / 周 $KEEP_WEEKLY / 月 $KEEP_MONTHLY（异地：scripts/pull-backup.sh 每周拉回开发机）"
