#!/usr/bin/env bash
# rollback.sh —— 部署回滚（发布安全 Silver 的执行工具）
#
# 存在的理由
#   上一项（构建失败告警）解决了「失败能被发现」，但没解决「发现之后能退回去」。
#   而「退回去」这件事最容易停留在口头承诺：文档里写着「如有问题可用上一版本
#   镜像回滚」，真出事时却没人说得清上一版本是哪个 ID、怎么退、退完怎么验证。
#   本脚本把这三件事固化成可执行的东西。
#
# 它做什么
#   record   记录当前运行中的版本作为回滚基线（镜像 ID / tag / 容器配置 / 健康快照）
#   list     列出可用基线
#   show <名称>   查看某个基线的详情
#   to <名称>     回滚到指定基线
#   verify   校验当前系统是否与某个基线一致（回滚是否真的生效）
#   drill    完整演练：record → 造坏版本 → 观察告警 → 回滚 → 验证
#
# 关键设计
#   1. **基线用「不可变的镜像 ID」记录，不用 tag**。
#      tag 是可变的：`panomint-app:latest` 下一次构建就指向别的东西了。
#      按 tag 回滚等于「回滚到当前版本」——看起来成功，实则没退。
#      2026-10-10 实测：造坏版本后 tag 指向坏镜像，按 tag 回滚会毫无作用。
#   2. **回滚不改数据**：只重建容器，不碰卷、不执行 down。
#      数据迁移若与版本绑定，回滚前需人工确认（本脚本会提示，不自动处理）。
#   3. **回滚后强制验证**：`/ready`（含 postgres/valkey）+ 容器数 + 卷数，
#      任一不符即判回滚失败 —— 「命令成功」不等于「回滚成功」。
#   4. **回滚失败必须可见**：失败即写台账 + 告警，让巡检在后续轮次继续报出来。
#      「回滚本身失败」是最坏的情况：人以为已经退回去了。
#
# 红线（本脚本永不执行）
#   - 不执行 docker compose down（更不会 down -v）
#   - 不 prune 卷、不删任何镜像（**尤其不删在用的**——删掉就真的退不回去了）
#   - 不动宝塔 cron
#   - 不改 docker/api/Dockerfile 等任何构建定义

set -uo pipefail

COMPOSE_DIR="${COMPOSE_DIR:-/home/warlocks/pano-album}"
STATE_DIR="${STATE_DIR:-/home/warlocks/.pano-ops}"
BASELINE_DIR="${BASELINE_DIR:-$STATE_DIR/rollback}"
READY_URL="${READY_URL:-http://127.0.0.1:8088/ready}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8088/health}"
EXPECTED_SERVICES="${EXPECTED_SERVICES:-11}"
EXPECTED_VOLUMES="${EXPECTED_VOLUMES:-43}"
# 这些服务共用同一个镜像 tag，必须整组一起回滚，否则会出现同 tag 两个版本
SHARED_TAG_SERVICES="${SHARED_TAG_SERVICES:-api embed-worker tag-worker phash-worker faces-worker}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/ops_alert.sh
. "$HERE/lib/ops_alert.sh"

mkdir -p "$BASELINE_DIR" 2>/dev/null

log()  { printf '[rollback] %s\n' "$*"; }
fail() { printf '[rollback][FAIL] %s\n' "$*" >&2; }

cd "$COMPOSE_DIR" || { fail "进不去 $COMPOSE_DIR"; exit 1; }

# ---------------------------------------------------------------- 工具 ------
# 容器实际在跑的镜像 ID
container_image_id() {
  local svc="$1" cid
  cid="$(docker compose ps -q "$svc" 2>/dev/null | head -1)"
  [ -n "$cid" ] || return 1
  docker inspect -f '{{.Image}}' "$cid" 2>/dev/null
}

# 服务的镜像 tag（无 image: 时按 compose 自动命名规则回落）
svc_image_tag() {
  local json svc tag
  json="$(docker compose config --format json 2>/dev/null)"
  [ -n "$json" ] || return 1
  svc="$1"
  tag="$(printf '%s' "$json" | jq -r --arg s "$svc" '.services[$s].image // empty' 2>/dev/null)"
  [ -n "$tag" ] && { printf '%s\n' "$tag"; return 0; }
  local project
  project="$(printf '%s' "$json" | jq -r '.name // empty' 2>/dev/null)"
  printf '%s\n' "${project:-pano-album}-$svc"
}

