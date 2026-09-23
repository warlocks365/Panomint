<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">应用密码（第三方客户端）</h2>
      <span class="state" :class="stateClass" data-testid="app-pw-state">{{ stateLabel }}</span>
    </div>
    <p class="card-desc">
      给 WebDAV 等第三方客户端专用的独立密码：主密码不必交给客户端，
      泄漏后也可单独轮换/清除，不影响登录密码。生成或轮换后，
      在客户端里把密码填成这串应用密码、账号仍填邮箱即可。
    </p>

    <p v-if="msg" class="msg" :class="msgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="app-pw-msg">{{ msg }}</p>

    <!-- ① 刚生成/轮换：明文仅此一次，必须当场复制走 -->
    <template v-if="generated">
      <div class="actions">
        <label class="inline-field inline-field--wide">
          <span class="inline-label">应用密码（只显示这一次，请立即保存）</span>
          <input
            ref="valueInput"
            v-model="generated"
            data-testid="app-pw-value"
            type="text"
            readonly
            class="mono"
            @focus="selectValue"
          />
        </label>
        <button class="btn" data-testid="app-pw-copy" :disabled="copied" @click="onCopy">
          {{ copied ? '已复制' : '复制' }}
        </button>
      </div>
      <p class="hint">服务端只保存它的散列，刷新或关闭后<strong>永远无法再查看</strong>。已复制可点下方按钮收起。</p>
      <div class="actions">
        <button class="btn btn--ghost" data-testid="app-pw-done" :disabled="busy" @click="onDone">
          我已保存，收起
        </button>
      </div>
    </template>

    <!-- ② 常态：生成/轮换/清除入口（都要当前密码，同改密的「捡到开着的电脑」防线） -->
    <template v-else>
      <div class="actions">
        <label class="inline-field">
          <span class="inline-label">当前密码</span>
          <input
            v-model="current"
            data-testid="app-pw-current"
            type="password"
            autocomplete="current-password"
            :disabled="busy"
          />
        </label>
        <button v-if="!set" class="btn" data-testid="app-pw-generate" :disabled="busy || !current" @click="onGenerate">
          {{ busy ? '处理中…' : '生成应用密码' }}
        </button>
        <template v-else>
          <button class="btn" data-testid="app-pw-rotate" :disabled="busy || !current" @click="onGenerate">
            {{ busy ? '处理中…' : '轮换' }}
          </button>
          <button class="btn btn--danger" data-testid="app-pw-clear" :disabled="busy || !current || confirming" @click="onClear">
            {{ busy ? '处理中…' : (confirming ? '再点一次确认清除' : '清除') }}
          </button>
        </template>
      </div>
      <p v-if="set" class="hint">
        轮换后旧应用密码立即失效，使用它的客户端需更新；清除后这些客户端会认证失败，
        只能改回主密码或重新生成。清除需点两次确认，防止误触。
      </p>
      <p v-else class="hint">需要输入当前登录密码才能生成 —— 只凭开着的登录状态不能铸造长期凭据。</p>
    </template>
  </section>
</template>

<script setup>
// 应用密码设置卡（Job000098）——SettingsView 的拆分件，样式与交互范式同 MfaSettingsCard。
// 明文只在生成/轮换响应里出现一次，因此「展示 + 复制」是这张卡的核心路径；
// 局域网 http 是非安全上下文，clipboard API 可能不存在，复制走逐级降级。
import { computed, onMounted, ref } from 'vue'
import { useAuthStore, errMessage, errCode } from '../../stores/auth'

const auth = useAuthStore()

const set = ref(false) // 服务端是否已设置应用密码（GET /user/app-password 的唯一来源）
const generated = ref('') // 刚生成/轮换拿到的明文；空串 = 不在展示态
const current = ref('')
const busy = ref(false)
const confirming = ref(false)
const copied = ref(false)
const valueInput = ref(null)
const msg = ref('')
const msgKind = ref('ok')

const stateLabel = computed(() => (set.value ? '已设置' : '未设置'))
const stateClass = computed(() => ({
  'state--on': set.value,
  'state--off': !set.value
}))

