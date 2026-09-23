<template>
  <template v-if="playset.length > 1">
    <button class="nav-arrow prev" :disabled="!hasPrev" title="上一张（←）" @click="$emit('go', -1)">
      <svg viewBox="0 0 24 24" width="26" height="26" fill="none">
        <path d="M15 5l-7 7 7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </button>
    <button class="nav-arrow next" :disabled="!hasNext" title="下一张（→）" @click="$emit('go', 1)">
      <svg viewBox="0 0 24 24" width="26" height="26" fill="none">
        <path d="M9 5l7 7-7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </button>
    <div class="nav-counter">{{ idx + 1 }} / {{ playset.length }}</div>
  </template>
</template>

<script setup>
// PlayerView 拆解（Job000091）：播放集导航（prev/next 箭头 + 计数器）——
// 播放集长度 <=1 时不渲染（单媒体无导航语义）；点击原样上抛 go(delta)，路由切换由宿主编排。
defineProps({
  playset: { type: Array, required: true },
  idx: { type: Number, required: true },
  hasPrev: { type: Boolean, required: true },
  hasNext: { type: Boolean, required: true }
})
defineEmits(['go'])
</script>

<style scoped>
.nav-arrow {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  z-index: 25;
  width: 48px;
  height: 48px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 50%;
  background: rgba(20, 24, 29, 0.5);
  color: var(--color-text-on-dark);
  backdrop-filter: blur(6px);
}

.nav-arrow:hover:not(:disabled) {
  background: rgba(20, 24, 29, 0.85);
  color: #fff;
}

.nav-arrow:disabled {
  opacity: 0.3;
  cursor: default;
}

.nav-arrow.prev {
  left: 14px;
}

.nav-arrow.next {
  right: 14px;
}

.nav-counter {
  position: absolute;
  left: 50%;
  bottom: 14px;
  transform: translateX(-50%);
  z-index: 25;
  padding: 3px 12px;
  border-radius: 999px;
  background: rgba(20, 24, 29, 0.6);
  color: var(--color-text-on-dark);
  font-size: var(--font-size-sm);
  font-variant-numeric: tabular-nums;
  backdrop-filter: blur(6px);
}
</style>
