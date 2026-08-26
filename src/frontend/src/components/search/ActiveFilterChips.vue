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
  gap: 6px;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px 4px 12px;
  border: 1px solid var(--color-primary);
  border-radius: 14px;
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
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
  color: var(--color-primary);
  padding: 0;
}

.chip-x:hover {
  background-color: var(--color-primary);
  color: #fff;
}
</style>
