<template>
  <!-- 权限门：与 GET /admin/debug/status 同一把锁——403（无 admin:system）时整卡不渲染，
       前端不暴露入口，后端也不给状态，不存在"只读泄露"通道（设计 §2.2/§11） -->
  <section v-if="!hidden" class="card">
    <div class="card-head">
      <h2 class="card-title">转码远程调试</h2>
      <span class="state" :class="stateClass" data-testid="debug-status">{{ stateLabel }}</span>
    </div>
    <p class="card-desc">
      一次性、短寿命的排障通道：开启后得到一个带密钥的 WebSocket 接入地址，调试代理（debugctl）
      凭它远程查看转码队列、暂停/恢复/取消排队任务。<strong>接入凭据只展示一次</strong>，到期或关闭即作废。
      仅用于排查转码故障，平时应保持关闭。
    </p>

    <p v-if="err" class="msg msg--error" data-testid="debug-err">{{ err }}</p>
    <p v-if="msg" class="msg" :class="msgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="debug-msg">{{ msg }}</p>

    <!-- ① 凭据回显（一次性）：开启/重置后仅此一次（子组件自持复制降级） -->
    <DebugCredsPanel v-if="creds" :creds="creds" :busy="busy" @done="onCredsDone" />

    <!-- ② 关闭态：开启入口 -->
    <template v-else-if="!status.enabled">
      <div class="actions">
        <label class="inline-field">
          <span class="inline-label">有效期</span>
          <select v-model.number="ttl" data-testid="debug-ttl" :disabled="busy">
            <option :value="1">1 小时</option>
            <option :value="8">8 小时</option>
            <option :value="24">24 小时</option>
            <option :value="72">72 小时</option>
          </select>
        </label>
        <button class="btn" data-testid="debug-toggle" :disabled="busy || loading" @click="onEnable">
          {{ busy ? '开启中…' : '开启远程调试' }}
        </button>
      </div>
      <p class="hint">调试代理凭「接入地址 + 密钥」经 WSS 直连本服务；有效期到后通道自动作废。</p>
    </template>

    <!-- ③ 开启态：倒计时 + 重置/关闭 -->
    <template v-else>
      <dl class="info-list">
        <div class="info-row">
          <dt>剩余时间</dt>
          <dd :class="{ 'countdown--warn': countdownWarn }" data-testid="debug-countdown">{{ countdownText }}</dd>
        </div>
        <div v-if="status.last_connect_at" class="info-row">
          <dt>最近接入</dt>
          <dd>{{ formatTime(status.last_connect_at) }}<span v-if="status.last_connect_ip">（{{ status.last_connect_ip }}）</span></dd>
        </div>
      </dl>
      <div class="actions">
        <button class="btn" data-testid="debug-rotate" :disabled="busy" @click="onRotate">
          {{ busy ? '处理中…' : (confirming === 'rotate' ? '再点一次确认重置（会踢掉当前连接）' : '重置密钥') }}
        </button>
        <button class="btn btn--danger" data-testid="debug-off" :disabled="busy" @click="onDisable">
          {{ busy ? '处理中…' : (confirming === 'off' ? '再点一次确认关闭（会踢掉当前连接）' : '关闭调试') }}
        </button>
      </div>
      <p class="hint">重置密钥后旧密钥立即失效、正在连接的代理会被断开；关闭后通道即作废，两者都需点两次确认，防止误触。</p>
    </template>

    <!-- ④ 审计摘要（子组件） -->
    <DebugAuditList v-if="auditItems.length" :items="auditItems" />
  </section>
</template>

<script setup>
// 转码远程调试设置卡（Job000121，设计 §11）——SettingsView 挂卡，范式同 AppPasswordCard：
// rotate/off 两步确认（3 秒自动还原）、操作失败即时刷新对齐服务端真值。
// 倒计时由 expires_at 本地推算 + 5s 轮询校准，不信赖前端时钟做权威判断（设计 §8.3）。
// 行数拆分：凭据回显 → DebugCredsPanel；审计摘要 → DebugAuditList（O1 棘轮）。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { errMessage, errCode } from '../../stores/auth'
import { getDebugStatus, enableDebug, rotateDebug, disableDebug } from '../../api/debug'
import { listAudit } from '../../api/admin'
import DebugCredsPanel from './DebugCredsPanel.vue'
import DebugAuditList from './DebugAuditList.vue'

