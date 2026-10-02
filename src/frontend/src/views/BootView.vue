<!-- BootView：系统启动等待页（Job000131 T3-B BootGate）。
     守卫探测 /ready 未就绪时全站路由汇集到此：轮询就绪后自动回跳原目标；
     展示 postgres/valkey/disk 分项进度；3 分钟未就绪降级为排查指引 + 手动重试。
     深化（DESIGN.md VIEW.12）：总进度 + 分项进度条 = 已等待 / 历史启动时长估算
     （localStorage 近 5 次启动中位数；无历史时不确定蠕动，封顶 100% 就绪即跳）。 -->
<template>
  <div class="boot-view" data-testid="boot-view">
    <span class="wordmark">PANOMINT</span>
    <div class="boot-card">
      <p class="boot-eyebrow">PANOMINT · BOOT GATE</p>
      <h1 class="boot-title">系统启动中</h1>
      <p class="boot-sub" data-testid="boot-sub">{{ subtitle }}<template v-if="!degraded"> · 已等待 {{ waitedSec }}s</template></p>

      <!-- 总进度：已等待 / 历史启动时长估算 -->
      <div class="btotal" data-testid="boot-progress">
        <div class="btop">
          <span class="btpct">{{ totalPct }}%</span>
          <span class="bteta">{{ totalEta }}</span>
        </div>
        <div class="btrack"><div class="btfill" :style="{ width: totalPct + '%' }"></div></div>
      </div>

      <ul v-if="checks" class="boot-checks" data-testid="boot-checks">
        <li
          v-for="(v, k) in checks"
          :key="k"
          class="bcheck"
          :class="{ 'is-ok': v === 'ok' }"
          :data-state="v"
          :data-testid="'boot-check-' + k"
        >
          <span class="dot" :class="'dot--' + v"></span>
          <span class="bname">{{ label(k) }}</span>
          <span class="beta">{{ v === 'ok' ? '已就绪' : '预计还需 ~' + itemEta + 's' }}</span>
          <span class="bst">{{ v === 'ok' ? '就绪' : '等待中' }}</span>
          <div class="bprog"><div class="bfill" :style="{ width: (v === 'ok' ? 100 : Math.min(96, totalPct)) + '%' }"></div></div>
        </li>
      </ul>

      <div v-if="degraded" class="boot-degraded" data-testid="boot-degraded">
        <p>等待时间较长，请检查：</p>
        <ol>
          <li>api 容器日志：<code>docker logs &lt;api容器名&gt;</code></li>
          <li>数据库与队列容器状态：<code>docker ps</code>（应全部 Up）</li>
          <li>磁盘空间是否已满</li>
        </ol>
        <button class="btn" data-testid="boot-retry" @click="retry">重试</button>
      </div>

      <p class="bnote">百分比 = 已等待 / 历史启动时长估算 · 就绪即满格并自动回跳</p>
    </div>
    <span class="vermark">BOOT GATE · 就绪后自动回跳</span>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import gsap from 'gsap'
import { probeReady, invalidateBootCache } from '../api/boot'

const route = useRoute()
const router = useRouter()
const checks = ref(null)
const degraded = ref(false)
const waitedSec = ref(0)

// ---- 启动时长估算（DESIGN.md VIEW.12）：localStorage 近 5 次启动耗时中位数 ----
const HIST_KEY = 'pano_boot_hist'
function loadHist() {
  try {
    const arr = JSON.parse(localStorage.getItem(HIST_KEY) || '[]')
    return Array.isArray(arr) ? arr.filter((n) => typeof n === 'number' && n > 0).slice(-5) : []
  } catch {
    return []
  }
}
function recordBoot(ms) {
  try {
    const hist = loadHist()
    hist.push(ms)
    localStorage.setItem(HIST_KEY, JSON.stringify(hist.slice(-5)))
  } catch {
    /* localStorage 不可用时静默：估算退化为不确定蠕动 */
  }
}
const hist = loadHist()
const estMs = hist.length ? hist.slice().sort((a, b) => a - b)[Math.floor(hist.length / 2)] : 0

