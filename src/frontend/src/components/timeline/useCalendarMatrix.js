import { computed, ref, watch } from 'vue'
import { buildMonthMatrix, headerKeyOf, pad2 } from './timelineDimensions'
import {
  monthIndexOf,
  yearMonthOf,
  shiftMonthSafe,
  yearOptions,
  yearHasAnyPhoto,
  daysInMonth
} from './calendarNav'

// 日历月矩阵的**纯状态层**（Job000145）。从 DatePickerPopover 拆出——连同四态样式，
// 该组件会超 300 行组织红线（frontend_org_guard O1）。
//
// 这里只管「当前看哪个月 / 每格处于四态中的哪一态 / 能不能点」，不碰 DOM，
// 故可与 timelineDimensions 一起被单测表驱动覆盖。
//
// 四态（与设计文档 §2.3 一致）：
//   selectable 有照片且非未来 → 实底数字 + 主色圆点
//   empty     过去但无照片   → --color-text-secondary 灰显 + aria-disabled
//   future    未来日期       → --color-text-disabled + disabled（WCAG 1.4.3 失效组件例外）
//   out       跨月补位格     → 键为空，天然不可定位
// 另叠加 today（底色区分）与 anchor（唯一实底态）两个正交修饰。

const WEEK = ['一', '二', '三', '四', '五', '六', '日']

