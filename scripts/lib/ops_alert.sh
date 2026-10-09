#!/usr/bin/env bash
# ops_alert.sh —— 告警适配器库（被 ops_drift_watch.sh / deploy.sh / rollback.sh 复用）
#
# 为什么是「适配器」而不是「一条通道」（2026-10-10 演进记录）
#   第一版只有本地文件 + SMTP 两个分支，结果卡在一个问题上：
#   .env.ops 里 OPS_SMTP_* 全是空串占位，邮件**从未真正投递过**，
#   于是告警只能落本地文件 —— 而「只写日志」等于「没人知道」。
#   第一版如实报出了这件事（这点是对的），但它**不能变成阻塞**：
#   用户手里可能有钉钉、可能有企微、也可能只配了 SMTP，
#   要求他先回答「你用哪个」才让告警生效，是把可选项做成了前置条件。
#
#   所以改成适配器：三个适配器并列存在，各读 .env.ops 里的一个键，
#   配了哪个就用哪个，全部没配才退化为纯本地。
#   **不需要用户先做决定**：填上任一适配器的键即可生效，其余留空即可。
#
# 三个适配器
#   1) file    本地文件，恒可用，永远执行。不算「外部送达」——
#            它只是证据留档。理由：把落盘当成送达，等于又造一个
#            「看起来有告警、实际没人收到」的哑通道。
#   2) webhook OPS_WEBHOOK_URL 非空则 POST JSON（钉钉/企微通用形状）
#   3) smtp    复用 .env.ops 已有的 OPS_SMTP_*，不新增第二套配置格式
#
# 送达判定（这是本库最要紧的语义，别看漏）
#   return 0  至少一个**外部**适配器（webhook / smtp）投递成功
#   return 3  没有任何外部适配器可用（纯本地留档）—— 巡检报 P1，需要凭据
#   return 4  外部适配器配了但**全部失败** —— 巡检报 P0「告警无法投递」
#   return 1  用法错误
#   返回 3 与返回 4 必须分开：前者是「还没配」，后者是「配了但坏了」。
#   混为一谈会导致「配错了 webhook 地址」被当成「没配」而长期无人处理。
#
# 每次 send_alert 都会把结果写进 $ALERT_STATE_DIR/last_delivery，
# 供 ops_drift_watch.sh 跨轮次判断「投递是不是持续失败」。
#
# 用法
#   source scripts/lib/ops_alert.sh
#   send_alert "标题" "正文"
#   ALERT_DRY_RUN=1  send_alert ...      # 只打印
#   ALERT_ADAPTERS="file" send_alert ...  # 强制只用某些适配器（自检用）
#   alert_channel_status                  # 打印当前适配器可用性
#   alert_channel_selftest                # 逐个适配器实发一遍并汇报

set -uo pipefail

# ---------------------------------------------------------------- 配置 ------
ALERT_ENV_OPS="${ALERT_ENV_OPS:-/home/warlocks/pano-album/.env.ops}"
ALERT_SPOOL_DIR="${ALERT_SPOOL_DIR:-/home/warlocks/.pano-ops/alerts}"
ALERT_STATE_DIR="${ALERT_STATE_DIR:-/home/warlocks/.pano-ops}"
ALERT_PREFIX="${ALERT_ALERT_PREFIX:-[Panomint 告警]}"
ALERT_DRY_RUN="${ALERT_DRY_RUN:-0}"
ALERT_SMTP_TIMEOUT="${ALERT_SMTP_TIMEOUT:-20}"
ALERT_WEBHOOK_TIMEOUT="${ALERT_WEBHOOK_TIMEOUT:-15}"
# 限制显式指定适配器（空格分隔）；留空 = 自动（file 常开 + 配了谁用谁）
ALERT_ADAPTERS="${ALERT_ADAPTERS:-}"