const totalPct = computed(() => {
  if (degraded.value) return 100
  if (!estMs) return Math.min(96, waitedSec.value * 2) // 无历史：不确定蠕动（约 50s 到顶）
  return Math.min(100, Math.round((waitedSec.value * 1000) / estMs * 100))
})
const totalEta = computed(() => {
  if (degraded.value) return '已降级为排查模式'
  if (!estMs) return '首次启动 · 时长未知'
  const remain = Math.max(0, Math.round((estMs - waitedSec.value * 1000) / 1000))
  return remain > 0 ? `预计还需 ~${remain}s（基于近 ${hist.length} 次启动）` : '即将就绪…'
})
// 分项预计剩余（无分依赖计时数据，与总估算一致——诚实显示）
const itemEta = computed(() => {
  if (!estMs) return '—'
  return Math.max(1, Math.round((estMs - waitedSec.value * 1000) / 1000))
})

const subtitle = computed(() =>
  degraded.value
    ? '服务长时间未就绪'
    : checks.value
      ? '正在等待依赖服务…'
      : '正在连接服务…'
)

const LABELS = { postgres: '数据库', valkey: '任务队列', disk: '存储磁盘' }
function label(k) {
  return LABELS[k] || k
}

let timer = 0
let delay = 2000
let attempts = 0

async function poll() {
  attempts += 1
  waitedSec.value = Math.round((attempts * delay) / 1000)
  if (waitedSec.value >= 180 && !degraded.value) degraded.value = true
  const st = await probeReady()
  checks.value = st.checks
  if (st.ready) {
    recordBoot(waitedSec.value * 1000 || attempts * delay) // 记录本次启动耗时进历史
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    // 防自环：redirect 指向 /boot 时回首页
    const target = redirect.startsWith('/boot') ? '/' : redirect
    invalidateBootCache()
    router.replace(target)
    return
  }
  delay = Math.min(delay * 1.3, 5000) // 指数退避至 5s 封顶
  timer = setTimeout(poll, delay)
}

function retry() {
  invalidateBootCache()
  delay = 2000
  degraded.value = false
  poll()
}

onMounted(poll)

// DESIGN.md §8 card-in：启动卡 y16 淡入（reduced-motion 跳过）
let cardCtx = null
onMounted(() => {
  const mm = gsap.matchMedia()
  mm.add('(prefers-reduced-motion: no-preference)', () => {
    cardCtx = gsap.context(() => {
      gsap.from('.boot-card', { y: 16, opacity: 0, duration: 0.55, ease: 'power2.out' })
    })
  })
})
onUnmounted(() => {
  if (cardCtx) cardCtx.revert()
})

onBeforeUnmount(() => clearTimeout(timer))
</script>