export function useCalendarMatrix(buckets) {
  const now = new Date()
  const cursorYear = ref(now.getFullYear())
  const cursorMonth = ref(now.getMonth() + 1)
  const pickedKey = ref('')
  const cursorTouched = ref(false)  // 用户手动导航过就别再自动改落点
  const todayKey = headerKeyOf('day', now)

  // 桶键 → 计数。用 Map 而非每次遍历数组：42 格 × 桶数在 day 档可达数千次查找
  const counts = computed(() => {
    const map = new Map()
    for (const b of buckets.value || []) {
      if (b && typeof b.bucket === 'string' && b.bucket !== 'unknown') map.set(b.bucket, b.count || 0)
    }
    return map
  })

  // 允许导航的范围：最早有照片的月份 → 当前月。不允许翻到未来的空月份。
  //
  // ⚠️ 全部走 monthIndexOf（y*12+(m-1)）这一套编码。
  // 旧实现用的是 `y*12+m`（m 为 1-based，一月 = y*12+1），而 calendarNav 的
  // monthIndexOf 用 y*12+(m-1)。两套并存会让 canPrev / maxIndex 的比较**整体错位一个月**，
  // 且错位不报错、只表现为「最早/最新月份多翻或少翻一次」，极难察觉。故在此统一。
  const minIndex = computed(() => {
    let min = null
    for (const k of counts.value.keys()) {
      const y = Number(k.slice(0, 4))
      const m = Number(k.slice(5, 7))
      if (!Number.isFinite(y) || !Number.isFinite(m) || m < 1 || m > 12) continue
      const idx = monthIndexOf(y, m)
      if (min === null || idx < min) min = idx
    }
    return min
  })

  const cursorIndex = computed(() => monthIndexOf(cursorYear.value, cursorMonth.value))

  const maxIndex = computed(() => monthIndexOf(now.getFullYear(), now.getMonth() + 1))

  const canPrev = computed(() => minIndex.value !== null && cursorIndex.value > minIndex.value)

  const canNext = computed(() => cursorIndex.value < maxIndex.value)

  /**
   * 最近一个**有照片**的月份（<= 当前月），没有则null。
   *
   * 用途：面板默认落点。实测场景——账号最新照片停在 2026-09，而面板默认开在当月
   * 2026-10，于是整屏灰 + 「这个月没有照片」，年份下拉里除当前年外全部禁用，
   * **用户看到的就是「点不动」，其实是落点选错了**。
   */
  const nearestPhotoIndex = computed(() => {
    if (!counts.value.size) return null
    let best = null
    for (const k of counts.value.keys()) {
      const y = Number(k.slice(0, 4))
      const m = Number(k.slice(5, 7))
      if (!Number.isFinite(y) || !Number.isFinite(m) || m < 1 || m > 12) continue
      const idx = monthIndexOf(y, m)
      if (idx > maxIndex.value) continue // 未来月份不参与
      if (best === null || idx > best) best = idx
    }
    return best
  })

  /** 把游标落到最近有照片的月份；已在该月或无数据时返回 false（不空转）。 */
  function jumpToNearestPhoto() {
    if (nearestPhotoIndex.value === null) return false
    const r = yearMonthOf(nearestPhotoIndex.value)
    if (r.year === cursorYear.value && r.month === cursorMonth.value) return false
    cursorYear.value = r.year
    cursorMonth.value = r.month
    pickedKey.value = ''
    return true
  }

  /**
   * 落点自动定位：**由 counts 驱动**，不依赖组件层 watch 时机。
   *
   * 为什么放在这里：直方图是异步到达的，而组件挂载那一刻 buckets 往往还是空的。
   * 监听 counts（随 buckets 变化而换新 Map，身份变化可靠）比监听 props.buckets
   * 的数组引用更稳 —— 后者在父级把 reactive 数组原地更新时引用不变，watch 永不触发
   * （这正是上一版「定位不生效」的真实原因之一）。
   *
   * once 语义：只在「尚未有游标」时落点一次，之后交给用户手动导航，
   * 否则用户选好月份后被数据更新抢回去。
   */
  watch(counts, () => {
    if (counts.value.size && !cursorTouched.value) jumpToNearestPhoto()
  }, { immediate: true })

  const cells = computed(() =>
    buildMonthMatrix(cursorYear.value, cursorMonth.value).map((c) => {
      const future = c.ts > now.getTime()
      const hasPhoto = c.key ? (counts.value.get(c.key) || 0) > 0 : false
      return {
        ...c,
        future,
        hasPhoto,
        selectable: !!c.key && !future && hasPhoto,
        out: !c.inMonth,
        today: c.key === todayKey
      }
    })
  )

  /** 当前视图整月无照片——用于给出「去最近有照片的月份」提示。 */
  const currentViewEmpty = computed(
    () => counts.value.size > 0 && !cells.value.some((c) => c.inMonth && c.hasPhoto)
  )

  const hasAny = computed(() => cells.value.some((c) => c.hasPhoto))

  /** 今天有照片才把游标落到今天；无照片则保持原状（不静默跳到别的月份）。 */
  function jumpToday() {
    if (!(counts.value.get(todayKey) > 0)) return false
    cursorYear.value = now.getFullYear()
    cursorMonth.value = now.getMonth() + 1
    pickedKey.value = todayKey
    return true
  }

  function shiftMonth(delta) {
    cursorTouched.value = true
    // 走 shiftMonthSafe：旧实现 `Math.floor(idx/12)` + `(idx%12)+1` 在跨年回退时
    // 会算出 month=0/负数（JS 的 % 对负数返回负值），而年月切换必然要跨年。
    const r = shiftMonthSafe(cursorYear.value, cursorMonth.value, delta)
    cursorYear.value = r.year
    cursorMonth.value = r.month
    pickedKey.value = ''
  }

  // —— 年 / 月三级切换（Job000145 缺陷修复）——
  // 可导航范围：最早有照片的月份 → 当前月。年份同理，避免跳到没有照片的年份。
  const minYear = computed(() =>
    minIndex.value === null ? now.getFullYear() : yearMonthOf(minIndex.value).year)
  const maxYear = computed(() => now.getFullYear())

  /** 该月是否有媒体（月桶键 YYYY-MM）。 */
  function hasPhotoIn(year, month) {
    return (counts.value.get(`${year}-${pad2(month)}`) || 0) > 0
  }

  /** 该年**最小**的有媒体月份（1..12），没有则 null。 */
  function firstPhotoMonthInYear(year) {
    for (let mm = 1; mm <= 12; mm++) {
      if ((counts.value.get(`${year}-${pad2(mm)}`) || 0) > 0) return mm
    }
    return null
  }

  /** 该年是否有照片（年选择器据此标注可跳/ 无照片）。 */
  const photoYears = computed(() => {
    const s = new Set()
    for (const k of counts.value.keys()) {
      const y = Number(k.slice(0, 4))
      if (Number.isFinite(y)) s.add(y)
    }
    return s
  })

  /** 年下拉的候选：按十年对齐，首尾各留一档空档年，避免跳板突兀。 */
  const yearChoices = computed(() =>
    yearOptions(minIndex.value === null ? now.getFullYear() : minYear.value, maxYear.value))

  /** 年下拉里该年是否有照片；无照片的年份仍可选（会看到「该月无照片」），只是不可定位。 */
  function yearHasPhoto(y) {
    return photoYears.value.has(y)
  }

  /** 切到指定年（夹到可导航范围）；返回是否真的改变了年份。 */
  function setYear(y) {
    cursorTouched.value = true
    const yy = Math.min(Math.max(Math.round(y), minYear.value), maxYear.value)
    if (yy === cursorYear.value) return false
    cursorYear.value = yy
    // 跳到更早的年时，月份可能超出该年可导航范围 → 一并夹到当前月
    if (monthIndexOf(cursorYear.value, cursorMonth.value) > maxIndex.value) {
      cursorMonth.value = now.getMonth() + 1
    }
    // 该年当前月没有媒体时（实测：切到 2024 年，原来停留的 9 月无媒体），
    // 面板会显示一整月灰格，看起来像「坏了」。改为从 1 月起找该年**最小**的有媒体月份。
    if (!hasPhotoIn(cursorYear.value, cursorMonth.value)) {
      const mm = firstPhotoMonthInYear(cursorYear.value)
      if (mm !== null) cursorMonth.value = mm
    }
    pickedKey.value = ''
    return true
  }

  /** 切到指定月（夹到该年允许范围）；返回是否真的改变了月份。 */
  function setMonth(m) {
    cursorTouched.value = true
    if (!Number.isFinite(m) || m < 1 || m > 12) return false
    let yy = cursorYear.value
    let mm = Math.round(m)
    // 超出 [minIndex, maxIndex] 就把年份一起带着走，避免落到没有照片的区间
    let idx = monthIndexOf(yy, mm)
    if (idx > maxIndex.value) { yy = now.getFullYear(); mm = now.getMonth() + 1 }
    if (minIndex.value !== null && idx < minIndex.value) {
      const r = yearMonthOf(minIndex.value)
      yy = r.year; mm = r.month
    }
    if (yy === cursorYear.value && mm === cursorMonth.value) return false
    cursorYear.value = yy
    cursorMonth.value = mm
    pickedKey.value = ''
    return true
  }

  /** 年下拉里该年每个月的天数（供月选择器标注 2 月 28/29 天）。 */
  function monthLength(y, m) {
    return daysInMonth(y, m)
  }

  /** 供年选择器判断某年是否存在（避免把整页做成死数据）。 */
  const hasAnyYear = computed(() => photoYears.value.size > 0)

  /** 只接受当前视图内且可选的键，防止外部传入未来/补位格造成无效跳转。 */
  function select(key) {
    const c = cells.value.find((x) => x.key === key)
    if (c && c.selectable) pickedKey.value = key
  }

  return {
    WEEK,
    now,
    todayKey,
    cursorYear,
    cursorMonth,
    pickedKey,
    counts,
    canPrev,
    canNext,
    cells,
    hasAny,
    nearestPhotoIndex,
    jumpToNearestPhoto,
    currentViewEmpty,
    shiftMonth,
    jumpToday,
    select,
    // 年 / 月切换（Job000145）
    minIndex,
    minYear,
    maxYear,
    maxIndex,
    yearChoices,
    hasAnyYear,
    yearHasPhoto,
    firstPhotoMonthInYear,
    setYear,
    setMonth,
    monthLength,
    clearPicked: () => {
      pickedKey.value = ''
    }
  }
}