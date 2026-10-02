<template>
  <aside class="tree-panel">
    <div v-if="loading" class="muted tree-tip">目录加载中…</div>
    <template v-else>
      <button
        class="tree-row"
        :class="{ 'tree-row--active': selectedPath === '' }"
        :style="{ paddingLeft: '12px' }"
        data-testid="folder-tree-root"
        @click="$emit('select', '')"
      >
        <span class="tree-caret tree-caret--leaf"></span>
        <span class="tree-icon" v-html="icons.folder"></span>
        <span class="tree-name">全部</span>
        <span class="tree-count">{{ totalCount }}</span>
      </button>
      <button
        v-for="row in flatRows"
        :key="row.node.path"
        class="tree-row"
        :class="{ 'tree-row--active': selectedPath === row.node.path }"
        :style="{ paddingLeft: 12 + row.depth * 18 + 'px' }"
        @click="onRowClick(row)"
      >
        <span
          class="tree-caret"
          :class="{ 'tree-caret--leaf': row.node.children.length === 0 }"
          @click.stop="toggleExpand(row.node.path)"
          v-html="caretIcon(row.node)"
        ></span>
        <span class="tree-icon" v-html="icons.folder"></span>
        <span class="tree-name">{{ row.node.name }}</span>
        <span class="tree-count">{{ row.node.count }}</span>
      </button>
    </template>
  </aside>
</template>

<script setup>
// 文件夹树面板——从 FoldersView 抽出（Job000063：预览/打开功能致其越基线，顺带拆分）。
// 默认展开第一层；点击行=选中目录，点击 caret=仅展开/收起。
import { computed, ref } from 'vue'

const props = defineProps({
  tree: { type: Object, default: null },
  loading: { type: Boolean, default: true },
  selectedPath: { type: String, default: '' },
  totalCount: { type: Number, default: 0 }
})
const emit = defineEmits(['select'])

const icons = {
  folder:
    '<svg viewBox="0 0 16 16" width="15" height="15" fill="none"><path d="M2 4a1 1 0 0 1 1-1h3.6l1.6 2H13a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V4z" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/></svg>',
  caretRight:
    '<svg viewBox="0 0 10 10" width="10" height="10" fill="none"><path d="M3.5 2l3.5 3-3.5 3" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  caretDown:
    '<svg viewBox="0 0 10 10" width="10" height="10" fill="none"><path d="M2 3.5l3 3.5 3-3.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>'
}

// 默认展开第一层
const expanded = ref(new Set())
if (props.tree?.children) {
  const s = new Set()
  for (const n of props.tree.children) s.add(n.path)
  expanded.value = s
}

// 拍平目录树为可见行（含层级），按展开状态裁剪子树
const flatRows = computed(() => {
  const rows = []
  const walk = (nodes, depth) => {
    for (const n of nodes || []) {
      rows.push({ node: n, depth })
      if (n.children.length > 0 && expanded.value.has(n.path)) {
        walk(n.children, depth + 1)
      }
    }
  }
  walk(props.tree?.children || [], 0)
  return rows
})

function caretIcon(node) {
  if (node.children.length === 0) return ''
  return expanded.value.has(node.path) ? icons.caretDown : icons.caretRight
}

function toggleExpand(path) {
  const s = new Set(expanded.value)
  if (s.has(path)) {
    s.delete(path)
  } else {
    s.add(path)
  }
  expanded.value = s
}

function onRowClick(row) {
  if (row.node.children.length > 0) toggleExpand(row.node.path)
  emit('select', row.node.path)
}
</script>

<style scoped>
/* 样式逐字迁移自 FoldersView（Job000063 拆分），未改一条声明 */
.tree-panel {
  /* 宽度由宿主 grid 轨道（minmax(220px, 280px) 1fr）控制，此处不再定宽 */
  width: auto;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 8px;
  overflow-y: auto;
  max-height: calc(100vh - var(--topbar-height) - 140px);
}

.tree-tip {
  padding: 16px 12px;
  font-size: var(--font-size-sm);
}

.tree-row {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 7px 12px;
  border: none;
  background: transparent;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  cursor: pointer;
  font-family: var(--font-family);
  text-align: left;
}

.tree-row:hover {
  background-color: var(--color-surface-hover);
}

.tree-row--active {
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
}

.tree-caret {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  color: var(--color-text-secondary);
  flex-shrink: 0;
}

.tree-caret--leaf {
  visibility: hidden;
}

.tree-row--active .tree-caret {
  color: var(--color-primary);
}

.tree-icon {
  display: inline-flex;
  color: var(--color-text-secondary);
  flex-shrink: 0;
}

.tree-row--active .tree-icon {
  color: var(--color-primary);
}

.tree-name {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tree-count {
  font-family: var(--font-mono);
  font-size: 10.5px;
  letter-spacing: 0.02em;
  color: var(--color-text-disabled);
}

.muted {
  color: var(--color-text-secondary);
}
</style>
