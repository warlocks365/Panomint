<template>
  <!-- 根类 .map-search 是与 useMapSearch「点面板外关闭」契约的边界（closest('.map-search')） -->
  <div class="map-search" @keydown.esc="$emit('close')">
    <div class="ms-box">
      <svg class="ms-icon" viewBox="0 0 16 16" width="14" height="14" fill="none">
        <circle cx="7" cy="7" r="5" stroke="currentColor" stroke-width="1.5" />
        <path d="M11 11l3.5 3.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
      </svg>
      <input
        v-model="qLocal"
        class="ms-input"
        type="text"
        placeholder="搜索地名，回车定位"
        @keyup.enter="$emit('enter')"
      />
      <button v-if="qLocal" class="ms-clear" type="button" title="清空" @click="$emit('clear')">
        <svg viewBox="0 0 12 12" width="10" height="10" fill="none">
          <path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>
    </div>

    <div v-if="open" class="ms-panel">
      <div v-if="searching" class="ms-hint">搜索中…</div>
      <div v-else-if="searchErr" class="ms-hint ms-hint--err">{{ searchErr }}</div>
      <div v-else-if="searchMsg" class="ms-hint">{{ searchMsg }}</div>
      <ul v-else class="ms-list">
        <li v-for="(c, i) in candidates" :key="`${c.name}-${i}`">
          <button class="ms-item" type="button" @click="$emit('pick', c)">
            <span class="ms-name">{{ c.name }}</span>
            <span v-if="c.provider" class="ms-provider">{{ providerLabel(c.provider) }}</span>
          </button>
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  q: { type: String, default: '' },
  candidates: { type: Array, default: () => [] },
  open: { type: Boolean, default: false },
  searching: { type: Boolean, default: false },
  searchErr: { type: String, default: '' },
  searchMsg: { type: String, default: '' }
})
const emit = defineEmits(['update:q', 'enter', 'pick', 'clear', 'close'])

const qLocal = computed({
  get: () => props.q,
  set: (v) => emit('update:q', v)
})

function providerLabel(p) {
  return { amap: '高德', nominatim: 'OSM' }[p] || p
}
</script>

<style scoped>
.map-search {
  width: 320px;
  max-width: 100%;
}

/* 移动端：宿主经 :class 绑定（.map-search--mobile），避开 scoped 父子选择器穿透问题 */
.map-search--mobile {
  align-self: stretch;
  width: auto;
}

.ms-box {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  padding: 0 12px;
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 8px;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.08);
  color: var(--color-text-disabled);
}

.ms-box:focus-within {
  border-color: var(--color-primary);
}

.ms-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  font-size: 13px;
  color: var(--color-text-primary);
}

.ms-clear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  border: none;
  border-radius: 50%;
  background: var(--color-border);
  color: var(--color-text-secondary);
  padding: 0;
}

.ms-panel {
  margin-top: 6px;
  max-height: 240px;
  overflow-y: auto;
  background: rgba(255, 255, 255, 0.98);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 8px;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.08);
  padding: 4px;
}

.ms-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.ms-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  border: none;
  background: transparent;
  text-align: left;
  padding: 7px 8px;
  border-radius: 6px;
  font-size: 13px;
  color: var(--color-text-primary);
}

.ms-item:hover {
  background: var(--color-surface-hover);
}

.ms-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ms-provider {
  flex-shrink: 0;
  font-size: 11px;
  color: var(--color-text-disabled);
}

.ms-hint {
  padding: 8px;
  font-size: 12px;
  color: var(--color-text-secondary);
}

.ms-hint--err {
  color: var(--color-danger);
}
</style>
