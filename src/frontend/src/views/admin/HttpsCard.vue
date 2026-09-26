<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">HTTPS 与证书</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="https-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <p class="card-desc">
      TLS 由反向代理（Caddy）终止：强制跳转在本应用层<strong>保存即热生效</strong>；
      证书上传只做校验落盘，<strong>需按指引重启 caddy 才对外生效</strong>。
    </p>

    <p v-if="forbidden" class="msg msg--error">当前账号缺少 admin:system 权限，无法管理 HTTPS 设置。</p>
    <p v-if="err" class="msg msg--error" data-testid="https-err">{{ err }}</p>
    <p v-if="msg" class="msg" :class="msgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="https-msg">{{ msg }}</p>

    <template v-if="!forbidden">
      <div class="https-metrics">
        <div class="metric">
          <span class="metric-label">当前请求协议</span>
          <span class="metric-value" :class="cfg.request_proto === 'https' ? 'is-ok' : 'is-warn'" data-testid="https-proto">
            {{ cfg.request_proto === 'https' ? 'HTTPS' : cfg.request_proto === 'http' ? 'HTTP（明文）' : '未知（无代理头）' }}
          </span>
        </div>
        <div class="metric">
          <span class="metric-label">证书剩余天数</span>
          <span class="metric-value" :class="daysClass" data-testid="https-cert-days">
            {{ certDaysLeft === null ? '未登记' : `${certDaysLeft} 天` }}
          </span>
        </div>
        <div class="metric">
          <span class="metric-label">强制 HTTPS</span>
          <span class="metric-value" :class="cfg.force_https ? 'is-ok' : ''" data-testid="https-force-state">
            {{ cfg.force_https ? '已开启' : '已关闭' }}
          </span>
        </div>
      </div>

      <label class="https-row" data-testid="https-force-row">
        <span>
          <span class="https-name">强制 HTTPS（HTTP 自动跳转）</span>
          <span class="https-hint">开启后，经反代进入的 HTTP 请求 301 跳转至 HTTPS（保存即生效，约 60 秒内收敛到全部节点）</span>
        </span>
        <input
          v-model="forceHttps"
          type="checkbox"
          class="https-switch"
          data-testid="https-force-toggle"
          :disabled="busy || loading"
          @change="onForceToggle"
        />
      </label>

      <!-- 访问端口（Job000128 点 9）：HTTPS 端口是 301 跳转目标的真实驱动源 -->
      <div class="https-ports" data-testid="https-ports">
        <label class="port-field">
          <span class="https-name">HTTP 端口</span>
          <input
            v-model.number="httpPort"
            type="number"
            min="1"
            max="65535"
            data-testid="https-http-port"
            :disabled="busy || loading"
          />
        </label>
        <label class="port-field">
          <span class="https-name">HTTPS 端口</span>
          <input
            v-model.number="httpsPort"
            type="number"
            min="1"
            max="65535"
            data-testid="https-https-port"
            :disabled="busy || loading"
          />
        </label>
        <button class="btn" type="button" :disabled="busy || loading || !portsChanged" data-testid="https-ports-save" @click="savePorts">
          保存端口
        </button>
      </div>
      <p class="https-hint" data-testid="https-ports-hint">
        {{ portHint }}
      </p>

      <div class="https-upload" data-testid="https-upload-block">
        <p class="https-name">上传证书（fullchain + 私钥）</p>
        <div class="https-upload-row">
          <label class="https-file-label">证书链 <input ref="certRef" type="file" accept=".pem,.crt,.cer" data-testid="https-cert-file" /></label>
          <label class="https-file-label">私钥 <input ref="keyRef" type="file" accept=".pem,.key" data-testid="https-key-file" /></label>
          <button class="btn" type="button" :disabled="busy || loading || !certRef?.files?.[0] || !keyRef?.files?.[0]" data-testid="https-cert-upload" @click="upload">
            上传并校验
          </button>
        </div>
        <p class="https-hint">服务端校验证书与私钥配对并自动解析到期日；PEM 非法将拒绝落盘。</p>
        <p v-if="applyHint" class="https-note" data-testid="https-apply-hint">{{ applyHint }}</p>
        <p v-if="certPath" class="https-hint">当前登记路径：{{ certPath }}</p>
      </div>
    </template>
  </section>
</template>

<script setup>
// Job000125：HTTPS/证书设置卡。读写 = GET/PUT /admin/https/config；
// 上传 = POST /admin/https/cert（multipart cert+key）。到期日 <30 天黄色、<7 天红色。
import { computed, onMounted, ref } from 'vue'
import { getHttpsConfig, putHttpsConfig, uploadHttpsCert } from '../../api/admin'

const cfg = ref({ force_https: false, request_proto: '', cert_path: '', cert_dir: '' })
const forceHttps = ref(false)
// ---- 访问端口（Job000128 点 9）----
const httpPort = ref(80)
const httpsPort = ref(443)
const savedHttpPort = ref(80)
const savedHttpsPort = ref(443)
const portsChanged = computed(
  () => Number(httpPort.value) !== savedHttpPort.value || Number(httpsPort.value) !== savedHttpsPort.value
)
const portHint = computed(() =>
  Number(httpsPort.value) === 443
    ? 'HTTPS 端口为 443 时，跳转目标不带端口（常规形态）。'
    : `开启强制 HTTPS 后，HTTP 请求将 301 跳转到 https://<域名>:${httpsPort.value}（保存即生效）。`
)
const certRef = ref(null)
const keyRef = ref(null)
const applyHint = ref('')
const loading = ref(false)
const busy = ref(false)
const forbidden = ref(false)
const err = ref('')
const msg = ref('')
const msgKind = ref('ok')

