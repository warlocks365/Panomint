<template>
  <!-- 调试通道审计摘要（Job000121 设计 §11「审计摘要」）：最近 8 条 debug.* 事件。
       全部调试事件（管理动作 + agent 生命周期 + 握手失败）target_type 都是 debug_channel，
       单查询 target_type=debug_channel 即可收齐（audit action 过滤是精确匹配，无前缀语义）。 -->
  <div class="audit" data-testid="debug-audit">
    <h3 class="audit-title">最近调试事件</h3>
    <ul class="audit-list">
      <li v-for="e in items" :key="e.id" class="audit-row">
        <span class="audit-time">{{ formatTime(e.at) }}</span>
        <code class="audit-action">{{ e.action }}</code>
        <span v-if="e.ip" class="audit-ip">{{ e.ip }}</span>
      </li>
    </ul>
  </div>
</template>

<script setup>
defineProps({
  items: { type: Array, default: () => [] } // audit Entry（at/action/ip/…）
})

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}
</script>

<style scoped>
.audit { margin-top: 16px; border-top: 1px solid var(--color-border); padding-top: 12px; }
.audit-title { margin: 0 0 8px; font-size: var(--font-size-sm); color: var(--color-text-secondary); font-weight: 600; }
.audit-list { margin: 0; padding: 0; list-style: none; }
.audit-row { display: flex; align-items: center; gap: 10px; padding: 3px 0; font-size: var(--font-size-sm); }
.audit-time { color: var(--color-text-secondary); font-variant-numeric: tabular-nums; flex: none; }
.audit-action { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.audit-ip { color: var(--color-text-secondary); }
</style>
