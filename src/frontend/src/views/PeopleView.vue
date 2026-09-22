<template>
  <div class="people-page">
    <header class="page-toolbar">
      <h2 class="page-title">人物</h2>
      <span v-if="!loading && !loadError" class="page-sub">
        {{ named.length }} 位已命名 · {{ unnamed.length }} 个待命名
      </span>
      <div class="spacer"></div>
      <button class="btn" :disabled="scanning" @click="triggerScan">
        {{ scanning ? '已提交…' : '重新扫描人脸' }}
      </button>
    </header>

    <p v-if="loading" class="page-tip">加载中…</p>
    <p v-else-if="loadError" class="page-tip error">
      {{ loadError }}
      <button class="retry-btn" @click="load">重试</button>
    </p>

    <template v-else>
      <p v-if="scanNotice" class="notice">{{ scanNotice }}</p>

      <section v-if="named.length" class="section">
        <h3 class="section-title">
          已命名
          <span v-if="namedSelected.length" class="batch-bar-entity" data-testid="people-batch-bar">
            <span class="pb-count">已选 {{ namedSelected.length }} 项</span>
            <button class="mini" data-testid="pb-hide" @click="batchHide(true)">隐藏</button>
            <button class="mini" data-testid="pb-unhide" @click="batchHide(false)">取消隐藏</button>
            <button class="mini" data-testid="pb-clear" @click="namedSelected = []">清除</button>
          </span>
        </h3>
        <div class="grid">
          <div v-for="p in named" :key="p.id" class="card" :class="{ dimmed: p.hidden }">
            <span
              class="pick"
              :class="{ on: namedSelected.includes(p.id) }"
              data-testid="people-pick"
              @click.stop="toggleNamedPick(p.id)"
            ></span>
            <div class="cover" @click="openPerson(p)">
              <img v-if="covers[p.id]" :src="covers[p.id]" alt="" />
              <div v-else class="cover-empty">{{ initial(p.name) }}</div>
            </div>
            <div class="card-body">
              <div class="card-name" :title="p.name">{{ p.name || '未命名' }}</div>
              <div class="card-meta">
                {{ countOf(p) }} 张照片<span v-if="p.is_pet"> · 宠物</span
                ><span v-if="p.hidden"> · 已隐藏</span>
              </div>
            </div>
            <div class="card-actions">
              <button class="mini" @click="startRename(p)">改名</button>
              <button class="mini" @click="toggleHidden(p)">
                {{ p.hidden ? '取消隐藏' : '隐藏' }}
              </button>
            </div>
          </div>
        </div>
      </section>

      <section class="section">
        <h3 class="section-title">
          未命名聚类
          <span class="hint">勾选多个可合并为同一人</span>
          <button v-if="selected.length" class="mini primary" data-testid="people-merge-open" @click="openMerge">
            合并命名（已选 {{ selected.length }}）
          </button>
        </h3>
        <p v-if="!unnamed.length" class="page-tip">没有待命名的人脸聚类</p>
        <div v-else class="grid">
          <div
            v-for="c in unnamed"
            :key="c.cluster_id"
            class="card cluster-card"
            :class="{ picked: selected.includes(c.cluster_id) }"
            @click="togglePick(c.cluster_id)"
          >
            <span class="pick" :class="{ on: selected.includes(c.cluster_id) }"></span>
            <div class="cover">
              <img v-if="covers[c.cluster_id]" :src="covers[c.cluster_id]" alt="" />
              <div v-else class="cover-empty">?</div>
            </div>
            <div class="card-body">
              <div class="card-name">未命名</div>
              <div class="card-meta">{{ countOf(c) }} 张照片</div>
            </div>
          </div>
        </div>
      </section>
    </template>

    <!-- 合并 / 改名自 Job000081 起走统一对话框宿主（dialogs.form / dialogs.prompt），内联对话框已清 -->
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import http from '../api/http'
import { loadThumbUrl } from '../components/timeline/mediaLoader'
import { dialogs } from '../components/dialogs/dialogs'