# 从 .env.ops 读一个键的值（去掉行首空白、引号、行尾空白）。
# ⚠️ 必须允许「键不存在」返回空而不是报错：.env.ops 是用户填的，
#    里面只会有他配了的那几个键，缺键是正常情况。
ops_env_get() {
  local key="$1"
  [ -f "$ALERT_ENV_OPS" ] || return 0
  local line
  line="$(grep -E "^[[:space:]]*${key}=" "$ALERT_ENV_OPS" 2>/dev/null | head -1)" || return 0
  [ -n "$line" ] || return 0
  local v="${line#*=}"
  v="${v%$'\r'}"
  # 去掉成对的首尾引号
  v="${v%\"}"; v="${v#\"}"
  v="${v%\'}"; v="${v#\'}"
  # 去掉首尾空白
  v="${v#"${v%%[![:space:]]*}"}"
  v="${v%"${v##*[![:space:]]}"}"
  printf '%s' "$v"
}

# ================================================================ 适配器 1：file
# 恒执行，永远成功。定位是「证据留档」，不是「外部送达」。
adapter_file() {
  local subject="$1" body="$2" meta="$3"
  local dir="$ALERT_SPOOL_DIR"
  if ! mkdir -p "$dir" 2>/dev/null; then
    echo "无法创建 $dir"
    return 1
  fi
  local f="$dir/$(date +%Y%m%d-%H%M%S)-$$.txt"
  {
    echo "时间: $(date -Is)"
    echo "主机: $(hostname)"
    echo "适配器: file（本地留档，非外部送达）"
    echo "标题: $ALERT_PREFIX $subject"
    echo "----"
    printf '%s\n' "$body"
    echo "----"
    printf '%s\n' "$meta"
  } > "$f" 2>/dev/null || { echo "写入 $f 失败"; return 1; }
  echo "$f"
  return 0
}

adapter_file_configured() { return 0; }   # 恒可用

# ================================================================ 适配器 2：webhook
# 钉钉 / 企微机器人的 JSON 形状一致（msgtype=text + text.content），
# 所以一个适配器覆盖两家。URL 由用户在 .env.ops 的 OPS_WEBHOOK_URL 填。
adapter_webhook_configured() {
  local url; url="$(ops_env_get OPS_WEBHOOK_URL)"
  [ -n "$url" ]
}

adapter_webhook() {
  local subject="$1" body="$2"
  local url; url="$(ops_env_get OPS_WEBHOOK_URL)"
  if [ -z "$url" ]; then
    echo "OPS_WEBHOOK_URL 为空"
    return 2
  fi
  command -v curl >/dev/null 2>&1 || { echo "缺少 curl"; return 2; }

  local out rc
  # 用 python3 拼 JSON：shell 手拼 JSON 在标题含引号/换行时会产出非法请求体，
  # 而 webhook 会静默丢弃非法体 —— 表现就是「配了但永远收不到」。
  out="$(
    ALERT_SUBJECT="$ALERT_PREFIX $subject" ALERT_BODY="$body" \
    ALERT_URL="$url" ALERT_TIMEOUT="$ALERT_WEBHOOK_TIMEOUT" python3 -c '
import json, os, subprocess, sys

url     = os.environ["ALERT_URL"]
subject = os.environ["ALERT_SUBJECT"]
body    = os.environ["ALERT_BODY"]
timeout = os.environ.get("ALERT_TIMEOUT", "15")

# 钉钉与企微机器人的文本消息形状相同；content 超出长度会被平台丢弃，
# 所以这里截断并在尾部留可辨识标记，避免「超长被丢」被误判成投递成功。
text = subject + "\n\n" + body
LIMIT = 3500
if len(text) > LIMIT:
    text = text[:LIMIT] + "\n…(已截断，原文见本地留档)"

payload = json.dumps({"msgtype": "text", "text": {"content": text}}, ensure_ascii=False)

# 一次调用同时取 http 码与退出码；curl -f 让 4xx/5xx 直接表现为非 0 退出码，
# 这样「HTTP 400 但 curl 自己成功」这种最容易被误判为投递成功的情况不会漏网。
p = subprocess.run(
    ["curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}",
     "--max-time", timeout, "-X", "POST",
     "-H", "Content-Type: application/json",
     "-d", payload, url],
    capture_output=True, text=True,
)
code = (p.stdout or "").strip()
if p.returncode != 0:
    print("WEBHOOK_FAIL curl_rc=%d http=%s %s"
          % (p.returncode, code, (p.stderr or "").strip()[:200]))
    sys.exit(1)
if not code.startswith("2"):
    print("WEBHOOK_FAIL http=%s" % code)
    sys.exit(1)
print("WEBHOOK_OK http=%s len=%d" % (code, len(text)))
' 2>&1)"
  rc=$?
  printf '%s\n' "$out"
  return $rc
}

# ================================================================ 适配器 3：smtp
# 复用 .env.ops 已有的 OPS_SMTP_*（与 ops_check.sh 完全同一套字段/格式）。
adapter_smtp_configured() {
  local host to
  host="$(ops_env_get OPS_SMTP_HOST)"
  to="$(ops_env_get OPS_SMTP_TO)"
  [ -n "$host" ] && [ -n "$to" ]
}

adapter_smtp() {
  local subject="$1" body="$2"
  command -v python3 >/dev/null 2>&1 || { echo "缺少 python3"; return 2; }

  ALERT_SUBJECT="$ALERT_PREFIX $subject" ALERT_BODY="$body" \
  ALERT_TIMEOUT="$ALERT_SMTP_TIMEOUT" python3 - <<'PY' 2>&1
import os, smtplib, ssl, sys
from email.message import EmailMessage

host = os.environ.get("OPS_SMTP_HOST", "").strip()
port = int(os.environ.get("OPS_SMTP_PORT", "465") or 465)
mode = (os.environ.get("OPS_SMTP_TLS", "ssl") or "ssl").lower()
user = os.environ.get("OPS_SMTP_USER", "").strip()
pw   = os.environ.get("OPS_SMTP_PASS", "").strip()
frm  = os.environ.get("OPS_SMTP_FROM", "").strip() or user
to   = [x.strip() for x in os.environ.get("OPS_SMTP_TO", "").split(",") if x.strip()]
timeout = int(os.environ.get("ALERT_TIMEOUT", "20"))

if not host or not to:
    print("SMTP 未配置完整（缺 OPS_SMTP_HOST 或 OPS_SMTP_TO）")
    sys.exit(2)

msg = EmailMessage()
msg["Subject"] = os.environ.get("ALERT_SUBJECT", "[Panomint 告警]")
msg["From"] = frm
msg["To"] = ", ".join(to)
msg.set_content(os.environ.get("ALERT_BODY", ""))

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
    print("SMTP_OK to=%s" % ",".join(to))
except Exception as e:
    print("SMTP_FAIL %s: %s" % (type(e).__name__, e))
    sys.exit(1)
PY
}

# ================================================================ 调度 ========
# 适配器注册表。新增渠道只需在这里加一行 + 写好 XXX_configured / XXX。
ADAPTER_NAMES="file webhook smtp"

adapter_configured() {
  case "$1" in
    file)    adapter_file_configured ;;
    webhook) adapter_webhook_configured ;;
    smtp)    adapter_smtp_configured ;;
    *)       return 1 ;;
  esac
}

