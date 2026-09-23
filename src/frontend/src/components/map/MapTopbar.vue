<template>
  <div class="map-topbar">
    <span class="mt-title">地图</span>
    <!-- 移动端数值统计让位（原 .map-view--mobile .mt-stat 隐藏规则）：v-show 等价 display:none，避开 scoped 父子选择器穿透 -->
    <span v-show="!isMobile" class="mt-stat">{{ clusterCount }} 个位置 · {{ pointCount }} 项</span>
    <span v-if="err" class="mt-err">{{ err }}</span>
    <button class="mt-icon-btn" type="button" @click="$emit('toggle-icons')">图标</button>
    <!-- 移动端筛选栏平时收起，点此按钮以浮层展开（桌面端常驻侧栏，无需开关） -->
    <button
      v-if="isMobile"
      class="mt-icon-btn"
      type="button"
      data-testid="map-filter-toggle"
      @click="$emit('toggle-filter')"
    >筛选</button>
  </div>
</template>

<script setup>
defineProps({
  clusterCount: { type: Number, default: 0 },
  pointCount: { type: Number, default: 0 },
  err: { type: String, default: '' },
  isMobile: { type: Boolean, default: false }
})
defineEmits(['toggle-icons', 'toggle-filter'])
</script>

<style scoped>
.map-topbar {
  display: flex;
  align-items: center;
  gap: 10px;
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 8px;
  padding: 6px 12px;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.08);
}

.mt-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text-primary);
}

.mt-stat {
  font-size: 12px;
  color: var(--color-text-secondary);
  font-variant-numeric: tabular-nums;
}

.mt-err {
  font-size: 12px;
  color: var(--color-danger);
}

.mt-icon-btn {
  margin-left: auto;
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: #fff;
  border-radius: 6px;
  padding: 3px 10px;
  font-size: 12px;
  color: var(--color-text-secondary);
  cursor: pointer;
}

.mt-icon-btn:hover {
  border-color: rgba(15, 23, 42, 0.28);
  color: var(--color-text-primary);
}

/* 顶栏第二个按钮（移动端「筛选」）不再抢 auto 外边距，与「图标」并排靠右 */
.mt-icon-btn + .mt-icon-btn {
  margin-left: 0;
}
</style>