// 人物页（API 契约 v1.1 §6 人物）：
//   GET   /people            → { named:[...], unnamed:[{cluster_id,count,cover}] }
//   POST   /people           → 命名并合并：{ cluster_ids:[...], name, is_pet }
//   PATCH  /people/:id       → { name } | { hidden }
//   POST   /ai/faces         → 主动重算：{ scope:"all" }
//
// 说明：人脸缩略图目前复用媒体缩略图端点 GET /media/:id/thumb?size=sm
// （尚无「人脸裁剪图」端点，故聚类封面是包含该脸的那张媒体图的缩略图）。
//
// Job000081 实体级批量：已命名人物多选 → 批量隐藏/取消隐藏（PATCH 循环，逐人独立成败不互阻断）；
// 合并/改名对话框迁入统一宿主（dialogs.form 支持 check 型字段）。named 合并需后端支持（named 不返
// cluster_ids），本轮不做——登记簿已挂未来项。

const router = useRouter()

const named = ref([])
const unnamed = ref([])
const covers = reactive({}) // key(personId|clusterId) -> objectURL
const loading = ref(false)
const loadError = ref('')
const selected = ref([]) // 未命名聚类勾选（合并用）
const namedSelected = ref([]) // Job000081 已命名人物勾选（批量隐藏用）

const scanning = ref(false)
const scanNotice = ref('')

function errMsg(e, fallback) {
  return e?.response?.data?.error?.message || fallback
}

function initial(name) {
  return (name || '?').charAt(0).toUpperCase()
}

function countOf(entry) {
  return entry?.face_count ?? entry?.count ?? entry?.media_count ?? 0
}

// 封面媒体 ID：命名人物用 cover_media_id，未命名聚类用 cover
function coverId(entry) {
  return entry?.cover_media_id || entry?.cover || entry?.first_media_id || null
}

async function loadCover(key, mediaId) {
  if (!mediaId || covers[key]) return
  try {
    covers[key] = await loadThumbUrl({ id: mediaId }, 'sm')
  } catch (e) {
    // 缩略图缺失不致命：卡片显示占位符
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const { data } = await http.get('/people')
    named.value = Array.isArray(data?.named) ? data.named : []
    unnamed.value = Array.isArray(data?.unnamed) ? data.unnamed : []
    selected.value = []
    namedSelected.value = []
    for (const p of named.value) loadCover(p.id, coverId(p))
    for (const c of unnamed.value) loadCover(c.cluster_id, coverId(c))
  } catch (e) {
    loadError.value = errMsg(e, '人物列表加载失败')
  } finally {
    loading.value = false
  }
}

function openPerson(p) {
  // 复用搜索页的人物筛选（契约 §8 GET /search?person=），无需新路由
  router.push({ name: 'search', query: { person: p.id } })
}

function togglePick(clusterId) {
  const i = selected.value.indexOf(clusterId)
  if (i >= 0) selected.value.splice(i, 1)
  else selected.value.push(clusterId)
}

// ---- Job000081 已命名人物：多选 + 批量隐藏 ----
function toggleNamedPick(id) {
  const i = namedSelected.value.indexOf(id)
  if (i >= 0) namedSelected.value.splice(i, 1)
  else namedSelected.value.push(id)
}

async function batchHide(hidden) {
  const targets = named.value.filter((p) => namedSelected.value.includes(p.id))
  if (!targets.length) return
  const ok = await dialogs.confirm({
    title: hidden ? '隐藏人物' : '取消隐藏人物',
    text: `将${hidden ? '隐藏' : '取消隐藏'}选中的 ${targets.length} 位人物。`,
    confirmText: hidden ? '隐藏' : '取消隐藏'
  })
  if (!ok) return
  let fail = 0
  for (const p of targets) {
    try {
      await http.patch(`/people/${p.id}`, { hidden })
      p.hidden = hidden
    } catch (e) {
      fail++
    }
  }
  namedSelected.value = []
  if (fail) scanNotice.value = `批量操作完成，${fail} 项失败`
}

