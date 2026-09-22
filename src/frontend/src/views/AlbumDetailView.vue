<template>
  <div class="album-page">
    <p v-if="loading" class="page-tip">加载中…</p>
    <p v-else-if="loadError" class="page-tip error">
      {{ loadError }}
      <button class="retry-btn" @click="load">重试</button>
    </p>

    <template v-else-if="album">
      <header class="album-header">
        <div class="cover-box">
          <img v-if="coverUrl" :src="coverUrl" :alt="album.name" class="cover-img" />
          <div v-else class="cover-placeholder">
            <svg viewBox="0 0 24 24" width="40" height="40" fill="none">
              <rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5" />
              <path d="M3 15l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
              <circle cx="9" cy="9.5" r="1.6" stroke="currentColor" stroke-width="1.5" />
            </svg>
          </div>
        </div>

        <div class="header-main">
          <div class="title-row">
            <template v-if="!editing">
              <h2 class="album-name">{{ album.name }}</h2>
              <span v-if="album.kind === 'smart'" class="kind-tag">智能</span>
              <button class="icon-btn" title="编辑名称与描述" @click="startEdit">
                <svg viewBox="0 0 24 24" width="15" height="15" fill="none">
                  <path d="M4 20h4l11-11-4-4L4 16v4z" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
                  <path d="M13.5 6.5l4 4" stroke="currentColor" stroke-width="1.6" />
                </svg>
              </button>
            </template>
            <template v-else>
              <input v-model.trim="editForm.name" class="input name-input" type="text" maxlength="60" placeholder="相册名称" />
            </template>
          </div>

          <template v-if="!editing">
            <p class="album-desc">{{ album.description || '暂无描述' }}</p>
          </template>
          <template v-else>
            <textarea v-model.trim="editForm.description" class="input desc-input" rows="2" maxlength="200" placeholder="相册描述（可选）"></textarea>
            <div class="edit-actions">
              <button class="btn sm" @click="editing = false">取消</button>
              <button class="btn primary sm" :disabled="!editForm.name || saving" @click="saveEdit">
                {{ saving ? '保存中…' : '保存' }}
              </button>
            </div>
            <p v-if="saveError" class="save-error">{{ saveError }}</p>
          </template>

          <p class="album-meta">{{ items.length }} 项</p>

          <div class="header-actions">
            <button class="btn" @click="shareOpen = true">分享</button>
            <button class="btn" @click="shareManageOpen = true">分享管理</button>
            <button class="btn" :disabled="!items.length" @click="coverPickerOpen = true">设置封面</button>
            <button v-if="album.kind === 'smart'" class="btn" @click="criteriaOpen = true">编辑条件</button>
            <button v-if="album.kind !== 'smart'" class="btn primary" @click="pickerOpen = true">添加媒体</button>
            <router-link v-if="album.kind !== 'smart'" class="btn" data-testid="album-upload" :to="`/upload?album=${album.id}&albumName=${encodeURIComponent(album.name)}`">上传照片</router-link></div>
        </div>
      </header>

      <div v-if="album.kind === 'smart'" class="criteria-bar">
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none">
          <path d="M4 6h16M7 12h10M10 18h4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
        </svg>
        <span>{{ criteriaSummary }}</span>
        <span class="criteria-note">智能相册按条件自动收录，不支持手动增删</span>
      </div>

      <section class="media-section">
        <p v-if="!items.length" class="page-tip">
          {{ album.kind === 'smart' ? '当前条件未命中任何媒体，可点击「编辑条件」调整' : '相册还是空的，点击「添加媒体」放入第一张照片' }}
        </p>
        <MediaTileGrid
          v-else
          :items="items"
          :selectable="album.kind !== 'smart'"
          :removable="album.kind !== 'smart'"
          @open="openMedia"
          @remove="onRemove"
          @changed="load"
        />
      </section>

      <AlbumComments v-if="album" :album-id="album.id" />
    </template>

    <MediaPickerDialog
      v-if="pickerOpen && album"
      :album-id="album.id"
      :existing-ids="items.map((m) => m.id)"
      @cancel="pickerOpen = false"
      @added="onAdded"
    />

    <CoverPickerDialog
      v-if="coverPickerOpen && album"
      :album-id="album.id"
      :items="items"
      :current-cover-id="album.cover_media_id || ''"
      @cancel="coverPickerOpen = false"
      @saved="onCoverSaved"
    />

    <AlbumFormDialog
      v-if="criteriaOpen && album"
      :album="album"
      criteria-editable
      @cancel="criteriaOpen = false"
      @saved="onCriteriaSaved"
    />

    <ShareCreateDialog
      v-if="shareOpen && album"
      kind="album"
      :target-id="album.id"
      :default-title="album.name"
      @cancel="shareOpen = false"
      @created="onShareCreated"
    />

    <div v-if="shareManageOpen" class="dlg-mask" @click.self="shareManageOpen = false">
      <div class="share-manage-dlg">
        <h3 class="confirm-title">分享管理</h3>
        <ShareManageList ref="shareManageRef" />
        <div class="dlg-actions" style="margin-top: 16px">
          <button class="btn" @click="shareManageOpen = false">关闭</button>
        </div>
      </div>
    </div>

  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import MediaTileGrid from '../components/media/MediaTileGrid.vue'