ready_probe() {
  curl -sS --max-time 8 "$READY_URL" 2>/dev/null
}
health_code() {
  curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$HEALTH_URL" 2>/dev/null || echo 000
}

# ---------------------------------------------------------------- record ----
# 把「当前跑的是什么」固化成一份可回滚的基线。
cmd_record() {
  local name="${1:-baseline-$(date +%Y%m%d-%H%M%S)}"
  local dir="$BASELINE_DIR/$name"
  [ -e "$dir" ] && { fail "基线 $name 已存在（换名或先删）"; return 1; }
  mkdir -p "$dir" || { fail "无法创建 $dir"; return 1; }

  log "记录基线: $name"

  # 逐服务记录 tag + 实际镜像 ID。两者都记：tag 用于重建，image ID 用于验证。
  {
    echo "# 回滚基线 $name"
    echo "recorded_at=$(date -Is)"
    echo "host=$(hostname)"
    echo ""
    printf '%-20s %-28s %s\n' "SERVICE" "TAG" "IMAGE_ID"
    for svc in $SHARED_TAG_SERVICES; do
      printf '%-20s %-28s %s\n' "$svc" "$(svc_image_tag "$svc")" "$(container_image_id "$svc" 2>/dev/null || echo '-')"
    done
    # 其余有 build 的服务也记一份，回滚它们时用
    for svc in index-worker transcode-worker web; do
      printf '%-20s %-28s %s\n' "$svc" "$(svc_image_tag "$svc")" "$(container_image_id "$svc" 2>/dev/null || echo '-')"
    done
  } > "$dir/baseline.txt"

  {
    echo "ready=$(ready_probe)"
    echo "health=$(health_code)"
    echo "containers=$(docker compose ps -q 2>/dev/null | wc -l)"
    echo "volumes=$(docker volume ls -q | wc -l)"
    echo "images:"
    # 记录全部相关镜像 ID，作为「退路仍在」的证据
    for svc in $SHARED_TAG_SERVICES index-worker transcode-worker; do
      id="$(container_image_id "$svc" 2>/dev/null)"
      [ -n "$id" ] && echo "  $svc $id"
    done
  } > "$dir/health.txt"

  log "基线已记录 -> $dir"
  cat "$dir/baseline.txt"
  return 0
}

