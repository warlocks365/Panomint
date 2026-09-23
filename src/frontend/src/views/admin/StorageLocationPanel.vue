<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">存储位置</h2>
      <div class="head-actions">
        <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="loc-reload" @click="load()">
          {{ loading ? '加载中…' : '刷新' }}
        </button>
        <button class="btn btn--primary" type="button" data-testid="loc-create" @click="openCreate">新建位置</button>
      </div>
    </div>

    <p class="hint">
      位置 = 命名物理存储根，对应部署时容器内映射的目录名（如 <code>archive-a</code>）。
      名称创建后不可修改；未归属位置的媒体留在默认存储根。仅系统管理员可管理。
    </p>

    <p v-if="err" class="msg msg--error" data-testid="loc-error">{{ err }}</p>
    <p v-if="msg" class="msg msg--ok" data-testid="loc-msg">{{ msg }}</p>
    <p v-if="forbidden" class="msg msg--error">当前账号缺少管理权限，无法管理存储位置。</p>

    <p v-if="!loading && !err && locations.length === 0" class="muted-empty" data-testid="loc-empty">
      暂无存储位置
    </p>

    <div v-for="loc in locations" :key="loc.id" class="loc-row" data-testid="loc-row">
      <div class="loc-main">
        <div class="loc-line">
          <span class="loc-name" data-testid="loc-name">{{ loc.name }}</span>
          <span class="badge" :class="loc.media_count > 0 ? 'badge--used' : 'badge--free'" data-testid="loc-count">
            {{ loc.media_count > 0 ? loc.media_count + ' 项媒体' : '空闲' }}
          </span>
        </div>
        <div class="loc-sub">
          <span v-if="loc.description">{{ loc.description }}</span>
          <span class="loc-date">创建于 {{ formatTime(loc.created_at) }}</span>
        </div>
      </div>
      <div class="loc-ops">
        <button class="btn btn--mini" type="button" data-testid="loc-edit" @click="openEdit(loc)">编辑描述</button>
        <button class="btn btn--mini btn--danger" type="button" data-testid="loc-delete" @click="remove(loc)">
          删除
        </button>
      </div>
    </div>
  </section>
</template>

<script setup>
// Job000103 存储位置管理面板：自持加载/新建/改描述/删除全流程。
// 表单与确认走统一 dialogs.js（Promise 化，可 e2e 机器断言）；名称创建后不可改（后端 NAME_IMMUTABLE）。
// 删除前置客户端闸：media_count>0 直接提示须先迁移（后端 409 LOCATION_IN_USE 为双保险）。
import { onMounted, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { dialogs } from '../../components/dialogs/dialogs'
import { createLocation, deleteLocation, listLocations, patchLocation } from '../../api/storage'

const NAME_RE = /^[a-z][a-z0-9-]{0,31}$/
const NAME_RULE = '小写字母开头，仅含小写字母/数字/连字符，最长 32 字符'

const locations = ref([])
const loading = ref(false)
const err = ref('')
const msg = ref('')
const forbidden = ref(false)

async function load(quiet = false) {
  if (!quiet) loading.value = true
  err.value = ''
  try {
    locations.value = await listLocations()
  } catch (e) {
    if (e?.response?.status === 403) forbidden.value = true
    else err.value = errMessage(e, '加载存储位置失败')
  } finally {
    loading.value = false
  }
}

function validateName(v) {
  if (!v) return '名称不能为空'
  if (!NAME_RE.test(v)) return NAME_RULE
  return ''
}

async function openCreate() {
  msg.value = ''
  const values = await dialogs.form({
    title: '新建存储位置',
    fields: [
      { key: 'name', label: '名称', placeholder: '如 archive-a', validate: validateName },
      { key: 'description', label: '描述（可选）', placeholder: '用途/物理位置备注' }
    ]
  })
  if (!values) return
  try {
    await createLocation({ name: values.name, description: values.description || '' })
    msg.value = '已创建存储位置 ' + values.name
    await load(true)
  } catch (e) {
    err.value = errMessage(e, '创建失败')
  }
}

async function openEdit(loc) {
  msg.value = ''
  const values = await dialogs.form({
    title: '编辑描述 — ' + loc.name,
    fields: [{ key: 'description', label: '描述', initial: loc.description || '' }]
  })
  if (!values) return
  try {
    await patchLocation(loc.id, { description: values.description || '' })
    msg.value = '已更新 ' + loc.name
    await load(true)
  } catch (e) {
    err.value = errMessage(e, '更新失败')
  }
}

async function remove(loc) {
  msg.value = ''
  err.value = ''
  if (loc.media_count > 0) {
    await dialogs.alert({
      title: '无法删除',
      text: `「${loc.name}」仍有 ${loc.media_count} 项媒体引用，须先将媒体迁回默认存储根。`
    })
    return
  }
  const ok = await dialogs.confirm({
    title: '删除存储位置',
    text: `确定删除「${loc.name}」？删除后媒体将归属默认存储根。`,
    confirmText: '删除',
    danger: true
  })
  if (!ok) return
  try {
    await deleteLocation(loc.id)
    msg.value = '已删除 ' + loc.name
    await load(true)
  } catch (e) {
    err.value = errMessage(e, '删除失败')
  }
}

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return t
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

onMounted(() => load())
</script>

<style scoped>
.card { background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-md); padding: 20px; }
.card-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.card-title { margin: 0; font-size: var(--font-size-lg); color: var(--color-text-primary); }
.head-actions { display: flex; gap: 8px; }
.hint { font-size: var(--font-size-sm); color: var(--color-text-secondary); margin: 0 0 14px; }
.hint code { background: var(--color-surface-hover); padding: 1px 6px; border-radius: var(--radius-sm); }
.msg { margin: 0 0 8px; font-size: var(--font-size-md); }
.msg--error { color: var(--color-danger); }
.msg--ok { color: var(--color-success); }
.muted-empty { padding: 32px 0; text-align: center; color: var(--color-text-secondary); font-size: var(--font-size-md); }

.loc-row { display: flex; gap: 12px; align-items: flex-start; justify-content: space-between; padding: 12px 0; border-bottom: 1px solid var(--color-border); }
.loc-row:last-of-type { border-bottom: none; }
.loc-main { flex: 1; min-width: 0; }
.loc-line { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.loc-name { font-size: var(--font-size-md); font-weight: 600; color: var(--color-text-primary); font-family: monospace; }
.loc-sub { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 4px; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.loc-ops { display: flex; gap: 6px; flex-shrink: 0; }

.badge { padding: 2px 8px; border-radius: var(--radius-sm); font-size: var(--font-size-sm); border: 1px solid var(--color-border); color: var(--color-text-secondary); }
.badge--used { color: var(--color-primary); border-color: var(--color-primary); }
.badge--free { color: var(--color-success); border-color: var(--color-success); }

.btn { padding: 6px 14px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); background: var(--color-surface); color: var(--color-text-primary); font-size: var(--font-size-md); cursor: pointer; }
.btn:hover { background: var(--color-surface-hover); }
.btn:disabled { opacity: 0.6; cursor: default; }
.btn--primary { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-surface); }
.btn--primary:hover { background: var(--color-primary-hover); }
.btn--ghost { border-color: transparent; color: var(--color-primary); }
.btn--mini { padding: 3px 10px; font-size: var(--font-size-sm); }
.btn--danger { border-color: var(--color-danger); color: var(--color-danger); }
.btn--danger:hover { background: var(--color-danger); color: #fff; }
</style>