const hidden = ref(false) // 无 admin:system → 整卡不渲染（与服务端同一把锁）
const loading = ref(false)
const busy = ref(false)
const confirming = ref('') // '' | 'rotate' | 'off'（两步确认态）
const ttl = ref(24)
const status = ref({ enabled: false, connected: false })
const creds = ref(null) // {url, key, expires_at} 一次性回显
const auditItems = ref([])
const err = ref('')
const msg = ref('')
const msgKind = ref('ok')
const now = ref(Date.now())

let pollTimer = null
let tickTimer = null
let confirmTimer = null

const stateLabel = computed(() => {
  if (!status.value.enabled) return '未开启'
  return status.value.connected ? '已连接' : '待连接'
})
const stateClass = computed(() => ({
  'state--on': status.value.enabled && status.value.connected,
  'state--warn': status.value.enabled && !status.value.connected,
  'state--off': !status.value.enabled
}))

// 倒计时：本地秒级推算；<30 分钟警示（设计 §11）
const remainingMs = computed(() => {
  if (!status.value.enabled || !status.value.expires_at) return 0
  return Math.max(0, new Date(status.value.expires_at).getTime() - now.value)
})
const countdownWarn = computed(() => remainingMs.value > 0 && remainingMs.value < 30 * 60 * 1000)
const countdownText = computed(() => {
  if (!status.value.enabled) return '—'
  const s = Math.floor(remainingMs.value / 1000)
  if (s <= 0) return '已到期'
  const h = String(Math.floor(s / 3600)).padStart(2, '0')
  const m = String(Math.floor((s % 3600) / 60)).padStart(2, '0')
  const ss = String(s % 60).padStart(2, '0')
  return `${h}:${m}:${ss}`
})

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}
function ok(text) { msgKind.value = 'ok'; msg.value = text }
function fail(e, fallback) { msgKind.value = 'error'; msg.value = errMessage(e, fallback) }

function startTimers() {
  stopTimers()
  tickTimer = setInterval(() => { now.value = Date.now() }, 1000)
  if (status.value.enabled) {
    pollTimer = setInterval(refresh, 5000) // 开启态 5s 轮询（设计 §8.3）
  }
}
function stopTimers() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  if (tickTimer) { clearInterval(tickTimer); tickTimer = null }
}

async function refresh() {
  try {
    const [st, au] = await Promise.all([
      getDebugStatus(),
      listAudit({ target_type: 'debug_channel', limit: 8 })
    ])
    status.value = st || { enabled: false, connected: false }
    auditItems.value = au?.items || []
    startTimers() // enabled 翻转时启停轮询
  } catch (e) {
    // 轮询失败不打断界面：下轮再试（持久失败时操作按钮仍会报错）
    if (e?.response?.status === 403) hidden.value = true // 权限被收回 → 立即隐藏
  }
}

onMounted(async () => {
  loading.value = true
  try {
    const st = await getDebugStatus()
    status.value = st || { enabled: false, connected: false }
    startTimers()
    refresh() // 顺带拉审计摘要
  } catch (e) {
    if (e?.response?.status === 403) {
      hidden.value = true // 无权限：整卡不渲染
      return
    }
    err.value = errMessage(e, '读取调试通道状态失败')
    startTimers()
  } finally {
    loading.value = false
  }
})

onUnmounted(stopTimers)

