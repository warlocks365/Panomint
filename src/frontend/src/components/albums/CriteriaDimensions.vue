<template>
  <div class="dims">
    <section v-for="(d, key) in dims" :key="key" class="dim">
      <div class="dim-head">
        <span class="dim-label">{{ d.label }}</span>
        <button
          v-if="d.options.length"
          type="button"
          class="chip add"
          :data-testid="`crit-add-${key}`"
          @click="d.open = !d.open"
        >
          {{ d.open ? '收起' : `选择${d.label}` }}
        </button>
        <span v-else class="dim-empty">无可用{{ d.label }}</span>
      </div>

      <div v-if="selected(key).length" class="chips" :data-testid="`crit-chips-${key}`">
        <button
          v-for="opt in selected(key)"
          :key="opt.value"
          type="button"
          class="chip on"
          :data-testid="`crit-chip-${key}`"
          :title="opt.value"
          @click="toggle(key, opt.value)"
        >
          {{ opt.label }} ×
        </button>
      </div>

      <div v-if="d.open && d.options.length" class="opts">
        <input v-model="d.q" class="input opt-q" type="text" :placeholder="`筛选${d.label}`" />
        <div class="opt-list" :data-testid="`crit-opts-${key}`">
          <button
            v-for="opt in filtered(key)"
            :key="opt.value"
            type="button"
            class="opt"
            :class="{ on: isOn(key, opt.value) }"
            @click="toggle(key, opt.value)"
          >
            <span v-if="opt.depth" :style="{ paddingLeft: opt.depth * 14 + 'px' }"></span>{{ opt.label
            }}<span v-if="opt.count != null" class="cnt">{{ opt.count }}</span>
          </button>
          <p v-if="!filtered(key).length" class="opt-none">无匹配项</p>
        </div>
      </div>

      <p class="dim-hint">{{ d.hint }}</p>
    </section>
  </div>
</template>

<script setup>
import { onMounted, reactive } from 'vue'
import http from '../../api/http'

// 智能相册三维多选（Job000068）：目录（含子目录前缀语义）/ 标签 / 人物。
// 三数组独立 v-model，与后端 Criteria.folder_paths/tag_ids/person_ids 对应。
const props = defineProps({
  folderPaths: { type: Array, default: () => [] },
  tagIds: { type: Array, default: () => [] },
  personIds: { type: Array, default: () => [] }
})
const emit = defineEmits(['update:folderPaths', 'update:tagIds', 'update:personIds'])

// 维度键 → props 名映射（emit 用）。
const arrKey = { folders: 'folderPaths', tags: 'tagIds', people: 'personIds' }

const dims = reactive({
  folders: { label: '目录', hint: '所选目录及其全部子目录', q: '', open: false, options: [] },
  tags: { label: '标签', hint: '命中任一标签', q: '', open: false, options: [] },
  people: { label: '人物', hint: '含任一人物的人脸', q: '', open: false, options: [] }
})

function curArr(dimKey) {
  return props[arrKey[dimKey]] || []
}

function isOn(dimKey, value) {
  return curArr(dimKey).includes(value)
}

function toggle(dimKey, value) {
  const prop = arrKey[dimKey]
  const cur = [...curArr(dimKey)]
  const i = cur.indexOf(value)
  if (i >= 0) cur.splice(i, 1)
  else cur.push(value)
  emit('update:' + prop, cur)
}

function selected(dimKey) {
  const opts = dims[dimKey].options
  return curArr(dimKey)
    .map((v) => opts.find((o) => o.value === v))
    .filter(Boolean)
}

function filtered(dimKey) {
  const d = dims[dimKey]
  const q = d.q.trim().toLowerCase()
  if (!q) return d.options
  return d.options.filter((o) => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q))
}

// 目录树拍平为缩进列表（value=folder_path，前缀语义在后端）。
function flatten(node, depth, out) {
  for (const ch of node.children || []) {
    out.push({ value: ch.path, label: ch.name, depth, count: ch.count })
    flatten(ch, depth + 1, out)
  }
  return out
}

onMounted(async () => {
  const [tree, tags, people] = await Promise.allSettled([
    http.get('/folders/tree'),
    http.get('/tags'),
    http.get('/people')
  ])
  if (tree.status === 'fulfilled' && tree.value.data) {
    dims.folders.options = flatten(tree.value.data, 0, [])
  }
  if (tags.status === 'fulfilled') {
    dims.tags.options = (tags.value.data?.tags || []).map((t) => ({ value: t.id, label: t.name }))
  }
  // 人物只列已命名（unnamed 是簇，没有 person_id 可供条件引用）。
  if (people.status === 'fulfilled') {
    dims.people.options = (people.value.data?.named || []).map((p) => ({
      value: p.id,
      label: p.name || '未命名'
    }))
  }
})
</script>

<style scoped>
.dims {
  margin-bottom: 12px;
}

.dim {
  margin-bottom: 12px;
}

.dim-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.dim-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.dim-empty {
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}

.dim-hint {
  margin-top: 4px;
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}

.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.chip {
  padding: 3px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-full);
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.chip.on {
  border-color: var(--color-primary);
  color: var(--color-primary);
  background-color: var(--color-primary-active-bg);
}

.opts {
  margin-top: 6px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  padding: 8px;
}

.opt-q {
  margin-bottom: 6px;
}

.opt-list {
  max-height: 160px;
  overflow-y: auto;
}

.opt {
  display: flex;
  align-items: center;
  width: 100%;
  padding: 5px 8px;
  border: none;
  font-size: var(--font-size-sm);
  text-align: left;
  color: var(--color-text-primary);
  background-color: transparent;
  cursor: pointer;
  border-radius: var(--radius-sm);
}

.opt:hover {
  background-color: var(--color-surface-hover);
}

.opt.on {
  color: var(--color-primary);
  font-weight: 600;
}

.cnt {
  margin-left: auto;
  padding-left: 10px;
  color: var(--color-text-disabled);
}

.opt-none {
  padding: 8px;
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}
</style>
