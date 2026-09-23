<template>
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
      <button class="btn btn--mini" type="button" data-testid="storage-edit" @click="$emit('edit', m)">编辑</button>
      <button
        v-if="confirmDeleteId !== m.id"
        class="btn btn--mini btn--danger"
        type="button"
        data-testid="storage-delete"
        @click="confirmDeleteId = m.id"
      >删除</button>
      <template v-else>
        <button class="btn btn--mini btn--danger" type="button" data-testid="storage-confirm-delete" @click="$emit('delete', m)">
          确认删除
        </button>
        <button class="btn btn--mini" type="button" @click="confirmDeleteId = ''">取消</button>
      </template>
    </div>
  </div>
</template>

<script setup>
// StorageTab 拆解（Job000085）：挂载列表行。
// 行内交互态自持（测试进行中/测试结果/两步确认删除），删除执行上报宿主（宿主握全局消息与刷新）。
import { ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { testMount } from '../../api/storage'

defineProps({
  mounts: { type: Array, required: true }
})
const emit = defineEmits(['edit', 'delete'])

const testingId = ref('')
const testResults = ref({})
const confirmDeleteId = ref('')

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

async function runTest(m) {
  testingId.value = m.id
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
</script>

<style scoped>
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
</style>
