<template>
  <div class="tree-mask" data-testid="tree-dialog" @click.self="onClose">
    <div class="tree-dlg" role="dialog" aria-label="选择扫描目录">
      <h3 class="tree-title">选择扫描目录</h3>
      <p class="tree-sub">媒体根：{{ rootLabel }}</p>

      <div class="tree-body" data-testid="tree-body">
        <DirTreeRow
          v-for="row in flatRows"
          :key="row.node.rel || '__root__'"
          :node="row.node"
          :depth="row.depth"
          :selected="selected === row.node.rel"
          @select="select"
          @toggle="toggle"
        />

        <!-- 懒加载行：展开后逐节点渲染 -->
        <template v-for="row in placeholderRows" :key="row.key">
          <div
            v-if="row.kind === 'empty'"
            class="tree-note"
            :style="{ paddingLeft: 8 + row.depth * 18 + 26 + 'px' }"
            data-testid="tree-empty"
          >（无子目录）</div>
          <div
            v-else-if="row.kind === 'denied'"
            class="tree-note tree-note--locked"
            :style="{ paddingLeft: 8 + row.depth * 18 + 26 + 'px' }"
            :data-testid="'tree-denied-' + row.for"
          >无权限访问</div>
          <div
            v-else
            class="tree-note tree-note--error"
            :style="{ paddingLeft: 8 + row.depth * 18 + 26 + 'px' }"
            data-testid="tree-load-error"
          >{{ row.text }}</div>
        </template>
      </div>

      <p v-if="loadErr" class="tree-err" data-testid="tree-error">{{ loadErr }}</p>

      <div class="tree-foot">
        <span class="tree-picked" data-testid="tree-picked">
          选中：{{ pickedLabel }}
        </span>
        <div class="tree-actions">
          <button
            v-if="allowClear"
            class="btn btn--danger-ghost"
            type="button"
            data-testid="tree-clear"
            @click="onClear"
          >
            清除分配
          </button>
          <button class="btn" type="button" data-testid="tree-cancel" @click="onClose">取消</button>
          <button class="btn btn--primary" type="button" data-testid="tree-confirm" @click="onConfirm">选择此目录</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
// 目录树选择对话框（Job000117）：扫描导入的目录选择器。
// 懒加载：展开节点时才请求目录树接口（seq 守卫防过期响应落地）；
// 边界：无权限子目录 → 锁定图标+禁止展开（后端 readable=false）；目标目录本身无权限 →
// 后端 200 unreadable=true，本组件就地显示「无权限访问」占位行，不报错；空目录 →
// 「（无子目录）」占位行。符号链接在后端已被排除（lstat 语义），树不会越出边界。
// 行渲染拆至 DirTreeRow.vue（frontend_org_guard O1 单文件 ≤300 行棘轮）。
//
// Job000123 参数化：loader 可换成任意同构数据源（成员端 /fs/tree 的边界是扫描根而非
// 媒体根）；allowClear 提供「清除分配」第三出口（管理员取消某账号的 scan_root）。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { listDirTree } from '../../api/admin'
import DirTreeRow from './DirTreeRow.vue'

const props = defineProps({
  // loader：目录树数据源；缺省用管理端 /admin/fs/tree（admin:system）。
  loader: { type: Function, default: null },
  // allowClear：显示「清除分配」按钮，点击 emit('clear')。
  allowClear: { type: Boolean, default: false }
})
const emit = defineEmits(['pick', 'close', 'clear'])

const fetchTree = props.loader || listDirTree

function makeNode(name, rel) {
  return { name, rel, readable: true, expanded: false, loading: false, loaded: false, children: [], error: '' }
}

const rootNode = ref(makeNode('（媒体根）', ''))
const selected = ref('')
const rootLabel = ref('')
const loadErr = ref('')
let seq = 0

