#!/usr/bin/env bash
# 全景相册定时巡检 + 邮件告警（Phase 6 T6.3，P1 轻量方案）
#
# 设计：零常驻开销，由 cron 每分钟~每 5 分钟调用一次；仅在有问题时发邮件。
# 邮件（SMTP）凭据**只从服务器端配置文件读取**，绝不入库、绝不硬编码：
#   /home/warlocks/pano-album/.env.ops   （权限 600，已 gitignore）
#
# .env.ops 字段（全部必填，除非只做本机日志巡检）：
#   OPS_SMTP_HOST    SMTP 服务器主机名
#   OPS_SMTP_PORT    端口（465=implicit TLS，587=STARTTLS，25=明文）
#   OPS_SMTP_TLS     加密方式：ssl | starttls | none            （默认 ssl）
#   OPS_SMTP_USER    SMTP 登录用户名（多数服务商=发件邮箱全址）
#   OPS_SMTP_PASS    SMTP 登录密码 / 授权码（**不是**邮箱登录密码）
#   OPS_SMTP_FROM    发件人地址（信封 From）
#   OPS_SMTP_TO      收件人地址，多个用逗号分隔
#   OPS_ALERT_PREFIX 主题前缀（可选，默认 [Panomint 告警]）
#
# 用法：
#   scripts/ops_check.sh                # 巡检并在有异常时告警
#   scripts/ops_check.sh --test-mail    # 只测试邮件通路（发一封自检邮件）
#   scripts/ops_check.sh --dry-run      # 只巡检/打印，不发邮件
#
# 红线：全部为只读检查；不 stop/start/rm 任何容器或卷。

set -uo pipefail

COMPOSE_DIR="${COMPOSE_DIR:-/home/warlocks/pano-album}"
BACKUP_DIR="${BACKUP_DIR:-/home/backups/pano}"
ENV_OPS="${ENV_OPS:-$COMPOSE_DIR/.env.ops}"
STATE_DIR="${STATE_DIR:-/home/warlocks/.pano-ops}"
PROJECT="${COMPOSE_PROJECT:-pano-album}"
READY_URL="${READY_URL:-http://127.0.0.1:8088/ready}"

BACKUP_MAX_AGE_H="${BACKUP_MAX_AGE_H:-36}"     # 备份新鲜度上限（小时）
ROOT_MIN_FREE_GB="${ROOT_MIN_FREE_GB:-1}"      # / 最小可用空间
VOLUME_MAX_PCT="${VOLUME_MAX_PCT:-80}"         # mediadata 卷使用率上限（%）
QUEUE_MAX_DEPTH="${QUEUE_MAX_DEPTH:-100}"      # 队列积压上限
QUEUE_MAX_OLD_MIN="${QUEUE_MAX_OLD_MIN:-30}"   # 最老逾期任务上限（分钟）
READY_FAIL_THRESHOLD="${READY_FAIL_THRESHOLD:-3}"  # /ready 连续失败几次告警
ALERT_MIN_INTERVAL="${ALERT_MIN_INTERVAL:-1800}"   # 同内容告警最小间隔（秒）
EXPECTED_SERVICES="${EXPECTED_SERVICES:-db valkey minio api web caddy index-worker transcode-worker embed-worker}"

mkdir -p "$STATE_DIR"
MODE="${1:-check}"
PROBLEMS=()
add_problem() { PROBLEMS+=("$1"); }

# ------------------------------------------------------------------ 巡检 -----
check_services() {
  local out; out="$(cd "$COMPOSE_DIR" && docker compose ps --format '{{.Service}}|{{.State}}|{{.Health}}' 2>/dev/null)"
  for svc in $EXPECTED_SERVICES; do
    local line; line="$(printf '%s\n' "$out" | grep "^$svc|" | head -n1)"
    if [ -z "$line" ]; then
      add_problem "[P0] 容器 $svc 未在运行（compose ps 无记录）"
      continue
    fi
    local state health; state="$(printf '%s' "$line" | cut -d'|' -f2)"
    health="$(printf '%s' "$line" | cut -d'|' -f3)"
    [ "$state" = "running" ] || add_problem "[P0] 容器 $svc 状态=$state（期望 running）"
    case "$health" in ""|"healthy"|"<nil>"|"<no value>") ;; *) add_problem "[P0] 容器 $svc 健康检查=$health" ;; esac
  done
}

