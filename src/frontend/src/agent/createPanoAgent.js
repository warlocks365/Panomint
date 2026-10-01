// PageAgent 装配层（Job000140 Phase 2，设计 §5）：防腐隔离点——page-agent 的 API 变更
// 只需改本文件。职责：
//   1. semanticTools 注册表 → page-agent customTools（zod schema 桥 + L2 确认桥）；
//   2. LLM 指向同源代理 /agent/llm/v1（服务端持真实上游 key，浏览器永不可见）；
//   3. instructions 注入语义优先级与行为约束；transformPageContent 出站脱敏。
// 安全默认：experimentalScriptExecutionTool 不开启（任意 JS 执行面永不开放）。
import { PageAgent } from 'page-agent'
import { z } from 'zod'
import { getAccessToken } from '../utils/tokenStore'
import { SEMANTIC_TOOLS } from './semanticTools'

// 参数描述 → zod schema（v1 仅两类：UUID 字符串 / 有界整数，其余字符串）。
function zodFromParams(params) {
  const shape = {}
  for (const [k, desc] of Object.entries(params || {})) {
    if (k === 'job_id') shape[k] = z.string().describe(desc)
    else if (k === 'lines') shape[k] = z.number().int().positive().max(500).optional().describe(desc)
    else if (k === 'auto_transcode' || k === 'realtime_transcode') shape[k] = z.boolean().optional().describe(desc)
    else shape[k] = z.string().optional().describe(desc)
  }
  return z.object(shape)
}

const SYSTEM_RULES = [
  '你是 Panomint 全景相册系统的运维助手（服务对象是系统管理员）。',
  '1. 凡涉及转码队列/任务/系统状态的意图，必须优先使用 pano_ 前缀的语义工具（结构化、可审计），',
  '   禁止用页面点击/表单操作去完成语义工具已覆盖的事情。',
  '2. 没有对应语义工具的页面功能（浏览相册、查看设置等），才使用页面 DOM 操作完成。',
  '3. 语义工具返回错误时，向用户如实报告错误信息；变更类（L2）工具失败后不得自动重试。',
  '4. 与系统无关的闲聊请求，礼貌说明你的职责范围。',
  '5. 全程使用简体中文回复。',
].join('\n')

// 页面语义指引（Phase 3，设计 §6.3）：按路由为 GUI 兜底层注入该页可做什么/什么别做。
const PAGE_INSTRUCTIONS = {
  '/timeline': '时间轴页：按日期浏览媒体，可点击缩略图打开播放器。',
  '/albums': '相册页：相册卡片列表；可点「新建相册」创建，点卡片进入详情。',
  '/albums/:id': '相册详情页：媒体网格；支持多选批量（分享/删除）、点击单个媒体查看。',
  '/map': '地图页：媒体地理位置分布；支持地名搜索与地点聚合。',
  '/places': '地点页：按地点聚合的照片列表。',
  '/people': '人物页：人脸聚类的人物列表，点人物看其照片。',
  '/tags': '标签页：标签列表与管理。',
  '/spaces': '共享空间页：空间与成员权限管理。',
  '/folders': '文件夹页：虚拟目录树（注意：虚拟目录不是物理存储目录）。',
  '/toolbox': '工具箱页：导入扫描、存储挂载、转码控制等运维工具入口。',
  '/upload': '上传页：批量上传媒体文件。',
  '/search': '语义搜索页：自然语言搜图（本地 AI 语义检索）。',
  '/settings': '设置页：账号安全、LLM 上游、调试通道等敏感配置卡。本页多数为敏感配置——凡语义工具已覆盖的操作（如转码配置）禁止用 DOM 点击完成；查看类 DOM 操作允许。',
  '/admin': '管理后台：用户管理、账号策略、审计日志、扫描任务管理。',
}
function getPageInstructions(url) {
  try {
    const path = new URL(url).pathname
    if (PAGE_INSTRUCTIONS[path]) return PAGE_INSTRUCTIONS[path]
    if (/^\/albums\/[^/]+/.test(path)) return PAGE_INSTRUCTIONS['/albums/:id']
    if (/^\/player\//.test(path)) return '播放器页：全景/普通视频播放器，支持陀螺仪与 VR 模式。'
    if (/^\/share\//.test(path)) return '公开分享页：访客视图，请勿在此页执行管理操作。'
  } catch (e) { /* url 解析失败忽略 */ }
  return undefined
}

// 出站脱敏：DOM 提取文本送 LLM 前，抹掉凭据形态的字符串。
function maskSensitive(content) {
  return content
    .replace(/sk-[A-Za-z0-9_-]{8,}/g, 'sk-***')
    .replace(/Bearer\s+[A-Za-z0-9._-]{10,}/g, 'Bearer ***')
    .replace(/(access_token|refresh_token|api_key|password|secret)"?\s*[:=]\s*"?[^",\s}]{4,}/gi, '$1: ***')
}

/**
 * createPanoAgent 创建 PageAgent 实例。
 * @param {object} opts
 * @param {(tool: object, input: object) => Promise<boolean>} opts.confirmL2
 *   L2 确认桥：resolve(true)=放行执行，resolve(false)=取消（AgentPanel 提供确认 UI）。
 * @returns {Promise<PageAgent>}
 */
export async function createPanoAgent(opts = {}) {
  const confirmL2 = opts.confirmL2 || (async () => false) // 无确认桥时 L2 恒拒（FailClosed）

  const customTools = {}
  for (const tool of SEMANTIC_TOOLS) {
    customTools[tool.name] = {
      description: (tool.level === 'L2' ? '[变更操作，需用户确认] ' : '') + tool.desc,
      inputSchema: zodFromParams(tool.params),
      execute: async (input) => {
        const invalid = tool.validate(input || {})
        if (invalid) return `参数校验失败：${invalid}`
        if (tool.level === 'L2') {
          const allowed = await confirmL2(tool, input)
          if (!allowed) return `用户取消了该操作，未执行任何变更。如用户仍需要，请再次确认后重新调用。`
        }
        try {
          if (tool.run) {
            const r = await tool.run(input || {})
            return r.text
          }
          const resp = await import('../api/agent').then((m) => m.postAgentCmd(tool.cmd, tool.payload(input || {})))
          return tool.render(resp.result)
        } catch (e) {
          const status = e?.response?.status
          const code = e?.response?.data?.code || 'HTTP_' + (status || 'ERROR')
          const msg = e?.response?.data?.message || e?.message || '命令执行失败'
          return `命令失败（${code}）：${msg}`
        }
      },
    }
  }

  return new PageAgent({
    // model/baseURL 指向同源代理；真实上游模型与 key 由服务端 agent_llm_config 决定，
    // 客户端 model 字段仅占位（代理透传请求体，服务端不改写 model——上游以服务端配置为准）。
    model: 'server-managed',
    baseURL: '/agent/llm/v1',
    apiKey: getAccessToken() || 'anonymous',
    language: 'zh-CN',
    maxSteps: 30,
    customTools,
    instructions: { system: SYSTEM_RULES, getPageInstructions },
    transformPageContent: maskSensitive,
  })
}
