<template>
  <div class="trash-thumb-inner">
    <img v-if="url" :src="url" :alt="item.filename" />
    <div v-else class="ph">
      <svg viewBox="0 0 24 24" width="20" height="20" fill="none">
        <rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.5" />
        <path d="M3 16l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
      </svg>
    </div>
    <span v-if="item.is_360 || item.type === '360'" class="badge-360">
      <svg viewBox="0 0 24 24" width="9" height="9" fill="none">
        <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.8" />
        <ellipse cx="12" cy="12" rx="8.5" ry="3.6" stroke="currentColor" stroke-width="1.6" />
      </svg>
      360
    </span>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { loadThumbUrl } from './mediaLoader'

const props = defineProps({
  item: { type: Object, required: true }
})

const url = ref('')

onMounted(async () => {
  try {
    url.value = await loadThumbUrl(props.item, 'sm')
  } catch (e) {
    url.value = ''
  }
})
</script>

<style scoped>
.trash-thumb-inner {
  position: relative;
  width: 100%;
  height: 100%;
  border-radius: var(--radius-sm);
  overflow: hidden;
  background-color: var(--color-surface-hover);
}

img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.ph {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-disabled);
}

.badge-360 {
  position: absolute;
  top: 3px;
  right: 3px;
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 1px 4px;
  border-radius: 4px;
  font-size: 10px;
  line-height: 1.3;
  color: #fff;
  background-color: rgba(0, 0, 0, 0.55);
}
</style>