check_backup() {
  local marker="$BACKUP_DIR/LAST_SUCCESS"
  if [ ! -f "$marker" ]; then
    add_problem "[P0] 从未产生成功备份（缺少 $marker）"
    return
  fi
  local ts now age_h
  ts="$(cat "$marker" 2>/dev/null || echo 0)"
  now="$(date +%s)"
  age_h=$(( (now - ts) / 3600 ))
  [ "$age_h" -le "$BACKUP_MAX_AGE_H" ] \
    || add_problem "[P0] 备份已过期：最近成功在 ${age_h}h 前（上限 ${BACKUP_MAX_AGE_H}h）"
}

check_disk() {
  local free_gb
  free_gb="$(df -BG --output=avail / | tail -n1 | tr -dc '0-9')"
  [ -n "$free_gb" ] && [ "$free_gb" -lt "$ROOT_MIN_FREE_GB" ] \
    && add_problem "[P0] 根分区 / 可用空间仅 ${free_gb}G（< ${ROOT_MIN_FREE_GB}G）"

  if (cd "$COMPOSE_DIR" && docker compose ps --status running --services 2>/dev/null | grep -qx api); then
    local pct
    pct="$(cd "$COMPOSE_DIR" && docker compose exec -T api df -P /data 2>/dev/null | awk 'NR==2{gsub(/%/,"",$5);print $5}')"
    if [ -n "$pct" ]; then
      [ "$pct" -lt "$VOLUME_MAX_PCT" ] \
        || add_problem "[P0] mediadata 卷使用率 ${pct}%（≥ ${VOLUME_MAX_PCT}%）"
    fi
  fi
}

check_ready() {
  local st_file="$STATE_DIR/ready_fails"
  local code; code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$READY_URL" 2>/dev/null || echo 000)"
  if [ "$code" = "200" ]; then
    echo 0 > "$st_file"
  else
    local n=0; [ -f "$st_file" ] && n="$(cat "$st_file" 2>/dev/null || echo 0)"
    n=$((n + 1)); echo "$n" > "$st_file"
    [ "$n" -ge "$READY_FAIL_THRESHOLD" ] \
      && add_problem "[P1] /ready 连续 $n 次非 200（最近 http=$code，阈值 $READY_FAIL_THRESHOLD）"
  fi
}

check_queue() {
  (cd "$COMPOSE_DIR" && docker compose ps --status running --services 2>/dev/null | grep -qx valkey) || return 0
  local now_ms; now_ms="$(( $(date +%s) * 1000 ))"
  for q in media transcode sys; do
    local k="q:$q"
    local waiting processing delayed failed
    waiting="$(cd "$COMPOSE_DIR" && docker compose exec -T valkey valkey-cli LLEN "$k:waiting" 2>/dev/null | tr -d '[:space:]')"
    processing="$(cd "$COMPOSE_DIR" && docker compose exec -T valkey valkey-cli LLEN "$k:processing" 2>/dev/null | tr -d '[:space:]')"
    delayed="$(cd "$COMPOSE_DIR" && docker compose exec -T valkey valkey-cli ZCARD "$k:delayed" 2>/dev/null | tr -d '[:space:]')"
    failed="$(cd "$COMPOSE_DIR" && docker compose exec -T valkey valkey-cli LLEN "$k:failed" 2>/dev/null | tr -d '[:space:]')"
    local depth=$(( ${waiting:-0} + ${processing:-0} + ${delayed:-0} ))
    [ "$depth" -le "$QUEUE_MAX_DEPTH" ] \
      || add_problem "[P1] 队列 $q 积压 depth=$depth（waiting=$waiting processing=$processing delayed=$delayed，上限 $QUEUE_MAX_DEPTH）"
    [ "${failed:-0}" -eq 0 ] \
      || add_problem "[P2] 队列 $q 死信 failed=$failed（需人工排查）"

    local oldest; oldest="$(cd "$COMPOSE_DIR" && docker compose exec -T valkey valkey-cli ZRANGE "$k:delayed" 0 0 WITHSCORES 2>/dev/null | tail -n1 | tr -d '[:space:]')"
    if [ -n "$oldest" ] && printf '%s' "$oldest" | grep -qE '^[0-9]+$'; then
      local age_min=$(( (now_ms - oldest) / 60000 ))
      [ "$age_min" -le "$QUEUE_MAX_OLD_MIN" ] \
        || add_problem "[P1] 队列 $q 最老逾期任务已等待 ${age_min} 分钟（上限 $QUEUE_MAX_OLD_MIN）"
    fi
  done
}

