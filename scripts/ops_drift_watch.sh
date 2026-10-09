#!/usr/bin/env bash
# ops_drift_watch.sh —— 构建/部署失败与产物漂移巡检（cron 每 2 分钟）
#
# 存在的理由（2026-10-09 Job000145 事故复盘）
#   那次 api 镜像构建失败（no required module provides package google.golang.org/protobuf），
#   从出事到被人发现过了约 2 小时。更糟的是当时的构建脚本把退出码吞进了管道
#   （`| tail`），构建失败也返回 0 —— **屏幕上显示的是「成功」**。
#   这暴露了两个独立的缺口：
#     A. 构建失败无人知晓（退出码被管道吞掉 + 没有失败告警）
#     B. 产物与源码漂移（源码更新了但镜像没重建，跑的是旧代码，且一切看起来正常）
#   本脚本把 A、B 变成可在 5 分钟内被自动发现的事件。
#
# 它检查什么（每项都能独立失败，不是「从不失败的检查」）
#   1. 漂移（对应 B）：把 Dockerfile 真正 COPY 进去的构建输入算指纹，
#      与「上次成功部署时记录的指纹」比对。不一致 = 源码比产物新。
#      同时用「构建输入最新 mtime vs 运行中镜像的构建时间」做独立交叉验证 ——
#      指纹基线可能被人为重置，时间线不会说谎。
#   2. 镜像/容器不一致（对应 B 的另一面）：compose 声明的镜像 tag 与容器实际在跑的
#      image ID 是否一致。这一项在本次巡检开发时就查出了真实漂移（见交付报告）。
#   3. 上次部署失败（对应 A）：读部署台账，若最近一次部署记录为 failed 且未清除，
#      持续告警 —— 不要求「失败瞬间抓到」，2 分钟一轮足以覆盖。
#   4. 入口存活（对应 A 的后果）：/ready 含 postgres/valkey 检查，比 /health 强。
#   5. 告警通道自身可用性：通道哑了就告警（否则 1~4 全是白跑）。
#
# 用法
#   bash scripts/ops_drift_watch.sh              # 巡检 + 有问题才告警
#   bash scripts/ops_drift_watch.sh --dry-run    # 只打印，不落盘不发信
#   bash scripts/ops_drift_watch.sh --accept     # 把当前指纹登记为基线（部署成功后用）
#   bash scripts/ops_drift_watch.sh --selftest   # 告警适配器自检（逐个实发）
#   bash scripts/ops_drift_watch.sh --status     # 打印当前状态，不告警
#
# 红线：全部只读。不 stop/start/rm 任何容器、镜像或卷；不动宝塔 cron。

set -uo pipefail

# ------------------------------------------------------------------ 配置 ----
COMPOSE_DIR="${COMPOSE_DIR:-/home/warlocks/pano-album}"
STATE_DIR="${STATE_DIR:-/home/warlocks/.pano-ops}"
BASELINE_FILE="${BASELINE_FILE:-$STATE_DIR/drift_baseline.json}"
DEPLOY_LEDGER="${DEPLOY_LEDGER:-$STATE_DIR/last_deploy.txt}"
READY_URL="${READY_URL:-http://127.0.0.1:8088/ready}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8088/health}"
# 漂移宽限：源码改完后给人一点时间重建，避免边改边重建时刷屏
DRIFT_GRACE_SEC="${DRIFT_GRACE_SEC:-600}"
READY_FAIL_THRESHOLD="${READY_FAIL_THRESHOLD:-2}"
ALERT_MIN_INTERVAL="${ALERT_MIN_INTERVAL:-600}"
# 参与漂移检测的服务。
#
# ⚠️ 这里**必须覆盖所有复用 app 镜像的服务**，不只是有 build 段的那几个。
#    实测（2026-10-10）：api 已重建到 982b5fc9，而 embed/tag/phash/faces-worker
#    仍在跑 6 天前的 dbe659c8 —— 它们和 api 声明同一个 tag panomint-app:latest。
#    只查 api 的话，这个漂移永远不会被发现；而这正是「重建了但只重建了一半」，
#    线上表现是 worker 用旧模型逻辑跑，症状极难归因。
#
#    判定「是否该纳入」的标准是**容器在跑、且镜像可定位**，而不是「compose 里有没有 build」：
#      - 有 image: 的服务（api + 4 个 worker 共用 panomint-app:latest）→ 参与镜像一致性比对
#      - 无 image: 的服务（web / index-worker / transcode-worker）→ compose 用
#        <project>-<service> 自动命名，镜像名要自己推出来，否则会因取不到 tag 而被静默跳过
WATCH_SERVICES="${WATCH_SERVICES:-api web index-worker transcode-worker embed-worker tag-worker phash-worker faces-worker}"

