import { computed, ref } from 'vue'
import { DEFAULT_DIMENSION, isDimension } from './timelineDimensions'

// 时间轴当前档位状态（Job000145）——**会话级**，刷新回默认档位。
//
// 为什么不做全局单例：档位只服务时间轴一个视图，挂在视图里可随组件卸载一起回收，
// 避免跨路由残留。刷新回默认是刻意的：档位与分页游标、已加载集合强相关，
// 持久化到 localStorage 会让「上次是日档 + 本次只有月分页数据」组合出错误的首屏锚点。
//
// 本模块只管「什么时候允许切、切到哪、切完做什么」，**不碰滚动与请求**：
// 锚点快照/重定位在 useTimelineSeek，翻页预取在 pager 的 loadMore。
// 这样四条切档验收点各自可测，且都不需要跨 composable 猜状态。

const SESSION_KEY = 'panomint.timeline.dimension'

function readSession() {
  try {
    const v = sessionStorage.getItem(SESSION_KEY)
    return isDimension(v) ? v : DEFAULT_DIMENSION
  } catch {
    return DEFAULT_DIMENSION // 隐私模式/禁用存储：退回默认，不阻断渲染
  }
}

export function useTimelineDimension() {
  const dimension = ref(readSession())
  // AC-07：分页在途时把切换延后到加载完成，期间控件 aria-busy=true，不静默丢弃
  const switching = ref(false)
  const pending = ref('')
  // AC-16：拖拽滑块期间拒绝切档（由宿主在拖拽起止时置位）
  const dragLocked = ref(false)

  const isPending = computed(() => !!pending.value)

  // 宿主（TimelineView）注入的协作句柄。声明在前，避免 applyDimension 引用到 TDZ。
  // 用 bind() 而非直接 import：两个 composable 互相 import 会成环，且档位并不拥有滚动。
  let onBeforeSwitch = null
  let onAfterSwitch = null
  let loadingNow = () => false

  function bind({ before, after, isLoading }) {
    if (typeof before === 'function') onBeforeSwitch = before
    if (typeof after === 'function') onAfterSwitch = after
    if (typeof isLoading === 'function') loadingNow = isLoading
  }

  function persist(d) {
    try {
      sessionStorage.setItem(SESSION_KEY, d)
    } catch {
      /* 存储不可用不影响功能，仅失去会话内记忆 */
    }
  }

  /**
   * 换档的**唯一**入口：抓锚点 → 改状态 → 按锚点重定位。
   * 收敛在此处，保证「切档不发请求、不重置分页、不丢锚点」这条不变式
   * 由单点保证 —— 新增调用点无法绕过锚点逻辑。
   */
  function applyDimension(next) {
    if (onBeforeSwitch) onBeforeSwitch()
    dimension.value = next
    persist(next)
    if (onAfterSwitch) onAfterSwitch()
  }

  /**
   * 请求切档。返回是否**立即生效**。
   * - 非法档位 / 重复点击 → false，不做任何事
   * - 拖拽中 → false（AC-16 拒绝，宿主据此禁用控件）
   * - 分页在途 → 记入 pending，等宿主在加载完成后调flushPending()（AC-07）
   */
  function requestDimension(next) {
    if (!isDimension(next) || next === dimension.value) return false
    if (dragLocked.value) return false
    if (loadingNow()) {
      pending.value = next
      switching.value = true
      return false
    }
    applyDimension(next)
    return true
  }

  /** 宿主在 pager.loading 落false 后调用；无挂起切换则空转。 */
  function flushPending() {
    if (!pending.value) return false
    const next = pending.value
    pending.value = ''
    switching.value = false
    applyDimension(next)
    return true
  }

  function setDragLocked(v) {
    dragLocked.value = !!v
  }

  return { dimension, switching, isPending, pending, dragLocked, requestDimension, flushPending, setDragLocked, bind }
}