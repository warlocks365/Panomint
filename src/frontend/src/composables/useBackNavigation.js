import { useRouter } from 'vue-router'

// Job000102 层级返回导航：下钻页（相册详情/播放器等）统一返回策略——
// 有路由历史（从列表下钻进来）则 router.back() 回来源页；
// 无历史（直达/刷新/外部链接）则 replace 到逻辑父级，不产生多余历史条目。
// 判定口径与 PlayerView exit（Job000091）逐字一致：window.history.state?.back。
// 注意 history.state 非响应式——本 composable 是命令式接口，不做模板显隐。
export function useBackNavigation() {
  const router = useRouter()

  function goBack(fallback) {
    if (window.history.state?.back) {
      router.back()
    } else {
      router.replace(fallback)
    }
  }

  return { goBack }
}
