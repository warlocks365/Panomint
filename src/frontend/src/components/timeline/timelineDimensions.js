// 时间轴三档维度的**单一真源**（Job000145）。
//
// 为什么必须有这个模块：改动前标题高度散落三处（JS 的 MONTH_H/DAY_H、CSS 的
// `.tl-header.month/.day` 的 height、DynamicScroller 的 :min-item-size），靠人工同步。
// 泛化到三档后「档位 × 层级」变二维，三处手工同步必然漂移（ADR-001 K4）。
//
// 本模块**无 Vue 依赖**（纯数据 + 纯函数），故可被 vitest 直接表驱动单测；
// useTimelineGrouping / useTimelineSeek / useTimelinePager / DateSlider /
// DatePickerPopover 全部共用，杜绝每处各写一份 pad2 与日期正则。

/** 档位枚举（白名单）。顺序即 UI 呈现顺序，也是方向键循环顺序。 */
export const DIMENSIONS = ['year', 'month', 'day']

/** 默认档位= month。现状双层标题 46/34 来自本仓，分页守卫与滑块月刻度均以月为单位，默认月档回归面最小。 */
export const DEFAULT_DIMENSION = 'month'

/** 档位 → 该档位**唯一可见**分组标题的高度（px，含下间距）。CSS 经 var(--tl-header-h) 消费同一值。 */
export const HEADER_HEIGHT = { year: 56, month: 46, day: 34 }

/**
 * 不变式（改动前最易踩的坑，务必保留本注释）：
 * DynamicScroller 的 :min-item-size 是所有流元素的**尺寸下界估计**。实际元素比它小，
 * 滚动条总高会算小、出现滚动抖动。故必须满足 `min-item-size ≤ 三档中最小标题高度`。
 * 当前最小值 = HEADER_HEIGHT.day = 34px，与 TimelineGrid.vue 的 :min-item-size="34" 一致，**无需改动**。
 * 若日后把 HEADER_HEIGHT.day 下调到 34 以下，**必须同步下调 :min-item-size**，否则切到日档即抖动。
 */
export const MIN_ITEM_SIZE = 34

/** 无拍摄日期媒体的占位键。后端 histogram 也会产出同名桶，两者必须同形才可比。 */
export const UNKNOWN_KEY = 'unknown'

const BUCKET_PATTERNS = {
  year: /^\d{4}$/,
  month: /^\d{4}-\d{2}$/,
  day: /^\d{4}-\d{2}-\d{2}$/
}

export function isDimension(v) {
  return DIMENSIONS.includes(v)
}

export function pad2(n) {
  return String(n).padStart(2, '0')
}

/** 档位→ 标题高度（px）。未知档位回落到默认档位，绝不返回 undefined（undefined 会写进 CSS 变量）。 */
export function headerHeightOf(dimension) {
  return HEADER_HEIGHT[dimension] ?? HEADER_HEIGHT[DEFAULT_DIMENSION]
}

export function bucketPatternOf(dimension) {
  return BUCKET_PATTERNS[dimension] ?? BUCKET_PATTERNS[DEFAULT_DIMENSION]
}

/**
 * 档位下的分组键 / 锚点键。**定长 + 零填充**（`2024` / `2024-06` / `2024-06-14`）——
 * 锚点兜底的「字典序 == 时间序」与翻页守卫的 `last <= key` 全依赖定长，
 * 为缩短键去掉补零会直接破坏定位，故此处不做任何「智能」压缩。
 */
export function headerKeyOf(dimension, date) {
  if (!(date instanceof Date) || Number.isNaN(date.getTime())) return UNKNOWN_KEY
  const y = date.getFullYear()
  const m = pad2(date.getMonth() + 1)
  if (dimension === 'year') return `${y}`
  if (dimension === 'day') return `${y}-${m}-${pad2(date.getDate())}`
  return `${y}-${m}`
}

/** 月键（与现状 monthKey 同形，日历的定位下界也用它）。 */
export function monthKeyOf(date) {
  return headerKeyOf('month', date)
}

