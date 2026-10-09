#!/usr/bin/env bash
# ops_alert.sh —— 告警发送库（被 ops_drift_watch.sh / deploy.sh 复用）
#
# 设计前提（2026-10-10 实测得出，别删）：
#   仓库里早就存在 scripts/ops_check.sh，它带一套 SMTP 邮件告警，读 /home/warlocks/pano-album/.env.ops。
#   但那套配置**从未真正投递过** —— .env.ops 里 OPS_SMTP_HOST / USER / PASS / TO 全是空串占位，
#   实测 `bash scripts/ops_check.sh --test-mail` 输出「SMTP 未配置完整…跳过发信」并 exit 0。
#   也就是说：告警通道看起来是有的，实际是哑的；而它 exit 0，于是 cron.log 一片绿色，
#   没有任何人会知道「告警根本发不出去」。
#
#   这就是本任务要消灭的第二种「从不失败的检查」：**通道自身的可用性也必须可观测**。
#   所以本库要求：
#     1. send_alert 返回值区分「已投递」与「未投递」，调用方必须能把投递失败升级成告警事件；
#     2. 每次投递都落一份本地文件（不依赖任何外部服务，一定成功），保证事后可查；
#     3. 支持多通道：有 SMTP 用 SMTP，没有就退回本地文件 + 明确的未投递标记。
#
# 通道优先级：
#   1) SMTP      —— 复用 .env.ops（若已配置真实凭据）
#   2) 本地文件  —— 恒可用兜底，落在 $ALERT_SPOOL_DIR
#
# 用法：
#   source scripts/lib/ops_alert.sh
#   send_alert "标题" "正文"          # 自动选通道
#   ALERT_FORCE_FILE=1 send_alert ...  # 强制走文件（自检用）
#   ALERT_DRY_RUN=1  send_alert ...   # 只打印不发送
#
# 返回码：0=已投递到外部通道；3=仅落本地文件（外部通道不可用）；1=用法错误

set -uo pipefail

# ---------------------------------------------------------------- 配置 ------
ALERT_ENV_OPS="${ALERT_ENV_OPS:-/home/warlocks/pano-album/.env.ops}"
ALERT_SPOOL_DIR="${ALERT_SPOOL_DIR:-/home/warlocks/.pano-ops/alerts}"
ALERT_PREFIX="${ALERT_ALERT_PREFIX:-[Panomint 告警]}"
ALERT_FORCE_FILE="${ALERT_FORCE_FILE:-0}"
ALERT_DRY_RUN="${ALERT_DRY_RUN:-0}"
ALERT_SMTP_TIMEOUT="${ALERT_SMTP_TIMEOUT:-20}"

# ---------------------------------------------------------------- 本地落盘 --
# 这一步必须永远成功：它是所有外部通道都挂掉时唯一还在的证据。
alert_spool() {
  local subject="$1" body="$2" channel="$3" rc="${4:-}"
  local dir="$ALERT_SPOOL_DIR"
  mkdir -p "$dir" 2>/dev/null || {
    echo "alert_spool: 无法创建 $dir" >&2
    return 1
  }
  local f="$dir/$(date +%Y%m%d-%H%M%S)-$$.txt"
  {
    echo "时间: $(date -Is)"
    echo "主机: $(hostname)"
    echo "通道: $channel"
    [ -n "$rc" ] && echo "通道返回: $rc"
    echo "标题: $subject"
    echo "----"
    printf '%s\n' "$body"
  } > "$f" 2>/dev/null
  echo "$f"
}

# ---------------------------------------------------------------- SMTP ------
# 与 ops_check.sh 读同一份 .env.ops（字段名完全一致），不新增第二套配置格式。
alert_smtp_configured() {
  [ -f "$ALERT_ENV_OPS" ] || return 1
  local host to
  host="$(grep -E '^OPS_SMTP_HOST=' "$ALERT_ENV_OPS" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '"'"'" | tr -d '[:space:]')"
  to="$(grep -E '^OPS_SMTP_TO=' "$ALERT_ENV_OPS" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '"'"'" | tr -d '[:space:]')"
  [ -n "$host" ] && [ -n "$to" ]
}

