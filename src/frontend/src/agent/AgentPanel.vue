<template>
  <!-- Agent 语义接口 · 页面内 AI 助手（Job000140 Phase 2，设计 §5/§8）。
       权限试探：GET /admin/agent/llm-config 403 → 悬浮球不渲染（与服务端同一把锁）。
       官方 Panel（page-agent 主包构造自带）承担对话 UI；本组件只承载：
       悬浮球开关、L2 确认桥 UI、agent 生命周期（dispose 防泄漏）。 -->
  <div v-if="!hidden" class="agent-root">
    <button
      v-if="!expanded"
      class="agent-ball"
      data-testid="agent-ball"
      title="AI 助手"
      @click="expand"
    >AI</button>

    <!-- L2 确认卡（独立 fixed 层，优先级最高；超时 120s 自动拒绝） -->
    <div v-if="pending" class="confirm-mask" data-testid="agent-confirm-mask">
      <div class="confirm-card">
        <p class="confirm-title">敏感操作确认</p>
        <p class="confirm-tool">{{ pending.tool.name }}</p>
        <p class="confirm-text">{{ pending.tool.confirmText(pending.input) }}</p>
        <pre v-if="pending.input && Object.keys(pending.input).length" class="confirm-args">{{ JSON.stringify(pending.input, null, 2) }}</pre>
        <div class="confirm-actions">
          <button class="btn btn--danger" data-testid="agent-confirm-deny" @click="resolveConfirm(false)">取消</button>
          <button class="btn" data-testid="agent-confirm-allow" @click="resolveConfirm(true)">{{ pending.allowLabel }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
// 生命周期：登录会话内常驻（ball 常显），路由切换不销毁；登出由 hidden 403 收敛。
import { onMounted, onUnmounted, ref } from 'vue'
import { getAgentLlmConfig } from '../api/agentLlm'

const hidden = ref(true)
const expanded = ref(false)
const pending = ref(null) // {tool, input, allowLabel, resolve}
let agent = null
let confirmTimer = null

onMounted(async () => {
  try {
    const cfg = await getAgentLlmConfig()
    hidden.value = false
    if (!cfg.enabled) markDisabled()
  } catch (e) {
    hidden.value = true // 403/未登录：整组件不渲染
  }
})

onUnmounted(dispose)

function markDisabled() {
  // LLM 上游未启用：球保留但点击提示配置（引导到设置页）。
  disabled = true
}

let disabled = false

async function expand() {
  if (disabled) {
    alert('AI 助手尚未启用：请先到 设置 → Agent 语义接口 · LLM 上游 完成配置并启用。')
    return
  }
  expanded.value = true
  try {
    const { createPanoAgent } = await import('./createPanoAgent')
    agent = await createPanoAgent({ confirmL2 })
  } catch (e) {
    console.error('[agent] 装配失败', e)
    expanded.value = false
    alert('AI 助手装配失败：' + (e?.message || '未知错误'))
  }
}

function confirmL2(tool, input) {
  return new Promise((resolve) => {
    pending.value = {
      tool,
      input,
      allowLabel: tool.name === 'pano_cancel_transcode_job' ? '确认取消任务' : '确认执行',
      resolve,
    }
    clearTimeout(confirmTimer)
    confirmTimer = setTimeout(() => {
      if (pending.value) {
        pending.value = null
        resolve(false) // 120s 无操作 = 拒绝（防挂起耗尽步数）
      }
    }, 120000)
  })
}

function resolveConfirm(ok) {
  clearTimeout(confirmTimer)
  const r = pending.value?.resolve
  pending.value = null
  if (r) r(!!ok)
}

function dispose() {
  clearTimeout(confirmTimer)
  if (agent) {
    try { agent.dispose() } catch { /* 已销毁 */ }
    agent = null
  }
  pending.value = null
}
</script>

<style scoped>
.agent-root { position: fixed; right: 24px; bottom: 24px; z-index: 2100; }
.agent-ball { width: 52px; height: 52px; border-radius: 50%; border: none; cursor: pointer;
  background-color: var(--color-primary, #2563eb); color: #fff; font-size: 15px; font-weight: 600;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.18); }
.agent-ball:hover { filter: brightness(1.06); }
.confirm-mask { position: fixed; inset: 0; z-index: 2200; background: rgba(0, 0, 0, 0.4);
  display: flex; align-items: center; justify-content: center; }
.confirm-card { background: var(--color-surface, #fff); border-radius: 12px; padding: 20px;
  width: min(420px, calc(100vw - 32px)); border: 1px solid var(--color-border, #e5e7eb); }
.confirm-title { margin: 0 0 6px; font-weight: 600; font-size: 15px; }
.confirm-tool { margin: 0 0 8px; font-size: 12px; color: var(--color-text-secondary, #6b7280); font-family: monospace; }
.confirm-text { margin: 0 0 10px; font-size: 14px; line-height: 1.6; }
.confirm-args { margin: 0 0 12px; padding: 8px; background: rgba(0, 0, 0, 0.04); border-radius: 6px;
  font-size: 12px; max-height: 140px; overflow: auto; }
.confirm-actions { display: flex; justify-content: flex-end; gap: 10px; }
.btn { height: 36px; padding: 0 16px; border: 1px solid transparent; border-radius: var(--radius-sm, 6px);
  background-color: var(--color-primary, #2563eb); color: #fff; font-size: 13px; cursor: pointer; }
.btn--danger { background-color: transparent; border-color: var(--color-border, #e5e7eb); color: var(--color-text, #111); }
</style>
