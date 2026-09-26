<template>
  <!-- Job000125：管理后台「网络」页签（HTTPS 与证书）。
       ⚠️ 单根约束：AdminView 以 v-show 切换页签，多根 fragment 会让 v-show 失效（#129 同型教训）。 -->
  <div class="network-tab">
    <HttpsCard />
  </div>
</template>

<script setup>
import HttpsCard from './HttpsCard.vue'
</script>
