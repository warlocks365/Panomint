<template>
  <div class="share-manage">
    <p v-if="loading" class="tip">加载中…</p>
    <p v-else-if="loadError" class="tip error">
      {{ loadError }}
      <button class="link-btn" @click="refresh">重试</button>
    </p>
    <p v-else-if="!shares.length" class="tip">还没有分享记录</p>

    <table v-else class="share-table">
      <thead>
        <tr>
          <th>标题</th>
          <th>类型</th>
          <th>访问次数</th>
          <th>有效期</th>
          <th>状态</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="s in shares" :key="s.id">
          <td class="title-cell" :title="s.title">{{ s.title || '未命名分享' }}</td>
          <td>{{ kindLabel(s.kind) }}</td>
          <td>{{ s.access_count ?? s.view_count ?? 0 }}</td>
          <td>{{ expireLabel(s) }}</td>
          <td>
            <span class="status-tag" :class="statusOf(s).cls">{{ statusOf(s).text }}</span>
          </td>
          <td class="op-cell">
            <button class="link-btn" @click="copyLink(s)">复制链接</button>
            <button
              class="link-btn danger"
              :disabled="statusOf(s).key !== 'active' || revokingId === s.id"
              @click="confirmTarget = s"
            >
              {{ revokingId === s.id ? '吊销中…' : '吊销' }}
            </button>
          </td>
        </tr>
      </tbody>
    </table>

    <p v-if="copyTip" class="copy-tip">{{ copyTip }}</p>

    <!-- 吊销确认 -->
    <div v-if="confirmTarget" class="dlg-mask" @click.self="confirmTarget = null">
      <div class="confirm-dlg" role="alertdialog">
        <h3 class="confirm-title">吊销分享</h3>
        <p class="confirm-text">吊销后链接「{{ confirmTarget.title || '未命名分享' }}」将立即失效，确定继续吗？</p>
        <p v-if="revokeError" class="confirm-error">{{ revokeError }}</p>
        <div class="dlg-actions">
          <button class="btn" @click="confirmTarget = null">取消</button>
          <button class="btn danger" :disabled="revokingId" @click="doRevoke">
            {{ revokingId ? '吊销中…' : '吊销' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { errMsg, listShares, revokeShare, shareLink } from './shareApi'

const shares = ref([])
const loading = ref(false)
const loadError = ref('')
const confirmTarget = ref(null)
const revokingId = ref('')
const revokeError = ref('')
const copyTip = ref('')

let copyTipTimer = null

function kindLabel(kind) {
  return kind === 'album' ? '相册' : kind === 'media' ? '媒体' : kind || '-'
}

function expireLabel(s) {
  if (!s.expire_at) return '永久'
  return new Date(s.expire_at).toLocaleDateString('zh-CN')
}

function statusOf(s) {
  if (s.revoked || s.revoked_at || s.status === 'revoked') return { key: 'revoked', text: '已吊销', cls: 'revoked' }
  if (s.status === 'expired' || (s.expire_at && new Date(s.expire_at).getTime() < Date.now())) {
    return { key: 'expired', text: '已过期', cls: 'expired' }
  }
  return { key: 'active', text: '有效', cls: 'active' }
}

async function refresh() {
  loading.value = true
  loadError.value = ''
  try {
    shares.value = await listShares()
  } catch (e) {
    loadError.value = errMsg(e, '分享列表加载失败')
  } finally {
    loading.value = false
  }
}

async function copyLink(s) {
  const link = shareLink(s.token)
  try {
    await navigator.clipboard.writeText(link)
  } catch (e) {
    const ta = document.createElement('textarea')
    ta.value = link
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
  copyTip.value = '链接已复制'
  clearTimeout(copyTipTimer)
  copyTipTimer = setTimeout(() => (copyTip.value = ''), 2000)
}

async function doRevoke() {
  if (!confirmTarget.value || revokingId.value) return
  revokingId.value = confirmTarget.value.id
  revokeError.value = ''
  try {
    await revokeShare(confirmTarget.value.id)
    confirmTarget.value = null
    await refresh()
  } catch (e) {
    revokeError.value = errMsg(e, '吊销失败')
  } finally {
    revokingId.value = ''
  }
}

onMounted(refresh)
defineExpose({ refresh })
</script>

<style scoped>
.share-manage {
  min-width: 0;
}

.tip {
  padding: 24px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.tip.error {
  color: var(--color-danger);
}

.share-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
}

.share-table th,
.share-table td {
  padding: 8px 10px;
  text-align: left;
  border-bottom: 1px solid var(--color-border);
  white-space: nowrap;
}

.share-table th {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  font-weight: normal;
}

.title-cell {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.status-tag {
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  font-size: 11px;
}

.status-tag.active {
  color: var(--color-success);
  background-color: #e8f7ee;
}

.status-tag.expired {
  color: var(--color-warning-text);
  background-color: var(--color-warning-bg);
}

.status-tag.revoked {
  color: var(--color-text-disabled);
  background-color: var(--color-surface-hover);
}

.op-cell {
  display: flex;
  gap: 10px;
}

.link-btn {
  border: none;
  background: none;
  padding: 0;
  font-size: var(--font-size-sm);
  color: var(--color-primary);
  cursor: pointer;
}

.link-btn.danger {
  color: var(--color-danger);
}

.link-btn:disabled {
  color: var(--color-text-disabled);
  cursor: not-allowed;
}

.copy-tip {
  margin-top: 8px;
  font-size: var(--font-size-sm);
  color: var(--color-success);
}

.dlg-mask {
  position: fixed;
  inset: 0;
  z-index: 110;
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

.btn {
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
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
</style>