/** 分组标题文案。唯一真源：与 DateSlider 拖拽气泡逐字同源。 */
export function headerLabelOf(dimension, date) {
  if (!(date instanceof Date) || Number.isNaN(date.getTime())) return '未知日期'
  const y = date.getFullYear()
  const m = date.getMonth() + 1
  if (dimension === 'year') return `${y}年`
  if (dimension === 'day') return `${y}年${m}月${date.getDate()}日`
  return `${y}年${m}月`
}

/** 键形状自证：档位与键不匹配时**显式失败**，不靠字符串长度猜档位（本项目已栽过这个坑）。 */
export function isValidKey(dimension, key) {
  return typeof key === 'string' && bucketPatternOf(dimension).test(key)
}

/** `YYYY` / `YYYY-MM` / `YYYY-MM-DD` → {y, m, d}。m、d 为 1 基。非法返回 null。 */
function parseKeyParts(key) {
  const seg = String(key).split('-').map(Number)
  if (!seg.length || seg.some((n) => !Number.isFinite(n))) return null
  if (seg.length === 1) return { y: seg[0], m: 1, d: 1 }
  if (seg.length === 2) return { y: seg[0], m: seg[1], d: 1 }
  return { y: seg[0], m: seg[1], d: seg[2] }
}

/** 桶的代表时间（滑块刻度落点用）：年→当年 7/1，月→当月 15 日，日→当日 12:00。 */
export function bucketTs(dimension, key) {
  const p = parseKeyParts(key)
  if (!p) return 0
  if (dimension === 'year') return new Date(p.y, 6, 1).getTime()
  if (dimension === 'day') return new Date(p.y, p.m - 1, p.d, 12).getTime()
  return new Date(p.y, p.m - 1, 15).getTime()
}

// 桶预算相关的常量与函数（HISTOGRAM_MAX_BUCKETS / TICK_LIMIT / keepNewest / selectTicks）
// 已拆到 histogramBudget.js —— 「键与标签」与「桶预算」是两个关注点，变更节奏不同。

/** 升序桶键（已剔除 unknown 与形状不符项）。 */
export function sortedBucketKeys(dimension, buckets) {
  const re = bucketPatternOf(dimension)
  return (Array.isArray(buckets) ? buckets : [])
    .map((b) => b && b.bucket)
    .filter((k) => typeof k === 'string' && re.test(k))
    .sort()
}

/**
 * 滑块时间范围下界：年→该年 1/1，月→该月 1 日 00:00，日→当日 00:00。
 */
function rangeFloorTs(dimension, key) {
  const p = parseKeyParts(key)
  if (!p) return 0
  if (dimension === 'year') return new Date(p.y, 0, 1).getTime()
  if (dimension === 'day') return new Date(p.y, p.m - 1, p.d).getTime()
  return new Date(p.y, p.m - 1, 1).getTime()
}

/**
 * 滑块时间范围上界：年→次年 1/1，月→次月 1 日，日→次日 00:00。
 * 取「桶的下一格起点」而非桶本身，使最旧桶与最新桶都占据非零长度（否则首尾刻度贴边不可拖）。
 */
function rangeCeilTs(dimension, key) {
  const p = parseKeyParts(key)
  if (!p) return 0
  if (dimension === 'year') return new Date(p.y + 1, 0, 1).getTime()
  if (dimension === 'day') return new Date(p.y, p.m - 1, p.d + 1).getTime()
  return new Date(p.y, p.m, 1).getTime()
}

/** 当前档位下的滑块时间范围 { oldestTs, newestTs }；无桶时全0。 */
export function bucketRange(dimension, buckets) {
  const keys = sortedBucketKeys(dimension, buckets)
  if (!keys.length) return { oldestTs: 0, newestTs: 0 }
  return {
    oldestTs: rangeFloorTs(dimension, keys[0]),
    newestTs: rangeCeilTs(dimension, keys[keys.length - 1])
  }
}

/** 时间戳 → 该档位键（拖拽落点反解）。 */
export function keyAtTs(dimension, ts) {
  const d = new Date(ts)
  return headerKeyOf(dimension, d)
}

