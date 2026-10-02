<template>
  <div class="manual-page" data-testid="manual-view">
    <header class="manual-head">
      <div class="ph-heading">
        <span class="ph-eyebrow">12</span>
        <h1 class="ph-title">操作手册</h1>
      </div>
      <p class="ph-sub">{{ chapters.length }} 章 · 每章含可展开的原理深入</p>
    </header>

    <div class="manual-cols">
      <aside class="manual-nav" data-testid="manual-nav">
        <div class="manual-nav-title">CHAPTERS · {{ chapters.length }}</div>
        <button
          v-for="(c, i) in chapters"
          :key="c.id"
          class="nav-item"
          :class="{ active: c.id === currentId }"
          :data-testid="'manual-chapter-' + c.id"
          @click="select(c.id)"
        >
          <span class="nav-no">{{ String(i + 1).padStart(2, '0') }}</span>
          <span class="nav-txt">{{ c.title }}</span>
        </button>
      </aside>

      <article class="manual-body" ref="bodyEl">
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
            <p class="deep-cap">DEEP DIVE · 实现层</p>
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
  </div>
</template>

<script setup>
// Job000119 应用内操作手册：左侧章节导航 + 正文（使用方法）+ 可展开的「原理深入」。
// 内容静态打包自 src/manual/*.js，无后端依赖；入口在设置页「帮助与操作手册」卡。
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import gsap from 'gsap'
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

// DESIGN.md §8 chapter-in：章节切换正文 y12 淡入（clearProps 还原；reduced-motion 跳过）
watch(currentId, async () => {
  await nextTick()
  requestAnimationFrame(() => {
    const mm = gsap.matchMedia()
    mm.add('(prefers-reduced-motion: no-preference)', () => {
      gsap.from('.manual-body', {
        y: 12,
        opacity: 0,
        duration: 0.4,
        ease: 'power2.out',
        clearProps: 'transform,opacity'
      })
    })
  })
})
</script>

<style scoped>
.manual-page {
  max-width: 1080px;
  margin: 0 auto;
  padding: 24px 20px 64px;
}

/* 页头规范（DESIGN.md §5） */
.manual-head {
  margin-bottom: 20px;
}

.ph-heading {
  display: flex;
  align-items: baseline;
  gap: 10px;
}

.ph-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  color: var(--color-text-disabled);
}

.ph-title {
  margin: 0;
  font-size: var(--font-size-lg);
  letter-spacing: 0.12em;
  color: var(--color-text-primary);
}

.ph-sub {
  margin: 4px 0 0;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--color-text-secondary);
}

.manual-cols {
  display: flex;
  gap: 28px;
  align-items: flex-start;
}

.manual-nav {
  flex: 0 0 250px;
  align-self: flex-start;
  position: sticky;
  top: calc(var(--topbar-height) + 16px);
  display: flex;
  flex-direction: column;
  gap: 2px;
  background: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 12px;
}

.manual-nav-title {
  margin: 0 10px 8px;
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.16em;
  color: var(--color-text-disabled);
}

.nav-item {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  text-align: left;
  padding: 9px 11px;
  border: none;
  border-radius: 9px;
  background: transparent;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
  line-height: 1.45;
  font-family: var(--font-family);
  transition: background-color 0.35s cubic-bezier(0.32, 0.72, 0, 1), color 0.35s cubic-bezier(0.32, 0.72, 0, 1);
}

.nav-item:hover {
  background: var(--color-surface-hover);
  color: var(--color-text-primary);
}

.nav-item.active {
  background: rgba(74, 90, 106, 0.12);
  color: var(--color-text-primary);
  font-weight: 500;
  box-shadow: inset 2.5px 0 0 var(--color-primary);
}

.nav-no {
  font-family: var(--font-mono);
  font-size: 9.5px;
  color: var(--color-text-disabled);
  width: 18px;
  flex-shrink: 0;
  padding-top: 2px;
}

.nav-item.active .nav-no {
  color: var(--color-primary);
}

.manual-body {
  flex: 1;
  min-width: 0;
  background: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 26px 30px 24px;
}

.manual-title {
  margin: 0 0 6px;
  font-size: 19px;
  font-weight: 500;
  letter-spacing: 0.05em;
  color: var(--color-text-primary);
}

/* 原理深入：雾蓝左线体（DESIGN.md 深化稿 VIEW.14） */
.deep {
  margin-top: 22px;
  border-top: 1px solid var(--color-border);
  padding-top: 14px;
}

.deep-toggle {
  width: auto;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  border: none;
  background: transparent;
  color: var(--color-primary);
  font-size: var(--font-size-sm);
  font-weight: 500;
  cursor: pointer;
}

.deep-toggle:hover {
  color: var(--color-primary-hover);
}

.chev {
  transition: transform 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.chev.up {
  transform: rotate(180deg);
}

.deep.open {
  background: transparent;
}

.deep-body {
  margin-top: 12px;
  background: rgba(74, 90, 106, 0.05);
  border-left: 2.5px solid var(--color-primary);
  border-radius: 0 12px 12px 0;
  padding: 14px 20px 14px;
}

.deep-cap {
  font-family: var(--font-mono);
  font-size: 9.5px;
  letter-spacing: 0.16em;
  color: var(--color-text-disabled);
  margin: 0 0 8px;
}

.chapter-flip {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid var(--color-border);
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.flip-spacer {
  flex: 1;
}

.btn-ghost {
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: 10px;
  background: var(--color-surface);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
  max-width: 46%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: var(--font-family);
  transition: border-color 0.35s cubic-bezier(0.32, 0.72, 0, 1), color 0.35s cubic-bezier(0.32, 0.72, 0, 1);
}

.btn-ghost:hover {
  border-color: var(--color-primary);
  color: var(--color-primary);
}

@media (max-width: 760px) {
  .manual-cols {
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
