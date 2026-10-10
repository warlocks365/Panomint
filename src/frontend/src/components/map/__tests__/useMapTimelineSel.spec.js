import { describe, expect, it } from 'vitest'
import { reactive, ref } from 'vue'
import { useMapTimelineSel } from '../useMapTimelineSel'

// 地图时间轴「范围选择」能力验证（需求符合性）
//
// 背景：useMapTimelineSel 实现了完整的拖拽框选状态机（onDown → onMove → onUp），
// 但 MapTimeline.vue 模板从未绑定 onMove（第 35 行绑的是onTrackMove，那是 hover 用）。
// 本组用例区分两件事：
//   (A) 逻辑层的范围选择是否可用；
//   (B) 组件层是否真的把它接出来了。

function makeBuckets(granularity) {
  // 12 个等距桶，count 递增，便于断言
  const out = []
  for (let i = 1; i <= 12; i++) {
    let bucket
    if (granularity === 'year') bucket = String(2000 + i)
    else if (granularity === 'day') bucket = `2024-01-${String(i).padStart(2, '0')}`
    else bucket = `2024-${String(i).padStart(2, '0')}`
    out.push({ bucket, count: i, photos: i, videos: 0, pano_photos: 0, pano_videos: 0 })
  }
  out.push({ bucket: 'unknown', count: 99, photos: 99, videos: 0, pano_photos: 0, pano_videos: 0 })
  return out
}

function mount(granularity = 'month', range = null) {
  const emitted = []
  // 🔴 必须传响应式对象本身，不能传 ref：组合式函数内部按 props.buckets 直接读，
  // 传 ref 会让 props.buckets 变undefined（记忆坑：跨包照抄写法）。
  const props = reactive({
    buckets: makeBuckets(granularity),
    range,
    loading: false,
    granularity,
    places: [],
    position: 'bottom'
  })
  const trackRef = ref({
    getBoundingClientRect: () => ({ left: 0, width: 1200 }),
    setPointerCapture: () => {}
  })
  const emit = (ev, payload) => emitted.push({ ev, payload })
  const sel = useMapTimelineSel(props, emit, trackRef)
  // clientX → 桶索引：indexAt 用 rect 换算，width=1200 / 12 桶 = 每桶 100px
  const xOf = (i) => Math.round((i + 0.5) * 100)
  return { sel, emitted, props, xOf }
}

