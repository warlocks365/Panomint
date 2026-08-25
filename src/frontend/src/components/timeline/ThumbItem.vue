<template>
  <div class="thumb" :title="item.filename" @click="$emit('open', item)">
    <img v-if="url" :src="url" :alt="item.filename" class="thumb-img" loading="lazy" />
    <div v-else class="thumb-placeholder">
      <svg viewBox="0 0 24 24" width="28" height="28" fill="none">
        <rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.5" />
        <path d="M3 16l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
        <circle cx="9" cy="8" r="1.6" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </div>

    <span v-if="is360" class="badge badge-360">
      <svg viewBox="0 0 24 24" width="11" height="11" fill="none">
        <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.6" />
        <ellipse cx="12" cy="12" rx="8.5" ry="3.6" stroke="currentColor" stroke-width="1.4" />
      </svg>
      360
    </span>

    <span v-if="item.type === 'video'" class="badge badge-video">
      <svg viewBox="0 0 24 24" width="10" height="10" fill="currentColor">
        <path d="M8 5.5v13l11-6.5z" />
      </svg>
      {{ formatDuration(item.duration) }}
    </span>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { loadThumbUrl } from './mediaLoader'

const props = defineProps({
  item: { type: Object, required: true }
})
defineEmits(['open'])

const url = ref('')
let alive = true

const is360 = computed(() => props.item.is_360 || props.item.type === '360')

watch(
  () => props.item.id,
  async () => {
    url.value = ''
    try {
      const u = await loadThumbUrl(props.item, 'md')
      if (alive) url.value = u
    } catch (e) {
      if (alive) url.value = ''
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  alive = false
})

function formatDuration(sec) {
  if (!sec && sec !== 0) return ''
  const s = Math.round(sec)
  const m = Math.floor(s / 60)
  const r = s % 60
  return `${m}:${String(r).padStart(2, '0')}`
}
</script>

<style scoped>
.thumb {
  position: relative;
  width: 100%;
  height: 100%;
  border-radius: var(--radius-sm);
  overflow: hidden;
  background-color: var(--color-surface-hover);
  cursor: pointer;
}

.thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.thumb:hover .thumb-img {
  filter: brightness(0.92);
}

.thumb-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-disabled);
}

.badge {
  position: absolute;
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  font-size: 11px;
  line-height: 1.4;
  color: #fff;
  background-color: rgba(0, 0, 0, 0.55);
}

.badge-360 {
  top: 6px;
  right: 6px;
}

.badge-video {
  right: 6px;
  bottom: 6px;
}
</style>