adapter_send() {
  case "$1" in
    file)    shift; adapter_file    "$@" ;;
    webhook) shift; adapter_webhook "$@" ;;
    smtp)    shift; adapter_smtp    "$@" ;;
    *)       echo "未知适配器: $1"; return 1 ;;
  esac
}

# 本轮要尝试哪些适配器
adapter_selection() {
  if [ -n "$ALERT_ADAPTERS" ]; then
    printf '%s\n' $ALERT_ADAPTERS
    return
  fi
  local a
  for a in $ADAPTER_NAMES; do
    if adapter_configured "$a"; then printf '%s\n' "$a"; fi
  done
}

# 记录投递结果，供巡检跨轮次判断
_record_delivery() {
  local rc="$1" delivered="$2" detail="$3"
  local f="$ALERT_STATE_DIR/last_delivery"
  mkdir -p "$ALERT_STATE_DIR" 2>/dev/null || return 0
  {
    echo "ts=$(date +%s)"
    echo "ts_iso=$(date -Is)"
    echo "rc=$rc"
    echo "delivered=$delivered"
    echo "detail=$(printf '%s' "$detail" | tr '\n' ';')"
  } > "$f.tmp.$$" 2>/dev/null && mv -f "$f.tmp.$$" "$f" 2>/dev/null
  return 0
}

