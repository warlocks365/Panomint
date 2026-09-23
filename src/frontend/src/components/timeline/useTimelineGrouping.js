import { computed } from 'vue'

// TimelineGrid 拆解（Job000088）：连续时间流分组逻辑——
// 按 taken_at（缺省 created_at）内联月/日分组标题，满一行（cols 格）即 flush；
// offsets 为每个流元素的精确纵向偏移（行高与标题高度均定值），供滑块二分定位；
// monthIndex 为「月键 → 流索引」，供滑块跳转。itemDate/pad2 供滑块端复用。
const MONTH_H = 46 // 月标题高度（含下间距，与 CSS 一致）
const DAY_H = 34 // 日标题高度（含下间距，与 CSS 一致）

export function itemDate(m) {
  const t = m.taken_at || m.created_at
  if (!t) return null
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? null : d
}

export function pad2(n) { return String(n).padStart(2, '0') }

export function useTimelineGrouping(items, cols, rowHeight) {
  const flat = computed(() => {
    const out = []
    let cells = []
    let lastMonth = ''
    let lastDay = ''

    const flushRow = () => {
      if (!cells.length) return
      const first = cells[0]
      const d = itemDate(first)
      out.push({
        __key: `row:${first.id}:${cols.value}:${out.length}`,
        header: false,
        cells,
        ts: d ? d.getTime() : 0
      })
      cells = []
    }

    for (const m of items) {
      const d = itemDate(m)
      const monthKey = d ? `${d.getFullYear()}-${pad2(d.getMonth() + 1)}` : 'unknown'
      const dayKey = d ? `${monthKey}-${pad2(d.getDate())}` : 'unknown'
      if (monthKey !== lastMonth) {
        flushRow()
        out.push({
          __key: `hm:${monthKey}`,
          header: true,
          level: 'month',
          monthKey,
          label: d ? `${d.getFullYear()} 年 ${d.getMonth() + 1} 月` : '未知日期',
          ts: d ? d.getTime() : 0
        })
        lastMonth = monthKey
        lastDay = ''
      }
      if (dayKey !== lastDay && d) {
        flushRow()
        out.push({
          __key: `hd:${dayKey}`,
          header: true,
          level: 'day',
          monthKey,
          label: `${d.getMonth() + 1} 月 ${d.getDate()} 日`,
          ts: d.getTime()
        })
        lastDay = dayKey
      }
      cells.push(m)
      if (cells.length === cols.value) flushRow()
    }
    flushRow()
    return out
  })

  /* 每个流元素的精确纵向偏移（行高与标题高度均为定值） */
  const offsets = computed(() => {
    const arr = new Array(flat.value.length)
    let y = 0
    for (let i = 0; i < flat.value.length; i++) {
      arr[i] = y
      const it = flat.value[i]
      y += it.header ? (it.level === 'month' ? MONTH_H : DAY_H) : rowHeight.value
    }
    return arr
  })

  /* 月标题 → 流索引（用于滑块跳转） */
  const monthIndex = computed(() => {
    const map = new Map()
    flat.value.forEach((it, i) => {
      if (it.header && it.level === 'month') map.set(it.monthKey, i)
    })
    return map
  })

  return { flat, offsets, monthIndex }
}
