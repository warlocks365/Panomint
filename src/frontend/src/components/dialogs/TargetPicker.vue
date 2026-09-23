<template>
  <!-- Job000100 移动/复制目标选择器：目录树 / 相册 双 tab 单选。
       由 DialogHost 以 kind='target' 渲染；确定走 dialogResolve 关闭并回传选择。 -->
  <div class="tp" data-testid="target-picker">
    <div class="tp-tabs" role="tablist">
      <button
        class="tp-tab"
        :class="{ active: tab === 'folder' }"
        data-testid="tp-tab-folder"
        role="tab"
        :aria-selected="tab === 'folder'"
        @click="tab = 'folder'"
      >
        目录
      </button>
      <button
        class="tp-tab"
        :class="{ active: tab === 'album' }"
        data-testid="tp-tab-album"
        role="tab"
        :aria-selected="tab === 'album'"
        @click="tab = 'album'"
      >
        相册
      </button>
    </div>

    <p class="tp-hint">{{ hint }}</p>
    <div v-if="loadErr" class="tp-error" data-testid="tp-error">{{ loadErr }}</div>

    <template v-else>
      <div v-show="tab === 'folder'" class="tp-list" data-testid="tp-folder-list">
        <button
          class="tp-row"
          :class="{ selected: isSel('folder', '') }"
          data-testid="tp-row"
          data-path=""
          @click="pickFolder('')"
        >
          <span class="tp-name">根目录</span>
          <span class="tp-count">{{ rootCount }} 项</span>
        </button>
        <button
          v-for="r in flatTree"
          :key="r.path"
          class="tp-row"
          :class="{ selected: isSel('folder', r.path) }"
          data-testid="tp-row"
          :data-path="r.path"
          :style="{ paddingLeft: 12 + r.depth * 16 + 'px' }"
          @click="pickFolder(r.path)"
        >
          <span class="tp-name">{{ r.name }}</span>
          <span class="tp-count">{{ r.count }} 项</span>
        </button>
        <p v-if="!flatTree.length && !loading" class="tp-empty">暂无子目录</p>
      </div>

      <div v-show="tab === 'album'" class="tp-list" data-testid="tp-album-list">
        <button
          v-for="a in albums"
          :key="a.id"
          class="tp-row"
          :class="{ selected: isSel('album', a.id), disabled: a.kind === 'smart' }"
          data-testid="tp-album-row"
          :data-id="a.id"
          :disabled="a.kind === 'smart'"
          :title="a.kind === 'smart' ? '智能相册为只读，不能手动添加' : ''"
          @click="pickAlbum(a)"
        >
          <span class="tp-name">{{ a.name }}</span>
          <span v-if="a.kind === 'smart'" class="tp-badge">智能·只读</span>
          <span class="tp-count">{{ a.media_count }} 项</span>
        </button>
        <p v-if="!albums.length && !loading" class="tp-empty">暂无相册</p>
      </div>
    </template>

    <div class="tp-actions">
      <button class="tp-btn" data-testid="dlg-cancel" @click="dialogCancel">取消</button>
      <button class="tp-btn primary" data-testid="tp-ok" :disabled="!sel" @click="onOk">
        {{ mode === 'move' ? '移动' : '复制' }}
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import http from '../../api/http'
import { dialogCancel, dialogResolve } from './dialogs'

const props = defineProps({
  mode: { type: String, default: 'move' }, // move | copy
  count: { type: Number, default: 0 }
})

const tab = ref('folder')
const loading = ref(true)
const loadErr = ref('')
const tree = ref(null)
const albums = ref([])
const sel = ref(null)

const rootCount = computed(() => tree.value?.count ?? 0)

// 目录树拍平（DFS 保序 + 深度缩进）；根目录单独渲染在上。
const flatTree = computed(() => {
  const out = []
  const walk = (nodes, depth) => {
    for (const n of nodes || []) {
      out.push({ path: n.path, name: n.name, count: n.count, depth })
      walk(n.children, depth + 1)
    }
  }
  walk(tree.value?.children, 1)
  return out
})

const hint = computed(() => {
  const n = props.count
  if (tab.value === 'folder') {
    return props.mode === 'move'
      ? `将选中的 ${n} 项移动到所选目录（仅改目录归属，文件位置不变）`
      : `将选中的 ${n} 项复制到所选目录`
  }
  return props.mode === 'move'
    ? `将选中的 ${n} 项加入所选相册（相册是收藏集合，原目录保留）`
    : `将选中的 ${n} 项复制副本并加入所选相册`
})

function isSel(type, key) {
  if (sel.value?.type !== type) return false
  return type === 'folder' ? sel.value.path === key : sel.value.id === key
}
function pickFolder(path) {
  sel.value = { type: 'folder', path }
}
function pickAlbum(a) {
  if (a.kind === 'smart') return // 与后端 SMART_READONLY 同口径，双保险
  sel.value = { type: 'album', id: a.id, name: a.name }
}
function onOk() {
  if (sel.value) dialogResolve(sel.value)
}

onMounted(async () => {
  try {
    const [t, a] = await Promise.all([http.get('/folders/tree'), http.get('/albums')])
    tree.value = t.data
    albums.value = Array.isArray(a.data?.albums) ? a.data.albums : []
  } catch (e) {
    loadErr.value = '加载失败：' + (e.response?.data?.error?.message || e.message)
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.tp-tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}

.tp-tab {
  padding: 6px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  font-family: var(--font-family);
  color: var(--color-text-secondary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.tp-tab.active {
  border-color: var(--color-primary);
  color: var(--color-primary);
}

.tp-hint {
  margin-bottom: 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.tp-error {
  padding: 12px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.tp-list {
  max-height: 300px;
  overflow: auto;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  margin-bottom: 14px;
}

.tp-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 12px;
  border: none;
  border-bottom: 1px solid var(--color-border);
  font-size: var(--font-size-md);
  font-family: var(--font-family);
  text-align: left;
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.tp-row:last-child {
  border-bottom: none;
}

.tp-row:hover {
  background-color: var(--color-surface-hover);
}

.tp-row.selected {
  background-color: var(--color-primary-bg, rgba(59, 130, 246, 0.12));
  color: var(--color-primary);
}

.tp-row.disabled {
  color: var(--color-text-secondary);
  cursor: not-allowed;
}

.tp-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tp-badge {
  flex-shrink: 0;
  padding: 1px 6px;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-xs, 12px);
  color: var(--color-text-secondary);
  background-color: var(--color-surface-hover);
}

.tp-count {
  flex-shrink: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.tp-empty {
  padding: 16px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  text-align: center;
}

.tp-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.tp-btn {
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  font-family: var(--font-family);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.tp-btn:hover {
  background-color: var(--color-surface-hover);
}

.tp-btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.tp-btn.primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