function ok(text) {
  msgKind.value = 'ok'
  msg.value = text
}
function fail(e, fallback) {
  msgKind.value = 'error'
  msg.value = errCode(e) === 'WRONG_PASSWORD' ? '当前密码不正确' : errMessage(e, fallback)
}

onMounted(async () => {
  try {
    const st = await auth.fetchAppPasswordStatus()
    set.value = !!st?.set
  } catch (e) {
    fail(e, '读取应用密码状态失败')
  }
})

async function onGenerate() {
  busy.value = true
  msg.value = ''
  copied.value = false
  try {
    const res = await auth.generateAppPassword(current.value)
    generated.value = res?.app_password || ''
    if (!generated.value) throw new Error('empty')
    set.value = true
    current.value = ''
    ok('应用密码已生成。请立即复制保存——它只会显示这一次。')
  } catch (e) {
    fail(e, '生成失败，请重试')
  } finally {
    busy.value = false
  }
}

let confirmTimer = null
async function onClear() {
  if (!confirming.value) {
    // 两步确认：第一次点击只进入确认态，3 秒后自动还原
    confirming.value = true
    clearTimeout(confirmTimer)
    confirmTimer = setTimeout(() => { confirming.value = false }, 3000)
    return
  }
  clearTimeout(confirmTimer)
  confirming.value = false
  busy.value = true
  msg.value = ''
  try {
    await auth.clearAppPassword(current.value)
    set.value = false
    generated.value = ''
    current.value = ''
    ok('应用密码已清除，使用它的第三方客户端将无法再认证。')
  } catch (e) {
    fail(e, '清除失败，请重试')
  } finally {
    busy.value = false
  }
}

function selectValue() {
  valueInput.value?.select?.()
}

async function onCopy() {
  const text = generated.value
  if (!text) return
  // 逐级降级：Clipboard API（需安全上下文）→ 选中文本 + execCommand → 提示手动复制
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    } else {
      selectValue()
      const legacyOk = document.execCommand?.('copy')
      if (!legacyOk) throw new Error('no-clipboard')
    }
    copied.value = true
    ok('已复制到剪贴板。')
  } catch {
    selectValue()
    ok('无法自动复制（当前环境不支持剪贴板）：密码已全选，请按 Ctrl+C 手动复制。')
  }
}

function onDone() {
  generated.value = ''
  copied.value = false
  msg.value = ''
}
</script>

<style scoped>
.card {
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  padding: 20px;
  margin-bottom: 16px;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.card-title { margin: 0; font-size: 16px; }

.card-desc {
  margin: 10px 0 0;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.state {
  font-size: var(--font-size-sm);
  padding: 2px 8px;
  border-radius: 999px;
  border: 1px solid var(--color-border);
}

.state--on { color: var(--color-success); border-color: var(--color-success); }
.state--off { color: var(--color-text-secondary); }

.actions {
  display: flex;
  align-items: flex-end;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 14px;
}

.inline-field { display: flex; flex-direction: column; gap: 6px; }
.inline-field--wide { flex: 1; min-width: 260px; }

.inline-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

input[type='text'],
input[type='password'] {
  height: 38px;
  width: 200px;
  padding: 0 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  outline: none;
}

.inline-field--wide input[type='text'] {
  width: 100%;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

input:focus { border-color: var(--color-primary); }

.btn {
  height: 38px;
  padding: 0 16px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary);
  color: #fff;
  font-size: var(--font-size-sm);
}

.btn:hover:not(:disabled) { background-color: var(--color-primary-hover); }
.btn:disabled { opacity: 0.6; cursor: not-allowed; }

.btn--ghost {
  background-color: transparent;
  color: var(--color-text-primary);
  border-color: var(--color-border);
}

.btn--danger { background-color: var(--color-danger); }

.hint {
  margin: 8px 0 0;
  font-size: var(--font-size-xs, 12px);
  color: var(--color-text-secondary);
  line-height: 1.6;
}

.msg { margin: 12px 0 0; font-size: var(--font-size-sm); }
.msg--error { color: var(--color-danger); }
.msg--ok { color: var(--color-success); }
</style>
