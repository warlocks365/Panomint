<template>
  <div class="icon-picker">
    <div class="ip-head">
      <span class="ip-title">地图点图标</span>
      <button class="ip-close" type="button" @click="$emit('close')">关闭</button>
    </div>

    <div class="ip-section">
      <div class="ip-label">基础形状</div>
      <div class="ip-shapes">
        <button
          v-for="s in shapes"
          :key="s.key"
          class="ip-shape"
          :class="{ active: isShapeActive(s.key) }"
          type="button"
          :title="s.name"
          @click="selectShape(s.key)"
        >
          <svg width="18" height="18" viewBox="0 0 18 18" v-html="s.path" :style="{ fill: pref.color || '#ef4444' }"></svg>
        </button>
      </div>
    </div>

    <div class="ip-section">
      <div class="ip-label">颜色</div>
      <div class="ip-colors">
        <button
          v-for="c in colors"
          :key="c"
          class="ip-color"
          :class="{ active: pref.color === c }"
          type="button"
          :style="{ background: c }"
          @click="selectColor(c)"
        ></button>
      </div>
    </div>

    <div class="ip-section">
      <div class="ip-label">内置图标</div>
      <div class="ip-presets">
        <button
          v-for="p in presets"
          :key="p.key"
          class="ip-preset"
          :class="{ active: pref.shape === p.key }"
          type="button"
          @click="selectPreset(p.key)"
        >
          <img :src="p.src" :alt="p.name" />
          <span class="ip-preset-name">{{ p.name }}</span>
        </button>
      </div>
    </div>

    <div class="ip-section">
      <div class="ip-label">自定义图标</div>
      <label class="ip-upload" :class="{ active: pref.shape === 'custom' }">
        <input type="file" accept="image/png" @change="onUpload" />
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none"><path d="M12 16V4m0 0L7 9m5-5l5 5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/><path d="M4 20h16" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>
        <span>{{ pref.shape === 'custom' && pref.data_url ? '已上传（点击更换）' : '上传 PNG' }}</span>
      </label>
      <div v-if="uploadError" class="ip-err">{{ uploadError }}</div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  pref: { type: Object, default: () => ({ shape: 'circle', color: '#ef4444' }) }
})
const emit = defineEmits(['update', 'close'])

// 矢量形状：SVG path（填充色由 pref.color 控制）
const shapes = [
  { key: 'circle', name: '圆形', path: '<circle cx="9" cy="9" r="8" />' },
  { key: 'triangle', name: '三角形', path: '<path d="M9 1l8 16H1z" />' },
  { key: 'diamond', name: '菱形', path: '<path d="M9 1l8 8-8 8-8-8z" />' },
  { key: 'star', name: '五角星', path: '<path d="M9 1l2.2 4.6 5 .7-3.6 3.5.9 5-4.5-2.4L4.5 14.8l.9-5L1.8 6.3l5-.7z" />' }
]

const colors = ['#ef4444', '#f97316', '#f59e0b', '#22c55e', '#06b6d4', '#3b82f6', '#8b5cf6', '#ec4899']

// 内置 PNG 图标（放 public/map-icons/）
const presets = [
  { key: 'pin', name: '图钉', src: '/map-icons/pin.png' },
  { key: 'inverted', name: '倒三角', src: '/map-icons/inverted.png' }
]

const uploadError = ref('')

function isShapeActive(key) {
  // 内置 PNG / 自定义 不属于矢量形状，矢量区高亮仅当 shape 匹配且非 custom/pin/inverted
  return props.pref.shape === key
}

function selectShape(key) {
  emit('update', { shape: key, color: props.pref.color, data_url: '' })
}

function selectColor(c) {
  // 改颜色只对矢量形状有意义；内置 PNG/自定义不跟色
  const shape = ['pin', 'inverted', 'custom'].includes(props.pref.shape) ? 'circle' : props.pref.shape
  emit('update', { shape, color: c, data_url: props.pref.data_url || '' })
}

function selectPreset(key) {
  emit('update', { shape: key, color: props.pref.color, data_url: '' })
}

function onUpload(e) {
  const file = e.target.files?.[0]
  if (!file) return
  uploadError.value = ''
  if (file.size > 512 * 1024) {
    uploadError.value = 'PNG 需小于 512KB'
    return
  }
  const reader = new FileReader()
  reader.onload = () => {
    emit('update', { shape: 'custom', color: props.pref.color, data_url: reader.result })
  }
  reader.readAsDataURL(file)
  e.target.value = '' // 允许重复上传同一文件
}
</script>

<style scoped>
.icon-picker {
  position: absolute;
  top: 52px;
  right: 12px;
  width: 280px;
  background: rgba(255, 255, 255, 0.98);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 10px;
  box-shadow: 0 8px 24px rgba(15, 23, 42, 0.14);
  padding: 12px 14px;
  z-index: 10;
}

.ip-head {
  display: flex;
  align-items: center;
  margin-bottom: 12px;
}

.ip-title {
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
}

.ip-close {
  margin-left: auto;
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: #fff;
  border-radius: 6px;
  padding: 2px 8px;
  font-size: 12px;
  color: #475569;
  cursor: pointer;
}

.ip-section {
  margin-bottom: 12px;
}

.ip-label {
  font-size: 12px;
  color: #64748b;
  margin-bottom: 6px;
}

.ip-shapes {
  display: flex;
  gap: 8px;
}

.ip-shape {
  width: 32px;
  height: 32px;
  border: 1px solid rgba(15, 23, 42, 0.1);
  background: #fff;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.ip-shape.active {
  border: 2px solid #2563eb;
}

.ip-colors {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.ip-color {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: none;
  cursor: pointer;
}

.ip-color.active {
  box-shadow: 0 0 0 2px #fff, 0 0 0 4px #2563eb;
}

.ip-presets {
  display: flex;
  gap: 14px;
}

.ip-preset {
  border: 1px solid rgba(15, 23, 42, 0.1);
  background: #fff;
  border-radius: 8px;
  padding: 6px 10px;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}

.ip-preset.active {
  border: 2px solid #2563eb;
}

.ip-preset img {
  width: 22px;
  height: 26px;
  object-fit: contain;
}

.ip-preset-name {
  font-size: 11px;
  color: #64748b;
}

.ip-upload {
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px dashed rgba(15, 23, 42, 0.2);
  border-radius: 8px;
  padding: 10px 12px;
  cursor: pointer;
  font-size: 12px;
  color: #64748b;
}

.ip-upload.active {
  border-color: #2563eb;
  color: #0f172a;
}

.ip-upload input {
  display: none;
}

.ip-err {
  font-size: 11px;
  color: #dc2626;
  margin-top: 4px;
}
</style>
