<template>
  <Teleport to="body">
    <div v-if="visible" ref="tipEl" class="media-tooltip" :style="tipStyle" role="tooltip">
      <div class="tt-name">{{ item.filename }}</div>
      <div class="tt-row">拍摄日期：{{ dateText }}</div>
      <div v-if="item.place" class="tt-row">拍摄地点：{{ item.place }}</div>
      <div v-if="item.type === 'video' && item.duration != null" class="tt-row">
        时长：{{ durationText }}
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

const props = defineProps({
  item: { type: Object, required: true },
  // 锚点元素（ThumbItem 根节点），浮窗相对它定位
  anchor: { type: Object, default: null }
})

const HOVER_DELAY = 400
const GAP = 6
const EDGE = 8

const hoverCapable =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(hover: hover)').matches

const visible = ref(false)
const tipEl = ref(null)
const pos = ref({ left: -9999, top: -9999 })
let timer = null

const tipStyle = computed(() => ({ left: `${pos.value.left}px`, top: `${pos.value.top}px` }))

const dateText = computed(() => {
  const t = props.item.taken_at
  if (!t) return '未知'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return '未知'
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
})

const durationText = computed(() => {
  const sec = props.item.duration
  if (!sec && sec !== 0) return ''
  const s = Math.round(sec)
  const m = Math.floor(s / 60)
  return `${m}:${String(s % 60).padStart(2, '0')}`
})

function clearTimer() {
  if (timer) {
    clearTimeout(timer)
    timer = null
  }
}

function hide() {
  clearTimer()
  visible.value = false
}

async function show() {
  const el = props.anchor
  if (!el) return
  // 先渲染到屏外，测量后再定位，避免位置闪烁
  pos.value = { left: -9999, top: -9999 }
  visible.value = true
  await nextTick()
  const tip = tipEl.value
  if (!tip) return
  const rect = el.getBoundingClientRect()
  // 卡片已滚出视口则不显示
  if (rect.bottom < 0 || rect.top > window.innerHeight) {
    hide()
    return
  }
  const tw = tip.offsetWidth
  const th = tip.offsetHeight
  // 优先卡片下方，空间不足则翻转到上方
  let top = rect.bottom + GAP
  if (top + th + EDGE > window.innerHeight && rect.top - GAP - th >= EDGE) {
    top = rect.top - GAP - th
  }
  let left = rect.left
  left = Math.max(EDGE, Math.min(left, window.innerWidth - tw - EDGE))
  top = Math.max(EDGE, Math.min(top, window.innerHeight - th - EDGE))
  pos.value = { left, top }
}

function onEnter() {
  clearTimer()
  timer = setTimeout(show, HOVER_DELAY)
}

function attach(el) {
  if (!el || !hoverCapable) return
  el.addEventListener('mouseenter', onEnter)
  el.addEventListener('mouseleave', hide)
}

function detach(el) {
  if (!el) return
  el.removeEventListener('mouseenter', onEnter)
  el.removeEventListener('mouseleave', hide)
}

watch(
  () => props.anchor,
  (el, old) => {
    detach(old)
    attach(el)
  },
  { immediate: true, flush: 'post' }
)

// 滚动/缩放时浮窗立即消失（含卡片滚出视口的情况）
window.addEventListener('scroll', hide, true)
window.addEventListener('resize', hide)

onBeforeUnmount(() => {
  detach(props.anchor)
  clearTimer()
  window.removeEventListener('scroll', hide, true)
  window.removeEventListener('resize', hide)
})
</script>

<style scoped>
.media-tooltip {
  position: fixed;
  z-index: 1000;
  max-width: 280px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  box-shadow: var(--shadow-card);
  font-size: var(--font-size-sm);
  line-height: 1.5;
  color: var(--color-text-secondary);
  pointer-events: none;
  word-break: break-all;
}

.tt-name {
  color: var(--color-text-primary);
  font-weight: 500;
  margin-bottom: 2px;
}
</style>
