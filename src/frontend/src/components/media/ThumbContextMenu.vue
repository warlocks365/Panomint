<script setup>
// Job000143：媒体卡操作菜单（**受控组件**）。
//
// 为什么是受控而非自管手势：本组件 Teleport 到 body，事件无法冒泡回卡片
// （.thumb 上），若把 contextmenu/touch 判定放在这里，就得靠 ref 穿透父子，
// 反而更绕。故手势在 ThumbItem（卡片自己）判定，本组件只负责「按坐标渲染 + 关闭」。
//
// 关闭时机：点击外部 / Esc / 滚动 / 缩放 / 点击任一菜单项。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps({
  item: { type: Object, required: true },
  // 由父级（ThumbItem）控制
  open: { type: Boolean, default: false },
  // 触发点（视口坐标）：{ x, y }
  at: { type: Object, default: () => ({ x: 0, y: 0 }) }
})
const emit = defineEmits(['action', 'close'])

const pos = ref({ x: 0, y: 0 })
const flipped = ref({ x: false, y: false })

const MENU_W = 176
const MENU_H = 210

// 按视口边缘翻转：菜单在屏幕右侧/底部时必须能翻回，否则会有一半在屏幕外。
function place() {
  if (!props.open) return
  const vw = window.innerWidth
  const vh = window.innerHeight
  const pad = 8
  const cx = props.at?.x || 0
  const cy = props.at?.y || 0
  const fx = cx + MENU_W + pad > vw
  const fy = cy + MENU_H + pad > vh
  flipped.value = { x: fx, y: fy }
  pos.value = {
    x: fx ? Math.max(pad, cx - MENU_W) : Math.min(cx, vw - MENU_W - pad),
    y: fy ? Math.max(pad, cy - MENU_H) : Math.min(cy, vh - MENU_H - pad)
  }
}

function close() {
  if (props.open) emit('close')
}

function fire(action) {
  emit('action', action, props.item)
  emit('close')
}

function onDocPointer(e) {
  if (!props.open) return
  if (e.target && e.target.closest && e.target.closest('[data-testid=thumb-menu]')) return
  close()
}

function onKey(e) {
  if (e.key === 'Escape') close()
}

// open/坐标变化时重算位置（受控组件不会自动调 place）
watch(() => [props.open, props.at], place, { immediate: true, deep: true })

onMounted(() => {
  document.addEventListener('pointerdown', onDocPointer, true)
  document.addEventListener('keydown', onKey)
  window.addEventListener('scroll', close, true)
  window.addEventListener('resize', close)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointer, true)
  document.removeEventListener('keydown', onKey)
  window.removeEventListener('scroll', close, true)
  window.removeEventListener('resize', close)
})

const label = computed(() => props.item?.filename || '该媒体')
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="tm-menu"
      data-testid="thumb-menu"
      :style="{ left: pos.x + 'px', top: pos.y + 'px' }"
      @contextmenu.prevent
    >
      <p class="tm-title" :title="label">{{ label }}</p>
      <div class="tm-items">
        <button
          class="tm-item"
          type="button"
          data-testid="tm-edit-metadata"
          @click="fire('edit-metadata')"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"
            stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 20h9" />
            <path d="M4 20h4l10-10-4-4L4 16v4z" />
            <path d="M13.5 6.5l4 4" />
          </svg>
          <span>编辑拍摄信息</span>
        </button>
        <button class="tm-item" type="button" @click="fire('open')">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"
            stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
          <span>查看大图</span>
        </button>
        <button class="tm-item" type="button" @click="fire('favorite')">
          <svg viewBox="0 0 24 24" width="14" height="14" :fill="item.favorite ? 'currentColor' : 'none'"
            stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"
            aria-hidden="true">
            <path d="M12 3.5l2.6 5.3 5.9.9-4.3 4.1 1 5.8-5.2-2.7-5.2 2.7 1-5.8L3.5 9.7l5.9-.9z" />
          </svg>
          <span>{{ item.favorite ? '取消收藏' : '收藏' }}</span>
        </button>
        <button class="tm-item tm-item--danger" type="button" @click="fire('delete')">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"
            stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M4 7h16" />
            <path d="M9 7V4h6v3" />
            <path d="M6 7l1 13h10l1-13" />
          </svg>
          <span>删除</span>
        </button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* Teleport 到 body，故用 fixed 定位；样式不依赖 scoped 变量以外的选择器 */
.tm-menu {
  position: fixed;
  z-index: 70; /* 高于查看器弹层(60)与选点弹层(60) */
  width: 176px;
  padding: 6px;
  background: var(--color-surface);
  border-radius: 10px;
  box-shadow: var(--shadow-lift);
}

.tm-title {
  margin: 0 0 4px;
  padding: 4px 8px 6px;
  font-size: 11px;
  color: var(--color-text-secondary);
  border-bottom: 1px solid var(--color-border));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tm-items {
  display: flex;
  flex-direction: column;
}

.tm-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 8px;
  font-size: var(--font-size-sm);
  font-family: inherit;
  text-align: left;
  color: var(--color-text-primary);
  background: transparent;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.14s ease;
}

.tm-item:hover {
  background: var(--color-bg);
}

.tm-item--danger {
  color: var(--color-danger);
}
</style>
