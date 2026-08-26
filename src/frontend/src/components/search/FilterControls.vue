<template>
  <div class="filter-controls">
    <section v-if="!only || only === 'type'" class="fc-section">
      <div class="fc-label">类型</div>
      <div class="fc-options">
        <button
          v-for="opt in typeOptions"
          :key="opt.value"
          class="fc-option"
          :class="{ active: modelValue.type === opt.value }"
          @click="patch({ type: opt.value })"
        >
          {{ opt.label }}
        </button>
      </div>
    </section>

    <section v-if="!only || only === 'favorites'" class="fc-section">
      <div class="fc-label">收藏</div>
      <div class="fc-options">
        <button
          class="fc-option"
          :class="{ active: !modelValue.favorites }"
          @click="patch({ favorites: false })"
        >
          全部
        </button>
        <button
          class="fc-option"
          :class="{ active: modelValue.favorites }"
          @click="patch({ favorites: true })"
        >
          仅收藏
        </button>
      </div>
    </section>

    <section v-if="!only || only === 'tag'" class="fc-section">
      <div class="fc-label">标签</div>
      <input
        :value="modelValue.tag"
        type="text"
        class="fc-input"
        placeholder="输入标签，回车确认"
        @keydown.enter="patch({ tag: $event.target.value.trim() })"
        @change="patch({ tag: $event.target.value.trim() })"
      />
    </section>

    <section v-if="!only || only === 'date'" class="fc-section">
      <div class="fc-label">日期</div>
      <div class="fc-dates">
        <input
          :value="modelValue.date_after"
          type="date"
          class="fc-input"
          title="起始日期"
          @change="patch({ date_after: $event.target.value })"
        />
        <span class="fc-sep">至</span>
        <input
          :value="modelValue.date_before"
          type="date"
          class="fc-input"
          title="结束日期"
          @change="patch({ date_before: $event.target.value })"
        />
      </div>
    </section>

    <section v-if="!only || only === 'place'" class="fc-section">
      <div class="fc-label">拍摄地</div>
      <input
        :value="modelValue.place"
        type="text"
        class="fc-input"
        placeholder="输入地点，回车确认"
        @keydown.enter="patch({ place: $event.target.value.trim() })"
        @change="patch({ place: $event.target.value.trim() })"
      />
    </section>
  </div>
</template>

<script setup>
const props = defineProps({
  modelValue: { type: Object, required: true },
  // 指定后只渲染某一组控件（桌面端下拉面板用）；缺省渲染全部（移动端抽屉用）
  only: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue', 'apply'])

const typeOptions = [
  { value: '', label: '全部类型' },
  { value: 'photo', label: '照片' },
  { value: 'video', label: '视频' },
  { value: '360', label: '360' }
]

function patch(partial) {
  emit('update:modelValue', { ...props.modelValue, ...partial })
  emit('apply')
}
</script>

<style scoped>
.filter-controls {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.fc-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin-bottom: 6px;
}

.fc-options {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.fc-option {
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: 14px;
  padding: 4px 12px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.fc-option:hover {
  background-color: var(--color-surface-hover);
}

.fc-option.active {
  background-color: var(--color-primary-active-bg);
  border-color: var(--color-primary);
  color: var(--color-primary);
  font-weight: 600;
}

.fc-input {
  width: 100%;
  height: 32px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  padding: 0 10px;
  outline: none;
}

.fc-input:focus {
  border-color: var(--color-primary);
}

.fc-dates {
  display: flex;
  align-items: center;
  gap: 6px;
}

.fc-sep {
  color: var(--color-text-disabled);
  font-size: var(--font-size-sm);
  flex-shrink: 0;
}
</style>
