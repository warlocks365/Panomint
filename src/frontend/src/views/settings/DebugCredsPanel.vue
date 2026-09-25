<template>
  <!-- 凭据一次性回显面板（Job000121 设计 §11「开启回显」）。
       范式同 AppPasswordCard：明文只此一次；复制逐级降级（局域网 http 非安全上下文）；
       「我已保存，收起」退出展示态（凭据此后永远无法再查看）。 -->
  <div>
    <div class="actions">
      <label class="inline-field inline-field--wide">
        <span class="inline-label">接入地址</span>
        <input ref="urlInput" :value="creds.url" data-testid="debug-url" type="text" readonly class="mono" @focus="selectUrl" />
      </label>
      <button class="btn" data-testid="debug-copy-url" :disabled="copiedUrl" @click="onCopy('url')">
        {{ copiedUrl ? '已复制' : '复制地址' }}
      </button>
    </div>
    <div class="actions">
      <label class="inline-field inline-field--wide">
        <span class="inline-label">接入密钥（只显示这一次，请立即保存）</span>
        <input ref="keyInput" :value="creds.key" data-testid="debug-key" type="text" readonly class="mono" @focus="selectKey" />
      </label>
      <button class="btn" data-testid="debug-copy" :disabled="copiedKey" @click="onCopy('key')">
        {{ copiedKey ? '已复制' : '复制密钥' }}
      </button>
    </div>
    <p class="hint hint--warn">
      密钥<strong>仅此一次展示</strong>，服务端只保存它的散列，刷新或关闭后永远无法再查看。
      到期时间：{{ formatTime(creds.expires_at) }}。
    </p>
    <div class="actions">
      <button class="btn btn--ghost" data-testid="debug-creds-done" :disabled="busy" @click="$emit('done')">
        我已保存，收起
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'

defineProps({
  creds: { type: Object, required: true }, // {url, key, expires_at}
  busy: { type: Boolean, default: false }
})
defineEmits(['done'])

const copiedUrl = ref(false)
const copiedKey = ref(false)
const urlInput = ref(null)
const keyInput = ref(null)

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}
function selectUrl() { urlInput.value?.select?.() }
function selectKey() { keyInput.value?.select?.() }

async function onCopy(which) {
  const isUrl = which === 'url'
  const text = isUrl ? urlInput.value?.value : keyInput.value?.value
  if (!text) return
  // 逐级降级：Clipboard API（需安全上下文）→ 选中文本 + execCommand → 提示手动复制
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    } else {
      if (isUrl) selectUrl(); else selectKey()
      const legacyOk = document.execCommand?.('copy')
      if (!legacyOk) throw new Error('no-clipboard')
    }
    if (isUrl) { copiedUrl.value = true } else { copiedKey.value = true }
  } catch {
    if (isUrl) selectUrl(); else selectKey()
  }
}
</script>

<style scoped>
.actions { display: flex; align-items: flex-end; gap: 10px; flex-wrap: wrap; margin-top: 14px; }
.inline-field { display: flex; flex-direction: column; gap: 6px; }
.inline-field--wide { flex: 1; min-width: 260px; }
.inline-label { font-size: var(--font-size-sm); color: var(--color-text-secondary); }
input[type='text'] { height: 38px; padding: 0 12px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); font-size: var(--font-size-md); outline: none; }
.inline-field--wide input[type='text'] { width: 100%; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.btn { height: 38px; padding: 0 16px; border: 1px solid transparent; border-radius: var(--radius-sm); background-color: var(--color-primary); color: #fff; font-size: var(--font-size-sm); }
.btn:hover:not(:disabled) { background-color: var(--color-primary-hover); }
.btn:disabled { opacity: 0.6; cursor: not-allowed; }
.btn--ghost { background-color: transparent; color: var(--color-text-primary); border-color: var(--color-border); }
.hint { margin: 8px 0 0; font-size: var(--font-size-xs, 12px); color: var(--color-text-secondary); line-height: 1.6; }
.hint--warn { color: var(--color-warning); }
</style>
