// 语义工具注册表（Job000140 Phase 1/3，设计 §6）：自然语言意图 → 结构化操作的
// 显式映射，单一事实源。Phase 2 由 createPanoAgent.js 装配为 page-agent customTools
// （zod inputSchema 桥接 + L2 确认桥）；Phase 1 起可独立驱动测试。
//
// 字段约定：
//   name        工具名（pano_ 前缀，LLM 工具面稳定标识）
//   cmd         对应后端命令名（agentHTTPCommands 白名单，显式映射不做名字变换）
//   desc        中文意图描述（装配时喂给 LLM 的工具 description）
//   level       'L1' 只读直执行 ｜ 'L2' 变更操作（必须经确认桥二次确认）
//   params      参数描述（Phase 2 生成 zod schema；此处先做前端侧校验）
//   validate(input) → 错误文本 | null（前端预校验，服务端仍强校验）
//   payload(input)  → POST /admin/agent/cmd 的 payload 字段（cmd 型工具）
//   run(input)      → 自定义执行器（REST 型工具，如转码配置——走既有 admin:system
//                     端点，审计由 REST 层记录；返回 {ok,text,result}）
//   render(result)  → 人类可读文本（回填 LLM 上下文 + 面板展示）
//   confirmText     L2 专用：确认卡文案（含后果说明）
// 工具面 = 后端 agentHTTPCommands（7 命令）+ REST 桥接（2 工具）——变更必须两侧同步。
import { postAgentCmd } from '../api/agent'
import { getTranscodeConfig, putTranscodeConfig } from '../api/admin'

const UUID_RE = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/

const uuidErr = (v) => (UUID_RE.test(v || '') ? null : 'job_id 需为 UUID 格式')

function renderTranscodeConfig(cfg, prefix) {
  const head = `${prefix ? prefix + '。' : ''}当前转码配置：`
  return [
    head,
    `- 自动转码（auto_transcode）：${cfg.auto_transcode ? '开' : '关'}`,
    `- 实时转码（realtime_transcode）：${cfg.realtime_transcode ? '开' : '关'}`,
    `- HLS 分片时长（hls_seg_seconds）：${cfg.hls_seg_seconds} 秒`,
    `- HLS 缓存档位（hls_cache_profile）：${cfg.hls_cache_profile || '（默认）'}`,
    cfg.stream_base_url ? `- 流基础地址（stream_base_url）：${cfg.stream_base_url}` : '- 流基础地址：未设置',
  ].join('\n')
}