<style scoped>
.boot-view {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  background-color: var(--color-bg, #e9e4de);
  /* mist-wash：双团超淡雾色（对齐登录页氛围） */
  background-image: radial-gradient(900px 420px at 12% -8%, rgba(244, 241, 237, 0.55), transparent 60%),
    radial-gradient(760px 380px at 92% 108%, rgba(74, 90, 106, 0.06), transparent 60%);
}

.wordmark {
  position: absolute;
  top: 18px;
  left: 36px;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.22em;
  color: var(--color-text-disabled, #a5a29a);
}

.vermark {
  position: absolute;
  bottom: 16px;
  right: 36px;
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.1em;
  color: var(--color-text-disabled, #a5a29a);
}

/* 卡片规范：无边框米白面 + 双层漫射阴影 */
.boot-card {
  max-width: 460px;
  width: 460px;
  padding: 36px 34px 24px;
  background: var(--color-surface, #f4f1ed);
  border-radius: var(--radius-lg, 14px);
  box-shadow: var(--shadow-card, 0 1px 3px rgba(65, 64, 60, 0.05), 0 10px 34px rgba(65, 64, 60, 0.07));
}
.boot-eyebrow {
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.2em;
  color: var(--color-text-disabled, #a5a29a);
  text-align: center;
  margin-bottom: 10px;
}
.boot-title {
  margin: 0 0 8px;
  font-size: 20px;
  font-weight: 500;
  letter-spacing: 0.12em;
  text-align: center;
  color: var(--color-text-primary, #41403c);
}
.boot-sub {
  margin: 0 0 18px;
  color: var(--color-text-secondary, #7d7a73);
  font-size: 11px;
  font-family: var(--font-mono);
  letter-spacing: 0.02em;
  text-align: center;
}

/* 总进度：已等待 / 历史启动时长估算 */
.btotal { margin: 0 0 16px; }
.btop { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 7px; }
.btpct { font-family: var(--font-mono); font-size: 15px; color: var(--color-primary, #4a5a6a); }
.bteta { font-family: var(--font-mono); font-size: 10.5px; color: var(--color-text-disabled, #a5a29a); }
.btrack { height: 5px; border-radius: 999px; background: rgba(74, 90, 106, 0.1); overflow: hidden; }
.btfill {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--stat-pano-photo, #c4a57a), var(--color-primary, #4a5a6a));
  transition: width 1s cubic-bezier(0.32, 0.72, 0, 1);
}

.boot-checks {
  list-style: none;
  margin: 0 0 8px;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.bcheck {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 14px 13px;
  border-radius: 10px;
  background: rgba(74, 90, 106, 0.05);
  font-size: 13px;
  flex-wrap: wrap;
}
.bname { flex: 1; min-width: 0; }
.beta { font-family: var(--font-mono); font-size: 10px; color: var(--color-text-disabled, #a5a29a); margin-left: auto; }
.bst { font-family: var(--font-mono); font-size: 11px; color: var(--color-text-secondary, #7d7a73); }
.bprog { flex-basis: 100%; height: 3px; border-radius: 999px; background: rgba(74, 90, 106, 0.1); overflow: hidden; margin-top: 9px; }
.bprog .bfill {
  height: 100%;
  border-radius: 999px;
  background: var(--stat-pano-photo, #c4a57a);
  transition: width 1s cubic-bezier(0.32, 0.72, 0, 1);
}
.bcheck.is-ok .bprog .bfill { background: var(--color-success, #6e8a72); }

.dot {
  display: inline-block;
  width: 9px;
  height: 9px;
  border-radius: 50%;
  flex-shrink: 0;
}
.dot--ok { background: var(--color-success, #6e8a72); }
.dot--fail { background: var(--stat-pano-photo, #c4a57a); animation: pulse 1.6s ease-in-out infinite; }
@keyframes pulse {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(196, 165, 122, 0.4); }
  50% { opacity: 0.55; box-shadow: 0 0 0 5px rgba(196, 165, 122, 0); }
}
@media (prefers-reduced-motion: reduce) {
  .dot--fail { animation: none; }
  .btfill, .bprog .bfill { transition: none; }
}

.bnote {
  font-family: var(--font-mono);
  font-size: 9.5px;
  color: var(--color-text-disabled, #a5a29a);
  text-align: center;
  margin: 14px 0 0;
}

.boot-degraded {
  border-top: 1px solid var(--color-border, #ddd8d0);
  padding-top: 12px;
  font-size: 13px;
  color: var(--color-text-secondary, #7d7a73);
  margin-top: 8px;
}
.boot-degraded ol { padding-left: 18px; margin: 6px 0 12px; }
.boot-degraded code {
  background: rgba(74, 90, 106, 0.08);
  padding: 1px 5px;
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 11px;
}
.boot-degraded .btn {
  height: 34px;
  padding: 0 16px;
  border: 1px solid var(--color-border, #ddd8d0);
  border-radius: 9px;
  background: var(--color-surface, #f4f1ed);
  color: var(--color-text-primary, #41403c);
  font-family: var(--font-family);
  font-size: 12.5px;
  cursor: pointer;
}
</style>
