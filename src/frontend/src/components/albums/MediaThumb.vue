<template>
  <div class="mthumb" :class="{ selectable }" @click="$emit('click', item)">
    <img v-if="url" :src="url" :alt="item.filename || '媒体'" class="mthumb-img" loading="lazy" />
    <div v-else class="mthumb-placeholder">
      <svg viewBox="0 0 24 24" width="26" height="26" fill="none">
        <rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.5" />
        <path d="M3 16l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
        <circle cx="9" cy="8" r="1.6" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </div>

    <span v-if="item.type === 'video'" class="mthumb-badge">
      <svg viewBox="0 0 24 24" width="10" height="10" fill="currentColor">
        <path d="M8 5.5v13l11-6.5z" />
      </svg>
      视频
    </span>

    <span v-if="selected" class="mthumb-check">
      <svg viewBox="0 0 24 24" width="12" height="12" fill="none">
        <path d="M5 12.5l4.5 4.5L19 7.5" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </span>

    <button
      v-if="removable"
      class="mthumb-remove"
      title="从相册移除"
      @click.stop="$emit('remove', item)"
    >
      <svg viewBox="0 0 24 24" width="12" height="12" fill="none">
        <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
      </svg>
    </button>

    <slot />
  </div>
</template>

<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { loadThumbUrl } from '../timeline/mediaLoader'

const props = defineProps({
  item: { type: Object, required: true },
  size: { type: String, default: 'md' },
  selectable: { type: Boolean, default: false },
  selected: { type: Boolean, default: false },
  removable: { type: Boolean, default: false }
})
defineEmits(['click', 'remove'])

const url = ref('')
let alive = true

watch(
  () => props.item.id,
  async () => {
    url.value = ''
    try {
      const u = await loadThumbUrl(props.item, props.size)
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
</script>

<style scoped>
.mthumb {
  position: relative;
  width: 100%;
  aspect-ratio: 1 / 1;
  border-radius: var(--radius-sm);
  overflow: hidden;
  background-color: var(--color-surface-hover);
  cursor: pointer;
}

.mthumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.mthumb:hover .mthumb-img {
  filter: brightness(0.92);
}

.mthumb-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-disabled);
}

.mthumb-badge {
  position: absolute;
  right: 6px;
  bottom: 6px;
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

.mthumb-check {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background-color: var(--color-primary);
}

.mthumb.selectable {
  outline: 2px solid transparent;
  outline-offset: -2px;
}

.mthumb.selectable:hover {
  outline-color: var(--color-border);
}

.mthumb-remove {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: 50%;
  display: none;
  align-items: center;
  justify-content: center;
  color: #fff;
  background-color: rgba(0, 0, 0, 0.55);
  cursor: pointer;
}

.mthumb:hover .mthumb-remove {
  display: flex;
}

.mthumb-remove:hover {
  background-color: var(--color-danger);
}
</style>
