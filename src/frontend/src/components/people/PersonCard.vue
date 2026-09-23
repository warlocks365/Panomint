<template>
  <!-- named 形态：整卡不可点（封面点击进搜索），操作钮改名/隐藏；dimmed 表示已隐藏 -->
  <div v-if="variant === 'named'" class="card" :class="{ dimmed: person.hidden }">
    <span
      class="pick"
      :class="{ on: selected }"
      data-testid="people-pick"
      @click.stop="$emit('pick', person.id)"
    ></span>
    <div class="cover" @click="$emit('open', person)">
      <img v-if="cover" :src="cover" alt="" />
      <div v-else class="cover-empty">{{ initial(person.name) }}</div>
    </div>
    <div class="card-body">
      <div class="card-name" :title="person.name">{{ person.name || '未命名' }}</div>
      <div class="card-meta">
        {{ countOf(person) }} 张照片<span v-if="person.is_pet"> · 宠物</span
        ><span v-if="person.hidden"> · 已隐藏</span>
      </div>
    </div>
    <div class="card-actions">
      <button class="mini" @click="$emit('rename', person)">改名</button>
      <button class="mini" @click="$emit('toggle-hidden', person)">
        {{ person.hidden ? '取消隐藏' : '隐藏' }}
      </button>
    </div>
  </div>

  <!-- cluster 形态：整卡点击勾选（合并用），picked 高亮 -->
  <div v-else class="card cluster-card" :class="{ picked: selected }" @click="$emit('pick', person.cluster_id)">
    <span class="pick" :class="{ on: selected }"></span>
    <div class="cover">
      <img v-if="cover" :src="cover" alt="" />
      <div v-else class="cover-empty">?</div>
    </div>
    <div class="card-body">
      <div class="card-name">未命名</div>
      <div class="card-meta">{{ countOf(person) }} 张照片</div>
    </div>
  </div>
</template>

<script setup>
// PeopleView 拆解（Job000090）：人物/聚类卡片双形态展示组件——
// named 形态（操作钮+封面进搜索+dimmed 隐藏态）与 cluster 形态（整卡勾选合并+picked 高亮）
// 结构同源（pick 圆点+cover+body），条件渲染两处差异；卡片样式全自持（含双形态变体类）。
defineProps({
  person: { type: Object, required: true }, // named:{id,name,...} | cluster:{cluster_id,count,...}
  cover: { type: String, default: '' }, // 封面 objectURL（缩略图缺失时空占位符）
  selected: { type: Boolean, default: false },
  variant: { type: String, default: 'named' } // named | cluster
})
defineEmits(['pick', 'open', 'rename', 'toggle-hidden'])

function initial(name) {
  return (name || '?').charAt(0).toUpperCase()
}

function countOf(entry) {
  return entry?.face_count ?? entry?.count ?? entry?.media_count ?? 0
}
</script>

<style scoped>
.card {
  position: relative; /* named 卡片的 .pick 勾选圆点绝对定位 */
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background-color: var(--color-surface);
  overflow: hidden;
}

.card.dimmed {
  opacity: 0.55;
}

.cluster-card {
  position: relative;
  cursor: pointer;
}

.cluster-card.picked {
  border-color: var(--color-primary);
  box-shadow: 0 0 0 2px var(--color-primary-active-bg);
}

.pick {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 1.5px solid #fff;
  background-color: rgba(0, 0, 0, 0.35);
  z-index: 1;
}

.pick.on {
  background-color: var(--color-primary);
  border-color: var(--color-primary);
}

.cover {
  aspect-ratio: 1 / 1;
  background-color: var(--color-surface-hover);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  cursor: pointer;
}

.cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.cover-empty {
  font-size: 28px;
  font-weight: 600;
  color: var(--color-text-disabled);
}

.card-body {
  padding: 8px 10px 4px;
}

.card-name {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.card-meta {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.card-actions {
  display: flex;
  gap: 6px;
  padding: 4px 10px 10px;
}

.mini {
  flex: 1;
  padding: 4px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.mini:hover {
  background-color: var(--color-surface-hover);
}
</style>
