<template>
  <!-- Job000143：右键 / 长按 600ms 打开操作菜单。
       手势判定在**本组件**（卡片自己），弹层渲染交给 ThumbContextMenu。
       这样安排的原因：菜单 Teleport 到 body，事件无法冒泡回卡片，
       若把判定放进子组件，就得靠 ref 穿透，反而更绕。
       ⚠️ @contextmenu 必须 .prevent，否则浏览器原生菜单会先弹出。 -->
  <div
    ref="rootEl"
    class="thumb"
    :class="{ 'thumb--selected': selected }"
    @click="onClick"
    @contextmenu.prevent="onContextMenu"
    @touchstart.passive="onTouchStart"
    @touchmove.passive="onTouchMove"
    @touchend.passive="onTouchEnd"
    @touchcancel.passive="onTouchEnd"
  >
    <label v-if="selectable" class="thumb-check" @click.stop>
      <input
        type="checkbox"
        :checked="selected"
        data-testid="thumb-check"
        @change="emit('toggle', item)"
      />
    </label>
    <img v-if="url" :src="url" :alt="item.filename" class="thumb-img" loading="lazy" />
    <!-- 晨雾 veil（DESIGN.md §4 媒体卡）：顶部薄雾让媒体「沉」进暖灰界面 -->
    <div v-if="url" class="thumb-veil" aria-hidden="true"></div>
    <div v-else class="thumb-placeholder">
      <svg viewBox="0 0 24 24" width="28" height="28" fill="none">
        <rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.5" />
        <path d="M3 16l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
        <circle cx="9" cy="8" r="1.6" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </div>

    <span v-if="is360" class="badge badge-360">
      <svg viewBox="0 0 24 24" width="11" height="11" fill="none">
        <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.6" />
        <ellipse cx="12" cy="12" rx="8.5" ry="3.6" stroke="currentColor" stroke-width="1.4" />
      </svg>
      360
    </span>

    <span v-if="item.semantic_only" class="badge badge-semantic" title="由语义检索匹配（关键词未直接命中）">
      <svg viewBox="0 0 24 24" width="11" height="11" fill="none">
        <path
          d="M12 3.2l1.9 4.4 4.7.5-3.5 3.2.9 4.7L12 13.7 8 16l.9-4.7L5.4 8.1l4.7-.5z"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linejoin="round"
        />
        <path d="M18.5 3.5l.6 1.4 1.4.6-1.4.6-.6 1.4-.6-1.4-1.4-.6 1.4-.6z" fill="currentColor" />
      </svg>
      语义
    </span>

    <span v-if="item.type === 'video'" class="badge badge-video">
      <svg viewBox="0 0 24 24" width="10" height="10" fill="currentColor">
        <path d="M8 5.5v13l11-6.5z" />
      </svg>
      {{ formatDuration(item.duration) }}
    </span>

    <MediaTooltip :item="item" :anchor="rootEl" />

    <ThumbContextMenu
      :item="item"
      :open="menuOpen"
      :at="menuAt"
      @close="menuOpen = false"
      @action="onMenuAction"
    />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { loadThumbUrl } from './mediaLoader'
import MediaTooltip from './MediaTooltip.vue'
import ThumbContextMenu from '../media/ThumbContextMenu.vue'

const props = defineProps({
  item: { type: Object, required: true },
  selectable: { type: Boolean, default: false }, // Job000079 时间轴批量操作
  selected: { type: Boolean, default: false }
})
const emit = defineEmits(['open', 'toggle', 'thumb-action'])

// ---- Job000143：右键 / 长按 → 打开操作菜单 ----
// ⚠️ 曾在这里挡掉「批量选择态」（理由：与选择语义打架）—— 但实测发现
//    TimelineGrid **常态就传 selectable**（批量是常驻模式，不是临时状态），
//    那样写等于右键功能完全不可用。故改为：桌面右键任何时候都能开菜单；
//    移动端长按在选择态下不触发（那里 touch 已被 checkbox 占用）。
const menuOpen = ref(false)
const menuAt = ref({ x: 0, y: 0 })
let holdTimer = null
let startPt = null

function openMenu(x, y) {
  menuAt.value = { x, y }
  menuOpen.value = true
}