# -------------------------------------------------------------- 邮件发送 -----
send_mail() {
  local subject="$1" body="$2"
  if [ ! -f "$ENV_OPS" ]; then
    printf '%s 未找到 %s，无法发送邮件；仅输出到本机日志\n' "$(date -Is)" "$ENV_OPS"
    return 0
  fi
  # 只取 OPS_ 前缀的键，避免污染环境
  set -a
  # shellcheck disable=SC1090
  . "$ENV_OPS"
  set +a

  command -v python3 >/dev/null || { printf '%s ERROR 缺少 python3，无法发信\n' "$(date -Is)"; return 1; }

  OPS_SUBJECT="$subject" OPS_BODY="$body" python3 - <<'PY'
import os, smtplib, ssl, sys
from email.message import EmailMessage

host = os.environ.get("OPS_SMTP_HOST", "")
port = int(os.environ.get("OPS_SMTP_PORT", "465") or 465)
mode = (os.environ.get("OPS_SMTP_TLS", "ssl") or "ssl").lower()
user = os.environ.get("OPS_SMTP_USER", "")
pw   = os.environ.get("OPS_SMTP_PASS", "")
frm  = os.environ.get("OPS_SMTP_FROM", user)
to   = [x.strip() for x in os.environ.get("OPS_SMTP_TO", "").split(",") if x.strip()]

if not host or not to:
    print("SMTP 未配置完整（缺 OPS_SMTP_HOST 或 OPS_SMTP_TO），跳过发信")
    sys.exit(0)

msg = EmailMessage()
msg["Subject"] = os.environ.get("OPS_SUBJECT", "[Panomint 告警]")
msg["From"] = frm
msg["To"] = ", ".join(to)
msg.set_content(os.environ.get("OPS_BODY", ""))

ctx = ssl.create_default_context()
try:
    if mode == "ssl":
        s = smtplib.SMTP_SSL(host, port, timeout=20, context=ctx)
    else:
        s = smtplib.SMTP(host, port, timeout=20)
        if mode == "starttls":
            s.starttls(context=ctx)
    with s:
        if user:
            s.login(user, pw)
        s.send_message(msg)
    print(f"MAIL_OK to={','.join(to)}")
except Exception as e:
    print(f"MAIL_FAIL {type(e).__name__}: {e}")
    sys.exit(1)
PY
}

# ------------------------------------------------------------------ 主流程 ----
if [ "$MODE" = "--test-mail" ]; then
  send_mail "${OPS_ALERT_PREFIX:-[Panomint 告警]} 邮件通路自检" \
            "这是一封来自 $(hostname) 的巡检自检邮件，收到即表示 SMTP 配置可用。"
  exit $?
fi

check_services
check_backup
check_disk
check_ready
check_queue

N="${#PROBLEMS[@]}"
if [ "$N" -eq 0 ]; then
  printf '%s 巡检通过（容器/备份/磁盘/就绪/队列 全正常）\n' "$(date -Is)"
  exit 0
fi

BODY="$(printf '%s\n' "巡检主机: $(hostname)" "巡检时间: $(date -Is)" "" "发现 $N 项问题：" "" "${PROBLEMS[@]}" "" "处置请参考 scripts/restore.sh help 与 /home/backups/pano/backup.log")"
printf '%s\n' "$BODY"
printf '%s 发现 %d 项问题\n' "$(date -Is)" "$N" >> "$STATE_DIR/ops.log"

if [ "$MODE" = "--dry-run" ]; then
  echo "（--dry-run：不发邮件）"
  exit 1
fi

# 同内容告警节流
HASH="$(printf '%s' "$BODY" | sha256sum | cut -d' ' -f1)"
LAST_HASH_FILE="$STATE_DIR/last_alert_hash"
LAST_TS_FILE="$STATE_DIR/last_alert_ts"
last_hash="$(cat "$LAST_HASH_FILE" 2>/dev/null || true)"
last_ts="$(cat "$LAST_TS_FILE" 2>/dev/null || echo 0)"
now="$(date +%s)"
if [ "$HASH" = "$last_hash" ] && [ $((now - last_ts)) -lt "$ALERT_MIN_INTERVAL" ]; then
  echo "同内容告警在 ${ALERT_MIN_INTERVAL}s 冷却期内，跳过发信"
  exit 1
fi

if send_mail "${OPS_ALERT_PREFIX:-[Panomint 告警]} $(hostname) 发现 ${N} 项异常" "$BODY"; then
  printf '%s' "$HASH" > "$LAST_HASH_FILE"
  printf '%s' "$now"  > "$LAST_TS_FILE"
fi
exit 1
