// @vitest-environment jsdom
// AI 悬浮球「展开后能否退出返回悬浮球态」行为测试
//
// 缺陷（2026-10-10 实机）：expanded 只有置 true 的路径（expand），没有任何用户可达的
// 置回 false 入口。官方 Panel 自带的「关闭」只隐藏它自己的 DOM，不改本组件 expanded，
// 而球是 v-if="!expanded" → 展开后球被摘掉且装不回来。三条退出路径实机全失败：
//   点面板关闭 → ballVisible=false / Esc → false / 点球原位 → false（球已不在 DOM 上）
// 即 RESULT=NO_WAY_BACK，用户再也无法打开 AI 助手。
//
// 🔴 为什么必须是「真实挂载 + 真实点击」而不是源码正则断言：
// 本项目已栽过两次同类跟头（地图时间轴 sel.onMove 零引用、这个缺陷），共同特征都是
// **逻辑层/源码层看起来都对，但线路没接上**。正则只能证明代码存在，证明不了
// 「点下去真的切换了状态」。@vue/test-utils 的 trigger 走真实 DOM 事件派发 +
// Vue 响应式重渲染，能把「事件绑没绑上」「v-if 有没有真的摘掉/装回」都验出来。
//
// 覆盖的场景（对应用户的验证要求：展开 → 输入 → 能否退出）：
//   A 初始悬浮球可见
//   B 点击球 → 展开（球消失、收起按钮出现、agent 实例被创建）
//   C 输入内容后 点「收起」 → 回到悬浮球态
//   D 输入内容后 按 Esc   → 回到悬浮球态
//   E 收起必须 dispose agent 实例（否则定时器/观察器泄漏在已隐藏 DOM 上）
//   F 反复展开/收起 3 轮不残留（防「只能收一次」的同类缺陷）

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { reactive } from 'vue'

// ── 外部依赖打桩 ────────────────────────────────────────────────────────────
// 1) vue-router：组件只用 useRoute().path 做公开路由判定
vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/timeline' })
}))

// 2) auth store：probe() 要求 auth.user 为真才发权限试探
const authStub = reactive({ user: { id: 'u1' } })
vi.mock('../../stores/auth', () => ({
  useAuthStore: () => authStub
}))

// 3) LLM 配置 API：返回 enabled，模拟「已启用 → 球渲染」
vi.mock('../../api/agentLlm', () => ({
  getAgentLlmConfig: async () => ({ enabled: true, base_url: '', model: '', has_key: true, key_tail: '***' })
}))

// 4) createPanoAgent：真实依赖 page-agent 主包 + zod，在 jsdom 里跑不动也不必要。
//    这里替换成可控替身，只暴露组件真正使用的两个成员：panel.show / dispose。
//    🔴 关键：保留「装配是异步的」这一真实特征（expand() 里 await 动态 import），
//    否则测的就不是生产时序了。
const agentLog = []
let disposeCount = 0
vi.mock('../createPanoAgent', () => ({
  createPanoAgent: async () => {
    const agent = {
      panel: {
        show: () => agentLog.push('show'),
        hide: () => agentLog.push('hide'),
      },
      dispose: () => { disposeCount++; agentLog.push('dispose') },
    }
    agentLog.push('create')
    // 真实 createPanoAgent() 在返回前会调 agent.panel.show()（官方 Panel 构造后默认
    // display:none，不显式 show 就会出现「球消失 + 面板不可见」）。替身必须复刻这个
    // 调用顺序，否则测的就不是生产装配序列。
    agent.panel.show()
    return agent
  }
}))

import AgentPanel from '../AgentPanel.vue'

// 官方 Panel 会往 body 上挂自己的 DOM（由 page-agent 主包负责）。
// 这里给一个最小的替身，模拟「面板输入框存在」这一事实——用户的需求场景正是
// 「在面板里输入内容后能否退出」，输入框必须计入可交互元素。
function mountPanel() {
  const wrapper = mount(AgentPanel, { attachTo: document.body })
  return wrapper
}

// 往 body 塞一个模拟「官方 Panel + 其输入框」，并挂上它自己的「关闭」行为：
// 真实 page-agent 的关闭只隐藏自己的 DOM，不碰宿主的 expanded —— 这正是缺陷成因。
function installFakePanel() {
  const el = document.createElement('div')
  el.dataset.testid = 'fake-official-panel'
  el.innerHTML = '<input data-testid="fake-panel-input" placeholder="输入新任务" />' +
    '<button data-testid="fake-panel-close">关闭</button>'
  el.querySelector('[data-testid="fake-panel-close"]').addEventListener('click', () => {
    el.style.display = 'none'
  })
  document.body.appendChild(el)
  return el
}

const flush = async () => { await new Promise((r) => setTimeout(r, 0)) }