const flatRows = computed(() => {
  const rows = []
  const walk = (node, depth) => {
    rows.push({ node, depth })
    if (node.expanded) {
      for (const c of node.children) walk(c, depth + 1)
    }
  }
  walk(rootNode.value, 0)
  return rows
})

// 展开节点的占位提示行（空目录 / 无权限 / 加载失败）——平铺渲染，不进 flatRows 主列。
const placeholderRows = computed(() => {
  const rows = []
  const walk = (node, depth) => {
    if (node.expanded && node.loaded) {
      if (node.denied) {
        rows.push({ kind: 'denied', for: node.rel, depth })
      } else if (node.children.length === 0) {
        rows.push({ kind: 'empty', depth })
      }
    }
    if (node.expanded) {
      for (const c of node.children) walk(c, depth + 1)
    }
  }
  walk(rootNode.value, 0)
  return rows.map((r, i) => ({ ...r, key: r.kind + '-' + r.for + '-' + i }))
})

const pickedLabel = computed(() => (selected.value === '' ? '（媒体根）' : selected.value))

function select(node) {
  selected.value = node.rel
}

async function toggle(node) {
  if (!node.readable) return // 锁定节点不可展开
  node.expanded = !node.expanded
  if (node.expanded && !node.loaded) await load(node)
}

async function load(node) {
  const mySeq = ++seq
  node.loading = true
  node.error = ''
  try {
    const resp = await fetchTree(node.rel)
    if (mySeq !== seq) return // 已被更新的展开/关闭取代
    rootLabel.value = resp.root || rootLabel.value
    node.loading = false
    node.loaded = true
    if (resp.unreadable) {
      node.denied = true
      node.children = []
      return
    }
    node.denied = false
    node.children = (resp.items || []).map((it) => {
      const n = makeNode(it.name, it.rel)
      n.readable = !!it.readable
      return n
    })
  } catch (e) {
    if (mySeq !== seq) return
    node.loading = false
    node.loaded = true
    node.denied = false
    node.children = []
    node.error = errMessage(e, '加载子目录失败')
    loadErr.value = node.error
  }
}

function onConfirm() {
  emit('pick', selected.value)
}

function onClear() {
  emit('clear')
}

function onClose() {
  emit('close')
}

function onKey(e) {
  if (e.key === 'Escape') onClose()
}

onMounted(async () => {
  window.addEventListener('keydown', onKey)
  rootNode.value.expanded = true
  await load(rootNode.value)
})

onUnmounted(() => {
  seq++ // 作废进行中的请求
  window.removeEventListener('keydown', onKey)
})
</script>

<style scoped>
.tree-mask {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.4);
}
.tree-dlg {
  width: 520px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  display: flex;
  flex-direction: column;
  padding: 20px;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}
.tree-title {
  margin: 0 0 4px;
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}
.tree-sub {
  margin: 0 0 12px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  font-family: monospace;
  word-break: break-all;
}
.tree-body {
  flex: 1;
  min-height: 120px;
  max-height: 46vh;
  overflow: auto;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  padding: 6px 4px;
}
.tree-note { padding-top: 3px; padding-bottom: 3px; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.tree-note--locked { color: var(--color-warning-text); }
.tree-note--error { color: var(--color-danger); }
.tree-err { margin: 8px 0 0; font-size: var(--font-size-sm); color: var(--color-danger); }
.tree-foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 14px; }
.tree-picked {
  flex: 1;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  font-family: monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tree-actions { display: flex; gap: 8px; }
.btn { padding: 8px 16px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); font-size: var(--font-size-md); color: var(--color-text-primary); background-color: var(--color-surface); cursor: pointer; }
.btn:hover { background-color: var(--color-surface-hover); }
.btn--primary { border-color: var(--color-primary); color: #fff; background-color: var(--color-primary); }
.btn--danger-ghost { border-color: var(--color-danger); color: var(--color-danger); }
.btn--danger-ghost:hover { background: var(--color-danger); color: var(--color-surface); }
</style>
