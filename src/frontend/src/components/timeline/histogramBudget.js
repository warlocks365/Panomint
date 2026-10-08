import { bucketTs } from './timelineDimensions'

// 直方图的**存储预算与呈现预算**（Job000145）。
//
// 从 timelineDimensions.js 拆出，因该文件已近 300 行组织红线。
// 按「键/标签/锚点」与「桶预算」两个关注点切开，不是为凑行数——
// 这两者��变更节奏完全不同：前者随档位语义变，后者随渲染性能诉求变。

/**
 * 直方图**存储**上限（安全阀，非性能预算）。
 *
 * 与 TICK_LIMIT 的区别是本模块最容易踩的坑，故写清：
 *   - 本值管「存多少」：日历的日期集合、bucketRange 的时间范围都要用**完整**数据，
 *     截断会直接破坏「最近日期没有圆点」与「滑块范围错位」；
 *   - TICK_LIMIT 管「画多少」：只是 DateSlider 的 DOM 节点数上限，呈现层裁剪。
 *
 * 上界取 20000：day 档约 3660/年，20000 桶覆盖约 5.5 年跨度，
 * 正常家庭相册（≤20 年）不会触发；真触发时保 newest（见 keepNewest）。
 */
export const HISTOGRAM_MAX_BUCKETS = 20000

/**
 * 直方图**渲染**上限（DOM 节点预算）。
 * 桶数超限时 selectTicks 按时间分箱聚合，而非截断——见该函数注释。
 */
export const TICK_LIMIT = 400

/**
 * 超限时**保留最新**的 N 个桶。
 *
 * 必须保留最新而不是最旧 —— 这是本期修掉的一个真实缺陷：
 * 后端 `ORDER BY 1 ASC` 返回**升序（最旧在前）**，若按直觉写 `slice(0, N)`
 * 会只留下最旧的 N 个桶，直接造成两处静默故障：
 *   1. 日历只给最旧那段时间打「有照片」圆点，最近日期全显示为无照片；
 *   2. bucketRange 从桶推导时间范围 → 滑块范围整体错位，滚动同步与拖拽跳转都定位错。
 * 滑块 fraction 定义是「0=最新、1=最旧」，只有保 newest 才与映射一致。
 */
export function keepNewest(buckets, limit) {
  const list = Array.isArray(buckets) ? buckets : []
  // limit 非正数一律返回空：把「非法上限」解释成「无上限」会让调用方的一个笔误
  // （传 0 期望清空）反而拿到全量数据，症状是「怎么限制都没用」。
  if (!(limit > 0)) return []
  if (list.length <= limit) return list
  return list.slice(-limit)
}

/**
 * 呈现层刻度选取：桶数超预算时按**时间分箱聚合**，而非截断。
 *
 * 为什么聚合而非截断：截断会在时间轴上留下一大段空白，密度条的形状也失真
 * （用户看到「这几个月没照片」，其实是没取数据）。聚合后每箱 count 求和，
 * 形状仍正确，且 DOM 节点数恒 ≤ budget。
 *
 * 分箱按**时间**而非按数组下标等分：桶本身的时间间隔不均匀
 * （month 桶有 28~31 天，day 桶有 365/366 天），按下标等分会得到宽度不均的箱。
 * 返回的每项是「箱的代表键 + 合计 count + 箱中点 ts」，供 DateSlider 直接渲染。
 */
export function selectTicks(dimension, buckets, budget = TICK_LIMIT) {
  const list = (Array.isArray(buckets) ? buckets : []).filter(
    (b) => b && typeof b.bucket === 'string'
  )
  if (!(budget > 0)) return []
  if (list.length <= budget) return list
  const oldestTs = bucketTs(dimension, list[0].bucket)
  const newestTs = bucketTs(dimension, list[list.length - 1].bucket)
  const span = Math.max(1, newestTs - oldestTs)
  const width = span / budget
  const bins = new Array(budget).fill(0)
  // 箱的代表时刻用**箱的中点**而非箱内某个桶的键：若取箱内最后一个桶，
  // 首箱会落在 oldestTs+width 处，让时间轴开头凭空少掉一箱（视觉上像「更早没数据」）
  const centers = new Array(budget).fill(0)
  for (const b of list) {
    const i = Math.min(budget - 1, Math.floor((bucketTs(dimension, b.bucket) - oldestTs) / width))
    bins[i] += b.count || 0
    centers[i] = oldestTs + (i + 0.5) * width
  }
  const out = []
  for (let i = 0; i < budget; i++) {
    if (!centers[i]) continue
    // key 用箱内最后一个桶的键（仅作 v-for 的稳定 key 与调试用），ts 才是定位依据
    out.push({ bucket: lastKeyOfBin(dimension, list, oldestTs, width, i), count: bins[i], ts: centers[i] })
  }
  return out
}

/** 找出第 i 箱内最后一个桶的键（倒序遍历，命中即止）。 */
function lastKeyOfBin(dimension, list, oldestTs, width, index) {
  const lo = oldestTs + index * width
  const hi = lo + width
  for (let k = list.length - 1; k >= 0; k--) {
    const ts = bucketTs(dimension, list[k].bucket)
    if (ts >= lo && ts < hi) return list[k].bucket
  }
  return ''
}