beforeEach(() => {
  agentLog.length = 0
  disposeCount = 0
  authStub.user = { id: 'u1' }
  document.body.innerHTML = ''
})

afterEach(() => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('AI 悬浮球 · 展开后能否退出（真实挂载 + 真实点击）', () => {
  it('A 初始态：已登录且 LLM 已启用时悬浮球可见', async () => {
    const w = mountPanel()
    await flush()
    expect(w.find('[data-testid="agent-ball"]').exists()).toBe(true)
    expect(w.find('[data-testid="agent-collapse"]').exists()).toBe(false)
  })

  it('B 点球展开：球被摘掉、收起按钮出现、agent 被创建且 panel.show() 被调用', async () => {
    const w = mountPanel()
    await flush()
    await w.find('[data-testid="agent-ball"]').trigger('click')
    await flush(); await flush()

    expect(w.find('[data-testid="agent-ball"]').exists()).toBe(false)
    expect(w.find('[data-testid="agent-collapse"]').exists()).toBe(true)
    expect(agentLog).toContain('create')
    expect(agentLog).toContain('show')
  })

  it('C 输入内容后点「收起」→ 回到悬浮球态（用户主诉场景）', async () => {
    const panel = installFakePanel()
    const w = mountPanel()
    await flush()
    await w.find('[data-testid="agent-ball"]').trigger('click')
    await flush(); await flush()

    // 模拟用户在面板输入框里打字
    const input = panel.querySelector('[data-testid="fake-panel-input"]')
    input.value = '列出最近的相册'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    expect(input.value).toBe('列出最近的相册')

    await w.find('[data-testid="agent-collapse"]').trigger('click')
    await flush()

    // 判据：球重新出现 + 收起按钮消失
    expect(w.find('[data-testid="agent-ball"]').exists(), '收起后悬浮球未恢复').toBe(true)
    expect(w.find('[data-testid="agent-collapse"]').exists()).toBe(false)
  })

  it('D 输入内容后按 Esc → 回到悬浮球态', async () => {
    const panel = installFakePanel()
    const w = mountPanel()
    await flush()
    await w.find('[data-testid="agent-ball"]').trigger('click')
    await flush(); await flush()

    const input = panel.querySelector('[data-testid="fake-panel-input"]')
    input.value = '查一下转码队列'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flush()

    expect(w.find('[data-testid="agent-ball"]').exists(), 'Esc 后悬浮球未恢复').toBe(true)
  })

  it('E 收起必须 dispose agent 实例（防定时器/观察器泄漏在已隐藏 DOM 上）', async () => {
    const w = mountPanel()
    await flush()
    await w.find('[data-testid="agent-ball"]').trigger('click')
    await flush(); await flush()
    expect(disposeCount).toBe(0)

    await w.find('[data-testid="agent-collapse"]').trigger('click')
    await flush()
    expect(disposeCount, '收起未 dispose —— page-agent 定时器会继续跑').toBe(1)
  })

  it('F 反复展开/收起 3 轮均正常，无残留、无重复实例', async () => {
    const w = mountPanel()
    await flush()
    for (let i = 1; i <= 3; i++) {
      await w.find('[data-testid="agent-ball"]').trigger('click')
      await flush(); await flush()
      expect(w.find('[data-testid="agent-collapse"]').exists(), `第 ${i} 轮展开失败`).toBe(true)
      await w.find('[data-testid="agent-collapse"]').trigger('click')
      await flush()
      expect(w.find('[data-testid="agent-ball"]').exists(), `第 ${i} 轮收起失败`).toBe(true)
    }
    // 每轮恰好 create 1 次 + dispose 1 次，不多不少
    expect(agentLog.filter((x) => x === 'create').length).toBe(3)
    expect(disposeCount).toBe(3)
  })

  it('G 官方 Panel 的「关闭」不能作为唯一出口：它只隐藏自己的 DOM', async () => {
    // 这条是缺陷成因的显式记录：点它之后球仍然不在 → 必须另有收起入口。
    // 若将来有人把 collapse() 删掉，C 就会红；这条则提前把成因钉住。
    const panel = installFakePanel()
    const w = mountPanel()
    await flush()
    await w.find('[data-testid="agent-ball"]').trigger('click')
    await flush(); await flush()

    panel.querySelector('[data-testid="fake-panel-close"]').click()
    await flush()
    expect(panel.style.display).toBe('none')
    expect(w.find('[data-testid="agent-ball"]').exists(),
      '官方 Panel 关闭不会让球回来（这是缺陷成因，修复靠独立收起按钮而非依赖它）').toBe(false)
    // 但独立收起出口必须在
    expect(w.find('[data-testid="agent-collapse"]').exists()).toBe(true)
  })
})