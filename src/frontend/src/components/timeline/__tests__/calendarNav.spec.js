import { describe, it, expect } from 'vitest'
import {
  monthIndexOf,
  yearMonthOf,
  shiftMonthSafe,
  daysInMonth,
  isLeapYear,
  isValidDayKey,
  clampYear,
  yearOptions,
  yearHasAnyPhoto
} from '../calendarNav'

describe('月序号 ↔ 年月互转', () => {
  it('往返一致（含跨年边界）', () => {
    ;[
      [2024, 1], [2024, 12], [2025, 1], [2023, 6], [2000, 11]
    ].forEach(([y, m]) => {
      expect(yearMonthOf(monthIndexOf(y, m))).toEqual({ year: y, month: m })
    })
  })

  it('2024-12 的下一个月是 2025-1', () => {
    expect(yearMonthOf(monthIndexOf(2024, 12) + 1)).toEqual({ year: 2025, month: 1 })
  })

  it('2024-1 的上一个月是 2023-12', () => {
    expect(yearMonthOf(monthIndexOf(2024, 1) - 1)).toEqual({ year: 2023, month: 12 })
  })
})

describe('跨年步进 · 负数取模（本次修复的既有崩溃点）', () => {
  it('1 月往前退到去年 12 月（旧实现会算出 month=0）', () => {
    expect(shiftMonthSafe(2024, 1, -1)).toEqual({ year: 2023, month: 12 })
  })

  it('连续后退 14 个月不产生非法月份', () => {
    let y = 2025, m = 3
    for (let i = 0; i < 14; i++) {
      const r = shiftMonthSafe(y, m, -1)
      expect(r.month).toBeGreaterThanOrEqual(1)
      expect(r.month).toBeLessThanOrEqual(12)
      y = r.year; m = r.month
    }
    // 2025-03 后退 12 个月 = 2024-03，再退 2 个月 = 2024-01
    expect({ year: y, month: m }).toEqual({ year: 2024, month: 1 })
  })

  it('向前推进 25 个月跨两年', () => {
    expect(shiftMonthSafe(2024, 6, 25)).toEqual({ year: 2026, month: 7 })
  })

  it('后一年的 12 月再后退一位得到 11 月（不回绕到下一年）', () => {
    expect(shiftMonthSafe(2025, 1, -2)).toEqual({ year: 2024, month: 11 })
  })
})

describe('月天数与闰年', () => {
  it('平年 / 闰年 2 月天数', () => {
    expect(daysInMonth(2023, 2)).toBe(28)
    expect(daysInMonth(2024, 2)).toBe(29)
    expect(daysInMonth(1900, 2)).toBe(28)   // 整百年非闰年
    expect(daysInMonth(2000, 2)).toBe(29)   // 400 年倍数是闰年
    expect(daysInMonth(2100, 2)).toBe(28)
  })

  it('isLeapYear 与月天数一致', () => {
    expect(isLeapYear(2024)).toBe(true)
    expect(isLeapYear(2023)).toBe(false)
    expect(isLeapYear(1900)).toBe(false)
    expect(isLeapYear(2000)).toBe(true)
  })

  it('各月天数（抽样大小月）', () => {
    expect(daysInMonth(2024, 1)).toBe(31)
    expect(daysInMonth(2024, 4)).toBe(30)
    expect(daysInMonth(2024, 12)).toBe(31)
  })
})

describe('日期合法性 · 非法日期必须被拒', () => {
  it('2 月 30 日 / 31 日被拒（平闰两年都拒）', () => {
    expect(isValidDayKey('2023-02-29')).toBe(false)
    expect(isValidDayKey('2023-02-30')).toBe(false)
    expect(isValidDayKey('2024-02-30')).toBe(false)
    expect(isValidDayKey('2024-02-31')).toBe(false)
  })

  it('2 月 29 日仅闰年合法', () => {
    expect(isValidDayKey('2024-02-29')).toBe(true)
    expect(isValidDayKey('2023-02-29')).toBe(false)
  })

  it('4/6/9/11 月 31 日被拒', () => {
    expect(isValidDayKey('2024-04-31')).toBe(false)
    expect(isValidDayKey('2024-06-31')).toBe(false)
    expect(isValidDayKey('2024-09-31')).toBe(false)
    expect(isValidDayKey('2024-11-31')).toBe(false)
  })

  it('月 0 / 月 13 / 日 0 / 日越界被拒', () => {
    expect(isValidDayKey('2024-00-10')).toBe(false)
    expect(isValidDayKey('2024-13-10')).toBe(false)
    expect(isValidDayKey('2024-01-00')).toBe(false)
    expect(isValidDayKey('2024-01-32')).toBe(false)
  })

  it('格式不符与非字符串被拒', () => {
    expect(isValidDayKey('2024-1-1')).toBe(false)
    expect(isValidDayKey('unknown')).toBe(false)
    expect(isValidDayKey('20240101')).toBe(false)
    expect(isValidDayKey(null)).toBe(false)
    expect(isValidDayKey(undefined)).toBe(false)
  })

  it('合法日期通过（跨月天数边界各抽一个）', () => {
    expect(isValidDayKey('2024-01-31')).toBe(true)
    expect(isValidDayKey('2024-02-29')).toBe(true)
    expect(isValidDayKey('2024-04-30')).toBe(true)
    expect(isValidDayKey('2023-02-28')).toBe(true)
    expect(isValidDayKey('2024-12-31')).toBe(true)
  })
})

describe('年份选择器 · 范围与夹取', () => {
  it('clampYear 夹到边界内', () => {
    expect(clampYear(2020, 2022, 2026)).toBe(2022)
    expect(clampYear(2030, 2022, 2026)).toBe(2026)
    expect(clampYear(2024, 2022, 2026)).toBe(2024)
  })

  it('max < min（无照片）时返回 min，不产生非法区间', () => {
    expect(clampYear(2024, 2026, 2022)).toBe(2026)
  })

  it('yearOptions 首尾对齐到 10 的倍数并含上下整档', () => {
    const opts = yearOptions(2023, 2026)
    expect(opts[0]).toBe(2020)
    expect(opts[opts.length - 1]).toBe(2030)
    expect(opts).toContain(2023)
    expect(opts).toContain(2026)
  })

  it('yearOptions 在 min > max 或非数字时返回空数组而非抛错', () => {
    expect(yearOptions(2026, 2022)).toEqual([])
    expect(yearOptions(NaN, 2022)).toEqual([])
  })

  it('yearHasAnyPhoto 按月序号判定年份是否可跳', () => {
    const months = [monthIndexOf(2024, 5), monthIndexOf(2024, 6), monthIndexOf(2026, 1)]
    expect(yearHasAnyPhoto(months, 2024)).toBe(true)
    expect(yearHasAnyPhoto(months, 2026)).toBe(true)
    expect(yearHasAnyPhoto(months, 2025)).toBe(false)
  })
})