# -------------------------------------------------------------- send_alert ----
# send_alert <subject> <body>
# 返回 0=外部送达 / 3=无外部适配器可用 / 4=外部适配器全失败 / 1=用法错误
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
    adapter_file "$subject" "$body" "dry-run=true" >/dev/null
    return 0
  fi

  local spool
  spool="$(adapter_file "$subject" "$body" "外部适配器投递结果见下方 send_alert 输出")" || spool=""

  local -a tried=() ok=() failed=()
  local a out rc
  while IFS= read -r a; do
    [ -n "$a" ] || continue
    [ "$a" = "file" ] && continue        # file 已在上面执行，且不算外部送达
    tried+=("$a")
    out="$(adapter_send "$a" "$subject" "$body" 2>&1)"
    rc=$?
    if [ $rc -eq 0 ]; then
      ok+=("$a")
      printf '[%s] %s\n' "$a" "$out"
    else
      failed+=("$a(rc=$rc)")
      printf '[%s] 失败: %s\n' "$a" "$out" >&2
    fi
  done <<< "$(adapter_selection)"

  if [ "${#ok[@]}" -gt 0 ]; then
    # 明确写出「投递到了哪个」——否则又变成「以为发出去了其实没发」
    local detail="delivered_via=${ok[*]} spool=$spool"
    echo "已外部送达（适配器: ${ok[*]}）。本地留档: ${spool:-无}"
    _record_delivery 0 "${ok[*]}" "$detail"
    return 0
  fi

  if [ "${#failed[@]}" -gt 0 ]; then
    # 配了但全坏 —— P0
    local detail="tried=${tried[*]} failed=${failed[*]} spool=$spool"
    echo "外部适配器全部失败（尝试: ${tried[*]}；失败: ${failed[*]}）。本地留档: ${spool:-无}" >&2
    _record_delivery 4 "" "$detail"
    return 4
  fi

  # 一个外部适配器都没配
  local detail="no_external_adapter spool=$spool"
  echo "无外部告警适配器可用（未配置 OPS_WEBHOOK_URL / OPS_SMTP_*）。本地留档: ${spool:-无}" >&2
  _record_delivery 3 "" "$detail"
  return 3
}