/** 时间戳 → 滑块分数（0=最新，1=最旧）。range 为空时返回 0。 */
export function fractionAtTs(range, ts) {
  const span = Math.max(1, range.newestTs - range.oldestTs)
  if (!range.newestTs) return 0
  return Math.min(1, Math.max(0, (range.newestTs - ts) / span))
}

/** 滑块分数 → 时间戳。 */
export function tsAtFraction(range, f) {
  const span = Math.max(1, range.newestTs - range.oldestTs)
  return range.newestTs - Math.min(1, Math.max(0, f)) * span
}

/**
 * flat →锚点查找表。一次遍历同时产出：
 *   index   : Map<anchorKey, 流索引>，精确命中 O(1)
 *   keysAsc : 升序键数组，兜底二分用
 * 显式排序而非依赖 flat 的遍历顺序（flat 是新→旧倒序），兜底语义与档位无关。
 * unknown 桶不进表 —— 无拍摄日期不可定位（AC-11）。
 */
export function buildHeaderLookup(flat) {
  const index = new Map()
  const keysAsc = []
  const list = Array.isArray(flat) ? flat : []
  for (let i = 0; i < list.length; i++) {
    const it = list[i]
    if (!it || !it.header) continue
    const k = it.anchorKey
    if (typeof k !== 'string' || !k || k === UNKNOWN_KEY) continue
    index.set(k, i)
    keysAsc.push(k)
  }
  keysAsc.sort()
  return { index, keysAsc }
}

/** 任意档位的合法锚点键形状（YYYY / YYYY-MM / YYYY-MM-DD）。用于档位无关的前置拒绝。 */
const ANY_DATE_KEY = /^\d{4}(-\d{2}(-\d{2})?)?$/

/**
 * 锚点解析（三段式的第三段，纯函数、无Vue 依赖、可无框架单测）：
 *   0) 形状守卫：非日期形状的键（如字面量 'unknown'）直接 -1。
 *      必须显式挡这一层 —— 'unknown' 在字典序上大于所有数字键（'u' > '2'），
 *      若放进二分比较会「恰好命中最后一个真实分组」，把无日期媒体静默定位成某个真实日期。
 *   1) 精确命中 lookup.index
 *   2. 兜底 = 升序键里**最后一个 <= key**（字典序 == 时间序，靠定长零填充成立）
 *   3. 再兜底 = -1（目标早于所有媒体 → 调用方回到顶部）
 * 不做比例估算、不在 flat 上遍历定位。
 */
export function resolveAnchorIndex(lookup, key) {
  if (typeof key !== 'string' || !ANY_DATE_KEY.test(key)) return -1
  const exact = lookup.index.get(key)
  if (exact != null) return exact
  const ks = lookup.keysAsc
  let lo = 0
  let hi = ks.length - 1
  let found = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (ks[mid] <= key) {
      found = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  return found >= 0 ? lookup.index.get(ks[found]) : -1
}

/** 按媒体 id 找它所在行的流索引（切档后重定位锚点用）。找不到返回 -1。 */
export function findRowIndexByMediaId(flat, id) {
  const list = Array.isArray(flat) ? flat : []
  for (let i = 0; i < list.length; i++) {
    const it = list[i]
    if (!it || it.header || !Array.isArray(it.cells)) continue
    for (const c of it.cells) {
      if (c && c.id === id) return i
    }
  }
  return -1
}

/**
 * 日历月矩阵：固定 6 行 × 7 列（42 格），周一为一周之首。
 * 跨月补位格 inMonth=false 且 key=''（不可定位、不可点）。
 */
export function buildMonthMatrix(year, month) {
  const lead = (new Date(year, month - 1, 1).getDay() + 6) % 7
  const cells = []
  for (let i = 0; i < 42; i++) {
    const d = new Date(year, month - 1, i - lead + 1)
    const inMonth = d.getMonth() === month - 1 && d.getFullYear() === year
    cells.push({
      key: inMonth ? headerKeyOf('day', d) : '',
      inMonth,
      day: d.getDate(),
      ts: d.getTime()
    })
  }
  return cells
}