# COPY 解析器看不到、但确实参与构建的输入（bind mount 只读挂载进 RUN 的那些）。
# api 镜像的 ONNX Runtime 原生库就是这么进的：ortassets 阶段 bind mount 上下文做架构断言，
# 字节拷贝由随后的 COPY --from= 完成。把别的架构的 .so 换进来时，指纹必须变，
# 否则运行期 dlopen 失败却无人告警 —— 详见 lib/ops_fingerprint.py 的说明。
api_extra_inputs() {
  # 目录名带版本号（onnxruntime-linux-x64-1.29.0），用通配兜住版本升级
  printf '%s\n' assets/lib/onnxruntime-linux-*/lib/libonnxruntime.so*
}

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/ops_alert.sh
. "$HERE/lib/ops_alert.sh"

mkdir -p "$STATE_DIR" 2>/dev/null

PROBLEMS=()
add_problem() { PROBLEMS+=("$1"); }

MODE="${1:-check}"

# ------------------------------------------------------------ 指纹计算 ------
# 从 compose 定义动态取 build context / dockerfile / build args。
# 用 compose 定义（而不是 ps 或硬编码表）：服务改名/加减时不会静默漏检。
compose_services_json() {
  (cd "$COMPOSE_DIR" && docker compose config --format json 2>/dev/null)
}

svc_field() {
  # svc_field <json> <service> <jq路径>
  printf '%s' "$1" | jq -r --arg s "$2" ".services[\$s]$3 // empty" 2>/dev/null
}

# 解析服务实际使用的镜像 tag。
#
# compose 有两种写法，漏掉任何一种都会让检查**静默失效**（取不到 tag → 直接 return →
# 这项检查悄悄不做了，且不报任何错 —— 这正是「从不失败的检查」的典型形态）：
#   1. 显式 image: panomint-app:latest        （api 与 4 个 worker 共用）
#   2. 不写 image:，由 compose 自动命名为 <project>-<service>（web / index-worker / transcode-worker）
# 实测踩过：早先只处理第 1 种，第 2 种整批被跳过，巡检报告里一项 mismatch 都没有，
# 看起来「全部一致」，实际是根本没查。
svc_image_tag() {
  local json="$1" svc="$2"
  local tag; tag="$(svc_field "$json" "$svc" '.image')"
  if [ -n "$tag" ]; then
    printf '%s\n' "$tag"
    return 0
  fi
  # 回落到 compose 的自动命名规则
  local project; project="$(printf '%s' "$json" | jq -r '.name // empty' 2>/dev/null)"
  if [ -n "$project" ]; then
    printf '%s\n' "$project-$svc"
    return 0
  fi
  printf '%s\n' "pano-album-$svc"
}

# 为单个服务算构建输入指纹。输出 JSON，失败输出空。
fingerprint_for() {
  local json="$1" svc="$2"
  local ctx df args project
  ctx="$(svc_field "$json" "$svc" '.build.context')"
  df="$(svc_field "$json" "$svc" '.build.dockerfile')"
  args="$(svc_field "$json" "$svc" '.build.args | to_entries | map("\(.key)=\(.value)") | join(",")')"
  project="$(printf '%s' "$json" | jq -r '.name // ""' 2>/dev/null)"
  [ -n "$ctx" ] && [ -n "$df" ] || return 1

  local extra=()
  if [ "$svc" = "api" ]; then
    while IFS= read -r p; do
      [ -n "$p" ] && extra+=(--extra-input "$p")
    done <<< "$(api_extra_inputs)"
  fi

  python3 "$HERE/lib/ops_fingerprint.py" \
    --context "$ctx" --dockerfile "$df" \
    --build-args "$args" --project "$project" "${extra[@]}" 2>/dev/null
}

