<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">网络挂载</h2>
      <div class="head-actions">
        <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="storage-reload" @click="load">
          {{ loading ? '加载中…' : '刷新' }}
        </button>
        <button class="btn btn--primary" type="button" data-testid="storage-create" @click="openDialog('create')">
          新建挂载
        </button>
      </div>
    </div>

    <p class="hint">
      挂载远程存储（WebDAV / SMB / NFS）并增量导入媒体库：worker 侧执行器自动对账，
      导入文件落在媒体库 <code>_imports/&lt;挂载ID前8位&gt;/</code> 前缀下，哈希去重可反复同步。
      凭据经服务端 AES-256-GCM 加密存储，界面不回显。
    </p>

    <p v-if="err" class="msg msg--error" data-testid="storage-error">{{ err }}</p>
    <p v-if="msg" class="msg msg--ok" data-testid="storage-msg">{{ msg }}</p>
    <p v-if="forbidden" class="msg msg--error">当前账号缺少管理权限，无法管理挂载。</p>

    <p v-if="!loading && !err && mounts.length === 0" class="muted-empty" data-testid="storage-empty">
      暂无挂载
    </p>

    <div v-for="m in mounts" :key="m.id" class="mount-row" data-testid="storage-row">
      <div class="mount-main">
        <div class="mount-line">
          <span class="mount-name">{{ m.name }}</span>
          <span class="badge" :class="'badge--' + statusCls(m.status)" data-testid="storage-status">{{ statusText(m.status) }}</span>
          <span class="badge badge--type">{{ typeText(m.type) }}</span>
          <span class="badge" :class="m.visibility === 'shared' ? 'badge--shared' : 'badge--personal'">
            {{ m.visibility === 'shared' ? '共享' : '个人' }}
          </span>
          <span v-if="m.has_creds" class="badge badge--creds">已设凭据</span>
        </div>
        <div class="mount-sub">
          <span class="mount-conn">{{ connSummary(m) }}</span>
          <span v-if="m.mount_path" class="mount-path">挂载点 {{ m.mount_path }}</span>
          <span class="mount-import">媒体库前缀 _imports/{{ m.id.slice(0, 8) }}/</span>
        </div>
        <p v-if="m.last_error" class="mount-error" :title="m.last_error">最近错误：{{ m.last_error }}</p>
        <ul v-if="testResults[m.id]" class="test-result" data-testid="storage-test-result">
          <li v-for="(c, i) in testResults[m.id].checks" :key="i">{{ c }}</li>
          <li :class="testResults[m.id].ok ? 'ok' : 'bad'">
            {{ testResults[m.id].ok ? '检查通过' : '检查未通过' }}
          </li>
        </ul>
      </div>
      <div class="mount-ops">
        <button class="btn btn--mini" type="button" :disabled="testingId === m.id" data-testid="storage-test" @click="runTest(m)">
          {{ testingId === m.id ? '检测中…' : '测试' }}
        </button>
        <button class="btn btn--mini" type="button" data-testid="storage-edit" @click="openDialog('edit', m)">编辑</button>
        <button
          v-if="confirmDeleteId !== m.id"
          class="btn btn--mini btn--danger"
          type="button"
          data-testid="storage-delete"
          @click="confirmDeleteId = m.id"
        >删除</button>
        <template v-else>
          <button class="btn btn--mini btn--danger" type="button" data-testid="storage-confirm-delete" @click="remove(m)">
            确认删除
          </button>
          <button class="btn btn--mini" type="button" @click="confirmDeleteId = ''">取消</button>
        </template>
      </div>
    </div>

    <!-- 新建 / 编辑对话框 -->
    <div v-if="dialogMode" class="dlg-mask" @click.self="closeDialog">
      <div class="dlg" role="dialog" aria-label="挂载编辑" data-testid="storage-dialog">
        <h3 class="dlg-title">{{ dialogMode === 'create' ? '新建挂载' : '编辑挂载' }}</h3>

        <label class="field">
          <span class="field-label">名称</span>
          <input v-model.trim="form.name" class="input" type="text" maxlength="120" data-testid="storage-dlg-name" />
        </label>

        <label class="field">
          <span class="field-label">协议类型</span>
          <select v-model="form.type" class="input" :disabled="dialogMode === 'edit'" data-testid="storage-dlg-type">
            <option value="webdav">WebDAV</option>
            <option value="smb">SMB</option>
            <option value="nfs">NFS</option>
          </select>
          <span v-if="dialogMode === 'edit'" class="hint-inline">类型创建后不可更改（连接配置结构不同）</span>
        </label>

        <template v-if="form.type === 'webdav'">
          <label class="field">
            <span class="field-label">服务器 URL</span>
            <input v-model.trim="form.url" class="input" type="text" placeholder="https://example.com/dav" data-testid="storage-dlg-url" />
          </label>
        </template>
        <template v-else-if="form.type === 'smb'">
          <label class="field">
            <span class="field-label">主机</span>
            <input v-model.trim="form.host" class="input" type="text" placeholder="192.168.1.10" data-testid="storage-dlg-host" />
          </label>
          <label class="field">
            <span class="field-label">共享名</span>
            <input v-model.trim="form.share" class="input" type="text" placeholder="photos" data-testid="storage-dlg-share" />
          </label>
          <label class="field">
            <span class="field-label">端口（可选，默认 445）</span>
            <input v-model.number="form.port" class="input" type="number" min="0" max="65535" data-testid="storage-dlg-port" />
          </label>
        </template>
        <template v-else>
          <label class="field">
            <span class="field-label">主机</span>
            <input v-model.trim="form.host" class="input" type="text" placeholder="192.168.1.10" data-testid="storage-dlg-host" />
          </label>
          <label class="field">
            <span class="field-label">导出路径</span>
            <input v-model.trim="form.export" class="input" type="text" placeholder="/srv/photos" data-testid="storage-dlg-export" />
          </label>
        </template>

        <template v-if="form.type !== 'nfs'">
          <label class="field">
            <span class="field-label">用户名（可选，匿名留空）</span>
            <input v-model="form.credsUser" class="input" type="text" :placeholder="userPlaceholder" data-testid="storage-dlg-user" autocomplete="off" />
          </label>
          <label class="field">
            <span class="field-label">密码</span>
            <input v-model="form.credsPass" class="input" type="password" :placeholder="passPlaceholder" data-testid="storage-dlg-pass" autocomplete="new-password" />
          </label>
          <label v-if="form.type === 'smb'" class="field">
            <span class="field-label">域（可选）</span>
            <input v-model="form.credsDomain" class="input" type="text" data-testid="storage-dlg-domain" autocomplete="off" />
          </label>
        </template>

        <label class="field">
          <span class="field-label">可见性</span>
          <select v-model="form.visibility" class="input" data-testid="storage-dlg-visibility">
            <option value="personal">个人（仅管理员与自己）</option>
            <option value="shared">共享（全部用户可用）</option>
          </select>
        </label>

        <p v-if="dialogMode === 'edit' && editing?.has_creds" class="hint">
          该挂载已设凭据：密码留 <code>********</code> 不变；输入新值整体覆盖；三项全空则清除凭据。
        </p>
        <p v-if="dialogError" class="msg msg--error" data-testid="storage-dlg-error">{{ dialogError }}</p>

        <div class="dlg-actions">
          <button class="btn" type="button" @click="closeDialog">取消</button>
          <button class="btn btn--primary" type="button" :disabled="submitting" data-testid="storage-submit" @click="submit">
            {{ submitting ? '保存中…' : dialogMode === 'create' ? '创建' : '保存' }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { createMount, deleteMount, listMounts, patchMount, testMount } from '../../api/storage'

// 网络挂载管理（Job000074，F4 配套 UI）：CRUD + 测试 + 状态角标。
// 状态机取值见 cmd/storagectl：''=未挂载 / online=在线 / error=错误
//（v2 探针落地后增补 offline/degraded，前端先行识别）。
const mounts = ref([])
const loading = ref(false)
const err = ref('')
const msg = ref('')
const forbidden = ref(false)

const testingId = ref('')
const testResults = ref({})
const confirmDeleteId = ref('')

const dialogMode = ref('') // '' | 'create' | 'edit'
const editing = ref(null)
const submitting = ref(false)
const dialogError = ref('')
const form = ref(emptyForm())

let pollTimer = null

function emptyForm() {
  return {
    name: '',
    type: 'webdav',
    url: '',
    host: '',
    share: '',
    export: '',
    port: null,
    credsUser: '',
    credsPass: '',
    credsDomain: '',
    visibility: 'personal'
  }
}

const credMask = '********'
const userPlaceholder = computed(() =>
  dialogMode.value === 'edit' && editing.value?.has_creds ? '已设置（留空不变）' : '可选'
)
const passPlaceholder = computed(() =>
  dialogMode.value === 'edit' && editing.value?.has_creds ? credMask + '（不改动）' : '可选'
)

function statusText(s) {
  return { online: '在线', error: '错误', offline: '离线', degraded: '降级' }[s] || '未挂载'
}

function statusCls(s) {
  return { online: 'ok', error: 'err', offline: 'muted', degraded: 'warn' }[s] || 'muted'
}

function typeText(t) {
  return { webdav: 'WebDAV', smb: 'SMB', nfs: 'NFS' }[t] || t
}

function connSummary(m) {
  const c = m.conn || {}
  if (m.type === 'webdav') return c.url || '—'
  if (m.type === 'smb') return (c.host || '—') + ' / ' + (c.share || '—') + (c.port ? ' :' + c.port : '')
  return (c.host || '—') + ' :' + (c.export || '—')
}

async function load(quiet = false) {
  if (!quiet) loading.value = true
  err.value = ''
  try {
    mounts.value = await listMounts()
  } catch (e) {
    if (e?.response?.status === 403) {
      forbidden.value = true
    } else {
      err.value = errMessage(e, '加载挂载列表失败')
    }
  } finally {
    loading.value = false
  }
}

async function runTest(m) {
  testingId.value = m.id
  msg.value = ''
  try {
    testResults.value = { ...testResults.value, [m.id]: await testMount(m.id) }
  } catch (e) {
    testResults.value = {
      ...testResults.value,
      [m.id]: { ok: false, checks: ['请求失败：' + errMessage(e, '网络错误')] }
    }
  } finally {
    testingId.value = ''
  }
}

function openDialog(mode, m) {
  dialogMode.value = mode
  dialogError.value = ''
  editing.value = mode === 'edit' ? m : null
  if (mode === 'edit') {
    const c = m.conn || {}
    form.value = {
      name: m.name,
      type: m.type,
      url: c.url || '',
      host: c.host || '',
      share: c.share || '',
      export: c.export || '',
      port: c.port || null,
      credsUser: '',
      credsPass: '',
      credsDomain: '',
      visibility: m.visibility || 'personal'
    }
  } else {
    form.value = emptyForm()
  }
}

function closeDialog() {
  dialogMode.value = ''
  editing.value = null
  dialogError.value = ''
}

function buildConn() {
  const f = form.value
  if (f.type === 'webdav') return { url: f.url }
  if (f.type === 'smb') {
    const c = { host: f.host, share: f.share }
    if (f.port) c.port = f.port
    return c
  }
  return { host: f.host, export: f.export }
}

async function submit() {
  if (submitting.value) return
  submitting.value = true
  dialogError.value = ''
  const f = form.value
  try {
    if (dialogMode.value === 'create') {
      const payload = { name: f.name, type: f.type, conn: buildConn(), visibility: f.visibility }
      if (f.type !== 'nfs') {
        payload.creds_user = f.credsUser
        payload.creds_pass = f.credsPass
        payload.creds_domain = f.credsDomain
      }
      await createMount(payload)
      msg.value = '已创建，worker 执行器将在下一个对账周期（约 10 秒）自动挂载'
    } else {
      const patch = { name: f.name, conn: buildConn(), visibility: f.visibility }
      if (f.type !== 'nfs') {
        const any = f.credsUser || f.credsPass || f.credsDomain
        if (any) {
          patch.creds_user = f.credsUser
          patch.creds_pass = f.credsPass
          patch.creds_domain = f.credsDomain
        } else {
          patch.creds_user = ''
          patch.creds_pass = editing.value?.has_creds ? credMask : ''
          patch.creds_domain = ''
        }
      }
      await patchMount(editing.value.id, patch)
      msg.value = '已保存'
    }
    closeDialog()
    await load(true)
  } catch (e) {
    dialogError.value = errMessage(e, '保存失败')
  } finally {
    submitting.value = false
  }
}

async function remove(m) {
  msg.value = ''
  err.value = ''
  try {
    await deleteMount(m.id)
    msg.value = '已删除，挂载卸载与媒体清理由执行器对账完成'
    await load(true)
  } catch (e) {
    err.value = errMessage(e, '删除失败')
  } finally {
    confirmDeleteId.value = ''
  }
}

onMounted(() => {
  load()
  // 状态由 worker 执行器每 10s 对账改写，列表轻量轮询跟随（避开共享限流桶的 15s 节拍）
  pollTimer = setInterval(() => {
    if (!document.hidden && !dialogMode.value) load(true)
  }, 15000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style scoped>
.card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 20px;
}
.card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.card-title {
  margin: 0;
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}
.head-actions {
  display: flex;
  gap: 8px;
}
.hint {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin: 0 0 14px;
}
.hint code {
  background: var(--color-surface-hover);
  padding: 1px 6px;
  border-radius: var(--radius-sm);
}
.hint-inline {
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
  margin-top: 4px;
}
.msg {
  margin: 0 0 8px;
  font-size: var(--font-size-md);
}
.msg--error {
  color: var(--color-danger);
}
.msg--ok {
  color: var(--color-success);
}
.muted-empty {
  padding: 32px 0;
  text-align: center;
  color: var(--color-text-secondary);
  font-size: var(--font-size-md);
}

.mount-row {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  justify-content: space-between;
  padding: 12px 0;
  border-bottom: 1px solid var(--color-border);
}
.mount-row:last-of-type {
  border-bottom: none;
}
.mount-main {
  flex: 1;
  min-width: 0;
}
.mount-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.mount-name {
  font-size: var(--font-size-md);
  font-weight: 600;
  color: var(--color-text-primary);
}
.mount-sub {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 4px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.mount-conn,
.mount-path,
.mount-import {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 420px;
}
.mount-error {
  margin: 4px 0 0;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.test-result {
  margin: 6px 0 0;
  padding-left: 18px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.test-result .ok {
  color: var(--color-success);
}
.test-result .bad {
  color: var(--color-danger);
}
.mount-ops {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

.badge {
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm);
  border: 1px solid var(--color-border);
  color: var(--color-text-secondary);
}
.badge--ok {
  color: var(--color-success);
  border-color: var(--color-success);
}
.badge--err {
  color: var(--color-danger);
  border-color: var(--color-danger);
}
.badge--warn {
  color: var(--color-warning-text);
  border-color: var(--color-warning-text);
}
.badge--type {
  color: var(--color-text-primary);
}
.badge--shared {
  color: var(--color-primary);
  border-color: var(--color-primary);
}

.btn {
  padding: 6px 14px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
}
.btn:hover {
  background: var(--color-surface-hover);
}
.btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.btn--primary {
  background: var(--color-primary);
  border-color: var(--color-primary);
  color: var(--color-surface);
}
.btn--primary:hover {
  background: var(--color-primary-hover);
}
.btn--ghost {
  border-color: transparent;
  color: var(--color-primary);
}
.btn--mini {
  padding: 3px 10px;
  font-size: var(--font-size-sm);
}
.btn--danger {
  border-color: var(--color-danger);
  color: var(--color-danger);
}
.btn--danger:hover {
  background: var(--color-danger);
  color: #fff;
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
.dlg {
  width: 460px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  overflow-y: auto;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 20px;
}
.dlg-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  margin: 0 0 16px;
}
.field {
  display: block;
  margin-bottom: 12px;
}
.field-label {
  display: block;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin-bottom: 4px;
}
.input {
  width: 100%;
  box-sizing: border-box;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  font-family: var(--font-family);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
}
.input:disabled {
  color: var(--color-text-disabled);
  background-color: var(--color-surface-hover);
}
.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}
</style>