async function openMerge() {
  const vals = await dialogs.form({
    title: '命名并合并',
    fields: [
      {
        key: 'name',
        label: `将选中的 ${selected.value.length} 个聚类合并为同一个人物，姓名`,
        placeholder: '如：张三',
        validate: (v) => (v && v.trim() ? '' : '姓名不能为空')
      },
      { key: 'pet', label: '这是宠物', type: 'check', initial: false }
    ]
  })
  if (!vals) return
  try {
    await http.post('/people', {
      cluster_ids: [...selected.value],
      name: vals.name.trim(),
      is_pet: !!vals.pet
    })
    await load()
  } catch (e) {
    await dialogs.alert(errMsg(e, '合并失败'))
  }
}

async function startRename(p) {
  const name = await dialogs.prompt({
    title: '修改姓名',
    label: '姓名',
    initial: p.name || '',
    validate: (v) => (v && v.trim() ? '' : '姓名不能为空')
  })
  if (name === null) return
  try {
    await http.patch(`/people/${p.id}`, { name: name.trim() })
    await load()
  } catch (e) {
    await dialogs.alert(errMsg(e, '改名失败'))
  }
}

async function toggleHidden(p) {
  try {
    await http.patch(`/people/${p.id}`, { hidden: !p.hidden })
    p.hidden = !p.hidden
  } catch (e) {
    scanNotice.value = errMsg(e, '操作失败')
  }
}

async function triggerScan() {
  if (scanning.value) return
  scanning.value = true
  scanNotice.value = ''
  try {
    const { data } = await http.post('/ai/faces', { scope: 'all' })
    scanNotice.value = data?.job_id
      ? `已提交人脸重算任务（job ${data.job_id}），完成后刷新本页查看结果`
      : '已提交人脸重算任务，完成后刷新本页查看结果'
  } catch (e) {
    scanNotice.value = errMsg(e, '提交人脸重算失败')
  } finally {
    scanning.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.people-page {
  padding: 20px 24px;
}

.page-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.page-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.page-sub {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.spacer {
  flex: 1;
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

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.btn.primary:hover {
  background-color: var(--color-primary-hover);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.page-tip {
  padding: 40px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.page-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
  font-size: var(--font-size-md);
}

.notice {
  margin-bottom: 12px;
  padding: 8px 12px;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
  font-size: var(--font-size-sm);
}

.section {
  margin-bottom: 28px;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  margin-bottom: 12px;
}

.hint {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  font-weight: 400;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(148px, 1fr));
  gap: 16px;
}

.card {
  position: relative; /* Job000081：named 卡片的 .pick 勾选圆点绝对定位 */
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background-color: var(--color-surface);
  overflow: hidden;
}

.batch-bar-entity {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-weight: 400;
}

.pb-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.card.dimmed {
  opacity: 0.55;
}

.cluster-card {
  position: relative;
  cursor: pointer;
}

.cluster-card.picked {
  border-color: var(--color-primary);
  box-shadow: 0 0 0 2px var(--color-primary-active-bg);
}

.pick {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 1.5px solid #fff;
  background-color: rgba(0, 0, 0, 0.35);
  z-index: 1;
}

.pick.on {
  background-color: var(--color-primary);
  border-color: var(--color-primary);
}

.cover {
  aspect-ratio: 1 / 1;
  background-color: var(--color-surface-hover);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  cursor: pointer;
}

.cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.cover-empty {
  font-size: 28px;
  font-weight: 600;
  color: var(--color-text-disabled);
}

.card-body {
  padding: 8px 10px 4px;
}

.card-name {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.card-meta {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.card-actions {
  display: flex;
  gap: 6px;
  padding: 4px 10px 10px;
}

.mini {
  flex: 1;
  padding: 4px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.mini:hover {
  background-color: var(--color-surface-hover);
}

.mini.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}
</style>
