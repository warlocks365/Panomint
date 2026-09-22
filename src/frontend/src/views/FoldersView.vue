<template>
  <div class="folders-view">
    <h1 class="page-title">文件夹</h1>

    <div v-if="loadError" class="error-banner">{{ loadError }}</div>

    <div class="folders-body">
      <FolderTree
        :tree="tree"
        :loading="treeLoading"
        :selected-path="selectedPath"
        :total-count="totalCount"
        @select="selectFolder"
      />

      <section class="grid-panel">
        <div class="grid-header">
          <nav class="crumbs" aria-label="当前目录">
            <button
              type="button"
              class="crumb"
              :class="{ 'crumb--active': !selectedPath }"
              data-testid="crumb-root"
              @click="selectFolder('')"
            >全部</button>
            <template v-for="(seg, i) in pathSegments" :key="seg.path">
              <span class="crumb-sep">/</span>
              <button
                type="button"
                class="crumb"
                :class="{ 'crumb--active': i === pathSegments.length - 1 }"
                :data-testid="'crumb-' + i"
                @click="selectFolder(seg.path)"
              >{{ seg.name }}</button>
            </template>
          </nav>
          <div class="header-actions">
            <button class="btn-ghost" data-testid="folder-new" type="button" @click="openManage('create')">
              新建文件夹
            </button>
            <template v-if="selectedOwner">
              <button class="btn-ghost" data-testid="folder-rename" type="button" @click="openManage('rename')">
                改名/移动
              </button>
              <button class="btn-ghost" data-testid="folder-grants" type="button" @click="openManage('grants')">
                权限
              </button>
              <button class="btn-ghost danger" data-testid="folder-delete" type="button" @click="openManage('delete')">
                删除目录
              </button>
            </template>
            <span v-if="!mediaLoading" class="muted">{{ filteredItems.length }} 项</span>
            <router-link
              class="btn-upload"
              data-testid="folder-upload"
              :to="'/upload?folder=' + (selectedPath || '')"
            >上传到此处</router-link>
          </div>
        </div>

        <div v-if="mediaLoading" class="muted grid-tip">加载中…</div>
        <div v-else-if="filteredItems.length === 0" class="muted grid-tip">该目录暂无媒体</div>

        <MediaTileGrid :items="filteredItems" selectable @open="openItem" @changed="reloadMedia" />
      </section>
    </div>

    <FolderManageDialog
      v-if="manageMode"
      :mode="manageMode"
      :target-path="selectedPath"
      :initial-grants="selectedGrants"
      :users="users"
      @close="manageMode = ''"
      @done="onManaged"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import http from '../api/http'
import FolderTree from './folders/FolderTree.vue'
import FolderManageDialog from './folders/FolderManageDialog.vue'
import MediaTileGrid from '../components/media/MediaTileGrid.vue'

const router = useRouter()
const route = useRoute()

// ---- 目录管理（Job000069）：选中节点 owner 时开放 改名/权限/删除 ----
const manageMode = ref('')
const users = ref([])

const selectedNode = computed(() => findNode(tree.value, selectedPath.value))
const selectedOwner = computed(() => !!selectedNode.value?.owner)
const selectedGrants = computed(() => selectedNode.value?.grants || [])

function findNode(node, path) {
  if (!node) return null
  if (node.path === path) return node
  for (const ch of node.children || []) {
    const hit = findNode(ch, path)
    if (hit) return hit
  }
  return null
}

function openManage(mode) {
  manageMode.value = mode
}

async function onManaged() {
  manageMode.value = ''
  await Promise.all([loadTree(), reloadMedia()])
}

function openItem(m) {
  router.push({ name: 'player', params: { id: m.id }

 })
}


const tree = ref(null)
const treeLoading = ref(true)
// 刷新/分享链接可还原目录：?folder=<path>（Job000069 重构时 selectFolder 被误删、
// 选中态永远停在空串，本轮补回并加上路由同步）
const selectedPath = ref(typeof route.query.folder === 'string' ? route.query.folder : '')
const loadError = ref('')

const mediaItems = ref([])
const mediaLoading = ref(true)

const totalCount = computed(() => tree.value?.count ?? mediaItems.value.length)

