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

      <!-- 雾面 veil：顶部薄雾让媒体「沉」进界面（DESIGN.md §4 媒体卡） -->
      <div v-if="coverUrl" class="acard-veil" aria-hidden="true"></div>

      <span v-if="album.kind === 'smart'" class="acard-tag tag-smart"><i class="tag-dot"></i>智能</span>
      <span v-else-if="isFavorites" class="acard-tag tag-fav"><i class="tag-dot"></i>收藏</span>
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
/* 卡片规范（DESIGN.md §4）：米白面、无边框、双层漫射阴影；hover = 阴影升至 lift 层（不位移不描边） */
.acard {
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  overflow: hidden;
  cursor: pointer;
  box-shadow: var(--shadow-card);
  transition: box-shadow 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.acard:hover {
  box-shadow: var(--shadow-lift);
}

.acard-cover {
  position: relative;
  width: 100%;
  aspect-ratio: 4 / 3;
  background-color: var(--color-surface-hover);
  overflow: hidden;
}

.acard-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.acard:hover .acard-img {
  transform: scale(1.03);
}

.acard-veil {
  position: absolute;
  inset: 0;
  background: linear-gradient(180deg, rgba(233, 228, 222, 0.16), transparent 34%);
  pointer-events: none;
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

/* 状态 chip：磨砂圆角胶囊 + 语义色圆点（DESIGN.md §4） */
.acard-tag {
  position: absolute;
  top: 8px;
  left: 8px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2.5px 9px;
  border-radius: 999px;
  font-size: 11px;
  line-height: 1.5;
  color: var(--color-text-primary);
  background: rgba(244, 241, 237, 0.82);
  box-shadow: 0 1px 3px rgba(65, 64, 60, 0.08);
}

.tag-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.tag-smart .tag-dot {
  background-color: var(--color-primary);
}

.tag-fav .tag-dot {
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
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.acard-meta {
  margin-top: 2px;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--color-text-secondary);
}

.acard-actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
  opacity: 0;
  transform: translateY(2px);
  transition: opacity 0.35s cubic-bezier(0.32, 0.72, 0, 1), transform 0.35s cubic-bezier(0.32, 0.72, 0, 1);
}

.acard:hover .acard-actions,
.acard:focus-within .acard-actions {
  opacity: 1;
  transform: translateY(0);
}

/* 触屏无 hover：操作钮恒显 */
@media (hover: none) {
  .acard-actions {
    opacity: 1;
    transform: none;
  }
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
