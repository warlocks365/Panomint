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
    <div class="cover cover-cluster">
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
/* 人物卡（DESIGN.md 深化稿 VIEW.04）：纵向圆形头像卡体系——
   无边框米白面 + 双层漫射阴影，hover 阴影升 lift + 头像 scale 1.03；
   cluster 形态虚线圆与已命名形成视觉区分 */
.card {
  position: relative; /* .pick 勾选圆点绝对定位 */
  border-radius: var(--radius-lg);
  background-color: var(--color-surface);
  overflow: hidden;
  box-shadow: var(--shadow-card);
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 18px 12px 12px;
  transition: box-shadow 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.card:hover {
  box-shadow: var(--shadow-lift);
}

.card.dimmed {
  opacity: 0.55;
}

.cluster-card {
  cursor: pointer;
}

.cluster-card.picked {
  outline: 2px solid var(--color-primary);
  outline-offset: 2px;
}

.pick {
  position: absolute;
  top: 10px;
  right: 10px;
  width: 17px;
  height: 17px;
  border-radius: 50%;
  border: 1.5px solid #fff;
  background-color: rgba(65, 64, 60, 0.34);
  z-index: 1;
}

.pick.on {
  background-color: var(--color-primary);
  border-color: var(--color-primary);
}

.cover {
  width: 88px;
  height: 88px;
  border-radius: 50%;
  background: linear-gradient(150deg, var(--color-surface-hover), var(--color-border));
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  cursor: pointer;
  flex-shrink: 0;
  transition: transform 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.card:hover .cover {
  transform: scale(1.03);
}

.cover-cluster {
  background: transparent;
  border: 1.5px dashed var(--color-text-disabled);
}

.cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.cover-empty {
  font-size: 26px;
  font-weight: 400;
  color: var(--color-text-disabled);
  font-family: var(--font-mono);
}

.card-body {
  padding: 10px 6px 2px;
  text-align: center;
  width: 100%;
  min-width: 0;
}

.card-name {
  font-size: var(--font-size-md);
  font-weight: 500;
  color: var(--color-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.card-meta {
  margin-top: 2px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  letter-spacing: 0.02em;
  color: var(--color-text-secondary);
}

.card-actions {
  display: flex;
  gap: 6px;
  padding: 8px 0 2px;
  width: 100%;
  opacity: 0;
  transform: translateY(2px);
  transition: opacity 0.35s cubic-bezier(0.32, 0.72, 0, 1), transform 0.35s cubic-bezier(0.32, 0.72, 0, 1);
}

.card:hover .card-actions,
.card:focus-within .card-actions {
  opacity: 1;
  transform: translateY(0);
}

/* 触屏无 hover：操作钮恒显 */
@media (hover: none) {
  .card-actions {
    opacity: 1;
    transform: none;
  }
}

.mini {
  flex: 1;
  padding: 4px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.mini:hover {
  background-color: var(--color-surface-hover);
  color: var(--color-text-primary);
}
</style>
