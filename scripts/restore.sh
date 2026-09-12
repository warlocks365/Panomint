#!/usr/bin/env bash
# 全景相册恢复工具（Phase 6 T6.3）
#
# 配套 scripts/backup.sh。**仅供人工在恢复场景下使用**，默认全部为非破坏性操作。
#
# 用法：
#   scripts/restore.sh                       # 打印 6 步恢复手册（默认）
#   scripts/restore.sh list                  # 列出可用备份（含校验和/时间）
#   scripts/restore.sh verify <备份目录>     # 校验一份备份是否可用（不改动任何数据）
#   scripts/restore.sh db     <备份目录> [--target 库名]
#                                            # 把 dump 还原到**新库**（默认 pano_album_restore_<时间戳>）
#   scripts/restore.sh media  <备份目录> [--volume 卷名]
#                                            # 把媒体还原到**新卷**（默认 pano-album_mediadata_restore_<时间戳>）
#   scripts/restore.sh reconcile             # DB media.path ↔ 磁盘文件 对账（孤儿/缺失检测）
#
# 红线（脚本已内建保护）：
#   · 绝不执行 `docker compose down` / `down -v` / 卷 prune / 删除任何现有卷
#   · 还原一律写入**新库名 / 新卷名**，原库与原卷保持不动，便于比对与回退
#   · reconcile 只读

set -euo pipefail

COMPOSE_DIR="${COMPOSE_DIR:-/home/warlocks/pano-album}"
BACKUP_DIR="${BACKUP_DIR:-/home/backups/pano}"
PROJECT="${COMPOSE_PROJECT:-pano-album}"
MEDIA_ROOT_IN_CONTAINER="${MEDIA_ROOT_IN_CONTAINER:-/data/media}"

log() { printf '%s %s\n' "$(date -Is)" "$*"; }
die() { printf '%s ERROR %s\n' "$(date -Is)" "$*" >&2; exit 1; }

[ -d "$COMPOSE_DIR" ] || die "compose 目录不存在：$COMPOSE_DIR"
cd "$COMPOSE_DIR"

envget() { [ -f "$COMPOSE_DIR/.env" ] && sed -nE "s/^$1=(.*)$/\1/p" "$COMPOSE_DIR/.env" | head -n1 || true; }
PG_USER="$(envget POSTGRES_USER)"; PG_USER="${PG_USER:-pano}"
PG_DB="$(envget POSTGRES_DB)";     PG_DB="${PG_DB:-pano_album}"

psql_db() { docker compose exec -T db psql -U "$PG_USER" -d "$1" "${@:2}"; }

# ---------------------------------------------------------------- runbook ----
runbook() {
cat <<'EOF'
================================================================================
全景相册恢复手册（6 步；对应「113 条 DB 记录成孤儿」场景）
================================================================================
背景：历史事故中 `docker compose down -v` 重建了 mediadata 卷，媒体文件实体灭失，
      而 DB 中 113 条 media 记录仍在 → 记录成为孤儿。恢复必须**同时**救 DB 与文件，
      再对账，缺一不可。

第 1 步 · 停写入（**只 stop，绝不 down / down -v**）
    cd /home/warlocks/pano-album
    docker compose stop api index-worker transcode-worker embed-worker
    # db / valkey 保持运行，供后续 psql / pg_restore 使用

第 2 步 · 校验备份可用（不碰数据）
    scripts/restore.sh verify /home/backups/pano/<时间戳>

第 3 步 · 救 DB（还原到**新库**，原库不动）
    scripts/restore.sh db /home/backups/pano/<时间戳>
    # 产出：pano_album_restore_<时间戳>
    # 若确认无误、要正式切换：改 .env 的 POSTGRES_DB 指向新库 → docker compose up -d db
    # （更稳的做法是保留原库 volume 不删，仅改库名指向）

第 4 步 · 救文件（解到**新卷**，原卷绝不动）
    scripts/restore.sh media /home/backups/pano/<时间戳>
    # 产出：卷 pano-album_mediadata_restore_<时间戳>
    # 确认无误后，把 docker-compose.yml 中挂载的卷名指向新卷，再 docker compose up -d

第 5 步 · 对账（以 DB 的 media.path / hash 为准）
    scripts/restore.sh reconcile
    # 报告两类问题：
    #   A. DB 有记录、磁盘无文件 → 从备份重取该文件；或 indexctl scan 重扫媒体根重建记录
    #   B. 磁盘有文件、DB 无记录 → 用 indexctl scan -dir <媒体根> 重新入库
    #      （⚠️ 必须对**媒体根**扫描：media.path 存的是相对扫描根的路径）
    # 缩略图与 HLS 可由队列重跑，无需从备份回填：
    #   docker compose run --rm --no-deps --entrypoint /usr/local/bin/indexctl api \
    #     scan -dir /data/media      # 重新入库（缺记录的媒体）
    #   docker compose run --rm --no-deps --entrypoint /usr/local/bin/embedgen api \
    #     -mode encode               # 补算向量（增量清扫 worker 也会自动补）

第 6 步 · 起服务并验证
    docker compose up -d
    docker compose restart web      # ⚠️ 必做：nginx 启动时解析 upstream，不跟随 api 重建 → 否则 502
    curl -sS http://127.0.0.1:8088/ready    # 期望 {"status":"ready",...}

第 7 步（可选）· 演练建议
    每季度做一次：把备份还原到**临时库 + 临时卷**，抽查 10 条 media 文件可达，
    然后删除临时库/临时卷（删除的必须是本次新建的临时对象）。
================================================================================
EOF
}