import MediaPickerDialog from '../components/albums/MediaPickerDialog.vue'
import CoverPickerDialog from '../components/albums/CoverPickerDialog.vue'
import AlbumFormDialog from '../components/albums/AlbumFormDialog.vue'
import AlbumComments from '../components/albums/AlbumComments.vue'
import ShareCreateDialog from '../components/shares/ShareCreateDialog.vue'
import ShareManageList from '../components/shares/ShareManageList.vue'
import { errMsg, getAlbum, removeAlbumItem, updateAlbum } from '../components/albums/albumApi'
import { summarizeCriteria } from '../components/albums/criteriaSummary'
import { loadThumbUrl } from '../components/timeline/mediaLoader'
import { dialogs } from '../components/dialogs/dialogs'

const route = useRoute()
const router = useRouter()

const album = ref(null)
const items = ref([])
const loading = ref(false)
const loadError = ref('')

const coverUrl = ref('')
const pickerOpen = ref(false)
const coverPickerOpen = ref(false)
const criteriaOpen = ref(false)
const shareOpen = ref(false)
const shareManageOpen = ref(false)
const shareManageRef = ref(null)

const editing = ref(false)
const editForm = ref({ name: '', description: '' })
const saving = ref(false)
const saveError = ref('')

let alive = true

const albumId = computed(() => route.params.id)

const coverId = computed(() => album.value?.cover_media_id || items.value[0]?.id || '')

const criteriaSummary = computed(() => summarizeCriteria(album.value?.criteria))

watch(coverId, async (id) => {
  coverUrl.value = ''
  if (!id) return
  try {
    const u = await loadThumbUrl({ id }, 'lg')
    if (alive) coverUrl.value = u
  } catch (e) {
    if (alive) coverUrl.value = ''
  }
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const data = await getAlbum(albumId.value)
    album.value = data
    items.value = Array.isArray(data.items) ? data.items : []
    editing.value = false
  } catch (e) {
    loadError.value = errMsg(e, '相册加载失败')
  } finally {
    loading.value = false
  }
}

function startEdit() {
  editForm.value = { name: album.value.name, description: album.value.description || '' }
  saveError.value = ''
  editing.value = true
}

async function saveEdit() {
  if (!editForm.value.name || saving.value) return
  saving.value = true
  saveError.value = ''
  try {
    const saved = await updateAlbum(album.value.id, {
      name: editForm.value.name,
      description: editForm.value.description
    })
    album.value = { ...album.value, ...(saved && saved.id ? saved : editForm.value) }
    editing.value = false
  } catch (e) {
    saveError.value = errMsg(e, '保存失败')
  } finally {
    saving.value = false
  }
}

function onAdded() {
  pickerOpen.value = false
  load()
}

function openMedia(m) {
  router.push({ name: 'player', params: { id: m.id } })
}

function onShareCreated() {
  // 创建成功后若管理列表已打开，刷新之
  shareManageRef.value?.refresh?.()
}

function onCoverSaved(saved) {
  coverPickerOpen.value = false
  if (saved && saved.cover_media_id) album.value.cover_media_id = saved.cover_media_id
  load()
}

function onCriteriaSaved() {
  criteriaOpen.value = false
  load()
}

async function onRemove(m) {
  const ok = await dialogs.confirm({
    title: '移除媒体',
    text: `确定将「${m.filename || '该媒体'}」从相册中移除吗？媒体本身不会被删除。`,
    danger: true,
    confirmText: '移除'
  })
  if (!ok) return
  try {
    await removeAlbumItem(album.value.id, m.id)
    items.value = items.value.filter((x) => x.id !== m.id)
  } catch (e) {
    await dialogs.alert(errMsg(e, '移除失败'))
  }
}

watch(albumId, () => {
  if (albumId.value) load()
})

onBeforeUnmount(() => {
  alive = false
})

load()
</script>

<style scoped>
.album-page {
  padding: 20px 24px;
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

.album-header {
  display: flex;
  gap: 20px;
  margin-bottom: 16px;
}

.cover-box {
  flex-shrink: 0;
  width: 240px;
  aspect-ratio: 4 / 3;
  border-radius: var(--radius-lg);
  overflow: hidden;
  background-color: var(--color-surface-hover);
  border: 1px solid var(--color-border);
}

.cover-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.cover-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-disabled);
}

.header-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.album-name {
  font-size: 20px;
  color: var(--color-text-primary);
}

.kind-tag {
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  font-size: 11px;
  color: #fff;
  background-color: var(--color-primary);
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

.album-desc {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  word-break: break-word;
}

.album-meta {
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}

.header-actions {
  margin-top: auto;
  display: flex;
  gap: 8px;
}

.name-input {
  max-width: 320px;
}

.desc-input {
  max-width: 480px;
  resize: vertical;
}

.edit-actions {
  display: flex;
  gap: 8px;
}

.save-error {
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.criteria-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 8px 12px;
  margin-bottom: 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background-color: var(--color-primary-active-bg);
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.criteria-note {
  color: var(--color-text-secondary);
}

.media-section {
  margin-bottom: 8px;
}

.input {
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  font-family: var(--font-family);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
}

.input:focus {
  outline: none;
  border-color: var(--color-primary);
}

.btn {
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

.btn.sm {
  padding: 6px 12px;
  font-size: var(--font-size-sm);
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

.share-manage-dlg {
  width: 720px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  overflow: auto;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 20px;
}
</style>
