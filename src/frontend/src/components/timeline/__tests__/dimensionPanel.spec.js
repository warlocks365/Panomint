// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import DimensionTabs from '../DimensionTabs.vue'
import { useTimelineDimensionPanel } from '../useTimelineDimensionPanel'

// 宿主装配的三条时序不变式（AC-04/05/07/12/15）。
//
// 这些全是「不报错、只是行为悄悄错」的那类：切档多发一个请求、锚点没复位、
// 分页在途时把用户的切档操作吞掉——都不会崩，只表现为「偶尔跳错位置」。
// 故必须用测试钉住，而不是靠 review 时肉眼确认。

function makeGridStub({ loading = false, items = [{ id: 1 }] } = {}) {
  const calls = { capture: 0, restore: 0, seek: [] }
  const loadingRef = ref(loading)
  const gridRef = ref({
    items,
    // 刻意暴露成**普通值**而非 ref：defineExpose 的返回值会被 proxyRefs 包一层并自动解包，
    // 真实宿主读到的就是 boolean。若这里用 ref 暴露，测试会掩盖
    // 「生产读 .value 得undefined」这个真实 bug（AC-07 曾因此静默失效）。
    get loading() {
      return loadingRef.value
    },
    captureAnchor: () => {
      calls.capture++
      return { id: 1, delta: -12, takenAt: '2024-06-14T10:00:00Z' }
    },
    restoreAnchor: (a) => {
      calls.restore++
      calls.restoredWith = a
    },
    seekTo: (p) => calls.seek.push(p)
  })
  // 供用例直接改加载态（模拟真实宿主拿到的是解包后的值）
  gridRef.value.__setLoading = (v) => {
    loadingRef.value = v
  }
  return { gridRef, calls, loadingRef }
}

function mountPanel(stub, hasAnyMedia = ref(true)) {
  const panel = useTimelineDimensionPanel({ gridRef: stub.gridRef, hasAnyMedia })
  return mount(
    {
      components: { DimensionTabs },
      setup: () => ({ panel }),
      template: `
        <div>
          <DimensionTabs
            :dimension="panel.dim.dimension.value"
            :busy="panel.dim.switching.value"
            :disabled="panel.dim.dragLocked.value"
            @change="panel.onDimensionChange"
          />
        </div>`
    },
    { attachTo: document.body }
  )
}

// 档位存在 sessionStorage 里（会话内记忆），用例间会互相污染 ——
// 每个用例前清空，保证都以默认月档起步。这本身也是一条被测事实：
// 「刷新回默认档位」正是刻意设计（档位与分页游标强相关，持久化会组合出错误首屏锚点）。
beforeEach(() => {
  sessionStorage.clear()
})

describe('AC-04 · 切档零网络请求', () => {
  it('requestDimension 只改状态，不触碰任何取数函数（本层无 http 依赖）', () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    w.vm.panel.onDimensionChange('year')
    expect(w.vm.panel.dim.dimension.value).toBe('year')
    // 唯一被调用的 grid 能力是锚点快照/复位，seekTo 未被触发（即无翻页请求）
    expect(stub.calls.seek).toHaveLength(0)
  })

  it('连续切三档，每次都只做一次锚点快照 + 一次复位', async () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    // month → year → month → day 共 3 次真实切换（每次都不同档）
    for (const d of ['year', 'month', 'day']) {
      w.vm.panel.onDimensionChange(d)
      await nextTick()
    }
    expect(stub.calls.capture).toBe(3)
    expect(stub.calls.restore).toBe(3)
  })

  it('重复点当前档位不产生锚点操作（无谓的快照会白白错位）', () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    w.vm.panel.onDimensionChange('month') // 已是 month
    expect(stub.calls.capture).toBe(0)
    expect(stub.calls.restore).toBe(0)
  })
})

describe('AC-05 · 切档以视口顶部媒体为锚点', () => {
  it('先抓锚点再改状态，改完立即按锚点复位（顺序不可颠倒）', () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    const order = []
    stub.gridRef.value.captureAnchor = () => {
      order.push('capture')
      return { id: 7, delta: -20, takenAt: '2024-06-14T10:00:00Z' }
    }
    stub.gridRef.value.restoreAnchor = (a) => {
      order.push('restore')
      stub.calls.restoredWith = a
    }

    w.vm.panel.onDimensionChange('day')
    expect(order).toEqual(['capture', 'restore'])
    expect(stub.calls.restoredWith).toMatchObject({ id: 7, delta: -20 })
  })

  it('grid 尚无锚点（空列表/未挂载）时不抛错，只跳过复位', () => {
    const stub = makeGridStub()
    stub.gridRef.value.captureAnchor = () => null
    const w = mountPanel(stub)
    expect(() => w.vm.panel.onDimensionChange('year')).not.toThrow()
    expect(stub.calls.restore).toBe(0)
  })
})

