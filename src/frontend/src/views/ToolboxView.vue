<template>
  <div class="toolbox-page">
    <header class="tb-head">
      <h2 class="tb-title">工具箱</h2>
      <p class="tb-sub">查找重复媒体、找回误删内容</p>
    </header>

    <div class="tb-tabs" data-testid="toolbox-tabs" role="tablist">
      <button
        class="tb-tab"
        :class="{ active: tab === 'dup' }"
        data-testid="toolbox-tab-dup"
        @click="tab = 'dup'"
      >
        重复项目
      </button>
      <button
        class="tb-tab"
        :class="{ active: tab === 'trash' }"
        data-testid="toolbox-tab-trash"
        @click="tab = 'trash'"
      >
        最近删除
      </button>
      <button
        class="tb-tab"
        :class="{ active: tab === 'restored' }"
        data-testid="toolbox-tab-restored"
        @click="tab = 'restored'"
      >
        已恢复
      </button>
    </div>

    <!-- 重复项目：本页主体 -->
    <section v-if="tab === 'dup'" class="tb-panel">
      <p class="tb-note">
        分组依据是感知哈希（pHash），<strong>只是建议</strong>：视频只取首帧参与比对，
        同一场景的 360 照片/视频也会被判为重复。请逐组确认后再删除，
        <strong>本页不会自动删除任何内容</strong>。
      </p>

      <p v-if="notice" class="tb-notice" data-testid="toolbox-notice">{{ notice }}</p>

      <p v-if="loading" class="tb-tip">正在比对，媒体较多时需要一点时间…</p>

      <div v-else-if="error" class="tb-tip error" data-testid="toolbox-error">
        {{ error }}
        <button class="retry-btn" @click="load">重试</button>
      </div>

      <div v-else-if="!groups.length" class="tb-empty" data-testid="toolbox-empty">
        <p class="empty-title">未发现重复项</p>
        <p class="empty-desc">
          已比对 {{ scanned }} 项媒体的感知哈希（阈值 {{ threshold }}），没有达到重复判定的分组。
        </p>
      </div>

      <template v-else>
        <p class="tb-summary">
          已比对 {{ scanned }} 项，命中 {{ total }} 组疑似重复
          <span v-if="truncated">（只显示前 {{ groups.length }} 组）</span>
        </p>

        <div class="tb-groups" data-testid="toolbox-dup-list">
          <article v-for="(g, gi) in groups" :key="g.key" class="dup-group" data-testid="toolbox-group">
            <header class="dup-head">
              <span class="dup-index">第 {{ gi + 1 }} 组</span>
              <span class="dup-count">{{ g.items.length }} 项</span>
              <span class="dup-phash" :title="`感知哈希 ${g.phash}`">{{ g.phash }}</span>
              <button
                class="btn danger sm"
                data-testid="toolbox-delete-rest"
                :disabled="deleting"
                @click="askDelete(g)"
              >
                删除其余 {{ g.items.length - 1 }} 项
              </button>
            </header>

            <p class="dup-hint">
              建议保留「{{ keepOf(g).filename }}」，其余 {{ g.items.length - 1 }} 项可删除。
              想换一张保留？点缩略图即可改选。
            </p>

            <div class="dup-grid">
              <div
                v-for="it in g.items"
                :key="it.id"
                class="dup-item"
                :class="{ keep: it.id === g.keepId }"
                data-testid="toolbox-item"
                @click="setKeep(g, it)"
              >
                <span v-if="it.id === g.keepId" class="dup-keep-badge" data-testid="toolbox-keep-badge">
                  建议保留
                </span>
                <MediaThumb :item="it" size="md" />
                <p class="dup-name" :title="it.filename">{{ it.filename }}</p>
                <p class="dup-meta">{{ itemMeta(it) }}</p>
              </div>
            </div>
          </article>
        </div>
      </template>
    </section>

    <!-- 最近删除 / 已恢复：只做引导，回收站本体唯一实现在时间轴面板里 -->
    <section v-else-if="tab === 'trash'" class="tb-panel tb-guide">
      <h3 class="guide-title">最近删除</h3>
      <p class="guide-text">
        删除是软删：媒体会先进入回收站，随时可以恢复。回收站的列表与恢复操作统一在
        时间轴的「回收站」面板里，这里只提供入口 —— 同一套逻辑放两处实现，迟早会各改各的。
      </p>
      <button class="btn primary" @click="goTrash">打开时间轴回收站</button>
    </section>

    <section v-else class="tb-panel tb-guide">
      <h3 class="guide-title">已恢复</h3>
      <p class="guide-text">
        恢复同样在回收站面板里完成（点条目右侧的「恢复」）。恢复后的媒体会按原拍摄时间回到
        时间轴，不会单独构成一张表 —— 服务端没有「恢复历史」查询接口，这里不编造这份数据。
      </p>
      <button class="btn" @click="goTrash">打开时间轴回收站</button>
    </section>

    <div v-if="confirmGroup" class="dlg-mask" @click.self="cancelDelete">
      <div class="confirm-dlg" role="alertdialog">
        <h3 class="confirm-title">删除重复项</h3>
        <p class="confirm-text">
          将删除本组 {{ confirmGroup.items.length - 1 }} 项，保留「{{ keepOf(confirmGroup).filename }}」。
          删除为软删，会进入回收站，之后仍可恢复。
        </p>
        <p v-if="deleteError" class="confirm-error">{{ deleteError }}</p>
        <div class="dlg-actions">
          <button class="btn" data-testid="toolbox-confirm-cancel" :disabled="deleting" @click="cancelDelete">
            取消
          </button>
          <button
            class="btn danger"
            data-testid="toolbox-confirm-ok"
            :disabled="deleting"
            @click="confirmDelete"
          >
            {{ deleting ? '删除中…' : '确认删除' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import MediaThumb from '../components/albums/MediaThumb.vue'
import { deleteMedia, getDuplicates } from '../api/media'

const router = useRouter()

const tab = ref('dup')

const groups = ref([])
const loading = ref(false)
const error = ref('')
const scanned = ref(0)
const threshold = ref(10)
const total = ref(0)
const truncated = ref(false)
const notice = ref('')

const confirmGroup = ref(null)
const deleting = ref(false)
const deleteError = ref('')

function errMsg(e, fallback) {
  return e.response?.data?.error?.message || fallback
}

async function load() {
  loading.value = true
  error.value = ''
  notice.value = ''
  try {
    const data = await getDuplicates()
    threshold.value = data?.threshold ?? 10
    scanned.value = data?.scanned ?? 0
    total.value = data?.total ?? 0
    truncated.value = !!data?.truncated
    // keepId 取出到本地状态：用户可改选，改选只影响本页的建议，不回写后端
    groups.value = (Array.isArray(data?.groups) ? data.groups : []).map((g) => ({
      key: `${g.keep_id}:${g.phash || ''}`,
      phash: g.phash || '',
      items: Array.isArray(g.items) ? g.items.slice() : [],
      keepId: g.keep_id || g.items?.[0]?.id || ''
    }))
  } catch (e) {
    // TOO_MANY_MEDIA 的 message 自带真实规模（后端刻意带回），原样透出比换成"失败"更有用
    error.value = errMsg(e, '重复项检测失败')
    groups.value = []
  } finally {
    loading.value = false
  }
}

function keepOf(g) {
  return g.items.find((it) => it.id === g.keepId) || g.items[0] || {}
}

function setKeep(g, it) {
  g.keepId = it.id
}

function itemMeta(it) {
  const parts = []
  if (it.width && it.height) parts.push(`${it.width}×${it.height}`)
  parts.push(it.type === 'video' ? '视频' : '照片')
  if (it.taken_at) parts.push(String(it.taken_at).slice(0, 10))
  return parts.join(' · ')
}

function askDelete(g) {
  deleteError.value = ''
  confirmGroup.value = g
}

function cancelDelete() {
  if (deleting.value) return
  confirmGroup.value = null
}

async function confirmDelete() {
  const g = confirmGroup.value
  if (!g || deleting.value) return
  deleting.value = true
  deleteError.value = ''
  const targets = g.items.filter((it) => it.id !== g.keepId)
  const done = []
  try {
    for (const it of targets) {
      await deleteMedia(it.id)
      done.push(it.id)
    }
    groups.value = groups.value.filter((x) => x !== g)
    confirmGroup.value = null
    notice.value = `已将 ${done.length} 项移入回收站，可在时间轴回收站中恢复。`
  } catch (e) {
    deleteError.value = errMsg(e, '删除失败，请稍后重试')
    // 逐条软删不是事务：把已删掉的先从组里摘掉，避免用户重试时又点一次已删条目
    if (done.length) {
      g.items = g.items.filter((it) => !done.includes(it.id))
      if (g.items.length < 2) {
        groups.value = groups.value.filter((x) => x !== g)
        confirmGroup.value = null
        notice.value = `已将 ${done.length} 项移入回收站，可在时间轴回收站中恢复。`
      }
    }
  } finally {
    deleting.value = false
  }
}

function goTrash() {
  router.push({ path: '/timeline', query: { trash: '1' } })
}

onMounted(load)
</script>

<style scoped>
.toolbox-page {
  padding: 20px 24px;
}

.tb-head {
  margin-bottom: 14px;
}

.tb-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.tb-sub {
  margin-top: 4px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.tb-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--color-border);
  margin-bottom: 16px;
}

.tb-tab {
  border: none;
  background: transparent;
  padding: 9px 14px;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
}

.tb-tab:hover {
  color: var(--color-text-primary);
}

.tb-tab.active {
  color: var(--color-primary);
  border-bottom-color: var(--color-primary);
}

.tb-note {
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background-color: var(--color-surface-hover);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.7;
}

.tb-notice {
  margin-top: 12px;
  padding: 8px 12px;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
  font-size: var(--font-size-sm);
}

.tb-summary {
  margin: 14px 0 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.tb-tip {
  padding: 40px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.tb-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
}

.tb-empty {
  padding: 64px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  text-align: center;
}

.empty-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.empty-desc {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.tb-groups {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.dup-group {
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background-color: var(--color-surface);
  padding: 12px 14px 14px;
}

.dup-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.dup-index {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  font-weight: 600;
}

.dup-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.dup-phash {
  font-size: 11px;
  color: var(--color-text-disabled);
  font-family: monospace;
}

.dup-head .btn {
  margin-left: auto;
}

.dup-hint {
  margin: 8px 0 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.dup-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 10px;
}

.dup-item {
  position: relative;
  padding: 6px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  cursor: pointer;
}

.dup-item:hover {
  border-color: var(--color-border);
}

.dup-item.keep {
  border-color: var(--color-primary);
  background-color: var(--color-primary-active-bg);
}

.dup-keep-badge {
  position: absolute;
  top: 10px;
  left: 10px;
  z-index: 2;
  padding: 1px 6px;
  border-radius: var(--radius-sm);
  font-size: 11px;
  color: #fff;
  background-color: var(--color-primary);
  pointer-events: none;
}

.dup-name {
  margin: 6px 0 2px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.dup-meta {
  margin: 0;
  font-size: 11px;
  color: var(--color-text-secondary);
}

.tb-guide {
  max-width: 620px;
}

.guide-title {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  margin-bottom: 8px;
}

.guide-text {
  font-size: var(--font-size-md);
  line-height: 1.8;
  color: var(--color-text-secondary);
  margin-bottom: 14px;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.btn:hover {
  background-color: var(--color-surface-hover);
}

.btn.sm {
  padding: 4px 10px;
  font-size: var(--font-size-sm);
}

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.btn.danger {
  border-color: var(--color-danger);
  color: var(--color-danger);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
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
  line-height: 1.7;
}

.confirm-error {
  margin-bottom: 12px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