# 运行中镜像的构建时间（epoch 秒）。取不到返回空。
image_created_epoch() {
  local tag="$1"
  local created
  created="$(docker image inspect "$tag" -f '{{.Created}}' 2>/dev/null)"
  [ -n "$created" ] || return 1
  date -d "$created" +%s 2>/dev/null
}

# ------------------------------------------------------------ 各项检查 ------
# 检查 1：漂移（源码比镜像新）。指纹为主，mtime vs 镜像构建时间为交叉验证。
check_drift() {
  local json; json="$(compose_services_json)"
  if [ -z "$json" ]; then
    add_problem "[P0] 无法读取 compose 定义（docker compose config 无输出）→ 漂移检测整段被跳过"
    return
  fi

  local baseline_ts=""
  if [ -f "$BASELINE_FILE" ]; then
    baseline_ts="$(jq -r '.meta.accepted_at_epoch // 0' "$BASELINE_FILE" 2>/dev/null)"
    if ! jq -e '.api.fp' "$BASELINE_FILE" >/dev/null 2>&1; then
      add_problem "[P0] 漂移基线文件损坏或格式不对（$BASELINE_FILE 读不出 .<service>.fp）→ 指纹漂移检测无法工作"
      return
    fi
  else
    add_problem "[P1] 尚无漂移基线（$BASELINE_FILE 不存在）→ 无法判定源码是否已漂移。请部署成功后执行 --accept 登记基线"
  fi

  local now; now="$(date +%s)"
  for svc in $WATCH_SERVICES; do
    # 只有带 build 段的服务才有「源码 → 产物」这条链。
    # 复用同一镜像的 worker（embed/tag/phash/faces 共用 panomint-app:latest）在 compose 里
    # 没有自己的 build 段，它们的新旧程度由 check_image_consistency 覆盖 —— 那种情况
    # 是「镜像换了但容器没跟上」，不是「源码没重建」，用指纹判定属于用错工具。
    local has_build; has_build="$(svc_field "$json" "$svc" '.build.context')"
    if [ -z "$has_build" ]; then
      continue
    fi

    local fp; fp="$(fingerprint_for "$json" "$svc")"
    if [ -z "$fp" ]; then
      add_problem "[P0] 服务 $svc 无法计算构建输入指纹（build context 解析失败或构建输入缺失）"
      continue
    fi
    local cur_fp; cur_fp="$(printf '%s' "$fp" | jq -r '.fingerprint')"
    # ⚠️ 这里必须用 jq 按 key 取，不能把基线压成 "k=v,k=v" 再用 awk -F= 反解。
    #    那样做会踩两个坑：① meta 这种没有 .fp 的条目会产出空值；
    #    ② awk 的字段切分在值里含分隔符时会把 key 切错，
    #    结果是 base_fp 取空 → 指纹比对整段被跳过 → **改源码也不会告警**（静默失效）。
    local base_fp; base_fp="$(jq -r --arg s "$svc" '.[$s].fp // empty' "$BASELINE_FILE" 2>/dev/null)"

    # 1a. 指纹不一致
    if [ -z "$base_fp" ]; then
      # 基线里没有这个服务的记录 → 指纹比对做不了。必须报出来，
      # 否则就是「这项检查对某个服务静默失效」——和退出码被吞是同一类错误。
      add_problem "[P1] 服务 $svc 无漂移基线记录（$BASELINE_FILE 里没有 .$svc.fp）→ 该服务的指纹漂移检测未生效，请执行 --accept"
    elif [ "$cur_fp" != "$base_fp" ]; then
      local newest_path; newest_path="$(printf '%s' "$fp" | jq -r '.inputs[0].path // "?"')"
      local newest_mtime; newest_mtime="$(printf '%s' "$fp" | jq -r '.newest_mtime // 0')"
      local age=$(( now - newest_mtime ))
      if [ "$age" -ge "$DRIFT_GRACE_SEC" ]; then
        add_problem "[P0] 产物漂移：服务 $svc 的构建输入已变更但镜像未重建（指纹 ${base_fp} -> ${cur_fp}；最近改动 ${newest_path}，$(human_age "$age")前）"
      else
        # 刚改完还在宽限期内在重建，不算漂移
        :
      fi
    fi

    # 1b. 交叉验证：构建输入最新 mtime 是否晚于运行中镜像的构建时间
    local tag; tag="$(svc_image_tag "$json" "$svc")"
    [ -n "$tag" ] || continue
    local created; created="$(image_created_epoch "$tag")"
    if [ -n "$created" ]; then
      local newest; newest="$(printf '%s' "$fp" | jq -r '.newest_mtime // 0')"
      if [ "$newest" -gt "$created" ]; then
        local d=$(( newest - created ))
        add_problem "[P0] 产物漂移（时间线交叉验证）：镜像 $tag 构建于 $(fmt_ts "$created")，但构建输入在 $(fmt_ts "$newest") 之后被改动（晚 $(( d / 60 )) 分钟）→ 线上跑的是旧代码"
      fi
    fi
  done
}

