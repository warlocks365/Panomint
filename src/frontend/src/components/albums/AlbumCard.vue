<template>
  <div class="acard" @click="$emit('open', album)">
    <div class="acard-cover">
      <img v-if="coverUrl" :src="coverUrl" :alt="album.name" class="acard-img" loading="lazy" />
      <div v-else class="acard-placeholder">
        <svg viewBox="0 0 24 24" width="34" height="34" fill="none">
          <rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5" />
          <path d="M3 15l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
          <circle cx="9" cy="9.5" r="1.6" stroke="currentColor" stroke-width="1.5" />
        </svg>
        <span v-if="!album.media_count" class="acard-placeholder-text">暂无内容</span>
      </div>

      <span v-if="album.kind === 'smart'" class="acard-tag tag-smart">智能</span>
      <span v-else-if="isFavorites" class="acard-tag tag-fav">收藏</span>
    </div>

    <div class="acard-body">
      <div class="acard-info">
        <p class="acard-name" :title="album.name">{{ album.name }}</p>
        <p class="acard-meta">{{ album.media_count ?? 0 }} 项 · {{ formatDate(album.updated_at) }}</p>
      </div>
      <div class="acard-actions" @click.stop>
        <button class="icon-btn" title="重命名" @click="$emit('rename', album)">
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none">
            <path d="M4 20h4l11-11-4-4L4 16v4z" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
            <path d="M13.5 6.5l4 4" stroke="currentColor" stroke-width="1.6" />
          </svg>
        </button>
        <button class="icon-btn danger" title="删除相册" @click="$emit('delete', album)">
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none">
            <path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6.5 7l1 13h9l1-13" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { loadThumbUrl } from '../timeline/mediaLoader'

const props = defineProps({
  album: { type: Object, required: true }
})
defineEmits(['open', 'rename', 'delete'])

const coverUrl = ref('')
let alive = true

const isFavorites = computed(() => props.album.kind === 'favorites' || props.album.id === 'favorites')

// 封面：优先 cover_media_id，其次列表接口回填的 first_media_id（首图）
const coverId = computed(() => props.album.cover_media_id || props.album.first_media_id || '')

watch(
  coverId,
  async (id) => {
    coverUrl.value = ''
    if (!id) return
    try {
      const u = await loadThumbUrl({ id }, 'md')
      if (alive) coverUrl.value = u
    } catch (e) {
      if (alive) coverUrl.value = ''
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  alive = false
})

function formatDate(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
</script>

<style scoped>
.acard {
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  overflow: hidden;
  cursor: pointer;
  box-shadow: var(--shadow-card);
  transition: border-color 0.15s ease;
}

.acard:hover {
  border-color: var(--color-primary);
}

.acard-cover {
  position: relative;
  width: 100%;
  aspect-ratio: 4 / 3;
  background-color: var(--color-surface-hover);
}

.acard-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.acard-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--color-text-disabled);
}

.acard-placeholder-text {
  font-size: var(--font-size-sm);
}

.acard-tag {
  position: absolute;
  top: 8px;
  left: 8px;
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  font-size: 11px;
  line-height: 1.5;
  color: #fff;
}

.tag-smart {
  background-color: var(--color-primary);
}

.tag-fav {
  background-color: var(--color-danger);
}

.acard-body {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 12px;
}

.acard-info {
  min-width: 0;
}

.acard-name {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.acard-meta {
  margin-top: 2px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.acard-actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.icon-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-secondary);
  background-color: transparent;
  cursor: pointer;
}

.icon-btn:hover {
  background-color: var(--color-surface-hover);
  color: var(--color-text-primary);
}

.icon-btn.danger:hover {
  color: var(--color-danger);
}
</style>
