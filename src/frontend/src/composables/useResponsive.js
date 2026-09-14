import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const DESKTOP_QUERY = '(min-width: 1024px)'
// 横屏媒体查询。注意：微信内置浏览器横屏时可能不派发 orientation 的 change（已知怪癖），
// 故另加 window.innerWidth > innerHeight 的尺寸兜底，见 isLandscape。
const LANDSCAPE_QUERY = '(orientation: landscape)'

function matchMediaSafe(query) {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return null
  return window.matchMedia(query)
}

// 尺寸兜底：宽 > 高 即视为横屏（竖屏时 innerHeight >= innerWidth）
function sizeIsLandscape() {
  if (typeof window === 'undefined') return false
  return window.innerWidth > window.innerHeight
}

// 断点判断：>=1024px 视为桌面端（与 CSS media query 保持一致）。
// 返回 { isDesktop, isMobile, isLandscape }；三者都是响应式 ref。
// ⚠️ isDesktop 的语义与取值与历史版本完全一致（PlayerView.vue / SearchResultsView.vue 依赖），
// 本次仅**新增** isMobile / isLandscape 两个字段，未改动 isDesktop 的任何判定逻辑。
export function useResponsive() {
  const mqDesktop = matchMediaSafe(DESKTOP_QUERY)
  const mqLandscape = matchMediaSafe(LANDSCAPE_QUERY)

  const isDesktop = ref(mqDesktop ? mqDesktop.matches : true)
  const isLandscapeMQ = ref(mqLandscape ? mqLandscape.matches : false)
  const isLandscapeBySize = ref(sizeIsLandscape())

  let desktopMq = null
  let landscapeMq = null

  const onDesktopChange = (e) => {
    isDesktop.value = e.matches
  }
  const onLandscapeChange = (e) => {
    isLandscapeMQ.value = e.matches
  }
  const onViewportChange = () => {
    isLandscapeBySize.value = sizeIsLandscape()
  }

  onMounted(() => {
    desktopMq = window.matchMedia(DESKTOP_QUERY)
    isDesktop.value = desktopMq.matches
    desktopMq.addEventListener('change', onDesktopChange)

    landscapeMq = window.matchMedia(LANDSCAPE_QUERY)
    isLandscapeMQ.value = landscapeMq.matches
    landscapeMq.addEventListener('change', onLandscapeChange)

    onViewportChange()
    window.addEventListener('resize', onViewportChange)
    window.addEventListener('orientationchange', onViewportChange)
  })

  onBeforeUnmount(() => {
    desktopMq?.removeEventListener('change', onDesktopChange)
    landscapeMq?.removeEventListener('change', onLandscapeChange)
    window.removeEventListener('resize', onViewportChange)
    window.removeEventListener('orientationchange', onViewportChange)
  })

  const isMobile = computed(() => !isDesktop.value)
  // 媒体查询与实测尺寸取或：任一判为横屏即横屏。
  // 尺寸是实时的，可覆盖「微信内 orientation MQ 未更新」的情况。
  const isLandscape = computed(() => isLandscapeMQ.value || isLandscapeBySize.value)

  return { isDesktop, isMobile, isLandscape }
}
