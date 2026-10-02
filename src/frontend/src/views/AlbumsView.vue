<template>
  <div class="albums-page">
    <header class="page-toolbar">
      <div class="ph-heading">
        <p class="ph-eyebrow">03</p>
        <h2 class="page-title">相册</h2>
      </div>
      <div class="spacer"></div>
      <button class="btn primary" @click="createOpen = true">
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none">
          <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
        </svg>
        新建相册
      </button>
    </header>

    <p v-if="loading" class="page-tip">加载中…</p>
    <p v-else-if="loadError" class="page-tip error">
      {{ loadError }}
      <button class="retry-btn" @click="load">重试</button>
    </p>
    <div v-else-if="!albums.length" class="empty">
      <svg viewBox="0 0 24 24" width="44" height="44" fill="none">
        <rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5" />
        <path d="M3 15l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
        <circle cx="9" cy="9.5" r="1.6" stroke="currentColor" stroke-width="1.5" />
      </svg>
      <p class="empty-title">还没有相册</p>
      <p class="empty-desc">新建普通相册手动整理照片，或新建智能相册按条件自动收录</p>
      <button class="btn primary" @click="createOpen = true">新建相册</button>
    </div>

    <div v-else class="album-grid">
      <AlbumCard
        v-for="a in albums"
        :key="a.id"
        :album="a"
        @open="openAlbum"
        @rename="renameTarget = $event"
        @delete="deleteTarget = $event"
      />
    </div>

    <AlbumFormDialog v-if="createOpen" @cancel="createOpen = false" @saved="onCreated" />
    <AlbumFormDialog
      v-if="renameTarget"
      :album="renameTarget"
      @cancel="renameTarget = null"
      @saved="onRenamed"
    />

    <div v-if="deleteTarget" class="dlg-mask" @click.self="deleteTarget = null">
      <div class="confirm-dlg" role="alertdialog">
        <h3 class="confirm-title">删除相册</h3>
        <p class="confirm-text">确定删除「{{ deleteTarget.name }}」吗？相册内的媒体不会被删除，此操作不可撤销。</p>
        <p v-if="deleteError" class="confirm-error">{{ deleteError }}</p>
        <div class="dlg-actions">
          <button class="btn" @click="deleteTarget = null">取消</button>
          <button class="btn danger" :disabled="deleting" @click="confirmDelete">
            {{ deleting ? '删除中…' : '删除' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { nextTick, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import gsap from 'gsap'
import AlbumCard from '../components/albums/AlbumCard.vue'
import AlbumFormDialog from '../components/albums/AlbumFormDialog.vue'
import { deleteAlbum, errMsg, getAlbum, listAlbums } from '../components/albums/albumApi'

const router = useRouter()

const albums = ref([])
const loading = ref(false)
const loadError = ref('')
const createOpen = ref(false)
const renameTarget = ref(null)
const deleteTarget = ref(null)
const deleting = ref(false)
const deleteError = ref('')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const list = await listAlbums()
    albums.value = list
    fillFirstCovers(list)
  } catch (e) {
    loadError.value = errMsg(e, '相册列表加载失败')
  } finally {
    loading.value = false
  }
}

// 无封面相册回填首图（仅在缺少 cover_media_id 且有内容时逐个取详情）
function fillFirstCovers(list) {
  for (const a of list) {
    if (a.cover_media_id || !a.media_count) continue
    getAlbum(a.id)
      .then((detail) => {
        const first = Array.isArray(detail.items) ? detail.items[0] : null
        if (first) a.first_media_id = first.id
      })
      .catch(() => {})
  }
}

function openAlbum(a) {
  router.push({ name: 'album-detail', params: { id: a.id } })
}

function onCreated(saved) {
  createOpen.value = false
  load()
  if (saved && saved.id) {
    router.push({ name: 'album-detail', params: { id: saved.id } })
  }
}

function onRenamed() {
  renameTarget.value = null
  load()
}

async function confirmDelete() {
  if (!deleteTarget.value || deleting.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    await deleteAlbum(deleteTarget.value.id)
    deleteTarget.value = null
    load()
  } catch (e) {
    deleteError.value = errMsg(e, '删除失败')
  } finally {
    deleting.value = false
  }
}

onMounted(load)

// DESIGN.md §8 grid-stagger：首屏一次性瀑布入场（clearProps 还原，不残留 transform；
// prefers-reduced-motion 时整体跳过——与 TimelineGrid 首屏 stagger 同模式）
let gridStaggerDone = false
watch(
  () => albums.value.length,
  async (n) => {
    if (n > 0 && !gridStaggerDone) {
      gridStaggerDone = true
      await nextTick()
      requestAnimationFrame(() => {
        const mm = gsap.matchMedia()
        mm.add('(prefers-reduced-motion: no-preference)', () => {
          gsap.from('.album-grid .acard', {
            y: 24,
            opacity: 0,
            duration: 0.55,
            ease: 'power2.out',
            stagger: 0.05,
            clearProps: 'transform,opacity'
          })
        })
      })
    }
  }
)
</script>

<style scoped>
.albums-page {
  padding: 20px 24px;
}

.page-toolbar {
  display: flex;
  align-items: center;
  margin-bottom: 16px;
}

/* 页头规范（DESIGN.md §5）：mono 灰序号 + 16px 标题（字距 .12em）+ 右侧主操作 */
.ph-heading {
  display: flex;
  align-items: baseline;
  gap: 10px;
}

.ph-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  color: var(--color-text-disabled);
}

.page-title {
  font-size: var(--font-size-lg);
  letter-spacing: 0.12em;
  color: var(--color-text-primary);
}

.spacer {
  flex: 1;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.btn:hover {
  background-color: var(--color-surface-hover);
}

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.btn.primary:hover {
  background-color: var(--color-primary-hover);
}

.btn.danger {
  border-color: var(--color-danger);
  color: #fff;
  background-color: var(--color-danger);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.page-tip {
  padding: 40px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.page-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
  font-size: var(--font-size-md);
}

.album-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 16px;
}

.empty {
  padding: 64px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  color: var(--color-text-disabled);
}

.empty-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.empty-desc {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  margin-bottom: 8px;
}

.dlg-mask {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.4);
}

.confirm-dlg {
  width: 380px;
  max-width: calc(100vw - 32px);
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 20px;
}

.confirm-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  margin-bottom: 10px;
}

.confirm-text {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  margin-bottom: 16px;
}

.confirm-error {
  margin-bottom: 12px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