# ================================================================ 保留策略 ----
# 告警目录是**运维留档**，不是日志归档。
# 2026-10-10 实测问题：一条常驻 P1 每 2 分钟产生一份文件，38 分钟 17 份、
# 23.4KB；按 cron 频率折算约 720 条/天、约 200MB/年。纯噪声。
# 重复上千次的告警等于没有告警 —— 它只会训练人忽略它。
#
# 两条策略（按天数 / 按条数）同时生效，**先满足者先删**：
#   - 超过 KEEP_DAYS 的文件删掉
#   - 只保留最近 KEEP_FILES 份，超出的最旧的删掉
# 取二者中更严格的保留量，避免磁盘被占满。
#
# 只删自己产生的 *.txt，不碰目录里的其它东西；删不掉的（权限等）跳过而非报错，
# 避免轮转本身成为新的故障源。
prune_alert_spool() {
  local keep_days="${ALERT_SPOOL_KEEP_DAYS:-30}"
  local keep_files="${ALERT_SPOOL_KEEP_FILES:-500}"
  local dir="$ALERT_SPOOL_DIR"

  [ -d "$dir" ] || return 0
  # 关掉即可停用；0 表示不按该项删
  [ "$keep_days" -gt 0 ] 2>/dev/null || keep_days=0
  [ "$keep_files" -gt 0 ] 2>/dev/null || keep_files=0
  [ "$keep_days" -eq 0 ] && [ "$keep_files" -eq 0 ] && return 0

  local -a files=()
  local f
  while IFS= read -r f; do
    [ -f "$f" ] && files+=("$f")
  done < <(find "$dir" -maxdepth 1 -type f -name '*.txt' 2>/dev/null | sort)

  local total="${#files[@]}"
  [ "$total" -eq 0 ] && return 0

  local -a doomed=()
  local cutoff now
  now="$(date +%s)"

  # 策略一：按天数
  if [ "$keep_days" -gt 0 ]; then
    cutoff=$(( now - keep_days * 86400 ))
    for f in "${files[@]}"; do
      local mt; mt="$(stat -c %Y "$f" 2>/dev/null)" || continue
      [ "$mt" -lt "$cutoff" ] && doomed+=("$f")
    done
  fi

  # 策略二：按条数（files 已按名字排序 = 按时间排序，末尾最新）
  if [ "$keep_files" -gt 0 ] && [ "$total" -gt "$keep_files" ]; then
    local cut=$(( total - keep_files ))
    local i=0
    for f in "${files[@]}"; do
      [ "$i" -lt "$cut" ] && doomed+=("$f")
      i=$(( i + 1 ))
    done
  fi

  [ "${#doomed[@]}" -eq 0 ] && return 0

  # 去重后再删（两条策略可能命中同一个文件）
  local -a uniq=()
  local seen=""
  for f in "${doomed[@]}"; do
    case "$seen" in *"|$f|"*) continue ;; esac
    seen="$seen|$f|"
    uniq+=("$f")
  done

  local n=0
  for f in "${uniq[@]}"; do
    rm -f "$f" 2>/dev/null && n=$(( n + 1 ))
  done
  printf '[prune] 告警目录轮转：删除 %d 份（原有 %d 份，保留策略 天数=%s 条数=%s）\n' \
    "$n" "$total" "$keep_days" "$keep_files"
  return 0
}

# ---------------------------------------------------------------- 状态 ------
# alert_channel_status：打印一行人类可读状态，返回码语义同 send_alert
alert_channel_status() {
  local avail=() ext=0
  local a
  for a in $ADAPTER_NAMES; do
    if adapter_configured "$a"; then avail+=("$a"); fi
  done
  for a in "${avail[@]}"; do [ "$a" = "file" ] || ext=1; done

  if [ "$ext" -eq 1 ]; then
    echo "adapters=${avail[*]} (file 为留档，不计入送达)"
    return 0
  fi
  echo "adapters=${avail[*]} | 无外部适配器（仅本地留档）"
  return 3
}

# 逐个适配器实发并汇报，用于「通道到底通不通」的一次性自检
alert_channel_selftest() {
  local rc_all=0
  echo "=== 告警适配器自检 @ $(date -Is) ==="
  local a
  for a in $ADAPTER_NAMES; do
    if adapter_configured "$a" && [ "$a" != "file" ]; then
      echo "--- $a：已配置，实发 ---"
      adapter_send "$a" "适配器自检" "这是一条来自 $(hostname) 的适配器自检消息（$(date -Is)）。收到即表示该通道可用。" 2>&1 | sed 's/^/    /'
      local rc=${PIPESTATUS[0]}
      [ $rc -ne 0 ] && rc_all=1
    elif [ "$a" = "file" ]; then
      echo "--- $a：恒可用 ---"
      adapter_file "适配器自检" "本地留档自检" "selftest" >/dev/null 2>&1
      echo "    OK（本地留档）"
    else
      echo "--- $a：未配置（跳过） ---"
    fi
  done
  echo "=== 自检结束 rc=$rc_all ==="
  return $rc_all
}