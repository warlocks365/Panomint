<template>
  <!-- 品牌 Logo（Job000144）。全站 UI 品牌位的**单一真源**：
       登录/注册/Setup、侧栏展开态、侧栏图标态都走这里，避免同一个资产路径散落四处。
       改资产只改本文件一处。 -->
  <img
    class="brand-logo"
    :class="[`brand-logo--${variant}`, { 'brand-logo--block': block }]"
    :src="src"
    :alt="alt"
    :width="naturalWidth"
    :height="naturalHeight"
    :style="{ height: typeof height === 'number' ? `${height}px` : height }"
    decoding="async"
  />
</template>

<script setup>
import { computed } from 'vue'

// 两版资产均为「紧裁内容框」的透明底 PNG，天然无冗余留白，落位不必再补padding。
//   full = 全标记版（含「panomint」字标），仅可用于浅底；深色底上字标对比度 1.09:1 等于消失。
//   disc = 盘面版（无字标），小尺寸与深色底通用。
// width/height 必须等于文件真实像素：这两个值是给浏览器的固有宽高比，用来在图片
// 加载前占位。填错会造成加载瞬间的布局抖动（Job000144 返工：曾误填 332x276/411x410，
// 而文件实际已是 256x213/64x64）。改资产尺寸时这里必须同步。
const ASSETS = {
  full: { src: '/brand/logo-full-128.png', width: 128, height: 106 },
  disc: { src: '/brand/logo-disc-64.png', width: 64, height: 64 }
}

const props = defineProps({
  variant: {
    type: String,
    default: 'full',
    // 字面量而非引用 ASSETS：defineProps 会被提升到 setup() 之外，
    // 不能引用 script setup 内声明的局部变量（否则编译期直接报错）。
    validator: (v) => v === 'full' || v === 'disc'
  },
  // 展示高度。给数字则按 px；也可传 CSS 长度（如 '2rem'）以跟随根字号缩放。
  height: { type: [Number, String], default: 28 },
  // 卡片式落位（登录/注册/Setup）：水平居中 + 与下方标题留白。
  // 收进组件而非各页 scoped 样式，是为了三页共用一套间距，且不给每页增行数
  // （RegisterView 原本294 行，加页内样式会顶破 300 行门禁）。
  block: { type: Boolean, default: false },
  // 装饰性重复场景（紧邻已有文字）传 ''，由 .brand-text 承担可访问名，避免读屏重复播报。
  alt: { type: String, default: '' }
})

// 生产构建会**剥离 validator**，届时非法 variant 不再有任何拦截。这里仍做一次兜底查找，
// 避免「一个拼错的属性名 = 整个页面白屏崩溃」——退回全标记版是可用页面，崩溃不是。
const asset = computed(() => ASSETS[props.variant] || ASSETS.full)
const src = computed(() => asset.value.src)
// width/height 属性给浏览器固有宽高比，CSS 只覆盖 height —— 图片加载前即占位，无布局跳动。
const naturalWidth = computed(() => asset.value.width)
const naturalHeight = computed(() => asset.value.height)
</script>

<style scoped>
.brand-logo {
  display: block;
  flex-shrink: 0;
  width: auto;
  object-fit: contain;
}

/* 卡片式落位：居中 + 与下方 eyebrow 标题留白 */
.brand-logo--block {
  margin: 0 auto 18px;
}
</style>