# 检查 2：容器实际在跑的 image ID 是否与 compose 声明的镜像一致。
# 这类漂移不会让 /ready 变红，但意味着「重建了镜像却没重建容器」，
# 或者「某些容器被单独 up 过、另一些没跟上」。
check_image_consistency() {
  local json; json="$(compose_services_json)"
  [ -n "$json" ] || return

  local svc tag want running
  for svc in $WATCH_SERVICES; do
    tag="$(svc_image_tag "$json" "$svc")"
    [ -n "$tag" ] || continue
    want="$(docker image inspect "$tag" -f '{{.Id}}' 2>/dev/null | cut -c1-19)"
    if [ -z "$want" ]; then
      add_problem "[P0] 服务 $svc 的镜像 $tag 在本机不存在 → 无法比对镜像一致性（构建失败或镜像被误删）"
      continue
    fi
    running="$(cd "$COMPOSE_DIR" && docker compose ps -q "$svc" 2>/dev/null | head -1)"
    [ -n "$running" ] || continue
    local run_id; run_id="$(docker inspect -f '{{.Image}}' "$running" 2>/dev/null | cut -c1-19)"
    if [ -n "$run_id" ] && [ "$run_id" != "$want" ]; then
      add_problem "[P0] 镜像不一致：服务 $svc 声明镜像 $tag 当前指向 $want，但容器实际在跑 $run_id（重建了镜像但容器未跟随）"
    fi
  done
}

# 检查 2b：同一个 tag 被不同服务以**不同 image ID** 运行 → 版本错配。
#
# 为什么必须单独一条规则（2026-10-10 实测踩到）：
#   api 与 embed/tag/phash/faces-worker 在 compose 里写的是同一个 tag
#   （panomint-app:latest），所以「tag → 当前 image ID」这个映射是唯一的，
#   检查 2 只能报出「哪些容器没跟上」，报不出「这个 tag 正在同时供应两个版本」。
#   而后者才是滚动升级时真正致命的状态：同一批数据、同一个模型目录，
#   api 用新逻辑、worker 用旧逻辑，行为不一致且极难归因。
#   实测就是靠这条发现 worker 停了 6 天而 api 已升级。
check_tag_image_split() {
  local json; json="$(compose_services_json)"
  [ -n "$json" ] || return

  # 收集 tag -> "服务=imageID" 映射
  local -A tag_map=()
  local svc tag cid run_id
  for svc in $WATCH_SERVICES; do
    tag="$(svc_image_tag "$json" "$svc")"
    [ -n "$tag" ] || continue
    cid="$(cd "$COMPOSE_DIR" && docker compose ps -q "$svc" 2>/dev/null | head -1)"
    [ -n "$cid" ] || continue
    run_id="$(docker inspect -f '{{.Image}}' "$cid" 2>/dev/null | cut -c1-19)"
    [ -n "$run_id" ] || continue
    tag_map["$tag"]="${tag_map[$tag]:-}${svc}=${run_id} "
  done

  local t entries ids uniq_count
  for t in "${!tag_map[@]}"; do
    entries="${tag_map[$t]}"
    ids="$(printf '%s\n' $entries | sed 's/.*=//' | sort -u)"
    uniq_count="$(printf '%s\n' "$ids" | grep -c . || true)"
    if [ "$uniq_count" -gt 1 ]; then
      add_problem "[P0] 同一 tag 供应多个镜像（版本错配）：tag $t 下同时存在 $uniq_count 个不同 image ID → 滚动升级期间 api 与 worker 会跑不同版本。实际映射: $(printf '%s' "$entries" | sed 's/ $//')"
    fi
  done
}

