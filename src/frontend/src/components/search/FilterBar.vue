<template>
  <div class="filter-bar">
    <div v-for="f in filterDefs" :key="f.key" class="fb-item">
      <button
        class="fb-btn"
        :class="{ active: f.isActive(modelValue) }"
        @click.stop="toggle(f.key)"
      >
        {{ f.label }}
        <svg viewBox="0 0 10 6" width="9" height="6" fill="none">
          <path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>
      <div v-if="openKey === f.key" class="fb-panel" @click.stop>
        <FilterControls :model-value="modelValue" :only="f.key" @update:model-value="onUpdate" />
        <div class="fb-panel-actions">
          <button class="fb-mini" @click="f.clear(); onApply()">清除</button>
          <button class="fb-mini primary" @click="close(); onApply()">确定</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import FilterControls from './FilterControls.vue'

const props = defineProps({
  modelValue: { type: Object, required: true }
})
const emit = defineEmits(['update:modelValue', 'apply'])

const openKey = ref('')

const filterDefs = [
  {
    key: 'type',
    label: '类型',
    isActive: (v) => !!v.type,
    clear: () => patch({ type: '' })
  },
  {
    key: 'favorites',
    label: '收藏',
    isActive: (v) => !!v.favorites,
    clear: () => patch({ favorites: false })
  },
  {
    key: 'tag',
    label: '标签',
    isActive: (v) => !!v.tag,
    clear: () => patch({ tag: '' })
  },
  {
    key: 'date',
    label: '日期',
    isActive: (v) => !!(v.date_after || v.date_before),
    clear: () => patch({ date_after: '', date_before: '' })
  },
  {
    key: 'place',
    label: '拍摄地',
    isActive: (v) => !!v.place,
    clear: () => patch({ place: '' })
  }
]

function patch(partial) {
  emit('update:modelValue', { ...props.modelValue, ...partial })
}

function onUpdate(v) {
  emit('update:modelValue', v)
}

function onApply() {
  emit('apply')
}

function toggle(key) {
  openKey.value = openKey.value === key ? '' : key
}

function close() {
  openKey.value = ''
}

function onDocClick() {
  close()
}

onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<style scoped>
.filter-bar {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.fb-item {
  position: relative;
}

.fb-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 30px;
  padding: 0 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

.fb-btn:hover {
  background-color: var(--color-surface-hover);
}

.fb-btn.active {
  border-color: var(--color-primary);
  color: var(--color-primary);
  background-color: var(--color-primary-active-bg);
  font-weight: 600;
}

.fb-panel {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: 20;
  width: 240px;
  padding: 14px;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-card);
}

.fb-panel-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}

.fb-mini {
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: var(--radius-sm);
  padding: 4px 14px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.fb-mini:hover {
  background-color: var(--color-surface-hover);
}

.fb-mini.primary {
  background-color: var(--color-primary);
  border-color: var(--color-primary);
  color: #fff;
}

.fb-mini.primary:hover {
  background-color: var(--color-primary-hover);
}
</style>
