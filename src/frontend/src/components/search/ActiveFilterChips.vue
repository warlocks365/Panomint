<template>
  <div v-if="chips.length" class="chips">
    <span v-for="chip in chips" :key="chip.key" class="chip">
      {{ chip.label }}
      <button class="chip-x" :title="`移除 ${chip.label}`" @click="$emit('remove', chip.key)">
        <svg viewBox="0 0 10 10" width="9" height="9" fill="none">
          <path d="M2 2l6 6M8 2l-6 6" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
        </svg>
      </button>
    </span>
  </div>
</template>

<script setup>
defineProps({
  chips: { type: Array, default: () => [] } // [{ key, label }]
})
defineEmits(['remove'])
</script>

<style scoped>
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

/* 磨砂胶囊 chip（DESIGN.md 深化稿 VIEW.10）：磨砂底 + 深字 + 可移除 × */
.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 8px 5px 13px;
  border: none;
  border-radius: 999px;
  background-color: rgba(244, 241, 237, 0.85);
  box-shadow: 0 1px 3px rgba(65, 64, 60, 0.08);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

.chip-x {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--color-text-secondary);
  padding: 0;
  cursor: pointer;
  transition: background-color 0.35s cubic-bezier(0.32, 0.72, 0, 1), color 0.35s cubic-bezier(0.32, 0.72, 0, 1);
}

.chip-x:hover {
  background-color: rgba(176, 104, 92, 0.14);
  color: var(--color-danger);
}
</style>
