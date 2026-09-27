<!-- BootView：系统启动等待页（Job000131 T3-B BootGate）。
     守卫探测 /ready 未就绪时全站路由汇集到此：轮询就绪后自动回跳原目标；
     展示 postgres/valkey/disk 分项进度；3 分钟未就绪降级为排查指引 + 手动重试。 -->
<template>
  <div class="boot-view" data-testid="boot-view">
    <div class="boot-card">
      <h1 class="boot-title">系统启动中</h1>
      <p class="boot-sub" data-testid="boot-sub">{{ subtitle }}</p>
      <ul v-if="checks" class="boot-checks" data-testid="boot-checks">
        <li v-for="(v, k) in checks" :key="k" :data-state="v" :data-testid="'boot-check-' + k">
          <span class="dot" :class="'dot--' + v"></span>{{ label(k) }}：{{ v === 'ok' ? '就绪' : '等待中' }}
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
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { probeReady, invalidateBootCache } from '../api/boot'

const route = useRoute()
const router = useRouter()
const checks = ref(null)
const degraded = ref(false)
const waitedSec = ref(0)

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
onBeforeUnmount(() => clearTimeout(timer))
</script>

<style scoped>
.boot-view {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--color-bg, #f7f7f5);
}
.boot-card {
  max-width: 460px;
  padding: 32px;
  background: #fff;
  border-radius: 12px;
  border: 1px solid rgba(0, 0, 0, 0.08);
}
.boot-title {
  margin: 0 0 8px;
  font-size: 20px;
  font-weight: 500;
}
.boot-sub {
  margin: 0 0 16px;
  color: var(--color-text-secondary, #666);
  font-size: 14px;
}
.boot-checks {
  list-style: none;
  margin: 0 0 12px;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: 14px;
}
.dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 8px;
}
.dot--ok { background: #2e9e5b; }
.dot--fail { background: #d9a400; }
.boot-degraded {
  border-top: 1px solid rgba(0, 0, 0, 0.08);
  padding-top: 12px;
  font-size: 13px;
  color: var(--color-text-secondary, #666);
}
.boot-degraded ol { padding-left: 18px; margin: 6px 0 12px; }
.boot-degraded code {
  background: rgba(0, 0, 0, 0.05);
  padding: 1px 5px;
  border-radius: 4px;
}
</style>
