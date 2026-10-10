import { ref } from 'vue'
import { itemDate } from './useTimelineGrouping'
import {
  bucketRange,
  findRowIndexByMediaId,
  fractionAtTs,
  headerKeyOf,
  isValidKey,
  monthKeyOf,
  resolveAnchorIndex
} from './timelineDimensions'

// 时间轴锚点定位（Job000088 拆解，Job000145 泛化为三段式 + 档位无关）——
// 滚动 → 滑块：rAF 节流 + offsets 二分定位当前流元素 → 映射到当前档位的桶分数；
// 滑块/日历 → 滚动：seekTo({dimension,key}) 按需翻页直到目标落入加载范围，滚至分组头。
// scrollEl 由宿主生命周期管理（DOM 归属宿主），本模块只消费元素句柄。
//
// seekTo 三段式（档位无关；键定长零填充使「字典序 == 时间序」成立，故 last <= key 无需按档位分支）：
//   1) 档位守卫：载荷维度与当前档位不符 → 直接返回，**不猜**
//   2) 翻页预取：在途请求不 abort，等其落地后再判断是否需要继续翻页
//   3) 精确定位：按 offsets 直取（O(1)）；未命中退到「最后一个 <= key 的分组」
//
// 降级事件经 window CustomEvent 上报（'panomint:timeline-anchor-degraded'），
// 不引入埋点 SDK —— 宿主挂一个 listener 即可，避免本期为一条埋点新增依赖。

const DEGRADED_EVENT = 'panomint:timeline-anchor-degraded'

function reportDegraded(reason, detail) {
  if (typeof window === 'undefined' || typeof window.dispatchEvent !== 'function') return
  window.dispatchEvent(new CustomEvent(DEGRADED_EVENT, { detail: { reason, ...detail } }))
}

export function useTimelineSeek({
  flat,
  offsets,
  headerIndex,
  histogram,
  dimension,
  items,
  loading,
  loadMore,
  whenSettled,
  finished
}) {
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
      let lo = 0
      let hi = offs.length - 1
      let idx = 0
      while (lo <= hi) {
        const mid = (lo + hi) >> 1
        if (offs[mid] <= top) {
          idx = mid
          lo = mid + 1
        } else {
          hi = mid - 1
        }
      }
      const it = flat.value[idx]
      if (!it || !it.ts || !histogram.value.length) return
      const range = bucketRange(dimension.value, histogram.value)
      if (!range.newestTs) return
      scrollFraction.value = fractionAtTs(range, it.ts)
    })
  }

  /** 最后一条**有日期**媒体的当前档位键（媒体按时间倒序，故末条即最旧）。 */
  function lastLoadedKey() {
    for (let i = items.length - 1; i >= 0; i--) {
      const d = itemDate(items[i])
      if (d) return headerKeyOf(dimension.value, d)
    }
    return ''
  }

  /**
   * 锚点跳转。载荷必须是 { dimension, key } 对象——靠字符串长度推断档位会让
   * 「档位不匹配」静默走错分支（ADR-001 记录的踩坑），对象载荷让它显式失败。
   */
  async function seekTo(payload, scrollEl) {
    if (!payload || typeof payload !== 'object') return
    const { dimension: want, key } = payload
    if (want !== dimension.value) {
      // 原为静默 return，导致「点查看这一天毫无反应」且无任何线索。
      // 仍不猜测该定位到哪一档（那会跳到错误位置），但必须**可见**：
      // 上报降级事件，宿主可据此提示用户或先行切档。
      reportDegraded('dimension-mismatch', { want, current: dimension.value })
      return
    }
    if (!isValidKey(want, key)) return
    if (seeking.value) return
    seeking.value = true
    try {
      let guard = 0
      while (!finished.value && guard < 300) {
        // AC-14：在途请求不 abort —— 等它落地（响应由 pager 的代次守卫正常并入）。
        // 等待**不消耗翻页预算**：否则一次并发就能把 300 次预算耗尽并静默定位失败。
        if (loading.value) {
          await whenSettled()
          continue
        }
        const last = lastLoadedKey()
        // 媒体按时间倒序：last <= key 说明目标已在加载范围内；无日期可定位时兜底退出
        if (!last || last <= key) break
        await loadMore()
        guard++
      }
      const idx = resolveAnchorIndex(headerIndex.value, key)
      if (idx < 0 || !scrollEl) return
      scrollEl.scrollTo({ top: offsets.value[idx], behavior: 'smooth' })
    } finally {
      seeking.value = false
    }
  }

  /**
   * 切档前的锚点快照：取视口顶部**完整可见**的那一行媒体，记下它与视口顶的像素偏移。
   * delta 为负表示该行已滚过一部分，重定位后必须复现同一负值（AC-05 要求 ±2px）。
   */
  function captureAnchor(el) {
    if (!el) return null
    const offs = offsets.value
    const top = el.scrollTop
    for (let i = 0; i < offs.length; i++) {
      const it = flat.value[i]
      if (!it || it.header) continue
      if (offs[i] <= top + 1) continue // 已滚出视口上方
      const cell = it.cells && it.cells[0]
      if (!cell) continue
      return {
        id: cell.id,
        delta: offs[i] - top,
        takenAt: cell.taken_at || cell.created_at || ''
      }
    }
    return null
  }

  /**
   * 切档后按快照重定位（瞬时滚动，不用 smooth —— 动画中测量无法满足 ±2px）。
   * 降级链：同一媒体 → 同月首项 → 保持 scrollTop 不变；每次降级均上报（AC-06）。
   */
  function restoreAnchor(el, anchor) {
    if (!el || !anchor) return
    let idx = findRowIndexByMediaId(flat.value, anchor.id)
    if (idx < 0) {
      const d = anchor.takenAt ? new Date(anchor.takenAt) : null
      const mKey = d && !Number.isNaN(d.getTime()) ? monthKeyOf(d) : ''
      // 同月首项：取该月键下第一个仍存在的媒体所在行
      const fallback = items.find((m) => {
        const md = itemDate(m)
        return md && monthKeyOf(md) === mKey
      })
      idx = fallback ? findRowIndexByMediaId(flat.value, fallback.id) : -1
      reportDegraded(idx < 0 ? 'anchor_missing' : 'anchor_to_month', { anchorId: anchor.id, monthKey: mKey })
    }
    if (idx < 0) {
      reportDegraded('scroll_kept', { anchorId: anchor.id })
      return
    }
    el.scrollTop = Math.max(0, offsets.value[idx] - anchor.delta)
  }

  function destroy() {
    if (rafId) cancelAnimationFrame(rafId)
    rafId = 0
  }

  return { scrollFraction, seeking, onScroll, seekTo, captureAnchor, restoreAnchor, destroy }
}