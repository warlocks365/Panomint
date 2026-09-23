<template>
  <!-- 裁剪选择层（仅照片，覆盖整个媒体框；宿主 stage 为定位上下文） -->
  <div v-if="cropMode" class="crop-overlay">
    <div class="crop-rect" :style="cropRectStyle" @pointerdown.prevent="onRectDown">
      <span class="crop-handle" @pointerdown.prevent.stop="onHandleDown"></span>
    </div>
  </div>
  <!-- 底部工具条：基本编辑组（幻灯片组由宿主经默认 slot 传入同一 .viewer-toolbar 容器） -->
  <div v-if="editable" class="tb-group">
    <span v-if="editStateText" class="tb-state" :class="editState">{{ editStateText }}</span>
    <template v-if="!cropMode">
      <button class="tb-btn" title="向左旋转 90°" @click="rotateBy(-90)">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none"><path d="M9 7H5V3M5.5 7.5A7 7 0 1 1 5 14" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </button>
      <button class="tb-btn" title="向右旋转 90°" @click="rotateBy(90)">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none"><path d="M15 7h4V3M18.5 7.5A7 7 0 1 0 19 14" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </button>
      <button class="tb-btn" title="裁剪" @click="startCrop">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none"><path d="M6 2v14a2 2 0 0 0 2 2h14M2 6h14a2 2 0 0 1 2 2v14" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" /></svg>
      </button>
      <button class="tb-btn primary" :disabled="!editDirty || editState === 'saving'" @click="saveEdits">保存</button>
      <button class="tb-btn" :disabled="!editDirty && !hasSavedEdits" @click="resetEdits">重置</button>
    </template>
    <template v-else>
      <span class="tb-hint">拖动选框 / 右下角缩放</span>
      <button class="tb-btn primary" @click="applyCrop">应用裁剪</button>
      <button class="tb-btn" @click="cancelCrop">取消</button>
    </template>
  </div>
</template>

<script setup>
// MediaViewer 拆解（Job000086）：基本编辑子系统整体自持——
// 非破坏式 CSS 预览（clip-path 裁剪 + transform 旋转）+ PATCH 保存/重置 + 裁剪选框拖动。
// 宿主经 expose 消费 mediaStyle（舞台媒体 :style）与 cropMode（pano 入口避让），
// 加载新媒体后调 initFromEdits 重初始化；保存成功 emit updated 供宿主同步 detail.edits。
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import http from '../../api/http'
const props = defineProps({
  editable: { type: Boolean, default: false },
  mediaId: { type: [String, Number], default: '' },
  // 宿主 stage 元素 ref（裁剪拖动换算与旋转适配测量的定位上下文）
  stageRef: { type: Object, default: null }
})
const emit = defineEmits(['updated'])
/* ---------------- 编辑态 ---------------- */
const edit = reactive({ rotate: 0, crop: null })
const saved = reactive({ rotate: 0, crop: null })
const editState = ref('') // '' | saving | saved | error

const cropMode = ref(false)
const cropSel = reactive({ x: 0.1, y: 0.1, w: 0.8, h: 0.8 })

const editDirty = computed(() => {
  if (edit.rotate !== saved.rotate) return true
  const a = edit.crop
  const b = saved.crop
  if (!a && !b) return false
  if (!a || !b) return true
  return Math.abs(a.x - b.x) > 1e-4 || Math.abs(a.y - b.y) > 1e-4 ||
    Math.abs(a.w - b.w) > 1e-4 || Math.abs(a.h - b.h) > 1e-4
})
const hasSavedEdits = computed(() => !!(saved.rotate || saved.crop))
const editStateText = computed(() => {
  if (editState.value === 'saving') return '保存中…'
  if (editState.value === 'saved') return '已保存'
  if (editState.value === 'error') return '保存失败'
  return ''
})

// 宿主加载新媒体后经 expose 调用：用已保存的编辑参数初始化本地编辑态
function initFromEdits(e) {
  saved.rotate = e?.rotate || 0
  saved.crop = e?.crop ? { ...e.crop } : null
  edit.rotate = saved.rotate
  edit.crop = saved.crop ? { ...saved.crop } : null
  editState.value = ''
  cropMode.value = false
}

/* ---------------- 舞台尺寸（旋转后等比缩放） ---------------- */
const stageW = ref(0)
const stageH = ref(0)
let stageRO = null
function measureStage() {
  const r = props.stageRef?.value?.getBoundingClientRect()
  if (r) {
    stageW.value = r.width
    stageH.value = r.height
  }
}
watch(
  () => props.stageRef?.value,
  (el) => {
    stageRO?.disconnect()
    stageRO = null
    if (el) {
      measureStage()
      stageRO = new ResizeObserver(measureStage)
      stageRO.observe(el)
    }
  },
  { immediate: true, flush: 'post' }
)

const fitScale = computed(() => {
  if (edit.rotate % 180 === 0 || !stageW.value || !stageH.value) return 1
  const ar = stageW.value / stageH.value
  return Math.min(ar, 1 / ar)
})

// 即时预览：先裁剪（clip-path，按未旋转方向）后旋转（transform）。
// 90°/270° 旋转后按舞台宽高比缩放，保证旋转后的图不出界（媒体框 == 舞台框）。
const mediaStyle = computed(() => {
  const c = edit.crop
  const clip = c
    ? `inset(${(c.y * 100).toFixed(3)}% ${((1 - c.x - c.w) * 100).toFixed(3)}% ${((1 - c.y - c.h) * 100).toFixed(3)}% ${(c.x * 100).toFixed(3)}%)`
    : 'none'
  // 裁剪选择时临时按未旋转方向显示，便于在原始方向框选
  const r = cropMode.value ? 0 : edit.rotate
  const s = cropMode.value ? 1 : fitScale.value
  return { clipPath: clip, transform: `rotate(${r}deg) scale(${s})` }
})

