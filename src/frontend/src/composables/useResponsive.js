import { onBeforeUnmount, onMounted, ref } from 'vue'

const DESKTOP_QUERY = '(min-width: 1024px)'

// 断点判断：>=1024px 视为桌面端（与 CSS media query 保持一致）
export function useResponsive() {
  const isDesktop = ref(
    typeof window !== 'undefined' ? window.matchMedia(DESKTOP_QUERY).matches : true
  )
  let mq = null
  const onChange = (e) => {
    isDesktop.value = e.matches
  }
  onMounted(() => {
    mq = window.matchMedia(DESKTOP_QUERY)
    isDesktop.value = mq.matches
    mq.addEventListener('change', onChange)
  })
  onBeforeUnmount(() => {
    mq?.removeEventListener('change', onChange)
  })
  return { isDesktop }
}
