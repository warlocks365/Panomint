<template>
  <!-- Agent 语义接口 · LLM 上游配置（Job000140 Phase 2，设计 §9 区块 B）：
       权限门同 DebugSettingsCard（403 整卡不渲染）；key 永不回显（尾 4 位）。 -->
  <section v-if="!hidden" class="card">
    <div class="card-head">
      <h2 class="card-title">Agent 语义接口 · LLM 上游</h2>
      <span class="state" :class="stateClass" data-testid="agent-llm-state">{{ stateLabel }}</span>
    </div>
    <p class="card-desc">
      为页面内 AI 助手配置 OpenAI 兼容上游（如 DashScope 兼容模式、vLLM）。
      API Key 加密存储于服务端，<strong>永不下发浏览器</strong>；AI 助手经同源代理
      <code>/agent/llm/v1</code> 调用，每次调用记审计（仅模型/耗时/token 数，不含内容）。
    </p>

    <p v-if="err" class="msg msg--error" data-testid="agent-llm-err">{{ err }}</p>
    <p v-if="msg" class="msg" :class="msgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="agent-llm-msg">{{ msg }}</p>

    <div class="form">
      <label class="field">
        <span class="label">Base URL（OpenAI 兼容，不带 /chat/completions）</span>
        <input v-model="form.base_url" data-testid="agent-llm-base-url" placeholder="https://dashscope.aliyuncs.com/compatible-mode/v1" :disabled="busy" />
      </label>
      <label class="field">
        <span class="label">模型名</span>
        <input v-model="form.model" data-testid="agent-llm-model" placeholder="qwen3.5-plus" :disabled="busy" />
      </label>
      <label class="field">
        <span class="label">
          API Key
          <span v-if="hasKey" class="key-tail">已保存（尾 {{ keyTail }}），留空保留 / 输入新值覆盖 / 空格清除</span>
          <span v-else class="key-tail">未保存</span>
        </span>
        <input v-model="keyInput" data-testid="agent-llm-key" type="password" autocomplete="new-password" placeholder="sk-..." :disabled="busy" />
      </label>
      <label class="field field--check">
        <input v-model="form.enabled" data-testid="agent-llm-enabled" type="checkbox" :disabled="busy" />
        <span>启用（启用后页面内 AI 助手才可用）</span>
      </label>
    </div>

    <div class="actions">
      <button class="btn" data-testid="agent-llm-save" :disabled="busy" @click="onSave">{{ busy ? '处理中…' : '保存配置' }}</button>
      <button class="btn btn--ghost" data-testid="agent-llm-test" :disabled="busy" @click="onTest">{{ busy ? '…' : '测试连通' }}</button>
    </div>
    <p class="hint">测试连通：服务端向上游 GET /models 发一次请求（不消耗对话 token）。</p>
  </section>
</template>

<script setup>
// 范式同 DebugSettingsCard：403 权限门整卡不渲染；操作失败即时刷新对齐服务端真值。
import { computed, onMounted, ref } from 'vue'
import { errMessage, errCode } from '../../stores/auth'
import { getAgentLlmConfig, saveAgentLlmConfig, testAgentLlmConfig } from '../../api/agentLlm'

const hidden = ref(false)
const busy = ref(false)
const hasKey = ref(false)
const keyTail = ref('')
const form = ref({ base_url: '', model: '', enabled: false })
const keyInput = ref('')
const err = ref('')
const msg = ref('')
const msgKind = ref('ok')

const stateLabel = computed(() => (form.value.enabled ? '已启用' : '未启用'))
const stateClass = computed(() => ({ 'state--on': form.value.enabled, 'state--off': !form.value.enabled }))

function ok(text) { msgKind.value = 'ok'; msg.value = text }
function fail(e, fallback) { msgKind.value = 'error'; msg.value = errMessage(e, fallback) }

async function refresh() {
  const st = await getAgentLlmConfig()
  form.value.base_url = st.base_url || ''
  form.value.model = st.model || ''
  form.value.enabled = !!st.enabled
  hasKey.value = !!st.has_key
  keyTail.value = st.key_tail || ''
}

onMounted(async () => {
  try {
    await refresh()
  } catch (e) {
    if (e?.response?.status === 403) hidden.value = true
    else err.value = errMessage(e, '读取 LLM 配置失败')
  }
})

// keyInput 三态归一：'' → null（保留）；' '（空白）→ ''（清除）；其余 → 新值。
function normalizeKey() {
  if (keyInput.value === '') return null
  if (keyInput.value.trim() === '') return ''
  return keyInput.value.trim()
}

async function onSave() {
  busy.value = true
  err.value = ''
  msg.value = ''
  try {
    await saveAgentLlmConfig({
      base_url: form.value.base_url,
      model: form.value.model,
      api_key: normalizeKey(),
      enabled: form.value.enabled,
    })
    keyInput.value = ''
    await refresh()
    ok('LLM 上游配置已保存。')
  } catch (e) {
    await refresh().catch(() => {})
    fail(e, '保存失败')
  } finally {
    busy.value = false
  }
}

async function onTest() {
  busy.value = true
  err.value = ''
  msg.value = ''
  try {
    const key = normalizeKey()
    const res = await testAgentLlmConfig({
      base_url: form.value.base_url || undefined,
      api_key: key === '' ? undefined : (key ?? undefined),
    })
    if (res.ok) ok('上游连通正常。')
    else { msgKind.value = 'error'; msg.value = res.error || '上游不可达' }
  } catch (e) {
    if (errCode(e) === 'CIPHER_KEY_MISSING') fail(e, '服务端未配置加密密钥')
    else fail(e, '测试失败')
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.card { background-color: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-lg); padding: 20px; margin-bottom: 16px; }
.card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.card-title { margin: 0; font-size: 16px; }
.card-desc { margin: 10px 0 0; color: var(--color-text-secondary); font-size: var(--font-size-sm); line-height: 1.6; }
.state { font-size: var(--font-size-sm); padding: 2px 8px; border-radius: 999px; border: 1px solid var(--color-border); }
.state--on { color: var(--color-success); border-color: var(--color-success); }
.state--off { color: var(--color-text-secondary); }
.form { display: flex; flex-direction: column; gap: 12px; margin-top: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.field--check { flex-direction: row; align-items: center; gap: 8px; font-size: var(--font-size-sm); }
.label { font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.key-tail { margin-left: 8px; color: var(--color-text-secondary); }
input[type="text"], input[type="password"], input:not([type]) { height: 38px; padding: 0 10px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); font-size: var(--font-size-md); background-color: var(--color-surface); outline: none; }
.actions { display: flex; gap: 10px; margin-top: 14px; }
.btn { height: 38px; padding: 0 16px; border: 1px solid transparent; border-radius: var(--radius-sm); background-color: var(--color-primary); color: #fff; font-size: var(--font-size-sm); }
.btn:hover:not(:disabled) { background-color: var(--color-primary-hover); }
.btn:disabled { opacity: 0.6; cursor: not-allowed; }
.btn--ghost { background-color: transparent; border-color: var(--color-border); color: var(--color-text); }
.msg { margin: 12px 0 0; font-size: var(--font-size-sm); }
.msg--error { color: var(--color-danger); }
.msg--ok { color: var(--color-success); }
.hint { margin: 8px 0 0; font-size: var(--font-size-xs, 12px); color: var(--color-text-secondary); line-height: 1.6; }
</style>
