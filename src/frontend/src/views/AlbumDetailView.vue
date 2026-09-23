<template>
  <div class="album-page">
    <p v-if="loading" class="page-tip">加载中…</p>
    <p v-else-if="loadError" class="page-tip error">
      {{ loadError }}
      <button class="retry-btn" @click="load">重试</button>
    </p>

    <template v-else-if="album">
      <AlbumHeaderPanel
        :album="album"
        :items="items"
        @action="onHeaderAction"
        @saved="onHeaderSaved"
        @back="onBack"
      />

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

    <ShareManageDialog
      v-if="shareManageOpen"
      ref="shareManageRef"
      @close="shareManageOpen = false"
    />
  </div>
</template>

<script setup>
// Job000087 拆解：相册头部（封面/名称描述编辑/条件条/动作按钮组）→ AlbumHeaderPanel；
// 分享管理内联对话框 → ShareManageDialog（ShareManageList 内聚，expose refresh 供新建后刷新）。
// 本页保留：数据加载、各对话框开关分派、移除媒体（统一确认对话框+API）、跳转播放器。
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import MediaTileGrid from '../components/media/MediaTileGrid.vue'
import MediaPickerDialog from '../components/albums/MediaPickerDialog.vue'
import CoverPickerDialog from '../components/albums/CoverPickerDialog.vue'
import AlbumFormDialog from '../components/albums/AlbumFormDialog.vue'
import AlbumComments from '../components/albums/AlbumComments.vue'
import ShareCreateDialog from '../components/shares/ShareCreateDialog.vue'
import ShareManageDialog from '../components/albums/ShareManageDialog.vue'
import AlbumHeaderPanel from '../components/albums/AlbumHeaderPanel.vue'
import { useBackNavigation } from '../composables/useBackNavigation'
import { errMsg, getAlbum, removeAlbumItem } from '../components/albums/albumApi'
import { dialogs } from '../components/dialogs/dialogs'

const route = useRoute()
const router = useRouter()
const { goBack } = useBackNavigation()

// Job000102 层级返回：从列表下钻（/albums、/spaces 分组）→ 历史后退回来源页；
// 直达/刷新（无历史）→ replace 到逻辑父级 /albums（不产生多余历史条目）
function onBack() {
  goBack({ name: 'albums' })
}

const album = ref(null)
const items = ref([])
const loading = ref(false)
const loadError = ref('')

const pickerOpen = ref(false)
const coverPickerOpen = ref(false)
const criteriaOpen = ref(false)
const shareOpen = ref(false)
const shareManageOpen = ref(false)
const shareManageRef = ref(null)

const albumId = computed(() => route.params.id)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const data = await getAlbum(albumId.value)
    album.value = data
    items.value = Array.isArray(data.items) ? data.items : []
  } catch (e) {
    loadError.value = errMsg(e, '相册加载失败')
  } finally {
    loading.value = false
  }
}

/* ---------------- 头部动作分派与名称描述保存 ---------------- */
function onHeaderAction(name) {
  if (name === 'share') shareOpen.value = true
  else if (name === 'shareManage') shareManageOpen.value = true
  else if (name === 'coverPicker') coverPickerOpen.value = true
  else if (name === 'criteria') criteriaOpen.value = true
  else if (name === 'picker') pickerOpen.value = true
}

function onHeaderSaved({ saved, form }) {
  album.value = { ...album.value, ...(saved && saved.id ? saved : form) }
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

.media-section {
  margin-bottom: 8px;
}
</style>
