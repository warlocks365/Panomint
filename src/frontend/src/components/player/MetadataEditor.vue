<script setup>
// Job000143：媒体元数据编辑器（拍摄时间 / 拍摄地 / 详细地址 / GPS）。
//
// 放在播放器信息面板内**原地展开**（不用弹层）：信息面板本身是沉浸式上下文，
// 弹层会打断「看图 → 改地点 → 再看图」的连续性。
//
// 三个设计要点：
//  1. **只提交变化了的字段**。后端是三态语义（缺席=不动 / 空串=清空），
//     若把四个字段都发过去，未编辑但为空的字段会把已有数据清掉 ——
//     这是最容易出的一类 bug，故这里显式 diff。
//  2. **坐标系标记**：地图选点与搜索候选都来自高德（GCJ-02），
//     提交时带 coordSource='gcj02' 让服务端转 WGS-84；
//     **手动输入的数字一律按 WGS-84**（用户对照的是 EXIF 原始值）。
//  3. **seq 守卫**：搜索请求带自增序号，只接受最新序号的响应，
//     防「慢响应覆盖快响应」（用户连续输入时必然发生）。
import { computed, reactive, ref, watch } from 'vue'
import { searchPlaces } from '../../api/map'
import { updateMetadata } from '../../api/media'
import GeoPickerModal from './GeoPickerModal.vue'

const props = defineProps({
  detail: { type: Object, required: true }
})
const emit = defineEmits(['saved', 'cancel'])

const form = reactive({ takenAt: '', place: '', address: '', lat: null, lng: null })
const saving = ref(false)
const errMsg = ref('')
const pickerOpen = ref(false)
// 坐标是否来自高德链路（搜索候选 / 地图选点）→ 决定提交时的坐标系标记。
const coordFromAmap = ref(false)

const q = ref('')
const candidates = ref([])
const searching = ref(false)
const searchErr = ref('')
let searchSeq = 0
let debounceTimer = null

// 编辑前的原值快照，用于 diff
let original = null

function snapshot() {
  return {
    takenAt: props.detail?.taken_at || '',
    place: props.detail?.place || '',
    address: props.detail?.address || '',
    lat: props.detail?.gps?.lat ?? null,
    lng: props.detail?.gps?.lon ?? null
  }
}

function reset() {
  const s = snapshot()
  form.takenAt = toLocalInput(s.takenAt)
  form.place = s.place
  form.address = s.address
  form.lat = s.lat
  form.lng = s.lng
  original = s
  coordFromAmap.value = false
}

watch(() => props.detail?.id, reset, { immediate: true })

