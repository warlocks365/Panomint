<template>
  <div class="grid-header">
    <nav class="crumbs" aria-label="当前目录">
      <button
        type="button"
        class="crumb"
        :class="{ 'crumb--active': !selectedPath }"
        data-testid="crumb-root"
        @click="$emit('select', '')"
      >全部</button>
      <template v-for="(seg, i) in segments" :key="seg.path">
        <span class="crumb-sep">/</span>
        <button
          type="button"
          class="crumb"
          :class="{ 'crumb--active': i === segments.length - 1 }"
          :data-testid="'crumb-' + i"
          @click="$emit('select', seg.path)"
        >{{ seg.name }}</button>
      </template>
    </nav>
    <div class="header-actions">
      <button class="btn-ghost" data-testid="folder-new" type="button" @click="$emit('manage', 'create')">
        新建文件夹
      </button>
      <template v-if="selectedOwner">
        <button class="btn-ghost" data-testid="folder-rename" type="button" @click="$emit('manage', 'rename')">
          改名/移动
        </button>
        <button class="btn-ghost" data-testid="folder-grants" type="button" @click="$emit('manage', 'grants')">
          权限
        </button>
        <button class="btn-ghost danger" data-testid="folder-delete" type="button" @click="$emit('manage', 'delete')">
          删除目录
        </button>
      </template>
      <span v-if="!mediaLoading" class="muted count-mono">{{ count }} 项</span>
      <router-link
        class="btn-upload"
        data-testid="folder-upload"
        :to="'/upload?folder=' + (selectedPath || '')"
      >上传到此处</router-link>
    </div>
  </div>
</template>

<script setup>
// 文件夹网格头部（Job000092 从 FoldersView 抽出）：面包屑 + 管理操作区。
// 纯展示+事件上抛：segments/selectedOwner/count/mediaLoading/selectedPath 经 props 注入，
// select(path)/manage(mode) 事件原样上抛，宿主持状态与执行。
defineProps({
  segments: { type: Array, default: () => [] },
  selectedPath: { type: String, default: '' },
  selectedOwner: { type: Boolean, default: false },
  count: { type: Number, default: 0 },
  mediaLoading: { type: Boolean, default: true }
})
defineEmits(['select', 'manage'])
</script>

<style scoped>
.grid-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.count-mono {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
}

.crumbs {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
  min-width: 0;
}

.crumb {
  border: none;
  background: transparent;
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
  cursor: pointer;
  font-family: var(--font-family);
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.crumb:hover {
  background-color: var(--color-surface-hover);
  color: var(--color-primary);
}

.crumb--active {
  color: var(--color-primary);
}

.crumb-sep {
  color: var(--color-text-disabled);
}

.muted {
  color: var(--color-text-secondary);
}

.btn-upload {
  display: inline-flex;
  align-items: center;
  padding: 6px 12px;
  margin-left: 10px;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary);
  color: #fff;
  font-size: var(--font-size-sm);
  text-decoration: none;
}

.btn-upload:hover {
  background-color: var(--color-primary-hover);
}
</style>