# 检查 3：上次部署是否失败（对应失败形态 A —— 退出码被管道吞掉的场景）。
check_last_deploy() {
  [ -f "$DEPLOY_LEDGER" ] || return 0   # 没有台账说明没用本脚本部署过，不算异常
  local status; status="$(grep -E '^status=' "$DEPLOY_LEDGER" 2>/dev/null | head -1 | cut -d= -f2)"
  local ts; ts="$(grep -E '^finished_at=' "$DEPLOY_LEDGER" 2>/dev/null | head -1 | cut -d= -f2-)"
  # 台账里的键是 targets=（复数，deploy.sh 写的就是这个）。
  # 曾误写成 grep '^target=' —— 正则少个 s 就静默取不到值，告警里只能显示 target=?，
  # 而告警文本本身不报错。这种「查不到就显示未知」的坑与退出码被吞是同一类错误。
  local target; target="$(grep -E '^targets=' "$DEPLOY_LEDGER" 2>/dev/null | head -1 | cut -d= -f2-)"
  local reason; reason="$(grep -E '^reason=' "$DEPLOY_LEDGER" 2>/dev/null | head -1 | cut -d= -f2-)"
  if [ "$status" = "failed" ]; then
    add_problem "[P0] 上次部署失败且未恢复（服务=${target:-未知}，原因=${reason:-未知}，时间 ${ts:-未知}）→ 线上可能仍是旧版本。详见 $DEPLOY_LEDGER"
  fi
}

# 检查 4：入口存活。用 /ready（含 postgres/valkey），比 /health 强。
check_ready() {
  local st_file="$STATE_DIR/drift_ready_fails"
  local code; code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 8 "$READY_URL" 2>/dev/null || echo 000)"
  if [ "$code" = "200" ]; then
    echo 0 > "$st_file"
    return
  fi
  # 附带 /health 交叉判断：/ready 挂了但 /health 正常，通常是依赖（DB/valkey）出问题，
  # 这比「整个 api 挂了」更需要人立刻知道。
  local hcode; hcode="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$HEALTH_URL" 2>/dev/null || echo 000)"
  local n=0; [ -f "$st_file" ] && n="$(cat "$st_file" 2>/dev/null || echo 0)"
  n=$(( n + 1 )); echo "$n" > "$st_file"
  if [ "$n" -ge "$READY_FAIL_THRESHOLD" ]; then
    if [ "$hcode" = "200" ]; then
      add_problem "[P0] $READY_URL 连续 $n 次非 200（最近 $code），但 /health 仍 200 → api 进程活着、依赖（postgres/valkey）异常"
    else
      add_problem "[P0] $READY_URL 连续 $n 次非 200（最近 $code），/health 亦为 $hcode → api 入口不可用"
    fi
  fi
}