export const SEMANTIC_TOOLS = [
  {
    name: 'pano_ping',
    cmd: 'ping',
    desc: '探测服务连通性，返回服务端标识与时间',
    level: 'L1',
    params: {},
    validate: () => null,
    payload: () => ({}),
    render: (r) => `服务在线（${r.server}，服务器时间 ${r.server_time}）`,
  },
  {
    name: 'pano_view_transcode_snapshot',
    cmd: 'snapshot',
    desc: '查看转码全量现状：活动任务列表与队列深度',
    level: 'L1',
    params: {},
    validate: () => null,
    payload: () => ({}),
    render: (r) => {
      const jobs = r.jobs || []
      const head = `队列深度 ${r.queue && r.queue.depth != null ? r.queue.depth : JSON.stringify(r.queue)}，活动任务 ${jobs.length} 个`
      if (!jobs.length) return head
      const lines = jobs.map(
        (j) => `- ${j.id} 状态=${j.status}${j.profile ? ` 档位=${j.profile}` : ''}`
      )
      return [head, ...lines].join('\n')
    },
  },
  {
    name: 'pano_view_queue_stats',
    cmd: 'queue.stats',
    desc: '仅查看转码队列积压深度',
    level: 'L1',
    params: {},
    validate: () => null,
    payload: () => ({}),
    render: (r) => `转码队列深度：${r.queue && r.queue.depth != null ? r.queue.depth : JSON.stringify(r.queue)}`,
  },
  {
    name: 'pano_view_job_log',
    cmd: 'job.log.tail',
    desc: '查看指定转码任务的档案与相关审计事件（有界，最多 500 行）',
    level: 'L1',
    params: { job_id: '任务 UUID', lines: '行数（可选，默认 100，上限 500）' },
    validate: (i) => uuidErr(i.job_id),
    payload: (i) => ({ job_id: i.job_id, lines: i.lines || 100 }),
    render: (r) => {
      const j = r.job || {}
      const head = `任务 ${j.id}：状态=${j.status}${j.profile ? ` 档位=${j.profile}` : ''}（创建于 ${j.created_at}）`
      const tail = (r.audit_tail || []).map(
        (e) => `- [${e.at}] ${e.action}${e.detail ? ` ${typeof e.detail === 'string' ? e.detail : JSON.stringify(e.detail)}` : ''}`
      )
      return [head, `相关审计 ${tail.length} 条`, ...tail].join('\n')
    },
  },
  {
    name: 'pano_pause_transcode_job',
    cmd: 'job.pause',
    desc: '暂停一个排队中的转码任务',
    level: 'L2',
    params: { job_id: '任务 UUID' },
    validate: (i) => uuidErr(i.job_id),
    payload: (i) => ({ job_id: i.job_id }),
    confirmText: (i) => `确认暂停转码任务 ${i.job_id}？`,
    render: (r) => `任务 ${r.job_id} 已暂停（状态=${r.status}）`,
  },
  {
    name: 'pano_resume_transcode_job',
    cmd: 'job.resume',
    desc: '恢复一个已暂停的转码任务',
    level: 'L2',
    params: { job_id: '任务 UUID' },
    validate: (i) => uuidErr(i.job_id),
    payload: (i) => ({ job_id: i.job_id }),
    confirmText: (i) => `确认恢复转码任务 ${i.job_id}？`,
    render: (r) => `任务 ${r.job_id} 已恢复（状态=${r.status}）`,
  },
  {
    name: 'pano_cancel_transcode_job',
    cmd: 'job.cancel',
    desc: '取消一个转码任务（不可逆操作）',
    level: 'L2',
    params: { job_id: '任务 UUID' },
    validate: (i) => uuidErr(i.job_id),
    payload: (i) => ({ job_id: i.job_id }),
    confirmText: (i) => `确认取消转码任务 ${i.job_id}？取消不可恢复。`,
    render: (r) => `任务 ${r.job_id} 已取消（状态=${r.status}）`,
  },
  {
    // REST 桥接（设计 §6.4 Phase 3）：走既有 /admin/transcode-config（admin:system 同锁，
    // PUT 为部分更新语义「缺失不改」，审计由 REST 层记录）。
    name: 'pano_get_transcode_config',
    desc: '查看系统级转码配置：自动转码开关、实时转码开关、HLS 分片时长、缓存档位、流基础地址',
    level: 'L1',
    params: {},
    validate: () => null,
    run: async () => {
      const cfg = await getTranscodeConfig()
      return { ok: true, text: renderTranscodeConfig(cfg), result: cfg }
    },
  },
  {
    name: 'pano_set_transcode_param',
    desc: '修改系统级转码配置（部分更新，只传要改的字段）。字段：auto_transcode（true|false 自动转码总开关）、realtime_transcode（true|false 实时转码开关）、hls_seg_seconds（2-20 整数，HLS 分片秒数）、hls_cache_profile（字符串缓存档位）、stream_base_url（字符串流基础地址）',
    level: 'L2',
    params: {
      auto_transcode: '布尔，自动转码总开关（可选）',
      realtime_transcode: '布尔，实时转码开关（可选）',
      hls_seg_seconds: '整数 2-20，HLS 分片秒数（可选）',
      hls_cache_profile: '字符串，HLS 缓存档位（可选）',
      stream_base_url: '字符串，流基础地址（可选）',
    },
    validate: (i) => {
      const keys = ['auto_transcode', 'realtime_transcode', 'hls_seg_seconds', 'hls_cache_profile', 'stream_base_url']
      const has = keys.some((k) => i[k] !== undefined && i[k] !== null && i[k] !== '')
      return has ? null : '至少提供一个要修改的字段'
    },
    confirmText: (i) => {
      const keys = ['auto_transcode', 'realtime_transcode', 'hls_seg_seconds', 'hls_cache_profile', 'stream_base_url']
      const will = keys.filter((k) => i[k] !== undefined && i[k] !== null && i[k] !== '')
        .map((k) => `${k}=${JSON.stringify(i[k])}`).join('，')
      return `确认修改转码配置：${will}？`
    },
    run: async (i) => {
      const patch = {}
      if (i.auto_transcode !== undefined) patch.auto_transcode = !!i.auto_transcode
      if (i.realtime_transcode !== undefined) patch.realtime_transcode = !!i.realtime_transcode
      if (i.hls_seg_seconds !== undefined && i.hls_seg_seconds !== '') patch.hls_seg_seconds = Number(i.hls_seg_seconds)
      if (i.hls_cache_profile !== undefined && i.hls_cache_profile !== '') patch.hls_cache_profile = i.hls_cache_profile
      if (i.stream_base_url !== undefined && i.stream_base_url !== '') patch.stream_base_url = i.stream_base_url
      const cfg = await putTranscodeConfig(patch)
      return { ok: true, text: renderTranscodeConfig(cfg, '已更新'), result: cfg }
    },
  },
]

// findTool 按 name 查找。
export function findTool(name) {
  return SEMANTIC_TOOLS.find((t) => t.name === name) || null
}

// runTool 执行一个语义工具：前端预校验 → cmd（/admin/agent/cmd）或 run（REST 桥）→ render。
// 返回 {ok, text, code?, status?}；L2 的确认拦截在确认桥内实现（本函数不做确认）。
export async function runTool(name, input) {
  const tool = findTool(name)
  if (!tool) return { ok: false, text: `未知语义工具 ${name}`, code: 'UNKNOWN_TOOL' }
  const invalid = tool.validate(input || {})
  if (invalid) return { ok: false, text: invalid, code: 'INVALID_PARAMS' }
  try {
    if (tool.run) return await tool.run(input || {})
    const resp = await postAgentCmd(tool.cmd, tool.payload(input || {}))
    return { ok: true, text: tool.render(resp.result), result: resp.result }
  } catch (e) {
    const status = e?.response?.status
    const code = e?.response?.data?.code || 'HTTP_' + (status || 'ERROR')
    const msg = e?.response?.data?.message || e?.message || '命令执行失败'
    return { ok: false, text: `${code}: ${msg}`, code, status }
  }
}