// taken_at 存的是带时区的 ISO，而 <input type="datetime-local"> 要的是
// 「本地时区的 yyyy-MM-ddTHH:mm」（无时区）。转换错了会差一个时区（8 小时）。
function toLocalInput(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

// datetime-local 的值没有时区信息，这里按**本地时区**补回偏移再转 RFC3339，
// 服务端只接受带时区的格式，直接发裸字符串会被判格式非法。
function fromLocalInput(v) {
  if (!v) return ''
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return ''
  const off = -d.getTimezoneOffset()
  const sign = off >= 0 ? '+' : '-'
  const abs = Math.abs(off)
  const p = (x) => String(x).padStart(2, '0')
  return `${v}:00${sign}${p(Math.floor(abs / 60))}:${p(abs % 60)}`
}

const gpsLabel = computed(() => {
  if (form.lat === null || form.lng === null) return '没有数据'
  return `${Number(form.lat).toFixed(6)}, ${Number(form.lng).toFixed(6)}`
})

// 传给地图选点的初始位置：优先当前表单值，其次媒体原值。
// 坐标系：**表单里存的是 WGS-84**，而高德底图要 GCJ-02 —— 转换在
// GeoPickerModal 内部做（那里同时持有 WGS84ToGCJ02），这里只管传 WGS-84。
const gpsInit = computed(() => {
  if (form.lat === null || form.lng === null) return null
  return { lat: form.lat, lon: form.lng, wgs84: true }
})

// 🔴 关键词必须从**事件目标**读，不能读 q.value。
//
// 原因：同一个输入框上同时有 v-model="form.place" 与 @input="onSearchInput"，
// Vue 3 里两者的触发顺序不保证 @input 先于 v-model 更新；而 e2e 注入
// （原生 setter + dispatchEvent）更是只改 DOM 值、两种 ref 都不动。
// 结果是 onSearchInput 读到空串 → 提前 return → 「搜不出候选」。
// 从 el.value 取值与这两者都解耦，是唯一稳的写法。
function onSearchInput(e) {
  const el = e && e.target
  const term = (el && typeof el.value === 'string' ? el.value : q.value).trim()
  q.value = term
  clearTimeout(debounceTimer)
  if (term.length < 2) {
    candidates.value = []
    searchErr.value = ''
    searching.value = false
    return
  }
  // 350ms 防抖：太短会为每个按键发一次请求（上游有 QPS 限流），
  // 太长则感觉迟钝。350ms 是「打完一个词就搜」的体感。
  debounceTimer = setTimeout(() => doSearch(term), 350)
}

async function doSearch(term) {
  const keyword = (term || q.value).trim()
  if (keyword.length < 2) return
  const seq = ++searchSeq
  searching.value = true
  searchErr.value = ''
  try {
    const list = await searchPlaces(keyword)
    if (seq !== searchSeq) return // seq 守卫：只采纳最新请求
    candidates.value = list || []
  } catch (e) {
    if (seq !== searchSeq) return
    searchErr.value = e?.response?.data?.error?.message || '搜索失败，请稍后重试'
    candidates.value = []
  } finally {
    if (seq === searchSeq) searching.value = false
  }
}

// pickCandidate 一次填三项：拍摄地取 name（用户在搜索框输入的就是他想记的名字）、
// 详细地址取 address、坐标取 lon/lat（高德 GCJ-02）。
function pickCandidate(c) {
  form.place = c.name || ''
  if (c.address) form.address = c.address
  if (typeof c.lat === 'number' && typeof c.lon === 'number') {
    form.lat = c.lat
    form.lng = c.lon
    coordFromAmap.value = true
  }
  candidates.value = []
  q.value = ''
}

function onPicked(pos) {
  // 🔴 字段名：GeoPickerModal emit 的是 { lat, lon }（与 maplibre 的 LngLat 同名），
  //    这里原本读 pos.lng → undefined → 表单里出现 NaN，
  //    而弹层内的坐标显示正常（用的是 pos.lon），所以「看着对、存下去是 NaN」。
  //    e2e 实测逮到：回填值 "39.909000, NaN"。
  if (!pos || typeof pos.lat !== 'number' || typeof pos.lon !== 'number') {
    errMsg.value = '选点结果无效，请重新选点'
    pickerOpen.value = false
    return
  }
  // 选点结果是 GCJ-02（与高德底图一致），由 coordFromAmap 标记提交时转 WGS-84。
  form.lat = pos.lat
  form.lng = pos.lon
  coordFromAmap.value = true
  pickerOpen.value = false
}

function clearGps() {
  form.lat = null
  form.lng = null
  coordFromAmap.value = false
}

function cancel() {
  reset()
  emit('cancel')
}

async function save() {
  if (saving.value) return
  errMsg.value = ''
  const o = original || snapshot()
  const payload = {}

  const nextTakenLocal = form.takenAt
  if (nextTakenLocal !== toLocalInput(o.takenAt)) {
    payload.taken_at = nextTakenLocal ? fromLocalInput(nextTakenLocal) : ''
  }
  if (form.place !== o.place) payload.place = form.place
  if (form.address !== o.address) payload.address = form.address

  const coordsChanged = form.lat !== o.lat || form.lng !== o.lng
  if (coordsChanged) {
    const empty = form.lat === null || form.lng === null
    if (empty) {
      payload.lat = 0 // 后端约定 0 = 清空 gps
      payload.lng = 0
    } else {
      payload.lat = form.lat
      payload.lng = form.lng
    }
  }

  if (Object.keys(payload).length === 0) {
    emit('cancel') // 无变化 = 取消
    return
  }
  // 坐标成对自查（后端也会拒，但前端先拦能给更准的提示）
  if (('lat' in payload) !== ('lng' in payload)) {
    errMsg.value = '经纬度需成对提供'
    return
  }

  saving.value = true
  try {
    const resp = await updateMetadata(props.detail.id, payload, {
      coordSource: coordFromAmap.value ? 'gcj02' : 'wgs84'
    })
    emit('saved', resp)
  } catch (e) {
    const code = e?.response?.data?.error?.code
    const msg = e?.response?.data?.error?.message
    errMsg.value = code === 'BAD_METADATA' ? msg || '输入不合法' : msg || '保存失败，请重试'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="md-editor" data-testid="metadata-editor">
    <div class="md-row">
      <label class="md-label" for="md-taken">拍摄时间</label>
      <input id="md-taken" v-model="form.takenAt" class="md-input" type="datetime-local" />
    </div>

    <div class="md-row md-row--stack">
      <label class="md-label" for="md-place">拍摄地</label>
      <input
        id="md-place"
        v-model="form.place"
        class="md-input"
        type="text"
        placeholder="搜索地点或手填，如「景山前街」"
        autocomplete="off"
        @input="onSearchInput"
      />
    </div>

    <!-- 候选列表：绝对定位在输入框下方，不挤占表单布局 -->
    <div v-if="candidates.length || searching || searchErr" class="md-cands" data-testid="md-candidates">
      <div v-if="searching" class="md-hint">搜索中…</div>
      <div v-else-if="searchErr" class="md-hint md-hint--err">{{ searchErr }}</div>
      <button
        v-for="(c, i) in candidates"
        :key="`${c.name}-${i}`"
        class="md-cand"
        type="button"
        @click="pickCandidate(c)"
      >
        <span class="md-cand-name">{{ c.name }}</span>
        <span v-if="c.address" class="md-cand-addr">{{ c.address }}</span>
      </button>
    </div>

    <div class="md-row">
      <label class="md-label" for="md-addr">详细地址</label>
      <input
        id="md-addr"
        v-model="form.address"
        class="md-input"
        type="text"
        placeholder="选填，如「北京市东城区景山前街 4 号」"
      />
    </div>

    <div class="md-row">
      <span class="md-label">GPS</span>
      <div class="md-gps">
        <span class="md-gps-text" data-testid="md-gps">{{ gpsLabel }}</span>
        <button class="md-btn md-btn--ghost" type="button" @click="pickerOpen = true">地图选点</button>
        <button v-if="form.lat !== null" class="md-btn md-btn--ghost" type="button" @click="clearGps">清除</button>
      </div>
    </div>

    <p v-if="errMsg" class="md-hint md-hint--err" data-testid="md-error">{{ errMsg }}</p>

    <div class="md-actions">
      <button
        class="md-btn md-btn--primary"
        type="button"
        :disabled="saving"
        data-testid="md-save"
        @click="save"
      >
        {{ saving ? '保存中…' : '保存' }}
      </button>
      <button class="md-btn" type="button" :disabled="saving" @click="cancel">取消</button>
    </div>

    <GeoPickerModal
      v-if="pickerOpen"
      :init="gpsInit"
      @pick="onPicked"
      @close="pickerOpen = false"
    />
  </div>
</template>

<style scoped>
/* 沿用 Morandi tokens（设计系统 §2），不引入新色系。
   卡片：--surface + --shadow-card；圆角 10px（控件）/ 8px（小片）。 */
.md-editor {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 10px;
  padding: 12px;
  background: var(--color-surface, #f4f1ed);
  border-radius: 10px;
  box-shadow: var(--shadow-card);
}

.md-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.md-label {
  width: 60px;
  flex-shrink: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

/* 拍摄地行改成上下堆叠：候选列表要挂在它下面，同排会被输入框挤掉 */
.md-row--stack {
  flex-direction: column;
  align-items: stretch;
  gap: 6px;
}

.md-input {
  flex: 1;
  min-width: 0;
  padding: 6px 8px;
  font-size: var(--font-size-sm);
  font-family: inherit;
  color: var(--color-text-primary);
  background: var(--color-bg, #e9e4de);
  border: 1px solid transparent;
  border-radius: 8px;
  outline: none;
  transition: border-color 0.18s ease;
}

.md-input:focus {
  border-color: var(--color-primary, #4a5a6a);
}

.md-cands {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin: -4px 0 0;
  padding: 4px;
  background: var(--color-surface, #f4f1ed);
  border-radius: 8px;
  box-shadow: var(--shadow-lift);
  max-height: 190px;
  overflow-y: auto;
}

.md-cand {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  padding: 6px 8px;
  text-align: left;
  background: transparent;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.16s ease;
}

.md-cand:hover {
  background: var(--color-bg, #e9e4de);
}

.md-cand-name {
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.md-cand-addr {
  font-size: var(--font-size-xs, 11px);
  color: var(--color-text-secondary);
}

.md-gps {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.md-gps-text {
  font-family: var(--font-mono, monospace);
  font-size: var(--font-size-xs, 11px);
  color: var(--color-text-secondary);
}

.md-hint {
  margin: 0;
  font-size: var(--font-size-xs, 11px);
  color: var(--color-text-secondary);
}

.md-hint--err {
  color: var(--color-danger, #b0685c);
}

.md-actions {
  display: flex;
  gap: 8px;
  margin-top: 2px;
}

.md-btn {
  padding: 6px 14px;
  font-size: var(--font-size-sm);
  font-family: inherit;
  color: var(--color-text-primary);
  background: var(--color-bg, #e9e4de);
  border: none;
  border-radius: 999px; /* 胶囊：与项目既有的分段切换控件同语言 */
  cursor: pointer;
  transition: background 0.16s ease, opacity 0.16s ease;
}

.md-btn:hover {
  background: var(--color-bg-hover, #ddd7cf);
}

.md-btn--ghost {
  padding: 4px 10px;
  font-size: var(--font-size-xs, 11px);
  color: var(--color-text-secondary);
  background: transparent;
  box-shadow: inset 0 0 0 1px var(--color-border, rgba(74, 90, 106, 0.22));
}

.md-btn--primary {
  color: var(--color-on-primary, #f4f1ed);
  background: var(--color-primary, #4a5a6a);
}

.md-btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

@media (max-width: 768px) {
  .md-row {
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
  }

  .md-label {
    width: auto;
  }
}
</style>
