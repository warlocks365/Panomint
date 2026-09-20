<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">地图配置</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="mapcfg-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <p v-if="err" class="msg msg--error" data-testid="mapcfg-error">{{ err }}</p>
    <p v-if="msg" class="msg msg--ok" data-testid="mapcfg-msg">{{ msg }}</p>
    <p v-if="forbidden" class="msg msg--error">当前账号缺少 admin:system 权限，无法查看地图配置。</p>

    <form v-if="cfg" class="cfg-form" data-testid="mapcfg-form" @submit.prevent="onSave">
      <div class="form-grid">
        <label class="field">
          <span class="field-label">中国底图来源</span>
          <select v-model="form.china_provider" data-testid="mapcfg-china-provider">
            <option value="amap">高德</option>
            <option value="osm">OpenStreetMap</option>
            <option value="none">不启用</option>
          </select>
        </label>
        <label class="field">
          <span class="field-label">中国底图瓦片 URL（留空用来源默认）</span>
          <input v-model.trim="form.china_tile_url" data-testid="mapcfg-china-url" type="text" placeholder="https://…/{z}/{x}/{y}.png" />
        </label>
        <label class="field">
          <span class="field-label">国际底图来源</span>
          <select v-model="form.intl_provider" data-testid="mapcfg-intl-provider">
            <option value="osm">OpenStreetMap</option>
            <option value="amap">高德</option>
            <option value="none">不启用</option>
          </select>
        </label>
        <label class="field">
          <span class="field-label">国际底图瓦片 URL（留空用来源默认）</span>
          <input v-model.trim="form.intl_tile_url" data-testid="mapcfg-intl-url" type="text" placeholder="https://…/{z}/{x}/{y}.png" />
        </label>
      </div>

      <dl class="info-list">
        <div class="info-row">
          <dt>高德 Key 状态</dt>
          <dd data-testid="mapcfg-key-state">{{ cfg.china_api_key_state || '—' }}</dd>
        </div>
        <div class="info-row">
          <dt>Key 来源</dt>
          <dd>{{ cfg.china_api_key_source || '—' }}</dd>
        </div>
        <div class="info-row">
          <dt>最近更新</dt>
          <dd>{{ formatTime(cfg.updated_at) }}</dd>
        </div>
      </dl>

      <p class="hint">
        高德 Key 不在此界面写入：数据库该列为加密存储，而系统尚无密钥管理设施。
        请通过部署环境的 <code>AMAP_KEY</code> 配置（界面只显示其可用状态）。
      </p>

      <button class="btn btn--primary" type="submit" :disabled="busy" data-testid="mapcfg-submit">
        {{ busy ? '保存中…' : '保存配置' }}
      </button>
    </form>
  </section>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { getMapConfig, putMapConfig } from '../../api/admin'

const cfg = ref(null)
const form = ref({ china_provider: '', china_tile_url: '', intl_provider: '', intl_tile_url: '' })
const loading = ref(false)
const busy = ref(false)
const err = ref('')
const msg = ref('')
const forbidden = ref(false)

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    cfg.value = await getMapConfig()
    form.value = {
      china_provider: cfg.value.china_provider || 'amap',
      china_tile_url: cfg.value.china_tile_url || '',
      intl_provider: cfg.value.intl_provider || 'osm',
      intl_tile_url: cfg.value.intl_tile_url || ''
    }
  } catch (e) {
    if (e?.response?.status === 403) {
      forbidden.value = true
    } else {
      err.value = errMessage(e, '加载地图配置失败')
    }
  } finally {
    loading.value = false
  }
}

async function onSave() {
  busy.value = true
  err.value = ''
  msg.value = ''
  // 只提交相对加载时发生变化的字段（PUT 是部分更新语义）
  const patch = {}
  for (const k of Object.keys(form.value)) {
    if (form.value[k] !== (cfg.value[k] || '')) patch[k] = form.value[k]
  }
  if (!Object.keys(patch).length) {
    msg.value = '没有需要保存的改动'
    busy.value = false
    return
  }
  try {
    await putMapConfig(patch)
    msg.value = '已保存'
    await load()
  } catch (e) {
    err.value = errMessage(e, '保存地图配置失败')
  } finally {
    busy.value = false
  }
}

onMounted(load)
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
.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 12px;
  margin-bottom: 14px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.field-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
input,
select {
  padding: 7px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background: var(--color-surface);
}
input:focus,
select:focus {
  outline: 2px solid var(--color-primary-active-bg);
  border-color: var(--color-primary);
}
.info-list {
  margin: 0 0 12px;
}
.info-row {
  display: flex;
  gap: 12px;
  padding: 6px 0;
  border-bottom: 1px solid var(--color-border);
}
.info-row:last-child {
  border-bottom: none;
}
.info-row dt {
  width: 110px;
  flex-shrink: 0;
  color: var(--color-text-secondary);
  font-size: var(--font-size-md);
}
.info-row dd {
  margin: 0;
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
}
.hint {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin: 0 0 12px;
}
.hint code {
  background: var(--color-surface-hover);
  padding: 1px 6px;
  border-radius: var(--radius-sm);
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
</style>
