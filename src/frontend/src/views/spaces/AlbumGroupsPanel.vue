<template>
  <div class="groups-panel" data-testid="album-groups-panel">
    <AlbumGroupSection
      v-for="g in groups"
      :key="g.album_id"
      :album-id="g.album_id"
      :name="g.name"
      :kind="g.kind"
      :count="g.count"
      :items="g.items"
      :truncated="g.truncated"
      @open="emit('open', $event)"
      @view-album="emit('view-album', $event)"
    />

    <!-- 未分组桶：不在本人任何相册里的 personal 媒体（与 /media?album=none 同口径） -->
    <AlbumGroupSection
      v-if="ungrouped && (ungrouped.count > 0 || ungrouped.items.length > 0)"
      album-id=""
      :name="'未分组'"
      kind="ungrouped"
      :count="ungrouped.count"
      :items="ungrouped.items"
      :truncated="ungrouped.has_more"
      @open="emit('open', $event)"
    />

    <div v-if="groups.length === 0 && (!ungrouped || ungrouped.count === 0)" class="muted empty-tip" data-testid="groups-empty">
      暂无相册，可先在「相册」页创建
    </div>
  </div>
</template>

<script setup>
// 按相册分组面板（Job000101）：个人空间分组视图主体。
// 数据形状 = GET /albums/groups 响应（groups + ungrouped），本组件零数据获取、纯渲染+透传事件。
import AlbumGroupSection from './AlbumGroupSection.vue'

defineProps({
  groups: { type: Array, default: () => [] }, // GroupSummary[]
  ungrouped: { type: Object, default: null } // UngroupedBucket（count/items/has_more/next_cursor）
})

const emit = defineEmits(['open', 'view-album'])
</script>

<style scoped>
.groups-panel {
  display: flex;
  flex-direction: column;
  gap: 28px;
}

.empty-tip {
  padding: 32px 0;
  text-align: center;
}
</style>
