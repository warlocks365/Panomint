import { computed } from 'vue'
import {
  HEADER_HEIGHT,
  buildHeaderLookup,
  headerKeyOf,
  headerLabelOf,
  monthKeyOf
} from './timelineDimensions'

// 时间轴连续流分组（Job000088 拆解，Job000145 参数化为三档维度）——
// 按 taken_at（缺省 created_at）内联**当前档位唯一**的分组标题，满一行（cols 格）即 flush；
// offsets 为每个流元素的精确纵向偏移（行高与标题高度均为定值），供锚点定位按 offset 精确命中；
// headerIndex 为「该档位锚点键 → 流索引」，供 seek 跳转。
//
// 语义铁律（ADR-001 已纠正的初稿误读）：档位即**唯一可见**的标题粒度。
// year 档不产出 month/day 标题，month 档不产出 day 标题，day 档只产出 day 标题。
// 若改为「三档全渲染」，会导出「年档标题比日档更多」的自相矛盾结果，且违反 AC-01。
//
// 标题高度取自 timelineDimensions.HEADER_HEIGHT（单一真源），本文件不再自带魔数。

export function itemDate(m) {
  const t = m.taken_at || m.created_at
  if (!t) return null
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? null : d
}

export function useTimelineGrouping(items, cols, rowHeight, dimension) {
  const flat = computed(() => {
    const dim = dimension.value
    const out = []
    let cells = []
    let lastKey = ''

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
      const key = headerKeyOf(dim, d)
      if (key !== lastKey) {
        flushRow()
        out.push({
          __key: `h:${dim}:${key}`,
          header: true,
          level: dim,
          anchorKey: key,
          // monthKey 保留：日历降级定位与「同月首项」兜底按月键查，且与现状兼容
          monthKey: d ? monthKeyOf(d) : key,
          label: headerLabelOf(dim, d),
          ts: d ? d.getTime() : 0
        })
        lastKey = key
      }
      cells.push(m)
      if (cells.length === cols.value) flushRow()
    }
    flushRow()
    return out
  })

  /* 每个流元素的精确纵向偏移（行高与标题高度均为定值，与 cols 无关——offsets 不随列数漂移） */
  const offsets = computed(() => {
    const arr = new Array(flat.value.length)
    let y = 0
    for (let i = 0; i < flat.value.length; i++) {
      arr[i] = y
      const it = flat.value[i]
      y += it.header ? HEADER_HEIGHT[it.level] ?? HEADER_HEIGHT.month : rowHeight.value
    }
    return arr
  })

  /* 当前档位锚点键 → 流索引（精确 O(1) + 升序键数组供兜底二分） */
  const headerIndex = computed(() => buildHeaderLookup(flat.value))

  return { flat, offsets, headerIndex }
}