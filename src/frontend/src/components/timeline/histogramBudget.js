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
 *
 * 【为什么首屏就预取三档 —— 别急着优化，先看真实代价】
 * 实测 10 年（3650 个 day 桶）的响应体积：
 *   - 原始 JSON **124.3 KB**（超出直觉，但桶键高度重复，压缩率极佳）
 *   - **gzip 14.2 KB**（压缩率 11.4%）／ brotli 6.0 KB
 * 自托管 nginx 默认开 gzip，故线上实际传输约 **14 KB**，
 * 量级约等于一张缩略图 JPEG。对照：120 条 month 桶 3.9 KB、10 条 year 桶 311 B，
 * 三档合计 gzip 后约 18 KB。
 *
 * 换来的好处是 AC-04「切档零网络请求」。曾评估过「首屏只取 year+month，
 * day 桶首次打开日历时按需取」——**已评估并否决**，理由不是体积而是：
 * 日档下滑块的 bucketRange 依赖桶数组，按需取会让用户首次打开日历之前
 * 滑块没有任何数据（空滑块），用功能缺失换 14 KB 不划算。
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
 *
 * 【防御性排序 —— 为什么不在此处断言输入已升序】
 * 后端契约确实是 `ORDER BY 1 ASC`（`internal/media/histogram.go:42`），
 * 桶键定长零填充，故字符串序 == 时间序。但**依赖契约不做校验**的风险是：
 * 一旦后端排序变更（或将来引入缓存/合并逻辑打乱顺序），`slice(-limit)`
 * 会静默取到**最旧**的一批 —— 即最坏的那个 bug 原地复活，且开发期毫无征兆。
 * 而断言只在开发环境提醒，生产环境照样静默反向。
 * 排序 3650 个元素约 1 ms × 3 档，挂载时一次，代价可忽略，故直接排。
 * 排完取末 N 个：**与输入顺序无关**，无论后端给什么顺序都拿到最新。
 */
export function keepNewest(buckets, limit) {
  const list = Array.isArray(buckets) ? buckets : []
  // limit 非正数一律返回空：把「非法上限」解释成「无上限」会让调用方的一个笔误
  // （传 0 期望清空）反而拿到全量数据，症状是「怎么限制都没用」。
  if (!(limit > 0)) return []
  // 只在**有效桶**里取最新 N 个：无效桶沉底后若直接 slice(-limit)，
  // 沉在末尾的无效桶会正好落进窗口，把真桶挤掉（数字键更是会排到日期键之后）。
  const sorted = sortedByKey(list)
  const validCount = sorted.length - sorted.filter((b) => !b || typeof b.bucket !== 'string').length
  if (validCount <= limit) return sorted
  return sorted.slice(validCount - limit, validCount)
}

/**
 * 按桶键升序排（复制原数组，不改调用方数据）。
 *
 * 桶键定长零填充（`2024` / `2024-06` / `2024-06-14`）故字符串序 == 时间序，
 * 无需解析成 Date——那会慢一个数量级且引入时区风险。
 *
 * 【无效桶必须沉底，不能靠「当空字符串排」】
 * 直觉写法是`ka = ''` 然后正常比较，但那样 `{bucket: 123}` 这类数字键会
 * 排在所有日期键**之后**（`'2024-06-14' < '123'` 为真 → 数字键更大），
 * 于是它会落进「保留最新 N 个」的窗口，把一个真桶挤出去。
 * 故显式判类型：非字符串键一律排到末尾之后。
 */
function sortedByKey(list) {
  const valid = []
  const invalid = []
  for (const b of list) {
    if (b && typeof b.bucket === 'string') valid.push(b)
    else invalid.push(b) // null / 非字符串键：沉底，不参与时间序
  }
  valid.sort((a, b) => (a.bucket < b.bucket ? -1 : a.bucket > b.bucket ? 1 : 0))
  return invalid.length ? valid.concat(invalid) : valid
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
  // 同样防御性排序：本函数用 list[0] 当最旧、list[last] 当最新来算分箱边界，
  // 若输入乱序则整条时间轴的刻度定位都会错（且不报错）。见 keepNewest 的说明。
  const list = sortedByKey(
    (Array.isArray(buckets) ? buckets : []).filter((b) => b && typeof b.bucket === 'string')
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