import { ref } from 'vue'
import { itemDate, pad2 } from './useTimelineGrouping'

// TimelineGrid 拆解（Job000088）：日期滑块双向同步——
// 滚动 → 滑块：rAF 节流 + offsets 二分定位当前流元素 → 映射到月桶分数；
// 滑块 → 滚动：seekTo 按需 loadMore 翻页直到目标月落入加载范围，smooth 滚至分组头。
// scrollEl 由宿主生命周期管理（DOM 归属宿主），本模块只消费元素句柄。
export function useTimelineSeek({ flat, offsets, monthIndex, histogram, items, loadMore, finished }) {
  const scrollFraction = ref(0)
  const seeking = ref(false)
  let rafId = 0

  function onScroll(el) {
    if (rafId) return
    rafId = requestAnimationFrame(() => {
      rafId = 0
      if (!el) return
      const top = el.scrollTop + 4
      const offs = offsets.value
      if (!offs.length) return
      // 二分：最后一个 offset <= top 的元素
      let lo = 0, hi = offs.length - 1, idx = 0
      while (lo <= hi) {
        const mid = (lo + hi) >> 1
        if (offs[mid] <= top) { idx = mid; lo = mid + 1 } else hi = mid - 1
      }
      const it = flat.value[idx]
      if (!it || !it.ts || !histogram.value.length) return
      const keys = histogram.value
        .map((b) => b.bucket)
        .filter((k) => /^\d{4}-\d{2}$/.test(k))
        .sort()
      if (!keys.length) return
      const [oy, om] = keys[0].split('-').map(Number)
      const [ny, nm] = keys[keys.length - 1].split('-').map(Number)
      const oldest = new Date(oy, om - 1, 1).getTime()
      const newest = new Date(ny, nm, 1).getTime()
      const range = Math.max(1, newest - oldest)
      scrollFraction.value = Math.min(1, Math.max(0, (newest - it.ts) / range))
    })
  }

  function lastLoadedMonth() {
    for (let i = items.length - 1; i >= 0; i--) {
      const d = itemDate(items[i])
      if (d) return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}`
    }
    return ''
  }

  async function seekTo(monthKey, scrollEl) {
    if (seeking.value) return
    seeking.value = true
    try {
      let guard = 0
      while (!finished.value && guard < 300) {
        const last = lastLoadedMonth()
        // 媒体按时间倒序：last <= monthKey 说明目标月已在加载范围内；unknown = 无媒体可定位
        if (last === 'unknown' || (last && last <= monthKey)) break
        await loadMore()
        guard++
      }
      let idx = monthIndex.value.get(monthKey)
      if (idx == null) {
        // 当月无媒体：找不晚于该月的最近一个月份分组
        for (let i = 0; i < flat.value.length; i++) {
          const it = flat.value[i]
          if (it.header && it.level === 'month' && it.monthKey !== 'unknown' && it.monthKey <= monthKey) {
            idx = i
            break
          }
        }
      }
      if (idx != null && scrollEl) {
        scrollEl.scrollTo({ top: offsets.value[idx], behavior: 'smooth' })
      }
    } finally {
      seeking.value = false
    }
  }

  function destroy() {
    if (rafId) cancelAnimationFrame(rafId)
    rafId = 0
  }

  return { scrollFraction, seeking, onScroll, seekTo, destroy }
}
