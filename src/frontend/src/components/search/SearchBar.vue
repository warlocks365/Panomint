<template>
  <div class="search-box">
    <svg class="search-icon" viewBox="0 0 16 16" width="14" height="14" fill="none">
      <circle cx="7" cy="7" r="5" stroke="currentColor" stroke-width="1.5" />
      <path d="M11 11l3.5 3.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
    </svg>
    <input
      v-model="text"
      type="text"
      placeholder="搜索文件名、地点、文件夹、标签"
      @keydown.enter="submit"
    />
    <button v-if="text" class="clear-btn" title="清空" @click="clear">
      <svg viewBox="0 0 12 12" width="10" height="10" fill="none">
        <path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
      </svg>
    </button>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSearchStore } from '../../stores/search'

const router = useRouter()
const route = useRoute()
const search = useSearchStore()

const text = ref(search.query)

// 离开搜索页或 store 被外部清除时同步输入框
watch(
  () => search.query,
  (q) => {
    if (q !== text.value) text.value = q
  }
)

function submit() {
  search.setQuery(text.value)
  if (route.name !== 'search') {
    router.push({ name: 'search' })
  } else {
    search.run()
  }
}

function clear() {
  text.value = ''
  search.clearAll()
}
</script>

<style scoped>
.search-box {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 320px;
  max-width: 100%;
  padding: 0 12px;
  height: 34px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  color: var(--color-text-disabled);
  background-color: var(--color-bg);
}

.search-box:focus-within {
  border-color: var(--color-primary);
}

.search-box input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.clear-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border: none;
  border-radius: 50%;
  background-color: var(--color-border);
  color: var(--color-text-secondary);
  padding: 0;
}

.clear-btn:hover {
  background-color: var(--color-text-disabled);
  color: var(--color-surface);
}

@media (max-width: 1023px) {
  .search-box {
    width: auto;
    flex: 1;
  }
}
</style>