# 检查 5：告警通道自身是否可用。
#
# 分三级，因为三种情况的处理完全不同，混在一起报等于没报：
#   无外部适配器（未配 webhook/smtp） → P1：功能受限但本地有留档，等凭据即可
#   外部适配器全失败（配了但坏了）   → P0：告警正在丢失，必须立刻修
#   最近一次投递就是全失败            → P0：即使本轮无异常，也要报（说明上轮告警没送达）
check_alert_channel() {
  local st; st="$(alert_channel_status)"
  case "$st" in
    *adapters=*file*|*无外部适配器*)
      add_problem "[P1] 告警仅落本地留档（$ALERT_SPOOL_DIR），未配置外部适配器 → 无人会收到通知。请在 $ALERT_ENV_OPS 填 OPS_WEBHOOK_URL（钉钉/企微机器人）或 OPS_SMTP_*（邮件），任一即可" ;;
  esac

  local f="$ALERT_STATE_DIR/last_delivery"
  if [ -f "$f" ]; then
    local rc; rc="$(grep -E '^rc=' "$f" 2>/dev/null | head -1 | cut -d= -f2)"
    local ts; ts="$(grep -E '^ts_iso=' "$f" 2>/dev/null | head -1 | cut -d= -f2-)"
    local detail; detail="$(grep -E '^detail=' "$f" 2>/dev/null | head -1 | cut -d= -f2-)"
    if [ "$rc" = "4" ]; then
      add_problem "[P0] 上一次告警投递失败（外部适配器全部失败，时间 ${ts:-?}）→ 告警正在丢失。详情: ${detail:-?}"
    fi
  fi
}

# ---------------------------------------------------------------- 工具 ------
fmt_ts() { date -d "@${1:-0}" '+%Y-%m-%d %H:%M:%S' 2>/dev/null || echo "?"; }
human_age() {
  local s="${1:-0}"
  if [ "$s" -lt 60 ]; then echo "${s}秒"
  elif [ "$s" -lt 3600 ]; then echo "$(( s / 60 ))分钟"
  else echo "$(( s / 3600 ))小时"
  fi
}

# 登记基线：把当前各服务指纹写成「已部署」状态
do_accept() {
  local json; json="$(compose_services_json)"
  [ -n "$json" ] || { echo "读不到 compose 定义" >&2; exit 1; }
  local out="{}"
  for svc in $WATCH_SERVICES; do
    local fp; fp="$(fingerprint_for "$json" "$svc")"
    [ -n "$fp" ] || { echo "服务 $svc 指纹计算失败，未登记" >&2; continue; }
    out="$(printf '%s' "$out" | jq --arg s "$svc" \
      --arg fp "$(printf '%s' "$fp" | jq -r '.fingerprint')" \
      --arg ts "$(printf '%s' "$fp" | jq -r '.newest_mtime')" \
      --arg p "$(printf '%s' "$fp" | jq -r '.newest_path // ""')" \
      '. + {($s): {fp:$fp, newest_mtime:($ts|tonumber), newest_path:$p}}')"
  done
  local now; now="$(date +%s)"
  out="$(printf '%s' "$out" | jq --argjson t "$now" --arg d "$(date -Is)" \
    '. + {meta:{accepted_at_epoch:$t, accepted_at:$d}}')"
  printf '%s\n' "$out" | jq . > "$BASELINE_FILE.tmp" && mv "$BASELINE_FILE.tmp" "$BASELINE_FILE"
  echo "已登记漂移基线 -> $BASELINE_FILE"
  jq -r 'to_entries[] | select(.value|type=="object") | select(.value.fp) | "  \(.key)  fp=\(.value.fp)"' "$BASELINE_FILE"
}

do_status() {
  echo "=== 漂移基线 ($BASELINE_FILE) ==="
  if [ -f "$BASELINE_FILE" ]; then jq . "$BASELINE_FILE"; else echo "(不存在)"; fi
  echo "=== 部署台账 ($DEPLOY_LEDGER) ==="
  if [ -f "$DEPLOY_LEDGER" ]; then cat "$DEPLOY_LEDGER"; else echo "(不存在)"; fi
  echo "=== 告警通道（适配器）==="
  alert_channel_status
  echo "=== 上次投递结果 ==="
  cat "$ALERT_STATE_DIR/last_delivery" 2>/dev/null || echo "(尚无投递记录)"
  echo "=== 就绪探针 ==="
  echo "ready=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 8 "$READY_URL" 2>/dev/null || echo 000)"
  echo "health=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$HEALTH_URL" 2>/dev/null || echo 000)"
  echo "=== 同 tag 镜像映射 ==="
  local json svc tag cid run_id
  json="$(compose_services_json)"
  for svc in $WATCH_SERVICES; do
    tag="$(svc_image_tag "$json" "$svc")"
    [ -n "$tag" ] || continue
    cid="$(cd "$COMPOSE_DIR" && docker compose ps -q "$svc" 2>/dev/null | head -1)"
    [ -n "$cid" ] || continue
    run_id="$(docker inspect -f '{{.Image}}' "$cid" 2>/dev/null | cut -c1-19)"
    printf '  %-20s %-26s %s\n' "$svc" "$tag" "${run_id:-<无>}"
  done
}