describe('地图时间轴 · 范围选择（起止区间）', () => {
  it('bars 过滤掉 unknown 桶且按 key 升序', () => {
    const { sel } = mount()
    expect(sel.bars.value).toHaveLength(12)
    expect(sel.bars.value.map((b) => b.key)).not.toContain('unknown')
    expect(sel.bars.value[0].key).toBe('2024-01')
    expect(sel.bars.value[11].key).toBe('2024-12')
  })

  it('单击（位移< 6px）→ 单桶选择，range 的 from/to 同月', () => {
    const { sel, emitted, xOf } = mount()
    const x = xOf(3)
    sel.onDown({ clientX: x, pointerId: 1 })
    sel.onMove({ clientX: x + 2 })          // 未达阈值
    sel.onUp()
    expect(emitted).toHaveLength(1)
    const { from, to } = emitted[0].payload
    expect(from.slice(0, 7)).toBe('2024-04')
    expect(to.slice(0, 7)).toBe('2024-04')
  })

  it('拖拽（位移 > 6px）→ 范围选择，from/to 覆盖起止桶', () => {
    const { sel, emitted, xOf } = mount()
    sel.onDown({ clientX: xOf(2), pointerId: 1 })
    for (let x = xOf(2) + 20; x <= xOf(8); x += 40) sel.onMove({ clientX: x })
    sel.onUp()
    expect(emitted).toHaveLength(1)
    const { from, to } = emitted[0].payload
    expect(from.slice(0, 7)).toBe('2024-03')   // 第 3 桶
    expect(to.slice(0, 7)).toBe('2024-09')     // 第 9 桶
  })

  it('反向拖拽（从右到左）也能成范围，且 from<=to 归一化', () => {
    const { sel, emitted, xOf } = mount()
    sel.onDown({ clientX: xOf(9), pointerId: 1 })
    for (let x = xOf(9) - 20; x >= xOf(4); x -= 40) sel.onMove({ clientX: x })
    sel.onUp()
    const { from, to } = emitted[0].payload
    expect(from.slice(0, 7)).toBe('2024-05')
    expect(to.slice(0, 7)).toBe('2024-10')
    expect(from <= to).toBe(true)   // 关键：不能出现 from > to
  })

  it('拖拽中高亮区间实时跟随（inRange 反映 pending 选区）', () => {
    const { sel, xOf } = mount()
    sel.onDown({ clientX: xOf(1), pointerId: 1 })
    sel.onMove({ clientX: xOf(1) + 20 })
    sel.onMove({ clientX: xOf(5) })
    // 拖拽中 active 走 pending sel（索引 1..5）
    expect(sel.inRange(1)).toBe(true)
    expect(sel.inRange(5)).toBe(true)
    expect(sel.inRange(6)).toBe(false)
  })

  it('未达拖拽阈值时高亮保持单桶（不扩成范围）', () => {
    const { sel, xOf } = mount()
    sel.onDown({ clientX: xOf(4), pointerId: 1 })
    sel.onMove({ clientX: xOf(4) + 3 })   // 浮点抖动量级
    expect(sel.inRange(4)).toBe(true)
    expect(sel.inRange(5)).toBe(false)
  })

  it('rangeLabel 区分单选与范围', () => {
    const { sel, emitted, props, xOf } = mount()
    expect(sel.rangeLabel.value).toBe('全部时间')
    // 单选：emit 后由宿主把 range 回填进 props（真实链路如此），
    // committedIdx 再据此反推高亮 —— 这条断言的正是「回填后单选显示单键」。
    sel.onDown({ clientX: xOf(5), pointerId: 1 }); sel.onUp()
    props.range = emitted.at(-1).payload
    expect(sel.rangeLabel.value).toBe('2024-06')          // 单选 = 单键
    emitted.length = 0
    props.range = null
    sel.onDown({ clientX: xOf(2), pointerId: 2 })
    for (let x = xOf(2) + 20; x <= xOf(7); x += 40) sel.onMove({ clientX: x })
    sel.onUp()
    props.range = emitted.at(-1).payload
    expect(sel.rangeLabel.value).toBe('2024-03 ~ 2024-08') // 范围 = 起 ~ 止
  })

  it('已提交 range 反推高亮（committedIdx）：跨多桶区间正确', () => {
    const { sel } = mount('month', { from: '2024-03-01T00:00:00.000Z', to: '2024-08-31T23:59:59.999Z' })
    expect(sel.hasRange.value).toBe(true)
    expect(sel.inRange(2)).toBe(true)   // 2024-03
    expect(sel.inRange(7)).toBe(true)   // 2024-08
    expect(sel.inRange(8)).toBe(false)  // 2024-09
    expect(sel.inRange(1)).toBe(false)  // 2024-02
  })

  it('日粒度同样支持范围（不是只有月粒度可以）', () => {
    const { sel, emitted, xOf } = mount('day')
    sel.onDown({ clientX: xOf(3), pointerId: 1 })
    for (let x = xOf(3) + 20; x <= xOf(9); x += 40) sel.onMove({ clientX: x })
    sel.onUp()
    const { from, to } = emitted[0].payload
    expect(from.slice(0, 10)).toBe('2024-01-04')
    expect(to.slice(0, 10)).toBe('2024-01-10')
  })

  it('年粒度同样支持范围', () => {
    const { sel, emitted, xOf } = mount('year')
    sel.onDown({ clientX: xOf(1), pointerId: 1 })
    for (let x = xOf(1) + 20; x <= xOf(6); x += 40) sel.onMove({ clientX: x })
    sel.onUp()
    const { from, to } = emitted[0].payload
    expect(from.slice(0, 4)).toBe('2002')
    expect(to.slice(0, 4)).toBe('2007')
  })

  it('statTotals 随选区变化（范围时只统计区间内）', () => {
    const { sel, xOf } = mount()
    const totalAll = sel.statTotals.value.photos
    expect(totalAll).toBe((12 * 13) / 2)      // 1..12 求和 = 78
    sel.onDown({ clientX: xOf(2), pointerId: 1 })
    for (let x = xOf(2) + 20; x <= xOf(5); x += 40) sel.onMove({ clientX: x })
    // 索引 2..5 → count 3+4+5+6 = 18
    expect(sel.statTotals.value.photos).toBe(18)
  })

  it('clear() 复位选区并派发 null', () => {
    const { sel, emitted } = mount()
    sel.onDown({ clientX: 200, pointerId: 1 }); sel.onUp()
    emitted.length = 0
    sel.clear()
    expect(emitted[0]).toEqual({ ev: 'change', payload: null })
    expect(sel.hasRange.value).toBe(false)
  })
})

describe('地图时间轴 · 组件层接线（回归防线）', () => {
  // 这条用例是本次缺陷的直接防线：onMove 曾因未被模板绑定而形同虚设，
  // 单测只验逻辑层通过、实机却拖不出范围。必须从组件源码层面断言绑定存在。
  it('MapTimeline.vue 轨道必须绑定 down / move / up 三件套（否则拖拽范围选择整体失效）', async () => {
    const { readFileSync } = await import('node:fs')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(
      fileURLToPath(new URL('../MapTimeline.vue', import.meta.url)), 'utf8'
    )
    expect(src).toMatch(/@pointerdown="sel\.onDown"/)
    expect(src).toMatch(/@pointerup="sel\.onUp"/)
    // move 必须走合并入口 onTrackPointerMove（它内部再调sel.onMove）
    expect(src).toMatch(/@pointermove="onTrackPointerMove"/)
  })

  it('合并入口必须同时驱动 hover 与选择（两者缺一不可）', async () => {
    const { readFileSync } = await import('node:fs')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(
      fileURLToPath(new URL('../MapTimeline.vue', import.meta.url)), 'utf8'
    )
    // 悬停气泡（onTrackMove）与选区推进（sel.onMove）都必须在同一handler 内被调用
    const m = src.match(/function onTrackPointerMove\(e\)\s*\{([\s\S]*?)\n\}/)
    expect(m, '未找到 onTrackPointerMove 合并入口').toBeTruthy()
    expect(m[1]).toMatch(/onTrackMove\(e\)/)
    expect(m[1]).toMatch(/sel\.onMove\(e\)/)
  })
})