const certDaysLeft = computed(() => {
  if (!cfg.value.cert_not_after) return null
  const ms = new Date(cfg.value.cert_not_after).getTime() - Date.now()
  return Math.max(0, Math.round(ms / 86400000))
})

const daysClass = computed(() => {
  if (certDaysLeft.value === null) return ''
  if (certDaysLeft.value < 7) return 'is-danger'
  if (certDaysLeft.value < 30) return 'is-warn'
  return 'is-ok'
})

function applyView(d = {}) {
  cfg.value = { ...cfg.value, ...d }
  forceHttps.value = d.force_https === true
  if (typeof d.http_port === 'number') {
    httpPort.value = d.http_port
    savedHttpPort.value = d.http_port
  }
  if (typeof d.https_port === 'number') {
    httpsPort.value = d.https_port
    savedHttpsPort.value = d.https_port
  }
}

async function savePorts() {
  msg.value = ''
  const hp = Number(httpPort.value)
  const sp = Number(httpsPort.value)
  if (!Number.isInteger(hp) || hp < 1 || hp > 65535 || !Number.isInteger(sp) || sp < 1 || sp > 65535) {
    msgKind.value = 'error'
    msg.value = '端口必须是 1–65535 之间的整数'
    return
  }
  if (hp === sp) {
    msgKind.value = 'error'
    msg.value = 'HTTP 与 HTTPS 端口不能相同'
    return
  }
  busy.value = true
  try {
    applyView(await putHttpsConfig(undefined, hp, sp))
    msgKind.value = 'ok'
    msg.value = '端口配置已保存，约 60 秒内全量生效'
  } catch (e) {
    msgKind.value = 'error'
    msg.value = e?.response?.data?.error?.message || '保存失败，请重试'
  } finally {
    busy.value = false
  }
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    applyView(await getHttpsConfig())
  } catch (e) {
    if (e?.response?.status === 403) forbidden.value = true
    else err.value = e?.response?.data?.error?.message || '加载失败，请重试'
  } finally {
    loading.value = false
  }
}

async function onForceToggle() {
  msg.value = ''
  busy.value = true
  try {
    applyView(await putHttpsConfig(forceHttps.value))
    msgKind.value = 'ok'
    msg.value = forceHttps.value
      ? '已开启强制 HTTPS：HTTP 请求将 301 跳转至 HTTPS（约 60 秒内全量生效）'
      : '已关闭强制 HTTPS：恢复直连不跳转'
  } catch (e) {
    forceHttps.value = !forceHttps.value // 失败回滚开关姿态
    msgKind.value = 'error'
    msg.value = e?.response?.data?.error?.message || '保存失败，请重试'
  } finally {
    busy.value = false
  }
}

async function upload() {
  msg.value = ''
  const certF = certRef.value?.files?.[0]
  const keyF = keyRef.value?.files?.[0]
  if (!certF || !keyF) {
    msgKind.value = 'error'
    msg.value = '请同时选择证书链与私钥文件'
    return
  }
  busy.value = true
  try {
    const d = await uploadHttpsCert(certF, keyF)
    applyView(d.config)
    applyHint.value = d.apply_hint || ''
    certRef.value.value = ''
    keyRef.value.value = ''
    msgKind.value = 'ok'
    msg.value = '证书已校验并落盘（尚未对反向代理生效）'
  } catch (e) {
    applyHint.value = ''
    msgKind.value = 'error'
    msg.value = e?.response?.data?.error?.message || '上传失败，请重试'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.card-desc { margin: 0 0 12px; font-size: var(--font-size-sm); color: var(--color-text-secondary); line-height: 1.7; }
.msg { margin: 8px 0; font-size: var(--font-size-sm); }
.msg--ok { color: var(--color-success); }
.msg--error { color: var(--color-danger); }

.https-metrics { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; margin-bottom: 10px; }
.metric { background: var(--color-surface-2, var(--color-surface, transparent)); border: 1px solid var(--color-border); border-radius: var(--radius-md, 6px); padding: 10px 12px; }
.metric-label { display: block; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.metric-value { display: block; margin-top: 2px; font-size: 18px; font-weight: 600; }
.is-ok { color: var(--color-success); }
.is-warn { color: var(--color-warning); }
.is-danger { color: var(--color-danger); }

.https-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 0; border-top: 1px solid var(--color-border); cursor: pointer; }
.https-name { display: block; font-size: var(--font-size-md); font-weight: 600; color: var(--color-text-primary); }
.https-hint { display: block; margin-top: 2px; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.https-switch { width: 16px; height: 16px; accent-color: var(--color-primary); cursor: pointer; flex: 0 0 auto; }
.https-switch:disabled { cursor: default; opacity: 0.5; }

.https-upload { padding: 10px 0 2px; border-top: 1px solid var(--color-border); }
.https-upload-row { display: flex; align-items: center; gap: 12px; margin: 8px 0; flex-wrap: wrap; }
.https-file-label { font-size: var(--font-size-sm); color: var(--color-text-secondary); display: flex; align-items: center; gap: 6px; }
.https-note { margin: 8px 0 0; font-size: var(--font-size-sm); color: var(--color-warning); }

.https-ports { display: flex; align-items: flex-end; gap: 16px; padding: 10px 0; border-top: 1px solid var(--color-border); flex-wrap: wrap; }
.port-field { display: flex; flex-direction: column; gap: 4px; }
.port-field input { width: 110px; height: 34px; padding: 0 10px; border: 1px solid var(--color-border); border-radius: var(--radius-sm, 4px); font-size: var(--font-size-sm); }
.https-ports .btn { height: 34px; }
</style>