// 面包屑：把选中路径拆成逐级可点的祖先段（全部 / a / a/b …）
const pathSegments = computed(() => {
  if (!selectedPath.value) return []
  const segs = []
  let acc = ''
  for (const part of selectedPath.value.split('/').filter(Boolean)) {
    acc = acc ? acc + '/' + part : part
    segs.push({ name: part, path: acc })
  }
  return segs
})

// 目录切换：全量媒体已在内存，client 侧过滤即时生效（任意层级切换立即重载）；
// 同时写回路由 query 保持路径同步。挂载点在下方 selectFolder 之前的旧实现
// 只有 selectedPath.value = path 一行，Job000069 补管理功能时被整块误删。
function selectFolder(path) {
  selectedPath.value = path
  const q = { ...route.query }
  if (path) q.folder = path
  else delete q.folder
  router.replace({ query: q })
}

// 当前目录媒体：folder_path 等于选中路径，或位于其子目录下
const filteredItems = computed(() => {
  if (!selectedPath.value) return mediaItems.value
  const prefix = selectedPath.value + '/'
  return mediaItems.value.filter((m) => {
    const fp = m.folder_path || ''
    return fp === selectedPath.value || fp.startsWith(prefix)
  })
})

// 目录切换/媒体到位 → 预载可见项缩略图


async function fetchAllMedia() {
  const items = []
  let cursor = ''
  // 拉取全量（游标分页，上限保护 20 页）
  for (let i = 0; i < 20; i++) {
    const params = { limit: 200 }
    if (cursor) params.cursor = cursor
    const res = await http.get('/media', { params, timeout: 0 }) // 全量连拉上限 20 页，慢网/大库下 15s 默认超时不够
    items.push(...(res.data.items || []))
    cursor = res.data.next_cursor || ''
    if (!cursor) break
  }
  return items
}

function reloadMedia() {
  mediaItems.value = []
  mediaLoading.value = true
  fetchAllMedia().then((items) => {
    mediaItems.value = items
  }).catch((e) => {
    loadError.value = '媒体列表加载失败：' + (e.response?.data?.error?.message || '网络错误')
  }).finally(() => {
    mediaLoading.value = false
  })
}

async function loadTree() {
  const res = await http.get('/folders/tree')
  tree.value = res.data
}

onMounted(async () => {
  try {
    await loadTree()
    // 路由带入的目录若既未注册也无媒体（树里不存在），回退到全部，
    // 避免标题/面包屑显示一个不可达的空白路径
    if (selectedPath.value && !findNode(tree.value, selectedPath.value)) {
      selectFolder('')
    }
  } catch (e) {
    loadError.value = '目录树加载失败：' + (e.response?.data?.error?.message || '网络错误')
  } finally {
    treeLoading.value = false
  }

  // 授权对话框的用户候选（管理端点 owner/admin 可用；失败不阻塞主流程）。
  try {
    const res = await http.get('/admin/users')
    users.value = res.data.users || []
  } catch {
    users.value = []
  }

  try {
    mediaItems.value = await fetchAllMedia()
  } catch (e) {
    loadError.value = '媒体列表加载失败：' + (e.response?.data?.error?.message || '网络错误')
  } finally {
    mediaLoading.value = false
  }
})
</script>

<style scoped>
.folders-view {
  display: flex;
  flex-direction: column;
  gap: 20px;
  height: 100%;
}

.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.error-banner {
  padding: 10px 14px;
  border-radius: var(--radius-md);
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
  font-size: var(--font-size-sm);
}

.folders-body {
  display: flex;
  gap: 20px;
  min-height: 0;
  flex: 1;
}

.grid-panel {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.btn-upload {
  display: inline-flex;
  align-items: center;
  padding: 6px 12px;
  margin-left: 10px;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary);
  color: #fff;
  font-size: var(--font-size-sm);
  text-decoration: none;
}

.btn-upload:hover {
  background-color: var(--color-primary-hover);
}

.grid-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.crumbs {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
  min-width: 0;
}

.crumb {
  border: none;
  background: transparent;
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
  cursor: pointer;
  font-family: var(--font-family);
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.crumb:hover {
  background-color: var(--color-surface-hover);
  color: var(--color-primary);
}

.crumb--active {
  color: var(--color-primary);
}

.crumb-sep {
  color: var(--color-text-disabled);
}

.muted {
  color: var(--color-text-secondary);
}

.grid-tip {
  padding: 48px 0;
  text-align: center;
}


</style>
