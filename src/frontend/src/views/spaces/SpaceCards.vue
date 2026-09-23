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
.space-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 16px;
}

.space-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 18px 20px;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  cursor: pointer;
  text-align: left;
  font-family: var(--font-family);
}

.space-card:hover {
  background-color: var(--color-surface-hover);
}

.space-card--active {
  border-color: var(--color-primary);
  background-color: var(--color-primary-active-bg);
}

.space-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: var(--radius-md);
  background-color: var(--color-bg);
  color: var(--color-primary);
  flex-shrink: 0;
}

.space-card--active .space-icon {
  background-color: var(--color-surface);
}

.space-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.space-name {
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
}

.space-meta {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
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
