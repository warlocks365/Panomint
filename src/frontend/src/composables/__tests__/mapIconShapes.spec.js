import { describe, it, expect } from 'vitest'
import {
  MAP_ICON_SHAPES,
  MAP_ICON_SHAPE_PATHS,
  isMapIconShape
} from '../mapIconShapes'

// 本次缺陷的根因是「同一份形状有两处真源，且坐标系不同」，
// 于是选择器提供的形状与地图侧渲染表**对不上**。
// 下面几条断言就是为了让这类不同源再次发生时立刻变红。

describe('地图标记形状 · 单一真源一致性', () => {
  it('选择器提供的每种形状，地图侧渲染表里都必须有（此前 triangle/diamond/star 缺失）', () => {
    MAP_ICON_SHAPES.forEach((s) => {
      expect(MAP_ICON_SHAPE_PATHS[s.key], `缺少形状：${s.key}`).toBeTruthy()
      expect(MAP_ICON_SHAPE_PATHS[s.key]).toBe(s.path)
    })
  })

  it('渲染表不得有选择器未提供的多余项（避免两处再次漂移）', () => {
    expect(Object.keys(MAP_ICON_SHAPE_PATHS).sort())
      .toEqual(MAP_ICON_SHAPES.map((s) => s.key).sort())
  })

  it('四种基础形状齐全（回归锚点：circle/triangle/diamond/star）', () => {
    expect(MAP_ICON_SHAPES.map((s) => s.key)).toEqual([
      'circle', 'triangle', 'diamond', 'star'
    ])
  })
})

describe('地图标记形状 · 无静默失效', () => {
  it('isMapIconShape 对未知形状返回 false（供调用方走回退而非静默）', () => {
    expect(isMapIconShape('circle')).toBe(true)
    expect(isMapIconShape('triangle')).toBe(true)
    expect(isMapIconShape('star')).toBe(true)
    expect(isMapIconShape('pin')).toBe(false)      // PNG 分支，不是矢量形状
    expect(isMapIconShape('custom')).toBe(false)   // 上传分支
    expect(isMapIconShape('__proto__')).toBe(false)
  })

  it('每种形状的 path 非空且成对闭合（闭合不全会让填充整个糊掉）', () => {
    MAP_ICON_SHAPES.forEach((s) => {
      expect(s.path.length).toBeGreaterThan(4)
      if (s.path.includes('<path')) {
        expect(s.path.trim().endsWith('Z"/>'), `${s.key} 未以 Z 闭合`).toBe(true)
      }
    })
  })
})

describe('地图标记形状 · 坐标系必须统一在 24×24', () => {
  it('所有 path 的坐标数值都落在 0~24 内（防止误用 18 盒坐标导致形状偏小/大小不一）', () => {
    MAP_ICON_SHAPES.forEach((s) => {
      const nums = (s.path.match(/-?\d+(\.\d+)?/g) || []).map(Number)
      expect(nums.length, `${s.key} 未解析到坐标`).toBeGreaterThan(0)
      nums.forEach((n) => {
        expect(n, `${s.key} 的坐标 ${n} 越出 24×24 盒`).toBeGreaterThanOrEqual(0)
        expect(n, `${s.key} 的坐标 ${n} 越出 24×24 盒`).toBeLessThanOrEqual(24)
      })
    })
  })

  it('各形状视觉尺寸与圆形同一量级（圆形 r=9 → 直径 18；其余外接尺寸应在 16~20）', () => {
    const outerSize = (path) => {
      const nums = (path.match(/-?\d+(\.\d+)?/g) || []).map(Number)
      // 圆形是 cx/cy/r 三元组，其余是 path 的 x 跨度
      if (path.includes('<circle')) return nums[2] * 2
      return Math.max(...nums) - Math.min(...nums)
    }
    const base = outerSize(MAP_ICON_SHAPE_PATHS.circle)
    MAP_ICON_SHAPES.forEach((s) => {
      const size = outerSize(s.path)
      expect(size, `${s.key} 尺寸 ${size} 与圆形 ${base} 相差过大`).toBeGreaterThanOrEqual(base * 0.85)
      expect(size, `${s.key} 尺寸 ${size} 与圆形 ${base} 相差过大`).toBeLessThanOrEqual(base * 1.15)
    })
  })
})