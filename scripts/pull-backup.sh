#!/usr/bin/env bash
# 异地备份拉取（Phase 6 T6.3）——在**开发机（Windows + Git Bash）**上运行
#
# 背景：测试服本地备份无法抵御磁盘/整机故障，需每周把最新一份备份拉回本机归档。
# 本脚本复用现有 SFTP 链路，零新增设施：
#   .workbuddy/ssh_config.json   SSH 凭据（已 gitignore）
#   .workbuddy/ssh_exec.py       远程命令
#   .workbuddy/sftp_get.py       单文件下载
#
# 用法：
#   scripts/pull-backup.sh                # 拉取服务器上最新一份备份
#   scripts/pull-backup.sh --stamp 20260912-120000   # 拉取指定时间戳
#   scripts/pull-backup.sh --list         # 只列出服务器上现有备份
#
# 归档位置：<仓库>/.workbuddy/backups/<时间戳>/   （.workbuddy/ 已被 .gitignore 覆盖）
# 保留策略：本机保留最近 KEEP 份（默认 8，约两个月）
#
# 建议每周执行一次（Windows 任务计划程序 / 手动）。执行完由 sha256sum -c 本地复核。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKBUDDY="$ROOT/.workbuddy"
PY="${PY:-/c/Users/warlocks/.workbuddy/binaries/python/envs/default/Scripts/python.exe}"
SSH_EXEC="$WORKBUDDY/ssh_exec.py"
SFTP_GET="$WORKBUDDY/sftp_get.py"

REMOTE_DIR="${REMOTE_DIR:-/home/backups/pano}"
LOCAL_DIR="${LOCAL_DIR:-$WORKBUDDY/backups}"
KEEP="${KEEP:-8}"

log() { printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }
die() { printf '%s ERROR %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >&2; exit 1; }

# Windows 原生程序（python.exe）不认 MSYS 风格路径 /c/...，含中文时 MSYS 也不会自动转换；
# 故凡是要传给 python 的路径，一律先转成 Windows 形式（C:\...）。
winpath() {
  local p="$1"
  case "$p" in
    /*)
      if command -v cygpath >/dev/null 2>&1; then
        local w; w="$(cygpath -w "$p" 2>/dev/null)" && { printf '%s' "$w"; return; }
      fi
      printf '%s' "$p"
      ;;
    *) printf '%s' "$p" ;;
  esac
}

[ -x "$PY" ] || PY="python"          # 兜底：交给 PATH 上的 python
[ -f "$SSH_EXEC" ] || die "缺少 $SSH_EXEC"
[ -f "$SFTP_GET" ] || die "缺少 $SFTP_GET"
[ -f "$WORKBUDDY/ssh_config.json" ] || die "缺少 $WORKBUDDY/ssh_config.json（SSH 凭据）"

PY_W="$(winpath "$PY")"
SSH_EXEC_W="$(winpath "$SSH_EXEC")"
SFTP_GET_W="$(winpath "$SFTP_GET")"

# 远程执行并剥离 ssh_exec.py 附加的 [exit=N] 行
rexec() { "$PY_W" "$SSH_EXEC_W" warlocks "$1" 2>/dev/null | grep -v '^\[exit=' || true; }

STAMP=""
case "${1:-}" in
  --list)
    log "服务器 $REMOTE_DIR 下的备份："
    rexec "ls -1dt $REMOTE_DIR/*/ 2>/dev/null | xargs -r -n1 basename"
    exit 0
    ;;
  --stamp) STAMP="${2:-}"; [ -n "$STAMP" ] || die "用法：pull-backup.sh --stamp <时间戳>" ;;
  "") : ;;
  *) die "未知参数：$1（试试 --list / --stamp <时间戳>）" ;;
esac

if [ -z "$STAMP" ]; then
  log "查询服务器最新备份…"
  STAMP="$(rexec "ls -1dt $REMOTE_DIR/*/ 2>/dev/null | head -n1 | xargs -r -n1 basename" | tr -d '\r' | head -n1)"
  [ -n "$STAMP" ] || die "服务器上未找到任何备份（$REMOTE_DIR）"
fi

REMOTE_STAMP_DIR="$REMOTE_DIR/$STAMP"
DEST="$LOCAL_DIR/$STAMP"

log "目标备份：$REMOTE_STAMP_DIR"
FILES="$(rexec "ls -1 $REMOTE_STAMP_DIR" | tr -d '\r' | grep -v '^$' || true)"
[ -n "$FILES" ] || die "远程目录为空或不存在：$REMOTE_STAMP_DIR"

mkdir -p "$DEST"
DEST_W="$(winpath "$DEST")"
log "拉取 $(printf '%s\n' "$FILES" | wc -l | tr -d ' ') 个文件 → $DEST"
while IFS= read -r f; do
  [ -n "$f" ] || continue
  log "  ↓ $f"
  "$PY_W" "$SFTP_GET_W" "$REMOTE_STAMP_DIR/$f" "$DEST_W\\$f" > /dev/null
done <<< "$FILES"

# 本地完整性复核
if [ -f "$DEST/SHA256SUMS" ]; then
  log "本地校验 SHA256SUMS…"
  ( cd "$DEST" && sha256sum -c SHA256SUMS ) || die "本地校验失败，备份可能损坏"
  log "校验通过"
else
  log "警告：该备份无 SHA256SUMS，跳过校验"
fi

TOTAL="$(du -sh "$DEST" | cut -f1)"
log "完成：$DEST（$TOTAL）"

# 本机保留策略
prune() {
  local dir="$1" keep="$2"
  [ -d "$dir" ] || return 0
  ls -1dt "$dir"/*/ 2>/dev/null | tail -n +$((keep + 1)) | xargs -r rm -rf
}
prune "$LOCAL_DIR" "$KEEP"
log "本机保留最近 $KEEP 份（当前 $(ls -1d "$LOCAL_DIR"/*/ 2>/dev/null | wc -l | tr -d ' ') 份）"