// 右键事件：取 clientX/Y（@contextmenu 传的是 Event，不是坐标 ——
// 直接把 e 当 x 用会得到 undefined，菜单位置全错）。
function onContextMenu(e) {
  openMenu(e.clientX, e.clientY)
}

function onClick() {
  // 菜单开着时点卡片：关菜单而不是打开查看器（否则一次点击干两件事）
  if (menuOpen.value) {
    menuOpen.value = false
    return
  }
  emit('open', props.item)
}

function onMenuAction(action, it) {
  menuOpen.value = false
  emit('thumb-action', action, it)
}

// 移动端长按：600ms 触发；**touchmove 超过 10px 立即取消**（判定为滚动）——
// 这是长按手势最容易踩的坑：用户想滑动列表却总弹菜单。
function onTouchStart(e) {
  if (props.selectable || !e.touches || e.touches.length !== 1) return
  const t = e.touches[0]
  startPt = { x: t.clientX, y: t.clientY }
  clearTimeout(holdTimer)
  holdTimer = setTimeout(() => {
    if (startPt) openMenu(startPt.x, startPt.y)
  }, 600)
}

function onTouchMove(e) {
  if (!startPt || !e.touches || !e.touches.length) return
  const t = e.touches[0]
  if (Math.abs(t.clientX - startPt.x) > 10 || Math.abs(t.clientY - startPt.y) > 10) {
    clearTimeout(holdTimer)
  }
}

function onTouchEnd() {
  clearTimeout(holdTimer)
  // 长按已触发时不要立刻关（touchend 紧跟长按触发，关掉会一闪而过）
  startPt = null
}

const rootEl = ref(null)

const url = ref('')
let alive = true

const is360 = computed(() => props.item.is_360 || props.item.type === '360')

watch(
  () => props.item.id,
  async () => {
    url.value = ''
    try {
      const u = await loadThumbUrl(props.item, 'md')
      if (alive) url.value = u
    } catch (e) {
      if (alive) url.value = ''
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  alive = false
})

function formatDuration(sec) {
  if (!sec && sec !== 0) return ''
  const s = Math.round(sec)
  const m = Math.floor(s / 60)
  const r = s % 60
  return `${m}:${String(r).padStart(2, '0')}`
}
</script>

<style scoped>
.thumb {
  position: relative;
  width: 100%;
  height: 100%;
  border-radius: var(--radius-sm);
  overflow: hidden;
  background-color: var(--color-surface-hover);
  cursor: pointer;
}

.thumb--selected {
  outline: 2px solid var(--color-primary);
  outline-offset: -2px;
}

.thumb-check {
  position: absolute;
  top: 6px;
  left: 6px;
  z-index: 2;
  display: inline-flex;
  padding: 2px;
  background-color: rgba(255, 255, 255, 0.9);
  border-radius: var(--radius-sm);
  cursor: pointer;
}

.thumb-check input {
  width: 15px;
  height: 15px;
  accent-color: var(--color-primary);
  cursor: pointer;
}

.thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

/* 晨雾 veil（DESIGN.md §4）：只盖照片，不拦截交互 */
.thumb-veil {
  position: absolute;
  inset: 0;
  background: linear-gradient(180deg, rgba(233, 228, 222, 0.16), transparent 34%);
  pointer-events: none;
}

.thumb {
  transition: box-shadow 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.thumb:hover {
  box-shadow: 0 2px 6px rgba(65, 64, 60, 0.06), 0 14px 40px rgba(65, 64, 60, 0.12);
  z-index: 1;
}

.thumb:hover .thumb-img {
  filter: brightness(0.96);
}

.thumb-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-disabled);
}

.badge {
  position: absolute;
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  font-size: 11px;
  line-height: 1.4;
  color: #fff;
  background-color: rgba(0, 0, 0, 0.55);
}

.badge-360 {
  top: 6px;
  right: 6px;
}

/* 语义匹配：搜索结果中由向量召回（关键词未直接命中）的条目。
   放右下角，避开右上 360 与底部时长徽标。 */
.badge-semantic {
  bottom: 6px;
  right: 6px;
  background-color: rgba(37, 99, 235, 0.9);
}

.badge-video {
  right: 6px;
  bottom: 6px;
}
</style>
