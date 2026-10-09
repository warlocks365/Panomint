#!/usr/bin/env bash
# deploy.sh —— 统一部署入口（取代「人工按顺序敲多个命令 / 多个 .py 脚本」）
#
# 存在的理由（2026-10-09 Job000145 事故复盘）
#   那次 api 镜像构建失败，从出事到被发现过了约 2 小时。根因不只是「没人盯着」，
#   更是**流程本身允许失败被静默**：
#     - 构建命令的退出码被管道吞掉（`| tail` → 失败也返回 0），屏幕显示「成功」；
#     - 部署要人工按顺序敲多个脚本，任何一步漏掉都不会有人知道；
#     - 没有台账，事后无法回答「线上现在跑的是哪个版本」。
#   本脚本把这三点从结构上关掉：
#     1. 任何命令都不用管道收尾 —— 退出码直接是第一手的；
#     2. 一个入口跑完 build → up → 验证，不需要人记顺序；
#     3. 无论成败都写台账 $DEPLOY_LEDGER，失败即告警（不等下一次巡检）。
#
# 用法
#   bash scripts/deploy.sh api                # 构建并重建 api（及其复用同镜像的 worker）
#   bash scripts/deploy.sh web
#   bash scripts/deploy.sh index-worker transcode-worker
#   bash scripts/deploy.sh --check-only api   # 只看会不会漂移，不动手
#   bash scripts/deploy.sh --accept api       # 部署成功后登记漂移基线
#   bash scripts/deploy.sh --status           # 打印台账与当前镜像状态
#
# 红线
#   - 不执行 `docker compose down`（更不会 down -v），不删镜像、不 prune 卷；
#   - 构建失败**绝不**自动重试或降级，直接失败退出 —— 静默重试会掩盖真问题；
#   - 只重建显式指定的服务，不碰其他容器。
#
# 环境
#   COMPOSE_DIR    默认 /home/warlocks/pano-album
#   DEPLOY_LEDGER  默认 /home/warlocks/.pano-ops/last_deploy.txt
#   DEPLOY_TIMEOUT 默认 3600（构建超时秒数）
#   ALERT_DRY_RUN=1 只演练不真部署

set -uo pipefail

COMPOSE_DIR="${COMPOSE_DIR:-/home/warlocks/pano-album}"
STATE_DIR="${STATE_DIR:-/home/warlocks/.pano-ops}"
DEPLOY_LEDGER="${DEPLOY_LEDGER:-$STATE_DIR/last_deploy.txt}"
DEPLOY_TIMEOUT="${DEPLOY_TIMEOUT:-3600}"
READY_URL="${READY_URL:-http://127.0.0.1:8088/ready}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/ops_alert.sh
. "$HERE/lib/ops_alert.sh"

mkdir -p "$STATE_DIR"

CHECK_ONLY=0
ACCEPT=0
STATUS=0
TARGETS=()
for arg in "$@"; do
  case "$arg" in
    --check-only) CHECK_ONLY=1 ;;
    --accept)     ACCEPT=1 ;;
    --status)     STATUS=1 ;;
    -*)           echo "未知参数: $arg" >&2; exit 2 ;;
    *)            TARGETS+=("$arg") ;;
  esac
done

if [ "$STATUS" = "1" ]; then
  echo "=== 部署台账 ($DEPLOY_LEDGER) ==="
  [ -f "$DEPLOY_LEDGER" ] && cat "$DEPLOY_LEDGER" || echo "(尚无部署记录)"
  echo
  echo "=== 当前镜像 ==="
  (cd "$COMPOSE_DIR" && docker compose ps --format '{{.Service}}|{{.State}}|{{.Image}}' 2>/dev/null)
  exit 0
fi

if [ "${#TARGETS[@]}" -eq 0 ]; then
  echo "用法: bash scripts/deploy.sh [--check-only|--accept|--status] <service> [service...]" >&2
  echo "可部署服务（compose 中带 build 段的）:" >&2
  (cd "$COMPOSE_DIR" && docker compose config --services 2>/dev/null | sed 's/^/  /') >&2
  exit 2
fi

log()  { printf '[deploy] %s\n' "$*"; }
fail() { printf '[deploy][FAIL] %s\n' "$*" >&2; }