alert_send_smtp() {
  local subject="$1" body="$2"
  command -v python3 >/dev/null 2>&1 || { echo "缺少 python3"; return 1; }

  # 只导出 OPS_ 前缀，避免污染环境
  set -a
  # shellcheck disable=SC1090
  . "$ALERT_ENV_OPS" 2>/dev/null || { echo "读取 $ALERT_ENV_OPS 失败"; return 1; }
  set +a

  OPS_SUBJECT="$ALERT_PREFIX $subject" OPS_BODY="$body" \
  OPS_TIMEOUT="$ALERT_SMTP_TIMEOUT" python3 - <<'PY' 2>&1
import os, smtplib, ssl, sys
from email.message import EmailMessage

host = os.environ.get("OPS_SMTP_HOST", "").strip()
port = int(os.environ.get("OPS_SMTP_PORT", "465") or 465)
mode = (os.environ.get("OPS_SMTP_TLS", "ssl") or "ssl").lower()
user = os.environ.get("OPS_SMTP_USER", "").strip()
pw   = os.environ.get("OPS_SMTP_PASS", "").strip()
frm  = os.environ.get("OPS_SMTP_FROM", "").strip() or user
to   = [x.strip() for x in os.environ.get("OPS_SMTP_TO", "").split(",") if x.strip()]
timeout = int(os.environ.get("OPS_TIMEOUT", "20"))

if not host or not to:
    print("SMTP 未配置完整（缺 OPS_SMTP_HOST 或 OPS_SMTP_TO）")
    sys.exit(2)

msg = EmailMessage()
msg["Subject"] = os.environ.get("OPS_SUBJECT", "[Panomint 告警]")
msg["From"] = frm
msg["To"] = ", ".join(to)
msg.set_content(os.environ.get("OPS_BODY", ""))

ctx = ssl.create_default_context()
try:
    if mode == "ssl":
        s = smtplib.SMTP_SSL(host, port, timeout=timeout, context=ctx)
    elif mode == "starttls":
        s = smtplib.SMTP(host, port, timeout=timeout)
        s.starttls(context=ctx)
    else:
        s = smtplib.SMTP(host, port, timeout=timeout)
    with s:
        if user:
            s.login(user, pw)
        s.send_message(msg)
    print("MAIL_OK to=%s" % ",".join(to))
except Exception as e:
    print("MAIL_FAIL %s: %s" % (type(e).__name__, e))
    sys.exit(1)
PY
}

# ---------------------------------------------------------------- 入口 ------
# send_alert <subject> <body>
# 返回：0 已投递外部通道 / 3 仅落本地 / 1 用法错误
send_alert() {
  local subject="${1:-}"
  local body="${2:-}"
  if [ -z "$subject" ]; then
    echo "用法: send_alert <subject> <body>" >&2
    return 1
  fi

  if [ "$ALERT_DRY_RUN" = "1" ]; then
    echo "[DRY-RUN] $ALERT_PREFIX $subject"
    printf '%s\n' "$body"
    alert_spool "$subject" "$body" "dry-run" >/dev/null
    return 0
  fi

  # 无论外部通道成不成功，先落本地盘
  local spool; spool="$(alert_spool "$subject" "$body" "file-fallback")"

  if [ "$ALERT_FORCE_FILE" = "1" ]; then
    echo "已落盘: $spool（强制文件通道）"
    return 3
  fi

  if alert_smtp_configured; then
    local out rc
    out="$(alert_send_smtp "$subject" "$body")"
    rc=$?
    printf '%s\n' "$out"
    if [ $rc -eq 0 ]; then
      echo "已投递(SMTP)。本地副本: $spool"
      return 0
    fi
    echo "SMTP 投递失败，已落盘: $spool"
    return 3
  fi

  echo "外部通道不可用（.env.ops 未配置真实 SMTP 凭据），已落盘: $spool"
  return 3
}

# 通道自检：给「通道是不是哑的」一个可断言的答案
alert_channel_status() {
  if alert_smtp_configured; then
    echo "smtp-configured"
    return 0
  fi
  echo "file-only"
  return 3
}