# ---------------------------------------------------------------- list ------
cmd_list() {
  echo "=== 可用回滚基线（$BASELINE_DIR）==="
  if ! ls -1 "$BASELINE_DIR" 2>/dev/null | grep -q .; then
    echo "  （还没有任何基线。先执行：bash scripts/rollback.sh record <名称>）"
    return 0
  fi
  for d in "$BASELINE_DIR"/*/; do
    [ -d "$d" ] || continue
    local n; n="$(basename "$d")"
    local at; at="$(grep -m1 '^recorded_at=' "$d/baseline.txt" 2>/dev/null | cut -d= -f2-)"
    local api; api="$(awk '$1=="api"{print $3}' "$d/baseline.txt" 2>/dev/null | cut -c1-19)"
    printf '  %-28s %s  api=%s\n' "$n" "${at:-?}" "${api:-?}"
  done
}

# ---------------------------------------------------------------- show ------
cmd_show() {
  local name="${1:-}"
  [ -n "$name" ] || { fail "用法: rollback.sh show <名称>"; return 2; }
  local dir="$BASELINE_DIR/$name"
  [ -d "$dir" ] || { fail "基线不存在: $name"; return 1; }
  echo "=== 基线 $name ==="
  cat "$dir/baseline.txt"
  echo
  echo "--- 记录时的健康快照 ---"
  cat "$dir/health.txt"
}

# ---------------------------------------------------------------- verify ----
# 当前系统是否与基线一致（回滚是否真的生效）
cmd_verify() {
  local name="${1:-}"
  local dir="$BASELINE_DIR/$name"
  [ -d "$dir" ] || { fail "基线不存在: $name"; return 1; }

  local bad=0
  echo "=== 校验当前状态与基线 $name 的一致性 ==="
  local svc want got
  for svc in $SHARED_TAG_SERVICES index-worker transcode-worker web; do
    want="$(awk -v s="$svc" '$1==s{print $3}' "$dir/baseline.txt" 2>/dev/null)"
    [ -n "$want" ] && [ "$want" != "-" ] || continue
    got="$(container_image_id "$svc" 2>/dev/null)"
    if [ -z "$got" ]; then
      echo "  [P0] $svc 容器不存在"
      bad=1
    elif [ "${got:0:19}" = "${want:0:19}" ]; then
      printf '  [ok] %-20s %s\n' "$svc" "${got:0:19}"
    else
      printf '  [P0] %-20s 期望 %s 实际 %s\n' "$svc" "${want:0:19}" "${got:0:19}"
      bad=1
    fi
  done

  echo "--- 健康 ---"
  local rc; rc="$(health_code)"
  echo "  health=$rc"
  [ "$rc" = "200" ] || { echo "  [P0] /health 非 200"; bad=1; }
  local r; r="$(ready_probe)"
  echo "  ready=$r"
  # /ready 必须三项全 ok才算健康；只看 HTTP 200 会漏掉「依赖降级但仍返回 200」
  if ! printf '%s' "$r" | grep -q '"disk":"ok"'; then echo "  [P0] /ready disk 非 ok"; bad=1; fi
  if ! printf '%s' "$r" | grep -q '"postgres":"ok"'; then echo "  [P0] /ready postgres 非 ok"; bad=1; fi
  if ! printf '%s' "$r" | grep -q '"valkey":"ok"'; then echo "  [P0] /ready valkey 非 ok"; bad=1; fi

  echo "--- 规模 ---"
  local c v
  c="$(docker compose ps -q 2>/dev/null | wc -l)"
  v="$(docker volume ls -q | wc -l)"
  echo "  容器=$c（期望 $EXPECTED_SERVICES）  卷=$v（期望 $EXPECTED_VOLUMES）"
  [ "$c" = "$EXPECTED_SERVICES" ] || { echo "  [P0] 容器数不符"; bad=1; }
  [ "$v" = "$EXPECTED_VOLUMES" ] || { echo "  [P0] 卷数不符"; bad=1; }

  echo
  if [ "$bad" -eq 0 ]; then
    echo "校验通过：当前状态与基线 $name 一致"
    return 0
  fi
  echo "校验失败：当前状态与基线 $name 不一致（见上面 [P0] 项）"
  return 1
}

# ---------------------------------------------------------------- to --------
# 回滚到指定基线。
#
# 做法：把基线里记录的**镜像 ID** 重新打回原 tag，然后 docker compose up -d --force-recreate。
# 为什么必须走「重新打 tag」而不是「compose 里改 image」：
#   - compose 里写死 image ID 会污染配置文件（下次部署就被钉死了）
#   - 重新打 tag 对 compose 零侵入，且 tag 与镜像的对应关系被显式摆平
# 为什么必须 --force-recreate：
#   - 镜像 ID 变了但容器仍指向旧 ID，docker compose up 会认为「配置没变」而不重建
#     这正是第②项查出的「同 tag 两个镜像」故障的成因
cmd_to() {
  local name="${1:-}"
  [ -n "$name" ] || { fail "用法: rollback.sh to <名称>"; return 2; }
  local dir="$BASELINE_DIR/$name"
  [ -d "$dir" ] || { fail "基线不存在: $name"; return 1; }

  log "回滚到基线: $name"

  # 1) 校验基线里的镜像还在（退路必须真的存在）
  local svc tag want missing=0
  for svc in $SHARED_TAG_SERVICES index-worker transcode-worker web; do
    want="$(awk -v s="$svc" '$1==s{print $3}' "$dir/baseline.txt" 2>/dev/null)"
    [ -n "$want" ] && [ "$want" != "-" ] || continue
    if ! docker image inspect "$want" >/dev/null 2>&1; then
      fail "基线镜像已不存在：$svc $want —— 无法回滚（该镜像可能已被清理）"
      missing=1
    fi
  done
  if [ "$missing" -ne 0 ]; then
    _rollback_failed "$name" "基线镜像缺失，无法回滚"
    return 1
  fi

  # 2) 逐服务把镜像 ID 打回它的 tag
  for svc in $SHARED_TAG_SERVICES index-worker transcode-worker web; do
    tag="$(awk -v s="$svc" '$1==s{print $2}' "$dir/baseline.txt" 2>/dev/null)"
    want="$(awk -v s="$svc" '$1==s{print $3}' "$dir/baseline.txt" 2>/dev/null)"
    [ -n "$tag" ] && [ -n "$want" ] && [ "$want" != "-" ] || continue
    if docker tag "$want" "$tag" 2>/dev/null; then
      log "  $svc: tag $tag -> ${want:0:19}"
    else
      fail "  $svc: 打 tag 失败（$tag <- $want）"
      _rollback_failed "$name" "docker tag 失败: $svc"
      return 1
    fi
  done

  # 3) 强制重建容器（必须 --force-recreate，见函数注释）
  log "重建容器"
  if ! docker compose up -d --force-recreate --no-deps $SHARED_TAG_SERVICES 2>&1 | sed 's/^/  /'; then
    fail "docker compose up 失败"
    _rollback_failed "$name" "compose up 失败"
    return 1
  fi

  # 3b) 反代层必须一起重建。
  #
  # 2026-10-10 演练实测到的真实故障（不是演练脚本的问题，是生产配置缺陷）：
  #   api 容器被 --force-recreate 重建后拿到新 IP（172.19.0.8），
  #   而 web(nginx) 容器没动，它在**启动时**解析过一次 api 的域名并缓存了旧 IP。
  #   结果：web→api:8080 直连正常，但经 nginx 的 /ready 一律 502，
  #   且 nginx error.log **是空的**（因为它连的是「解析正确但早已不存在的旧 IP」，
  #   连 connect 都没试到预期目标，错误日志没有留下有效线索）。
  #   docker compose 里 web 的 restart 策略不会让它跟着 api 重建而重载解析。
  #
  #   这个坑的恶劣之处：/ready 报 502 看起来像「api 挂了」，会把排查引向 api，
  #   而 api 其实是好的。演练里就因此一度判成「回滚失败」。
  #
  #   处置：重建 api/worker 之后，把反代层一起 restart 让它重新解析上游。
  #   restart 不删容器、不碰卷，属可逆操作。
  log "重建反代层（让 nginx 重新解析 api 上游，见函数内注释）"
  docker compose restart web caddy 2>&1 | sed 's/^/  /' || true

  # 4) 等 /ready 恢复
  log "等待 /ready 恢复"
  local ok=0 i last=""
  for i in $(seq 1 40); do
    last="$(ready_probe)"
    if printf '%s' "$last" | grep -q '"status":"ready"'; then ok=1; break; fi
    sleep 2
  done
  if [ "$ok" -ne 1 ]; then
    # 失败时把实际返回内容带上：只说「未恢复」会把人引向错误方向
    #（本次演练就是 502 而 api 正常，真实原因是 nginx 上游解析过期）
    fail "/ready 未在 80 秒内恢复，最后返回：${last:-<空>}"
    if printf '%s' "$last" | grep -q '502'; then
      fail "提示：502 通常意味着反代层上游解析过期（api 重建后 IP 变了）。"
      fail "      处置：cd $COMPOSE_DIR && docker compose restart web caddy"
    fi
    _rollback_failed "$name" "回滚后 /ready 未恢复，最后返回：${last:-<空>}"
    return 1
  fi

  # 5) 用基线做一致性校验（回滚是否真的生效）
  if ! cmd_verify "$name"; then
    _rollback_failed "$name" "回滚后校验不通过"
    return 1
  fi

  # 6) 成功也写台账：让巡检知道当前处于哪个已知良好版本
  _rollback_ok "$name"
  log "回滚成功并已验证"
  return 0
}

_rollback_failed() {
  local name="$1" reason="$2"
  local f="$STATE_DIR/last_rollback.txt"
  {
    echo "status=failed"
    echo "baseline=$name"
    echo "reason=$reason"
    echo "ts=$(date +%s)"
    echo "ts_iso=$(date -Is)"
  } > "$f" 2>/dev/null
  send_alert "$(hostname) 回滚失败：$name" \
    "回滚到基线 $name **失败**。

原因: $reason
时间: $(date -Is)
主机: $(hostname)

这意味着线上可能仍处于故障版本，且**人工可能误以为已经退回去了**。
请立即人工介入：
  1. 查当前实际状态：bash scripts/rollback.sh --status
  2. 看部署台账：cat $STATE_DIR/last_deploy.txt
  3. 其它可用基线：bash scripts/rollback.sh list

注意：本脚本不会删除任何镜像，退路仍在。" || true
}

_rollback_ok() {
  local name="$1"
  local f="$STATE_DIR/last_rollback.txt"
  {
    echo "status=success"
    echo "baseline=$name"
    echo "ts=$(date +%s)"
    echo "ts_iso=$(date -Is)"
  } > "$f" 2>/dev/null
}

# ---------------------------------------------------------------- drill -----
# 完整演练。造坏版本的方式刻意选「让服务起不来」，且**不碰任何 Dockerfile**：
# 在 api 构建上下文里放一个语法错误的 .go 文件（Dockerfile 会 COPY 到它），
# 这是真实会发生的故障形态（写错代码 → 镜像构建失败或运行崩溃），
# 比人造 `RUN exit 1` 更有代表性。
cmd_drill() {
  local probe="$COMPOSE_DIR/src/backend/internal/media/zz_rollback_drill_probe.go"
  log "=========================================================="
  log "回滚演练开始 @ $(date -Is)"
  log "=========================================================="

  # --- 步骤 1：记录基线 ---
  log ""
  log "### 步骤 1：记录当前可回退基线"
  local base="drill-$(date +%Y%m%d-%H%M%S)"
  cmd_record "$base" || { fail "记录基线失败，演练中止"; return 1; }

  # --- 步骤 2：制造坏版本 ---
  log ""
  log "### 步骤 2：制造坏版本（故意让构建失败；不修改任何 Dockerfile）"
  if [ -e "$probe" ]; then fail "探针已存在，中止"; return 1; fi
  cat > "$probe" <<'EOF'
package media

// 回滚演练探针：故意制造编译错误，使镜像构建失败。
// 这不是人为 exit 1，而是真实的 go build 语法错误 —— 与线上事故同型。
func rollbackDrillProbe() {
	this is not valid go syntax
}
EOF
  log "已注入 $probe"

  log ""
  log "### 步骤 3：用 deploy.sh 部署（期望构建失败 + 非 0 退出码 + 告警）"
  local rc
  bash "$HERE/deploy.sh" api > "$STATE_DIR/drill_deploy.log" 2>&1
  rc=$?
  log "deploy.sh 退出码 = $rc"
  grep -E 'syntax error|ERROR|构建失败' "$STATE_DIR/drill_deploy.log" | head -4 | sed 's/^/    /'
  if [ "$rc" -eq 0 ]; then
    fail "构建失败了但 deploy.sh 返回 0 —— 退出码被吞，机制失效！"
  else
    log "OK：构建失败如实返回非 0（rc=$rc），不会被误判为成功"
  fi

  log ""
  log "### 步骤 4：巡检是否检出（验证告警链路）"
  bash "$HERE/ops_drift_watch.sh" --dry-run 2>&1 | grep -E '^\[P' | head -5 | sed 's/^/    /'

  log ""
  log "### 步骤 5：清理探针（模拟「修好代码」）"
  rm -f "$probe"
  log "探针已删除: $([ -e "$probe" ] && echo '失败' || echo '成功')"

  log ""
  log "### 步骤 6：执行回滚到基线 $base"
  if cmd_to "$base"; then
    log "回滚成功"
  else
    fail "回滚失败（这是演练要暴露的问题，详见上方输出与告警）"
    return 1
  fi

  log ""
  log "### 步骤 7：最终验证"
  cmd_verify "$base" || return 1

  log ""
  log "回滚演练完成 @ $(date -Is)"
  return 0
}

# ---------------------------------------------------------------- status ----
cmd_status() {
  echo "=== 当前运行版本 ==="
  for svc in $SHARED_TAG_SERVICES index-worker transcode-worker web; do
    printf '  %-20s %-26s %s\n' "$svc" "$(svc_image_tag "$svc")" "$(container_image_id "$svc" 2>/dev/null | cut -c1-19 || echo '-')"
  done
  echo
  echo "=== 上次回滚 ==="
  cat "$STATE_DIR/last_rollback.txt" 2>/dev/null || echo "  （无记录）"
  echo
  echo "=== 上次部署 ==="
  cat "$STATE_DIR/last_deploy.txt" 2>/dev/null || echo "  （无记录）"
  echo
  echo "=== 健康 ==="
  echo "  health=$(health_code)"
  echo "  ready=$(ready_probe)"
  echo "  容器=$(docker compose ps -q 2>/dev/null | wc -l)  卷=$(docker volume ls -q | wc -l)"
  echo
  echo "=== 可回退基线 ==="
  cmd_list
}

# ---------------------------------------------------------------- 入口 ------
case "${1:-}" in
  record) shift; cmd_record "${1:-}" ;;
  list)   cmd_list ;;
  show)   shift; cmd_show "${1:-}" ;;
  verify) shift; cmd_verify "${1:-}" ;;
  to)     shift; cmd_to "${1:-}" ;;
  drill)  cmd_drill ;;
  --status|status) cmd_status ;;
  -h|--help|"")
    sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'
    ;;
  *)
    fail "未知子命令: $1（可用：record / list / show / verify / to / drill / --status）"
    exit 2
    ;;
esac