# 台账：无论成败都写。写台账失败必须让部署失败 —— 否则等于没有台账。
write_ledger() {
  local status="$1" reason="${2:-}"
  local tmp="$DEPLOY_LEDGER.tmp.$$"
  {
    echo "status=$status"
    echo "targets=${TARGETS[*]}"
    echo "started_at=$STARTED_AT"
    echo "finished_at=$(date -Is)"
    echo "host=$(hostname)"
    echo "reason=$reason"
  } > "$tmp" 2>/dev/null || { fail "写台账失败: $DEPLOY_LEDGER"; return 1; }
  mv -f "$tmp" "$DEPLOY_LEDGER" 2>/dev/null || { fail "台账落盘失败: $DEPLOY_LEDGER"; return 1; }
  return 0
}

STARTED_AT="$(date -Is)"

# ---------------------------------------------------------------- 前置 ------
cd "$COMPOSE_DIR" || { fail "进不去 $COMPOSE_DIR"; exit 1; }

log "目标服务: ${TARGETS[*]}"
log "开始前置检查"

# 服务名存在性校验。
#
# ⚠️ 这里必须能区分「服务名真的不存在」与「compose 命令这次没返回结果」。
#    实测踩坑（2026-10-10）：`docker compose config --services` 偶发返回空
#    —— 大概与 compose 解析配置时的时序有关（同一命令连跑 5 次都正常，
#    但在连续构建后的某些时刻会空一次）。原实现写成
#        docker compose config --services | grep -qx "$svc" || 判为「拼错了」
#    于是**一次偶发空返回就把合法部署拦下来**，退出码 2，紧急回滚会被卡住。
#    这类误判最危险：它恰好在「最需要部署成功」的时刻阻止部署。
#
#    正确做法：先看命令本身有没有成功拿到名单，拿到但名单里没有才是拼错；
#    拿不到就重试，重试仍失败则**放行并告警**（宁可部署一次未知服务，
#    也不能因为一个探测命令的抖动就挡住回滚）。
svc_known() {
  local svc="$1" i out
  for i in 1 2 3; do
    out="$(docker compose config --services 2>/dev/null)"
    if [ -n "$out" ]; then
      printf '%s\n' "$out" | grep -qx "$svc"
      return $?
    fi
    sleep 1
  done
  # 三次都拿不到名单：无法判定，不阻塞部署
  echo "WARN:  docker compose config --services 连续 3 次无输出，无法校验服务名，放行" >&2
  return 0
}

for svc in "${TARGETS[@]}"; do
  if ! svc_known "$svc"; then
    fail "服务 $svc 不在 compose 定义里（拼错了？）"
    exit 2
  fi
done

# 记录构建前的镜像 ID，用于事后确认「确实换了镜像」而不是「以为换了」
declare -A BEFORE_IMG
for svc in "${TARGETS[@]}"; do
  tag="$(docker compose config --format json 2>/dev/null | jq -r --arg s "$svc" '.services[$s].image // empty')"
  [ -n "$tag" ] || tag="pano-album-$svc"
  BEFORE_IMG["$svc"]="$(docker image inspect "$tag" -f '{{.Id}}' 2>/dev/null | cut -c1-19)"
done

if [ "$CHECK_ONLY" = "1" ]; then
  log "--check-only：只报告，不构建"
  for svc in "${TARGETS[@]}"; do
    tag="$(docker compose config --format json 2>/dev/null | jq -r --arg s "$svc" '.services[$s].image // empty')"
    [ -n "$tag" ] || tag="pano-album-$svc"
    now="$(docker image inspect "$tag" -f '{{.Id}}' 2>/dev/null | cut -c1-19)"
    printf '  %-20s 镜像 %-28s 当前=%s\n' "$svc" "$tag" "${now:-<不存在>}"
  done
  exit 0
fi

# ---------------------------------------------------------------- 构建 ------
# ⚠️ 关键纪律：**不要**写成 `docker compose build ... | tee log` 或 `| tail`。
#    管道会把退出码换成管道最后一节的退出码，构建失败也会返回 0 —— 2026-10-09 就是这么
#    让失败「显示为成功」的。这里让命令直接在当前终端输出，退出码是第一手的。
log "开始构建（超时 ${DEPLOY_TIMEOUT}s，输出不接管道以保留真实退出码）"
build_started="$(date +%s)"

timeout --signal=TERM --kill-after=60 "$DEPLOY_TIMEOUT" \
  docker compose build "${TARGETS[@]}"
build_rc=$?
build_secs=$(( $(date +%s) - build_started ))

if [ "$build_rc" -ne 0 ]; then
  fail "构建失败，退出码 $build_rc（耗时 ${build_secs}s）"
  write_ledger failed "build_exit=$build_rc" || true
  send_alert "$(hostname) 构建失败：${TARGETS[*]}" \
    "部署在**构建阶段**失败，线上仍是旧版本。