const cropRectStyle = computed(() => ({
  left: `${cropSel.x * 100}%`,
  top: `${cropSel.y * 100}%`,
  width: `${cropSel.w * 100}%`,
  height: `${cropSel.h * 100}%`
}))

function rotateBy(deg) {
  edit.rotate = ((edit.rotate + deg) % 360 + 360) % 360
  editState.value = ''
}

function startCrop() {
  cropSel.x = edit.crop?.x ?? 0.1
  cropSel.y = edit.crop?.y ?? 0.1
  cropSel.w = edit.crop?.w ?? 0.8
  cropSel.h = edit.crop?.h ?? 0.8
  cropMode.value = true
}

function cancelCrop() {
  cropMode.value = false
}

function applyCrop() {
  edit.crop = { x: +cropSel.x.toFixed(4), y: +cropSel.y.toFixed(4), w: +cropSel.w.toFixed(4), h: +cropSel.h.toFixed(4) }
  cropMode.value = false
  editState.value = ''
}

/* ---------------- 裁剪选框拖动：移动 / 右下角缩放 ---------------- */
let drag = null
const frameRect = () => props.stageRef?.value?.getBoundingClientRect() || null
function relPoint(e) {
  const r = frameRect()
  if (!r) return { x: 0, y: 0 }
  return { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height }
}
const clamp = (v, lo, hi) => Math.min(Math.max(v, lo), hi)

function onRectDown(e) {
  drag = { mode: 'move', start: relPoint(e), orig: { ...cropSel } }
  addDragListeners()
}
function onHandleDown(e) {
  drag = { mode: 'resize', start: relPoint(e), orig: { ...cropSel } }
  addDragListeners()
}
function onDragMove(e) {
  if (!drag) return
  const p = relPoint(e)
  const dx = p.x - drag.start.x
  const dy = p.y - drag.start.y
  if (drag.mode === 'move') {
    cropSel.x = clamp(drag.orig.x + dx, 0, 1 - cropSel.w)
    cropSel.y = clamp(drag.orig.y + dy, 0, 1 - cropSel.h)
  } else {
    cropSel.w = clamp(drag.orig.w + dx, 0.05, 1 - cropSel.x)
    cropSel.h = clamp(drag.orig.h + dy, 0.05, 1 - cropSel.y)
  }
}
function onDragUp() {
  drag = null
  removeDragListeners()
}
function addDragListeners() {
  window.addEventListener('pointermove', onDragMove)
  window.addEventListener('pointerup', onDragUp)
}
function removeDragListeners() {
  window.removeEventListener('pointermove', onDragMove)
  window.removeEventListener('pointerup', onDragUp)
}

/* ---------------- 保存 / 重置（PATCH edits） ---------------- */
async function saveEdits() {
  if (!props.mediaId || editState.value === 'saving') return
  editState.value = 'saving'
  const hasEdits = !!(edit.rotate || edit.crop)
  const payload = hasEdits
    ? { rotate: edit.rotate, ...(edit.crop ? { crop: { ...edit.crop } } : {}) }
    : null
  try {
    const res = await http.patch(`/media/${props.mediaId}`, { edits: payload })
    const e = res.data?.edits || null
    saved.rotate = e?.rotate || 0
    saved.crop = e?.crop ? { ...e.crop } : null
    editState.value = 'saved'
    emit('updated', e)
    setTimeout(() => { if (editState.value === 'saved') editState.value = '' }, 2000)
  } catch {
    editState.value = 'error'
  }
}

function resetEdits() {
  edit.rotate = 0
  edit.crop = null
  saveEdits() // 立即持久化清空（PATCH edits:null）
}

onBeforeUnmount(() => {
  removeDragListeners()
  stageRO?.disconnect()
  stageRO = null
})

defineExpose({ mediaStyle, cropMode, initFromEdits, cancelCrop })
</script>

<style scoped>
/* 裁剪选择层 */
.crop-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  z-index: 15;
}
.crop-rect {
  position: absolute;
  border: 2px solid #fff;
  cursor: move;
  box-sizing: border-box;
}
.crop-handle {
  position: absolute;
  right: -8px;
  bottom: -8px;
  width: 16px;
  height: 16px;
  background: #fff;
  border-radius: 3px;
  cursor: nwse-resize;
}

/* 编辑组按钮（与宿主幻灯片组按钮同款样式；scoped 隔离故与宿主逐字一致，拆分不改视觉） */
.tb-group {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.tb-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  border: none;
  border-radius: var(--radius-sm);
  padding: 5px 8px;
  background: rgba(255, 255, 255, 0.1);
  color: var(--color-text-on-dark);
  font-size: var(--font-size-sm);
}
.tb-btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.2);
}
.tb-btn:disabled {
  opacity: 0.45;
  cursor: default;
}
.tb-btn.primary {
  background: var(--color-primary);
  color: #fff;
}
.tb-hint,
.tb-state {
  opacity: 0.85;
}
.tb-state.saved {
  color: var(--player-success);
}
.tb-state.error {
  color: var(--player-danger);
}
</style>
