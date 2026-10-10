/**
 * 日历导航的纯计算（年月日三级切换的算术层）。
 *
 * 与 useCalendarMatrix.js 分开：那里管「当前看哪个月 / 每格能否点」，
 * 这里只做**不含状态的算术**，故可被 vitest 直接表驱动覆盖——
 * 而年月切换最容易出的错（跨年负数取模、闰年 2 月天数、月序号↔年月互转）
 * 恰好全在这层，不测就会漏到界面上。
 */

/** 月序号（连续、可为负）↔ 年月的互转。用「月先于年」编号避免跨年时的进位错误。 */
export function monthIndexOf(year, month) {
  return year * 12 + (month - 1)
}

export function yearMonthOf(index) {
  return {
    year: Math.floor(index / 12),
    month: (((index % 12) + 12) % 12) + 1
  }
}

/**
 * 按月步进 delta 个月。
 *
 * 旧实现 `Math.floor(idx/12)` + `(idx%12)+1` 在 idx 为负时崩溃：
 *   idx=-1 → year=floor(-1/12)=-1、month=(-1%12)+1=0  ← 月份 0 不存在
 *   （JS 的 % 对负数返回负值，而 Math.floor 与 % 的语义不匹配）。
 * 跨年步进是年月切换的必经路径，故必须在此收口。
 */
export function shiftMonthSafe(year, month, delta) {
  return yearMonthOf(monthIndexOf(year, month) + delta)
}

/** 某年某月的天数（闰年 2 月 = 29）。用 Date 归一化，不手写月份表。 */
export function daysInMonth(year, month) {
  return new Date(year, month, 0).getDate()
}

/** 是否闰年：公历规则被Date 归一化内建，无需自己判断。 */
export function isLeapYear(year) {
  return daysInMonth(year, 2) === 29
}

/**
 * 校验 YYYY-MM-DD 是否为**真实存在**的日期。
 * 2 月 30 日这类会在这里被拒——注意 new Date(2024,1,30) 会被归一到 3 月 1 日
 * 而「看起来合法」，所以必须比对回填后的键，而不是只判断能否构造。
 */
export function isValidDayKey(key) {
  if (typeof key !== 'string') return false
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(key)
  if (!m) return false
  const [, ys, ms, ds] = m
  const y = Number(ys), mo = Number(ms), d = Number(ds)
  if (mo < 1 || mo > 12) return false
  if (d < 1 || d > daysInMonth(y, mo)) return false
  return true
}

/** 把年份夹到 [min, max]；min > max（无可用范围）时返回 min，避免出现非法区间。 */
export function clampYear(year, min, max) {
  if (max < min) return min
  return Math.min(Math.max(year, min), max)
}

/**
 * 生成年选择器的年份序列：首尾各对齐到**10 的倍数**（十年一档），含 min/max 之外的空档年
 * （让「点得到但没照片」成为可能，而不是直接缺失该年导致跳板突兀）。
 *
 * 注意： 对齐基准是 10（十年），不是 span：曾误写成 `Math.floor(minYear/span)*span`，
 * span=12 时 2023 得到 2016——那是「月序号」的对齐方式，用在年份上毫无意义。
 */
export function yearOptions(minYear, maxYear) {
  if (!Number.isFinite(minYear) || !Number.isFinite(maxYear) || maxYear < minYear) return []
  const start = Math.floor(minYear / 10) * 10
  const end = Math.ceil(maxYear / 10) * 10
  const out = []
  for (let y = start; y <= end; y++) out.push(y)
  return out
}

/** 该年是否至少有一个月落在可导航范围内（用于年选择器里标注「有照片」）。 */
export function yearHasAnyPhoto(monthIndexes, year) {
  for (const idx of monthIndexes) {
    if (yearMonthOf(idx).year === year) return true
  }
  return false
}