describe('AC-07 · 分页在途时挂起切档，不静默丢弃', () => {
  it('loading 时切档不立即生效，转为 pending 且 aria-busy 置位', () => {
    const stub = makeGridStub({ loading: true })
    const w = mountPanel(stub)

    w.vm.panel.onDimensionChange('day')
    expect(w.vm.panel.dim.dimension.value).toBe('month') // 未立即生效
    expect(w.vm.panel.dim.switching.value).toBe(true) // 但也没丢
    expect(w.vm.panel.dim.pending.value).toBe('day')
  })

  it('加载完成后 flushPending 立即执行挂起的切换', async () => {
    const stub = makeGridStub({ loading: true })
    const w = mountPanel(stub)

    w.vm.panel.onDimensionChange('day')
    stub.gridRef.value.__setLoading(false)
    await nextTick()
    await nextTick()

    expect(w.vm.panel.dim.dimension.value).toBe('day')
    expect(w.vm.panel.dim.switching.value).toBe(false)
    expect(w.vm.panel.dim.pending.value).toBe('')
  })

  it('挂起期间锚点快照只在真正执行时抓一次（不重复抓）', async () => {
    const stub = makeGridStub({ loading: true })
    const w = mountPanel(stub)

    w.vm.panel.onDimensionChange('year')
    expect(stub.calls.capture).toBe(0) // 挂起阶段不抓
    stub.gridRef.value.__setLoading(false)
    await nextTick()
    await nextTick()
    expect(stub.calls.capture).toBe(1)
    expect(stub.calls.restore).toBe(1)
  })
})

describe('AC-16 · 拖拽滑块期间拒绝切档', () => {
  it('dragLocked 时 requestDimension 返回 false 且不改状态', () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    w.vm.panel.dim.setDragLocked(true)
    w.vm.panel.onDimensionChange('day')
    expect(w.vm.panel.dim.dimension.value).toBe('month')
    expect(w.vm.panel.dim.dragLocked.value).toBe(true)
  })

  it('解锁后可正常切档', () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    w.vm.panel.dim.setDragLocked(true)
    w.vm.panel.dim.setDragLocked(false)
    w.vm.panel.onDimensionChange('day')
    expect(w.vm.panel.dim.dimension.value).toBe('day')
  })
})

describe('AC-12/15 · 日历跳转', () => {
  it('已在日档时：直接发 seek 意图，不改媒体查询参数', async () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    w.vm.panel.onDimensionChange('day')
    await w.vm.$nextTick()
    w.vm.panel.toggleCalendar()
    w.vm.panel.onDaySeek({ dimension: 'day', key: '2024-06-14' })
    expect(stub.calls.seek).toHaveLength(1)
    expect(stub.calls.seek[0]).toEqual({ dimension: 'day', key: '2024-06-14' })
    expect(w.vm.panel.anchorDayKey.value).toBe('2024-06-14')
  })

  it('不在日档时：先切到日档，切档完成后补发 seek（否则定位被静默丢弃）', async () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    // 默认档位是 month，日历只会请求按日定位
    expect(w.vm.panel.dim.dimension.value).not.toBe('day')
    w.vm.panel.toggleCalendar()
    w.vm.panel.onDaySeek({ dimension: 'day', key: '2024-06-14' })
    // 此刻应已请求切档，但定位尚未发出
    expect(w.vm.panel.dim.dimension.value).toBe('day')
    expect(stub.calls.seek).toHaveLength(0)
    await w.vm.$nextTick()
    expect(stub.calls.seek).toHaveLength(1)
    expect(stub.calls.seek[0]).toEqual({ dimension: 'day', key: '2024-06-14' })
    // 锚点立即写入，不等定位完成
    expect(w.vm.panel.anchorDayKey.value).toBe('2024-06-14')
  })

  it('跳转后自动关闭日历', () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    w.vm.panel.toggleCalendar()
    expect(w.vm.panel.calendarOpen.value).toBe(true)
    w.vm.panel.onDaySeek({ dimension: 'day', key: '2024-06-14' })
    expect(w.vm.panel.calendarOpen.value).toBe(false)
  })

  it('无媒体时开日历被拒（AC-08）', () => {
    const stub = makeGridStub({ items: [] })
    const w = mountPanel(stub, ref(false))
    w.vm.panel.toggleCalendar()
    expect(w.vm.panel.calendarOpen.value).toBe(false)
  })

  it('AC-15：拖拽中点日历先终止拖拽（dragLocked 归零）再开面板', () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    w.vm.panel.dim.setDragLocked(true)
    w.vm.panel.toggleCalendar()
    expect(w.vm.panel.dim.dragLocked.value).toBe(false)
    expect(w.vm.panel.calendarOpen.value).toBe(true)
  })

  it('开关可来回切换（不会卡在打开态）', () => {
    const stub = makeGridStub()
    const w = mountPanel(stub)
    w.vm.panel.toggleCalendar()
    w.vm.panel.toggleCalendar()
    expect(w.vm.panel.calendarOpen.value).toBe(false)
  })
})

describe('AC-08 · 媒体总数为 0 时切档仍正常完成', () => {
  it('空列表下切档不报错', () => {
    const stub = makeGridStub({ items: [] })
    stub.gridRef.value.captureAnchor = () => null
    const w = mountPanel(stub, ref(false))
    expect(() => w.vm.panel.onDimensionChange('year')).not.toThrow()
    expect(w.vm.panel.dim.dimension.value).toBe('year')
  })
})