import { ref, watch } from 'vue'
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

  /**
   * 挂起的定位请求（目标档位尚未生效时暂存，档位到位后自动补发）。
   *
   * 为什么必须由**这里**等，而不是让调用方等：
   * 档位真源是 props.dimension（TimelineView 持有），而调用方
   * （useTimelineDimensionPanel）手里只有 useTimelineDimension 的**内部 ref**——
   * 内部 ref 先变、props 后变。若调用方在内部 ref 变化时就补发，
   * 这里读到的 dimension.value 仍是旧档位，守卫会把请求丢掉
   * （实测：跳转 2024-11-03 报 dimension-mismatch want=day current=month）。
   * 本 composable 拿的是 props 派生的权威 dimension，由它来等才不会抢跑。
   */
  let pendingSeek = null

  watch(dimension, (d) => {
    if (pendingSeek && pendingSeek.dimension === d) {
      const p = pendingSeek
      pendingSeek = null
      // 下一帧再执行，确保 props 变更已完成子组件取数与渲染
      requestAnimationFrame(() => seekTo(p, pendingScrollEl))
    }
  })

  /** 最近一次 seekTo 收到的滚动容器（供挂起请求补发时复用）。 */
  let pendingScrollEl = null

  /**
   * 锚点跳转。载荷必须是 { dimension, key } 对象——靠字符串长度推断档位会让
   * 「档位不匹配」静默走错分支（ADR-001 记录的踩坑），对象载荷让它显式失败。
   */
  async function seekTo(payload, scrollEl) {
    if (!payload || typeof payload !== 'object') return
    const { dimension: want, key } = payload
    pendingScrollEl = scrollEl
    if (want !== dimension.value) {
      // 目标档位尚未生效：挂起等它到位，而不是丢弃。上报一次降级便于观测。
      pendingSeek = payload
      reportDegraded('dimension-mismatch', { want, current: dimension.value, deferred: true })
      return
    }
    if (!isValidKey(want, key)) return
    if (seeking.value) return
    seeking.value = true
    try {
      if (!scrollEl) return
      let guard = 0
      let idx = -1
      // 以「能否命中目标分组」为终止条件，而不是用 lastLoadedKey 与 key 比字典序。
      //
      // 为什么改：切档后 items 被 reset 清空并正在重载，此时 lastLoadedKey 反映的是
      // 新档位**首屏**的末尾（较新），它与 key 的字典序比较会得出「已在范围内」的错误结论
      // （实测：跳 2024-11-03 却停在 2026-09-27），于是循环立刻 break，
      // 随后 resolveAnchorIndex 在只含首屏的 flat 里找不到目标 → 静默 return。
      // 直接问「命中了吗」把加载与定位绑死，不依赖任何推断。
      while (guard < 300) {
        idx = resolveAnchorIndex(headerIndex.value, key)
        if (idx >= 0) break
        // AC-14：在途请求不 abort —— 等它落地（响应由 pager 的代次守卫正常并入）。
        // 等待不消耗翻页预算，否则一次并发就能耗尽 300 次预算并静默定位失败。
        if (loading.value) {
          await whenSettled()
          continue
        }
        if (finished.value) break // 真的加载完了还找不到 → 放弃（不再静默，给出降级事件）
        await loadMore()
        guard++
      }
      if (idx < 0) {
        // 加载完毕仍无法定位：必须可见，不能像原来那样无声return。
        reportDegraded('anchor-not-found', { want, key, loaded: items.length })
        return
      }
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