# ---------------------------------------------------------------- 主流程 ----
case "$MODE" in
  --accept)
    do_accept
    exit $?
    ;;
  --status)
    do_status
    exit $?
    ;;
  --selftest)
    # 告警通道自检：逐个适配器实发一遍。
    # 这一条存在的意义是「通道可用性必须能被断言」——
    # 不能只靠「配置看起来对」就认为告警会到人手里。
    alert_channel_selftest
    exit $?
    ;;
esac

check_drift
check_image_consistency
check_tag_image_split
check_last_deploy
check_ready
check_alert_channel

N="${#PROBLEMS[@]}"
if [ "$N" -eq 0 ]; then
  printf '%s 漂移巡检通过（产物与源码一致 / 镜像与容器一致 / 上次部署成功 / 入口就绪）\n' "$(date -Is)"
  exit 0
fi

BODY="$(printf '%s\n' "巡检主机: $(hostname)" "巡检时间: $(date -Is)" "" "发现 $N 项问题：" "" "${PROBLEMS[@]}" "" \
  "本条告警由 scripts/ops_drift_watch.sh 自动生成（cron 每 2 分钟）。" \
  "若确认是误报或已处置，可执行：bash scripts/ops_drift_watch.sh --accept  重新登记基线。" \
  "部署请走：bash scripts/deploy.sh <service>  （不要手工按顺序敲多个命令）")"

echo "$BODY"
printf '%s 发现 %d 项问题\n' "$(date -Is)" "$N" >> "$STATE_DIR/drift.log"

if [ "$MODE" = "--dry-run" ]; then
  echo "（--dry-run：不发送告警）"
  exit 1
fi

# 同内容节流，避免每 2 分钟重复发同一封信
HASH="$(printf '%s' "$BODY" | sha256sum | cut -d' ' -f1)"
LAST_HASH_FILE="$STATE_DIR/drift_alert_hash"
LAST_TS_FILE="$STATE_DIR/drift_alert_ts"
last_hash="$(cat "$LAST_HASH_FILE" 2>/dev/null || true)"
last_ts="$(cat "$LAST_TS_FILE" 2>/dev/null || echo 0)"
now="$(date +%s)"
if [ "$HASH" = "$last_hash" ] && [ $(( now - last_ts )) -lt "$ALERT_MIN_INTERVAL" ]; then
  echo "同内容告警在 ${ALERT_MIN_INTERVAL}s 冷却期内，跳过"
  exit 1
fi

send_alert "$(hostname) 构建/部署巡检发现 ${N} 项异常" "$BODY"
send_rc=$?
# 只有「外部送达」（返回 0）才算送达，才写节流标记。
# 返回 3（无外部适配器）/ 4（全失败）都不写 —— 否则下一轮会认为「已经告警过」而跳过，
# 那正是本任务要消灭的静默失效：机制看起来在跑，实际没人被通知过。
case "$send_rc" in
  0)
    printf '%s' "$HASH" > "$LAST_HASH_FILE"
    printf '%s' "$now"  > "$LAST_TS_FILE"
    ;;
  3) echo "（本次仅本地留档，未外部送达；不写节流标记，下轮仍会尝试）" ;;
  4) echo "（外部适配器全部失败；不写节流标记，下轮仍会尝试）" >&2 ;;
  *) echo "send_alert 返回异常码 $send_rc" >&2 ;;
esac
exit 1
