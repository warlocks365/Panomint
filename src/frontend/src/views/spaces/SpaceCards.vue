<template>
  <div class="space-cards">
    <button
      class="space-card"
      :class="{ 'space-card--active': activeSpace === 'personal' }"
      data-testid="space-card-personal"
      @click="$emit('switch', 'personal')"
    >
      <span class="space-icon" v-html="icons.personal"></span>
      <span class="space-info">
        <span class="space-name">个人空间</span>
        <span v-if="personal" class="space-meta">
          {{ personal.media_count }} 个媒体 · {{ formatBytes(personal.used_bytes) }}
        </span>
        <span v-else class="space-meta muted">加载中…</span>
      </span>
      <span v-if="activeSpace === 'personal'" class="space-badge">ACTIVE</span>
    </button>

    <button
      class="space-card"
      :class="{ 'space-card--active': activeSpace === 'shared' }"
      data-testid="space-card-shared"
      @click="$emit('switch', 'shared')"
    >
      <span class="space-icon" v-html="icons.shared"></span>
      <span class="space-info">
        <span class="space-name">共享空间</span>
        <span class="space-meta muted">
          {{ shared.length > 0 ? shared.length + ' 个空间' : '暂无共享空间' }}
        </span>
      </span>
      <span v-if="activeSpace === 'shared'" class="space-badge">ACTIVE</span>
    </button>
  </div>

  <div v-if="activeSpace === 'shared' && shared.length === 0" class="empty-state" data-testid="space-empty">
    <span class="empty-icon" v-html="icons.shared"></span>
    <p class="empty-title">暂无共享空间</p>
    <p class="empty-desc">共享空间用于与家人朋友共同管理照片和视频。被邀请加入共享空间后，会显示在这里。</p>
  </div>
</template>

<script setup>
// 空间卡片区（Job000093 从 SpacesView 抽出）：个人/共享两卡片 + 共享空态。
// 纯展示+事件上抛：personal/shared/activeSpace/formatBytes 经 props 注入，
// switch(space) 事件原样上抛；图标内联 SVG 随组件（O2 锁定单一约定）。
defineProps({
  personal: { type: Object, default: null },
  shared: { type: Array, default: () => [] },
  activeSpace: { type: String, default: 'personal' },
  formatBytes: { type: Function, required: true }
})
defineEmits(['switch'])

const icons = {
  personal:
    '<svg viewBox="0 0 24 24" width="22" height="22" fill="none"><circle cx="12" cy="8" r="4" stroke="currentColor" stroke-width="1.6"/><path d="M4.5 20c1.2-3.9 4.1-6 7.5-6s6.3 2.1 7.5 6" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>',
  shared:
    '<svg viewBox="0 0 24 24" width="22" height="22" fill="none"><circle cx="8.5" cy="9" r="3.2" stroke="currentColor" stroke-width="1.6"/><path d="M2.8 19c1-3.2 3.2-4.8 5.7-4.8s4.7 1.6 5.7 4.8" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/><circle cx="16.5" cy="10" r="2.6" stroke="currentColor" stroke-width="1.6"/><path d="M15.4 14.4c2.4.2 4.3 1.7 5.2 4.6" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>'
}
</script>

<style scoped>
/* 空间卡（DESIGN.md 深化稿 VIEW.06）：2:1 不对称双卡（禁三等分）、无边框米白面、
   双层漫射阴影，hover 升 lift；active 雾蓝描边 + ACTIVE mono 徽标 */
.space-cards {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 16px;
}

@media (max-width: 768px) {
  .space-cards {
    grid-template-columns: 1fr;
  }
}

.space-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 20px 22px;
  background-color: var(--color-surface);
  border: 1.5px solid transparent;
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  cursor: pointer;
  text-align: left;
  font-family: var(--font-family);
  transition: box-shadow 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.space-card:hover {
  box-shadow: var(--shadow-lift);
}

.space-card--active {
  border-color: var(--color-primary);
}

.space-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background-color: rgba(74, 90, 106, 0.1);
  color: var(--color-primary);
  flex-shrink: 0;
}

.space-card--active .space-icon {
  background-color: rgba(74, 90, 106, 0.16);
}

.space-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.space-name {
  font-size: var(--font-size-md);
  font-weight: 500;
  color: var(--color-text-primary);
}

.space-meta {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--color-text-secondary);
}

.space-badge {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.08em;
  color: var(--color-primary);
  border: 1px solid rgba(74, 90, 106, 0.35);
  border-radius: 999px;
  padding: 3px 10px;
  flex-shrink: 0;
}

.muted {
  color: var(--color-text-secondary);
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 56px 24px;
  background-color: var(--color-surface);
  border: 1px dashed var(--color-border);
  border-radius: var(--radius-lg);
  text-align: center;
}

.empty-icon {
  color: var(--color-text-disabled);
  display: inline-flex;
}

.empty-title {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
}

.empty-desc {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  max-width: 420px;
}
</style>
