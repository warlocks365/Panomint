<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">HLS 流媒体</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="hls-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <p class="card-desc">
      分片时长与缓存策略<strong>仅影响新转码任务</strong>，存量分片不会重转；缓存策略对播放热路径即时生效（60 秒内收敛）。
      「HLS 开关」复用上方「自动 HLS 转码」总闸门，此处不重复设置。
    </p>

    <p v-if="forbidden" class="msg msg--error">当前账号缺少 admin:system 权限，无法管理 HLS 设置。</p>
    <p v-if="err" class="msg msg--error" data-testid="hls-err">{{ err }}</p>
    <p v-if="msg" class="msg" :class="msgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="hls-msg">{{ msg }}</p>

    <template v-if="!forbidden">
      <div class="hls-grid">
        <label class="hls-field">
          <span class="hls-label">分片时长</span>
          <span class="hls-control">
            <input
              v-model.number="segSeconds"
              type="number"
              min="2"
              max="20"
              step="1"
              class="hls-input hls-input--num"
              data-testid="hls-seg-input"
              :disabled="busy || loading"
            />
            <span class="hls-unit">秒（2–20）</span>
          </span>
        </label>

        <label class="hls-field">
          <span class="hls-label">缓存策略</span>
          <select v-model="cacheProfile" class="hls-input hls-input--select" data-testid="hls-cache-select" :disabled="busy || loading">
            <option value="balanced">balanced · 常规（清单不缓存，分片一年）</option>
            <option value="aggressive">aggressive · 只读归档（清单缓存 60s）</option>
            <option value="no_cache">no_cache · 调试（全不缓存，产物可覆盖）</option>
          </select>
        </label>

        <label class="hls-field">
          <span class="hls-label">流媒体地址</span>
          <span class="hls-control">
            <input
              v-model.trim="streamBase"
              type="text"
              class="hls-input"
              placeholder="留空使用站内地址（如 https://media.example.com）"
              data-testid="hls-base-input"
              :disabled="busy || loading"
            />
          </span>
        </label>
      </div>
      <p v-if="streamBase" class="hls-warn">
        外部地址需与站点同源或自行解决回源鉴权：HLS 清单/分片端点要求登录态，直连 CDN 会 401。
      </p>

      <div class="hls-actions">
        <button class="btn" type="button" :disabled="busy || loading || !dirty" data-testid="hls-save" @click="save">保存</button>
        <button class="btn btn--ghost" type="button" :disabled="busy || loading" data-testid="hls-reset" @click="resetDefaults">还原默认</button>
        <span class="hls-hint">校验：分片 2–20 整数；地址须 http(s):// 开头、不含查询串、不以 / 结尾</span>
      </div>
    </template>
  </section>
</template>

<script setup>
// Job000125：HLS 流媒体设置卡（分片时长/缓存策略/流媒体地址）。
// 读写 = GET/PUT /admin/transcode-config 的 hls_* 三字段（部分更新，undefined = 不改）；
// 校验与后端 validateHLSUpdate 同型（2-20 / 枚举 / URL 形态），前端先行拦截、服务端兜底。
import { computed, onMounted, ref } from 'vue'
import { getTranscodeConfig, putTranscodeConfig } from '../../api/admin'

const DEFAULTS = { seg: 4, profile: 'balanced', base: '' }

const segSeconds = ref(DEFAULTS.seg)
const cacheProfile = ref(DEFAULTS.profile)
const streamBase = ref(DEFAULTS.base)
const loading = ref(false)
const busy = ref(false)
const forbidden = ref(false)
const err = ref('')
const msg = ref('')
const msgKind = ref('ok')

const dirty = computed(() =>
  segSeconds.value !== DEFAULTS.seg ||
  cacheProfile.value !== DEFAULTS.profile ||
  streamBase.value !== DEFAULTS.base
)

function clientValidate() {
  if (!Number.isInteger(segSeconds.value) || segSeconds.value < 2 || segSeconds.value > 20) {
    return '分片时长需为 2-20 的整数'
  }
  if (streamBase.value) {
    if (!/^https?:\/\//.test(streamBase.value)) return '流媒体地址须以 http:// 或 https:// 开头（或留空）'
    if (/[?# \t\r\n]/.test(streamBase.value)) return '流媒体地址不得含查询串、片段或空白字符'
    if (streamBase.value.endsWith('/')) return '流媒体地址不应以 / 结尾'
  }
  return ''
}

function applyView(d = {}) {
  segSeconds.value = Number.isInteger(d.hls_seg_seconds) ? d.hls_seg_seconds : DEFAULTS.seg
  cacheProfile.value = d.hls_cache_profile || DEFAULTS.profile
  streamBase.value = d.stream_base_url || DEFAULTS.base
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    applyView(await getTranscodeConfig())
  } catch (e) {
    if (e?.response?.status === 403) forbidden.value = true
    else err.value = e?.response?.data?.error?.message || '加载失败，请重试'
  } finally {
    loading.value = false
  }
}

async function save() {
  msg.value = ''
  const invalid = clientValidate()
  if (invalid) {
    msgKind.value = 'error'
    msg.value = invalid
    return
  }
  busy.value = true
  try {
    applyView(await putTranscodeConfig({
      hls_seg_seconds: segSeconds.value,
      hls_cache_profile: cacheProfile.value,
      stream_base_url: streamBase.value
    }))
    msgKind.value = 'ok'
    msg.value = '已保存：分片时长对之后的转码任务生效；缓存策略对播放热路径即时生效（60 秒内收敛）'
  } catch (e) {
    msgKind.value = 'error'
    msg.value = e?.response?.data?.error?.message || '保存失败，请重试'
  } finally {
    busy.value = false
  }
}

async function resetDefaults() {
  segSeconds.value = DEFAULTS.seg
  cacheProfile.value = DEFAULTS.profile
  streamBase.value = DEFAULTS.base
  await save()
}

onMounted(load)
</script>

<style scoped>
.card-desc {
  margin: 0 0 12px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  line-height: 1.7;
}

.msg { margin: 8px 0; font-size: var(--font-size-sm); }
.msg--ok { color: var(--color-success); }
.msg--error { color: var(--color-danger); }

.hls-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 12px;
  padding: 4px 0 8px;
}

.hls-field {
  display: grid;
  grid-template-columns: 110px 1fr;
  align-items: center;
  gap: 12px;
}

.hls-label { font-size: var(--font-size-md); color: var(--color-text-primary); }

.hls-control { display: flex; align-items: center; gap: 8px; min-width: 0; }

.hls-input {
  flex: 1;
  min-width: 0;
  padding: 6px 10px;
  font-size: var(--font-size-sm);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md, 6px);
  background: var(--color-surface, transparent);
  color: var(--color-text-primary);
}

.hls-input--num { flex: 0 0 90px; }
.hls-input:disabled { opacity: 0.55; cursor: default; }

.hls-unit { font-size: var(--font-size-sm); color: var(--color-text-secondary); white-space: nowrap; }

.hls-warn {
  margin: 0 0 8px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.hls-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--color-border);
  flex-wrap: wrap;
}

.hls-hint { font-size: var(--font-size-sm); color: var(--color-text-disabled); }
</style>
