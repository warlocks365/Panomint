<template>
  <Teleport to="body">
    <div v-if="modelValue" class="drawer-mask" @click.self="close">
      <div class="drawer">
        <div class="drawer-head">
          <span class="drawer-title">筛选</span>
          <button class="drawer-close" title="关闭" @click="close">
            <svg viewBox="0 0 12 12" width="12" height="12" fill="none">
              <path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
            </svg>
          </button>
        </div>

        <div class="drawer-body">
          <FilterControls :model-value="draft" @update:model-value="draft = $event" />
        </div>

        <div class="drawer-foot">
          <button class="drawer-btn" @click="reset">重置</button>
          <button class="drawer-btn primary" @click="apply">完成</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch } from 'vue'
import FilterControls from './FilterControls.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false }, // 抽屉开关
  filters: { type: Object, required: true }
})
const emit = defineEmits(['update:modelValue', 'apply'])

// 草稿态：抽屉内编辑不立即生效，点「完成」统一应用
const draft = ref({ ...props.filters })

watch(
  () => props.modelValue,
  (open) => {
    if (open) draft.value = { ...props.filters }
  }
)

function close() {
  emit('update:modelValue', false)
}

function reset() {
  draft.value = { type: '', favorites: false, tag: '', date_after: '', date_before: '', place: '' }
}

function apply() {
  emit('apply', { ...draft.value })
  close()
}
</script>

<style scoped>
.drawer-mask {
  position: fixed;
  inset: 0;
  z-index: 100;
  background-color: rgba(0, 0, 0, 0.4);
  display: flex;
  align-items: flex-end;
}

.drawer {
  width: 100%;
  max-height: 76vh;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg) var(--radius-lg) 0 0;
  display: flex;
  flex-direction: column;
}

.drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid var(--color-border);
  flex-shrink: 0;
}

.drawer-title {
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
}

.drawer-close {
  border: none;
  background: transparent;
  color: var(--color-text-secondary);
  padding: 4px;
  display: inline-flex;
}

.drawer-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 18px;
}

.drawer-foot {
  display: flex;
  gap: 10px;
  padding: 12px 18px;
  border-top: 1px solid var(--color-border);
  flex-shrink: 0;
}

.drawer-btn {
  flex: 1;
  height: 38px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
}

.drawer-btn:hover {
  background-color: var(--color-surface-hover);
}

.drawer-btn.primary {
  background-color: var(--color-primary);
  border-color: var(--color-primary);
  color: #fff;
}

.drawer-btn.primary:hover {
  background-color: var(--color-primary-hover);
}
</style>
