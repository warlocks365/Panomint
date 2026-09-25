<template>
  <div class="manual-page" data-testid="manual-view">
    <aside class="manual-nav" data-testid="manual-nav">
      <div class="manual-nav-title">操作手册</div>
      <button
        v-for="c in chapters"
        :key="c.id"
        class="nav-item"
        :class="{ active: c.id === currentId }"
        :data-testid="'manual-chapter-' + c.id"
        @click="select(c.id)"
      >{{ c.title }}</button>
    </aside>

    <article class="manual-body">
      <h1 class="manual-title">{{ current.title }}</h1>
      <ManualBlocks :blocks="current.usage" />

      <section v-if="current.deep && current.deep.length" class="deep" :class="{ open: deepOpen }">
        <button class="deep-toggle" data-testid="manual-deep-toggle" @click="deepOpen = !deepOpen">
          <svg class="chev" :class="{ up: deepOpen }" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
            <path d="M3 6l5 5 5-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          <span>原理深入：这一章背后的实现</span>
        </button>
        <div v-show="deepOpen" class="deep-body" data-testid="manual-deep">
          <ManualBlocks :blocks="current.deep" />
        </div>
      </section>

      <nav class="chapter-flip">
        <button v-if="prevChapter" class="btn-ghost" data-testid="manual-prev" @click="select(prevChapter.id)">{{ prevChapter.title }}</button>
        <span class="flip-spacer"></span>
        <button v-if="nextChapter" class="btn-ghost" data-testid="manual-next" @click="select(nextChapter.id)">{{ nextChapter.title }}</button>
      </nav>
    </article>
  </div>
</template>

<script setup>
// Job000119 应用内操作手册：左侧章节导航 + 正文（使用方法）+ 可展开的「原理深入」。
// 内容静态打包自 src/manual/*.js，无后端依赖；入口在设置页「帮助与操作手册」卡。
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { manualChapters } from '../manual'
import ManualBlocks from '../components/manual/ManualBlocks.vue'

const chapters = manualChapters
const route = useRoute()
const currentId = ref(chapters.length ? chapters[0].id : '')
const deepOpen = ref(false)

const idx = computed(() => chapters.findIndex((c) => c.id === currentId.value))
const current = computed(() => chapters[idx.value] || chapters[0])
const prevChapter = computed(() => (idx.value > 0 ? chapters[idx.value - 1] : null))
const nextChapter = computed(() => (idx.value < chapters.length - 1 ? chapters[idx.value + 1] : null))

// 支持直接导航到指定章节（/manual?chapter=storage），便于其他页面深链
watch(
  () => route.query.chapter,
  (v) => {
    if (v && chapters.some((c) => c.id === v)) currentId.value = v
  },
  { immediate: true }
)

function select(id) {
  currentId.value = id
  deepOpen.value = false
}
</script>

<style scoped>
.manual-page {
  display: flex;
  gap: 28px;
  max-width: 1080px;
  margin: 0 auto;
  padding: 24px 20px 64px;
}

.manual-nav {
  flex: 0 0 210px;
  align-self: flex-start;
  position: sticky;
  top: calc(var(--topbar-height) + 16px);
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.manual-nav-title {
  margin: 0 10px 8px;
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--color-text-secondary);
}

.nav-item {
  text-align: left;
  padding: 8px 10px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
  line-height: 1.4;
}

.nav-item:hover {
  background: var(--color-surface-hover);
}

.nav-item.active {
  background: var(--color-primary-active-bg);
  color: var(--color-primary);
  font-weight: 600;
}

.manual-body {
  flex: 1;
  min-width: 0;
}

.manual-title {
  margin: 0 0 14px;
  font-size: 21px;
  color: var(--color-text-primary);
}

.deep {
  margin-top: 26px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-surface);
}

.deep-toggle {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 14px;
  border: none;
  background: transparent;
  color: var(--color-text-secondary);
  font-size: var(--font-size-md);
  font-weight: 600;
  cursor: pointer;
}

.deep-toggle:hover {
  color: var(--color-primary);
}

.chev {
  transition: transform 0.15s ease;
}

.chev.up {
  transform: rotate(180deg);
}

.deep.open {
  background: var(--color-surface-hover);
}

.deep-body {
  padding: 2px 14px 14px;
  border-top: 1px solid var(--color-border);
}

.chapter-flip {
  margin-top: 24px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.flip-spacer {
  flex: 1;
}

.btn-ghost {
  padding: 7px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.btn-ghost:hover {
  border-color: var(--color-primary);
  color: var(--color-primary);
}

@media (max-width: 760px) {
  .manual-page {
    flex-direction: column;
    gap: 16px;
  }

  .manual-nav {
    position: static;
    flex: none;
    flex-direction: row;
    flex-wrap: wrap;
  }

  .manual-nav-title {
    width: 100%;
  }
}
</style>