服务: ${TARGETS[*]}
退出码: $build_rc
耗时: ${build_secs}s
开始: $STARTED_AT
主机: $(hostname)

完整构建输出见本次部署终端会话；构建日志未接管道（刻意如此：
管道会吞掉退出码，2026-10-09 的 2 小时盲区就是这么来的）。

排查建议:
  1. 直接重跑本脚本（同一条命令），构建输出会完整打在屏幕上；
  2. 若报 no required module provides package ...，是 BuildKit cache mount 损坏
     （docker builder prune -f 后 go mod 目录被改写），重跑通常自愈；
  3. 不要用 '| tail' / '| tee' 收尾，那会让失败返回 0。" || true
  exit "$build_rc"
fi

log "构建成功（耗时 ${build_secs}s）"

# ---------------------------------------------------------------- 启动 ------
log "重建容器: ${TARGETS[*]}"
# 同样不接管道：docker compose up 的退出码必须原样传出
docker compose up -d --no-deps "${TARGETS[@]}"
up_rc=$?
if [ "$up_rc" -ne 0 ]; then
  fail "容器重建失败，退出码 $up_rc"
  write_ledger failed "up_exit=$up_rc" || true
  send_alert "$(hostname) 容器重建失败：${TARGETS[*]}" \
    "构建成功但 docker compose up 失败。

服务: ${TARGETS[*]}
退出码: $up_rc
时间: $(date -Is)

镜像可能已构建好但容器没起来，请检查 docker compose logs。" || true
  exit "$up_rc"
fi

# ---------------------------------------------------------------- 验证 ------
log "验证镜像是否真的换了"
changed=0
for svc in "${TARGETS[@]}"; do
  tag="$(docker compose config --format json 2>/dev/null | jq -r --arg s "$svc" '.services[$s].image // empty')"
  [ -n "$tag" ] || tag="pano-album-$svc"
  after="$(docker image inspect "$tag" -f '{{.Id}}' 2>/dev/null | cut -c1-19)"
  before="${BEFORE_IMG[$svc]:-}"
  cid="$(docker compose ps -q "$svc" 2>/dev/null | head -1)"
  running="$( [ -n "$cid" ] && docker inspect -f '{{.Image}}' "$cid" 2>/dev/null | cut -c1-19 )"
  if [ "$after" = "$before" ]; then
    log "  $svc: 镜像未变化（$after）—— 构建命中缓存，产物与当前源码一致"
  else
    changed=1
    log "  $svc: 镜像已更新 $before -> $after，容器在跑 $running"
  fi
  if [ -n "$running" ] && [ -n "$after" ] && [ "$running" != "$after" ]; then
    fail "  $svc: 容器仍在跑 $running，与镜像 $after 不一致"
    write_ledger failed "container_image_mismatch:$svc" || true
    send_alert "$(hostname) 容器未跟随新镜像：$svc" \
      "镜像已更新但容器仍在跑旧 image ID。

服务: $svc
镜像现为: $after
容器在跑: $running
时间: $(date -Is)

这会导致「重建了但没生效」的假象。请执行：
  cd $COMPOSE_DIR && docker compose up -d --force-recreate $svc" || true
    exit 1
  fi
done

log "验证 /ready（含 postgres/valkey）"
ready_ok=0
tries=0
while [ "$tries" -lt 30 ]; do
  tries=$(( tries + 1 ))
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$READY_URL" 2>/dev/null || echo 000)"
  if [ "$code" = "200" ]; then ready_ok=1; break; fi
  sleep 2
done

if [ "$ready_ok" -ne 1 ]; then
  fail "/ready 在 60s 内未恢复（最近 http=$code）"
  write_ledger failed "ready_not_ok_http=$code" || true
  send_alert "$(hostname) 部署后 /ready 未恢复：${TARGETS[*]}" \
    "容器已重建，但 /ready 60 秒内未返回 200。

服务: ${TARGETS[*]}
最近状态码: $code
时间: $(date -Is)

/ready 包含 disk/postgres/valkey 检查，通常是依赖未就绪或 api 启动迁移卡住。
排查: docker compose logs --tail=80 ${TARGETS[0]}" || true
  exit 1
fi

log "/ready 正常"

# ---------------------------------------------------------------- 收尾 ------
write_ledger success "changed=$changed" || exit 1
log "部署成功，台账已写入 $DEPLOY_LEDGER"

if [ "$ACCEPT" = "1" ]; then
  log "登记漂移基线"
  bash "$HERE/ops_drift_watch.sh" --accept || fail "基线登记失败"
fi

log "完成。${TARGETS[*]} 已部署并通过 /ready 验证。"
exit 0