# ------------------------------------------------------------------- list ----
cmd_list() {
  [ -d "$BACKUP_DIR" ] || die "备份目录不存在：$BACKUP_DIR"
  echo "备份根目录：$BACKUP_DIR"
  if [ -f "$BACKUP_DIR/LAST_SUCCESS" ]; then
    echo "最近一次成功：$(date -Is -d "@$(cat "$BACKUP_DIR/LAST_SUCCESS")")"
  else
    echo "最近一次成功：<无记录>"
  fi
  echo
  printf '%-20s %-10s %s\n' "时间戳" "大小" "校验和"
  for d in $(ls -1dt "$BACKUP_DIR"/*/ 2>/dev/null); do
    s="$(du -sh "$d" 2>/dev/null | cut -f1)"
    ok="缺 SHA256SUMS"
    [ -f "$d/SHA256SUMS" ] && ok="有"
    printf '%-20s %-10s %s\n' "$(basename "$d")" "$s" "$ok"
  done
  echo
  echo "周备份：$(ls -1d "$BACKUP_DIR"/weekly/*/ 2>/dev/null | wc -l) 份   月备份：$(ls -1d "$BACKUP_DIR"/monthly/*/ 2>/dev/null | wc -l) 份"
}

# ----------------------------------------------------------------- verify ----
cmd_verify() {
  local dir="${1:-}"; [ -n "$dir" ] || die "用法：restore.sh verify <备份目录>"
  [ -d "$dir" ] || die "备份目录不存在：$dir"
  log "校验 $dir"

  if [ -f "$dir/SHA256SUMS" ]; then
    ( cd "$dir" && sha256sum -c SHA256SUMS ) || die "SHA256SUMS 校验失败"
    log "SHA256SUMS 校验通过"
  else
    log "警告：无 SHA256SUMS，跳过校验和检查"
  fi

  if [ -f "$dir/pano_album.dump" ]; then
    local lst; lst="$(mktemp)"
    docker compose exec -T db pg_restore --list < "$dir/pano_album.dump" > "$lst" \
      || { rm -f "$lst"; die "pg_restore --list 无法解析 dump"; }
    log "dump 可解析：$(grep -c 'TABLE DATA' "$lst" || true) 个表数据段"
    rm -f "$lst"
  elif [ -f "$dir/backup.tar.gz.gpg" ]; then
    log "加密备份：需先 gpg --decrypt 后再校验"
  fi

  if [ -f "$dir/mediadata.tar.gz" ]; then
    tar -tzf "$dir/mediadata.tar.gz" > /dev/null || die "mediadata.tar.gz 无法读取"
    local n; n="$(tar -tzf "$dir/mediadata.tar.gz" | grep -cv '/$' || true)"
    log "mediadata.tar.gz 可读：$n 个文件条目"
  fi
  log "校验结束：备份可用"
}

# --------------------------------------------------------------------- db ----
cmd_db() {
  local dir="${1:-}" target="${2:-}"
  [ -n "$dir" ] || die "用法：restore.sh db <备份目录> [--target 库名]"
  local dump="$dir/pano_album.dump"
  [ -f "$dump" ] || die "未找到 $dump"
  target="${target:-pano_album_restore_$(date +%Y%m%d%H%M%S)}"

  docker compose ps --status running --services 2>/dev/null | grep -qx db \
    || die "db 容器未运行。请先 docker compose start db（严禁 down -v）"

  if psql_db postgres -tAc "SELECT 1 FROM pg_database WHERE datname='$target'" | grep -q 1; then
    die "目标库 $target 已存在，拒绝覆盖。请换 --target 名称"
  fi

  log "创建新库 $target（原库 $PG_DB 保持不动）"
  docker compose exec -T db createdb -U "$PG_USER" "$target"

  log "导入 dump 到 $target（--no-owner）"
  docker compose exec -T db pg_restore -U "$PG_USER" -d "$target" --no-owner < "$dump"

  local n; n="$(psql_db "$target" -tAc 'SELECT count(*) FROM media' | tr -d '[:space:]')"
  log "导入完成：$target 的 media 记录数 = $n"
  log "如需正式切换：把 .env 的 POSTGRES_DB 指向 $target 后 docker compose up -d db（原库卷勿删）"
}

# ------------------------------------------------------------------ media ----
cmd_media() {
  local dir="${1:-}" vol="${2:-}"
  [ -n "$dir" ] || die "用法：restore.sh media <备份目录> [--volume 卷名]"
  local tar="$dir/mediadata.tar.gz"
  [ -f "$tar" ] || die "未找到 $tar"
  vol="${vol:-${PROJECT}_mediadata_restore_$(date +%Y%m%d%H%M%S)}"

  if docker volume inspect "$vol" > /dev/null 2>&1; then
    die "目标卷 $vol 已存在，拒绝覆盖。请换 --volume 名称"
  fi

  log "创建新卷 $vol（原卷 ${PROJECT}_mediadata 绝不动）"
  docker volume create "$vol" > /dev/null

  log "解包到新卷（用 api 镜像做一次性容器，只读挂载备份目录）"
  docker compose run --rm --no-deps \
    -v "$vol":/restore \
    -v "$dir":/backup:ro \
    --entrypoint sh api -c 'tar -xzf /backup/mediadata.tar.gz -C /restore'

  local n; n="$(docker compose run --rm --no-deps -v "$vol":/restore --entrypoint sh api -c 'find /restore -type f | wc -l' | tr -d '[:space:]')"
  log "解包完成：$vol 共 $n 个文件"
  log "如需正式切换：把 docker-compose.yml 中 mediadata 的卷名指向 $vol，再 docker compose up -d（原卷勿删）"
}

# -------------------------------------------------------------- reconcile ----
cmd_reconcile() {
  local tmp; tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  docker compose ps --status running --services 2>/dev/null | grep -qx db || die "db 容器未运行"
  docker compose ps --status running --services 2>/dev/null | grep -qx api || die "api 容器未运行（reconcile 需读取媒体卷）"

  log "导出 DB media.path"
  psql_db "$PG_DB" -tAc 'SELECT path FROM media ORDER BY path' > "$tmp/db_paths.txt"
  log "导出磁盘文件清单（$MEDIA_ROOT_IN_CONTAINER）"
  docker compose exec -T api sh -c "cd '$MEDIA_ROOT_IN_CONTAINER' && find . -type f | sed 's|^\\./||'" > "$tmp/fs_files.txt"

  DB_N="$(grep -c . "$tmp/db_paths.txt" || true)"
  FS_N="$(grep -c . "$tmp/fs_files.txt" || true)"
  MISSING="$(comm -23 <(sort -u "$tmp/db_paths.txt") <(sort -u "$tmp/fs_files.txt") | grep -c . || true)"
  ORPHAN="$(comm -13 <(sort -u "$tmp/db_paths.txt") <(sort -u "$tmp/fs_files.txt") | grep -c . || true)"

  log "DB 记录=$DB_N  磁盘文件=$FS_N"
  log "A. DB 有记录但磁盘缺文件（孤儿记录）= $MISSING"
  comm -23 <(sort -u "$tmp/db_paths.txt") <(sort -u "$tmp/fs_files.txt") | head -n 10 | sed 's/^/    缺: /'
  log "B. 磁盘有文件但 DB 无记录（未入库文件）= $ORPHAN"
  comm -13 <(sort -u "$tmp/db_paths.txt") <(sort -u "$tmp/fs_files.txt") | head -n 10 | sed 's/^/    多: /'

  echo
  echo "处置建议："
  echo "  A → 从备份 media 卷中回填对应文件，或（确认文件永久丢失时）清理孤儿记录"
  echo "  B → docker compose run --rm --no-deps --entrypoint /usr/local/bin/indexctl api \\"
  echo "        scan -dir '$MEDIA_ROOT_IN_CONTAINER'    # ⚠️ 必须对媒体根扫描"
  if [ "$MISSING" = "0" ] && [ "$ORPHAN" = "0" ]; then
    log "对账通过：DB 与磁盘一致"
  fi
}

# ------------------------------------------------------------------ 入口 -----
case "${1:-help}" in
  help|-h|--help) runbook ;;
  list)      cmd_list ;;
  verify)    shift; cmd_verify "$@" ;;
  db)        shift; pos=(); tgt=""; while [ $# -gt 0 ]; do case "$1" in --target) tgt="$2"; shift 2 ;; *) pos+=("$1"); shift ;; esac; done; cmd_db "${pos[0]:-}" "$tgt" ;;
  media)     shift; pos=(); vol=""; while [ $# -gt 0 ]; do case "$1" in --volume) vol="$2"; shift 2 ;; *) pos+=("$1"); shift ;; esac; done; cmd_media "${pos[0]:-}" "$vol" ;;
  reconcile) cmd_reconcile ;;
  *) die "未知子命令：$1（试试 scripts/restore.sh help）" ;;
esac