async function onEnable() {
  busy.value = true
  msg.value = ''
  try {
    const res = await enableDebug(ttl.value)
    creds.value = { url: res.url, key: res.key, expires_at: res.expires_at }
    await refresh() // 变更后即时刷新，不等轮询
    ok('调试通道已开启。请立即保存接入地址与密钥——密钥只显示这一次。')
  } catch (e) {
    if (errCode(e) === 'DEBUG_ALREADY_ENABLED') {
      // 并发/重复开启：如实提示并刷新状态（设计 §8.1，不轮换正在使用的密钥）
      await refresh().catch(() => {})
      ok('调试通道已处于开启状态，已为你刷新最新状态。')
    } else {
      fail(e, '开启失败，请重试')
    }
  } finally {
    busy.value = false
  }
}

function armConfirm(kind) {
  if (confirming.value === kind) {
    clearTimeout(confirmTimer)
    confirming.value = ''
    return true
  }
  confirming.value = kind
  clearTimeout(confirmTimer)
  confirmTimer = setTimeout(() => { confirming.value = '' }, 3000)
  return false
}

async function onRotate() {
  if (!armConfirm('rotate')) return
  busy.value = true
  msg.value = ''
  try {
    const res = await rotateDebug() // 缺省沿用原档位
    creds.value = { url: res.url, key: res.key, expires_at: res.expires_at }
    await refresh()
    ok('密钥已重置，旧密钥与旧连接已失效。请立即保存新密钥——只显示这一次。')
  } catch (e) {
    await refresh().catch(() => {}) // 失败回滚姿态：对齐服务端真值
    fail(e, '重置失败，请重试')
  } finally {
    busy.value = false
  }
}

async function onDisable() {
  if (!armConfirm('off')) return
  busy.value = true
  msg.value = ''
  try {
    await disableDebug()
    creds.value = null
    await refresh()
    ok('调试通道已关闭，活动连接已被断开。')
  } catch (e) {
    await refresh().catch(() => {})
    fail(e, '关闭失败，请重试')
  } finally {
    busy.value = false
  }
}

function onCredsDone() {
  creds.value = null
  msg.value = ''
}
</script>

<style scoped>
.card { background-color: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-lg); padding: 20px; margin-bottom: 16px; }
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.card-title { margin: 0; font-size: 16px; }
.card-desc { margin: 10px 0 0; color: var(--color-text-secondary); font-size: var(--font-size-sm); line-height: 1.6; }
.state { font-size: var(--font-size-sm); padding: 2px 8px; border-radius: 999px; border: 1px solid var(--color-border); }
.state--on { color: var(--color-success); border-color: var(--color-success); }
.state--warn { color: var(--color-warning); border-color: var(--color-warning); }
.state--off { color: var(--color-text-secondary); }
.actions { display: flex; align-items: flex-end; gap: 10px; flex-wrap: wrap; margin-top: 14px; }
.inline-field { display: flex; flex-direction: column; gap: 6px; }
.inline-label { font-size: var(--font-size-sm); color: var(--color-text-secondary); }
select { height: 38px; padding: 0 8px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); font-size: var(--font-size-md); background-color: var(--color-surface); outline: none; }
.btn { height: 38px; padding: 0 16px; border: 1px solid transparent; border-radius: var(--radius-sm); background-color: var(--color-primary); color: #fff; font-size: var(--font-size-sm); }
.btn:hover:not(:disabled) { background-color: var(--color-primary-hover); }
.btn:disabled { opacity: 0.6; cursor: not-allowed; }
.btn--danger { background-color: var(--color-danger); }
.hint { margin: 8px 0 0; font-size: var(--font-size-xs, 12px); color: var(--color-text-secondary); line-height: 1.6; }
.msg { margin: 12px 0 0; font-size: var(--font-size-sm); }
.msg--error { color: var(--color-danger); }
.msg--ok { color: var(--color-success); }
.info-list { margin: 12px 0 0; }
.info-row { display: flex; gap: 12px; padding: 4px 0; font-size: var(--font-size-sm); }
.info-row dt { width: 72px; color: var(--color-text-secondary); flex: none; }
.info-row dd { margin: 0; font-variant-numeric: tabular-nums; }
.countdown--warn { color: var(--color-danger); font-weight: 600; }
</style>
