# Panomint「Agent 语义接口」设计方案 v1.0

> 关联：Job000121（转码远程调试通道，已交付）→ 本设计为其功能融合升级。
> 建议实施 Job 编号：Job000140 起（按实施时序占用）。
> 上游组件：alibaba/page-agent v1.12.4（MIT，https://github.com/alibaba/page-agent）。

---

## §1 阶段一结论：page-agent 授权合规分析

### 1.1 结论：可用（附合规动作清单）

page-agent 全依赖链许可证核验通过，可引入本项目并随闭源商用产品分发，**不构成本项目商用授权障碍**。唯一硬性义务：在分发物中保留各组件的版权与许可声明（含上游 browser-use 的派生声明块）。

### 1.2 证据矩阵（逐项实测，2026-10 核验）

| 组件 | 版本 | 许可证 | 核验来源 | 备注 |
|---|---|---|---|---|
| page-agent（仓库根） | 1.12.4 | MIT | LICENSE 文件全文 | 双版权行：Copyright (c) 2026 SimonLuvRamen + Alibaba Group Holding Limited |
| @page-agent/core | 1.12.4 | MIT | packages/core/package.json | Re-act 循环核心 |
| @page-agent/llms | 1.12.4 | MIT | packages/llms/package.json | OpenAI 兼容 LLM 客户端，自实现 fetch，无 SDK 依赖 |
| @page-agent/page-controller | 1.12.4 | MIT | packages/page-controller/package.json | DOM 提取/操作 |
| @page-agent/ui | 1.12.4 | MIT | packages/ui/package.json | 无任何 dependencies |
| page-agent（主包） | 1.12.4 | MIT | packages/page-agent/package.json | 以上各包的集成薄壳 |
| chalk | 6.0.1 | MIT | npm registry latest JSON `license` 字段 | 运行时依赖 |
| zod | 4.6.5 | MIT | npm registry latest JSON `license` 字段 | peer 依赖（schema 校验） |
| ai-motion | 0.4.8 | MIT | npm 包页 License 章节 | 零依赖 WebGL2 装饰组件，作者即 page-agent 作者 |
| devDependencies（vite/eslint/typescript/tailwind 等） | — | MIT 系 | 根 package.json | 仅构建期使用，**不进入运行时分发产物**，不产生分发义务 |

### 1.3 条款依据与风险说明

1. **传染性协议**：GPL/AGPL 全链未出现。MIT 允许商用、闭源、修改、再分发、再许可，唯一义务是「在所有副本或实质部分保留版权声明与许可声明」（MIT 第 1-2 段）。
2. **专利条款**：MIT 无 Apache-2.0 式显式专利授予、亦无专利报复条款。专利授权为司法实践中普遍承认的隐含授权。本项目与版权方无竞争对抗关系，该弱化风险评估为可接受，不需要额外专利条款保护。
3. **署名要求**：MIT 无使用界面署名义务。ai-motion README 中的 "Attribution & Community" 段落为道德倡议（"the MIT license allows free use without attribution requirements"，其原文自认），**不是许可条款义务**。
4. **双版权行**：SimonLuvRamen（原作者）+ Alibaba Group（收录方）。两者均需在声明中保留。
5. **源文件版权头与 LICENSE 并存**：部分源文件头标注 "Copyright (C) 2025 Alibaba Group Holding Limited / All rights reserved."，与仓库级 MIT LICENSE 并存。以仓库 LICENSE 为准（公开仓库 + LICENSE 授权 = 要约），此为业界常见做法，不构成额外风险；保留声明时照抄 LICENSE 文本即可。
6. **上游派生声明（必须保留）**：README 明确 DOM 处理组件与 prompt 派生自 browser-use（MIT, Copyright (c) 2024 Gregor Zunic），并给出标准声明块。该块属于 page-agent 侧的再分发条件，必须原样保留。
7. **商标**：MIT 不授予商标权。不得将 "Page Agent"、"Alibaba" 用作 Panomint 的产品/功能名；文档中事实性来源说明（"基于开源项目 alibaba/page-agent"）为正当使用。
8. **CDN demo 版不可用于生产**：`page-agent.demo.js` 走官方免费测试 LLM API 并附带独立 terms（docs/terms-and-privacy.md）。引入方式必须是 npm 自托管，杜绝运行时外部依赖。

### 1.4 合规动作清单（实施期执行）

| # | 动作 | 位置 |
|---|---|---|
| 1 | 追加声明条目：page-agent 1.12.4（MIT，Alibaba + SimonLuvRamen）、chalk、zod、ai-motion | `licenses.csv` |
| 2 | 原样保留 browser-use 派生声明块（README 提供的标准文本） | `licenses.csv` 对应行备注列或独立 THIRD_PARTY_NOTICES |
| 3 | 锁定精确版本 `page-agent@1.12.4` 入 package.json，不使用 CDN demo 构建 | `src/frontend/package.json` |
| 4 | 引入时保留源码仓 LICENSE 副本 | 构建脚本或 vendor 目录 |

---

## §2 现状盘点

### 2.1 现有「转码远程调试」通道（Job000121，已交付）

**定位**：一次性、短寿命、全审计的远程排障通道。管理员在设置页开启 → 系统签发 URL+密钥（明文仅一次）→ 开发者机器上的 CLI agent（`cmd/debugctl`）凭密钥经 WSS 接入 → 执行白名单内结构化命令。

已交付组件（本设计的复用基础）：

| 层 | 组件 | 要点 |
|---|---|---|
| DB | `debug_channel` 单行表（迁移 v41） | channel_id（24 hex，非敏感）+ key_digest（sha256，明文永不入库）+ TTL + last_connect_at/ip |
| 后端 | `internal/debug`（debug/ws/session/commands/admin/store/token/ratelimit.go） | 握手前置矩阵（400/429/404/409/401/426 升级）；单连接槽 Hub；心跳 30s；reaper 300s；advisory lock 742032 串行化凭据变更 |
| 协议 | JSON 信封 `{seq,evt,type,payload}`，proto_ver=1 | 应答 `result`/`error{code,message}`（机器可读码）；事件 `job.state`（evt 自增） |
| 命令白名单（10） | hello / ping / snapshot / queue.stats / subscribe / unsubscribe / job.pause / job.resume / job.cancel / job.log.tail | 底层绑定 `internal/transcode` 控制面；job.log.tail 有界（≤500 行，无 follow）；错误码 UNKNOWN_COMMAND/INVALID_PARAMS/JOB_NOT_FOUND/INVALID_STATE/INTERNAL |
| 管理面 | GET /admin/debug/status；POST enable/rotate/disable | `RequirePerm("admin:system")`；rotate/disable 两步确认；审计八事件 |
| agent 端 | `cmd/debugctl`（Go CLI） | -url/-key/-key-file/-once/-cmd；REPL + 指数退避重连 + 重连后 snapshot 对齐 |
| 前端 | `DebugSettingsCard.vue` + `DebugCredsPanel` + `DebugAuditList` + `api/debug.js` | 权限门 403 整卡不渲染；一次性凭据回显；倒计时 + 5s 轮询校准；审计摘要 |

**安全模型（保持不变的骨架）**：双段凭据、白名单指令（无 shell/文件系统/SQL 直通）、全量审计（detail 键名避开脱敏名单，用 channel_fp）、强制 WSS、失败锁定 fail-open。

### 2.2 page-agent v1.12.4 核心能力（源码级核验）

| 能力 | 机制 | 对本设计的意义 |
|---|---|---|
| 页面理解 | 纯文本 DOM 提取（PageController），无截图、无多模态依赖 | 无 GPU/视觉成本；文本可经 `transformPageContent` 脱敏后出站 |
| DOM 操作 | 内置工具集（click/type/导航等），执行时高亮遮罩提示 | 覆盖一切页面功能，无需为每个功能新写后端命令 |
| 指令执行 | **Re-act 宏工具 `AgentOutput`**：每步强制输出「反思 + 单个动作」，tool_choice 强制 | 每步动作确定性强、可观测、可拦截 |
| 自定义工具 | 构造期 `customTools: Record<name, PageAgentTool \| null>`；tool = {description, inputSchema(zod), execute(input, {signal})}；传 null 可删除内置工具 | **语义指令 → 后端命令映射的官方扩展点**，zod schema 与我们 WSS 协议 payload 天然同构 |
| 上下文/会话 | `execute(task)` 即独立会话；history（step/observation/retry 事件流）构成记忆；终止条件 = done 工具 / maxSteps（默认 40）/ 错误 / abort | 会话粒度清晰，无需自行管理对话状态 |
| LLM 接入 | `AgentConfig extends LLMConfig`：model/baseURL/apiKey/headers（OpenAI 兼容协议） | **baseURL 可指向自建代理** → 第三方 key 不落浏览器 |
| 生命周期钩子 | onBeforeTask/onAfterTask/onBeforeStep/onAfterStep/onDispose；`stop()`/`dispose()`；EventTarget 事件（statuschange/activity/historychange） | 审计上报、敏感操作确认、面板联动的挂点 |
| 安全开关 | `experimentalScriptExecutionTool` 默认关（任意 JS 执行）；`enableMask` 遮罩；`transformPageContent` 出站内容变换；`instructions.{system, getPageInstructions(url)}` 按页注入约束 | 默认姿态即安全；按页约束与脱敏均有现成挂点 |

---

## §3 目标与非目标

**目标**
1. 将「转码远程调试」统一升级为「Agent 语义接口」：一类能力沿用并增强远程功能调试（转码排查/日志获取/参数下发），另一类能力支持自然语言指令让 AI 直接操作相册系统。
2. 语义指令到具体操作的映射**显式、白名单、可审计**；GUI 操作为兜底层。
3. 与 Job000121 通道完全兼容：凭据模型、WSS 协议、debugctl、审计事件零破坏。

**非目标**
- 不做服务端自动化/爬虫（page-agent 定位即 client-side web enhancement）。
- 不引入多模态截图感知。
- 不改变普通用户（非 admin:system）的任何权限面。

---

## §4 总体架构与分层

```
┌─────────────────────── 管理员浏览器（admin:system 会话内） ───────────────────────┐
│                                                                                  │
│  设置页「Agent 语义接口」卡                AI 助手悬浮面板（@page-agent/ui Panel）  │
│  ├─ 通道开关/TTL/凭据（沿用）              ├─ 自然语言输入 → agent.execute(task)   │
│  ├─ LLM 上游配置（新）                    ├─ 步骤/反思/结果实时展示               │
│  └─ 语义工具开关（新）                    └─ L2 敏感操作确认按钮（阻断 Promise）   │
│                                                                                  │
│  PageAgent 实例（page-agent npm 包，随前端构建分发）                              │
│  ├─ L1/L2 语义工具层（customTools ← semantic_tools.ts 注册表）                    │
│  │    └─ 每工具: zod inputSchema ↔ {type,payload} ↔ POST /admin/agent/cmd        │
│  └─ L0 GUI 操作层（PageController 原生 DOM 工具，兜底）                           │
│         └─ instructions.getPageInstructions(url) 按路由注入页面语义说明            │
└──────────┬──────────────────────────────┬────────────────────────────────────────┘
           │ HTTPS                        │ HTTPS（OpenAI 兼容）
           ▼                              ▼
┌─────────────────────────┐    ┌──────────────────────────────────────────┐
│ POST /admin/agent/cmd   │    │ /agent/llm/v1/*（OpenAI 兼容 LLM 代理）    │
│ （新增 HTTP 命令面，与    │    │  Bearer = 调试通道密钥（或会话态校验）      │
│  WSS 命令面同构复用）     │    │  → 服务端持有真实上游 key，逐次审计        │
│  RequirePerm admin:system│    └──────────────────────────────────────────┘
│  复用 debug 命令实现      │
└──────────┬──────────────┘
           ▼
┌──────────────────────────────────────────────────────────────────────────────────┐
│ 后端既有资产（零改动复用）                                                        │
│  internal/debug/commands.go（命令实现抽独立）  internal/transcode 控制面           │
│  internal/audit（审计）  debug_channel（凭据/TTL）  Valkey 限流                    │
│  WSS /debug/channel/:id（外部 debugctl 专用，保持原样）                            │
└──────────────────────────────────────────────────────────────────────────────────┘
```

分层原则：
- **凭据与门禁层**（复用）：debug_channel + RequirePerm("admin:system") + 审计。
- **命令面层**（双形态同构）：同一套命令实现，两个入口——WSS（外部 debugctl，原样）+ HTTP（浏览器内 Agent，新增）。
- **语义工具层**（新增，浏览器内）：注册表驱动，zod schema ↔ 协议 payload 一一对应。
- **GUI 兜底层**（page-agent 原生）：无语义工具的页面功能由 DOM 操作完成。
- **LLM 代理层**（新增）：服务端持钥，出站内容脱敏，逐次审计。

---

## §5 模块职责

| 模块 | 位置（新建/改动） | 职责 |
|---|---|---|
| 命令核心抽取 | `internal/debug/commands_core.go`（从 commands.go 抽出，原文件保留 WSS 会话包装） | 把 snapshot/queue.stats/job.pause/job.resume/job.cancel/job.log.tail 的实现抽为不依赖 WS 会话的包级函数（入参 Pool/TransQ），WSS 与 HTTP 两面共用；**信号不变：错误码、参数校验、审计字段逐字节一致** |
| HTTP 命令面 | `internal/debug/agenthttp.go` + 路由 `POST /admin/agent/cmd` | body `{type, payload}` → 白名单分发（复用命令核心）→ `result/error` 信封；限流（复用 Valkey 桶，按用户维度）；每调用审计 `agent.cmd` |
| LLM 代理 | `internal/agentllm/` + 路由 `/agent/llm/v1/chat/completions` | 校验 Bearer（通道密钥 sha256 比对，同 WSS §5.2 矩阵的 ⑤⑦ 精简版）→ 读取管理员配置的上游（baseURL/model/key 加密存储）→ 流式转发；请求/响应体积上限；逐次审计 `agent.llm`（不含内容明文，只记 token 数/模型/耗时） |
| 上游配置存储 | 迁移 v44：`agent_llm_config`（或 system_config 键扩展） | baseURL/model/api_key（**列级加密**，沿用系统既有加密手段）/enabled；仅 admin:system 可读写 |
| 语义工具注册表 | `src/frontend/src/agent/semanticTools.js` | 每条目：name / zh 描述 / zod inputSchema / level(L1 只读·L2 变更) / payload 构造器 / confirm 文案（L2）/ 响应→文本渲染器 |
| Agent 装配层 | `src/frontend/src/agent/createPanoAgent.js` | 组装 PageAgent：customTools 注入（L2 工具包确认桥）、instructions.system（总约束：优先语义工具、禁止越权尝试、失败即停）、getPageInstructions（按路由）、transformPageContent（出站脱敏：密码/密钥/令牌值）、maxSteps、LLM 指向 /agent/llm/v1 |
| 确认桥 | `src/frontend/src/agent/confirmBridge.js` | L2 工具 execute → 面板弹出确认卡（操作名+参数+后果）→ 用户点「确认/取消」resolve/reject；**确认动作本身写审计**（经 POST /admin/agent/confirm 审计端点，或并入下次 cmd 调用的 detail） |
| 助手面板 | `src/frontend/src/components/agent/AgentPanel.vue` | 装配 Panel（@page-agent/ui）或自绘轻面板；会话入口/停止按钮/结果展示/错误条；仅 admin:system 渲染 |
| 设置卡升级 | `DebugSettingsCard.vue` → 改名「Agent 语义接口」，区块化 | 区块 A：调试通道（原功能原样）；区块 B：LLM 上游配置（新）；区块 C：语义工具开关清单（新，默认全开） |
| 契约 | `文档/API详细契约` 增章 | /admin/agent/cmd、/agent/llm/v1、agent_llm_config、审计事件清单 |

---

## §6 语义指令 → 具体操作的映射机制

### 6.1 映射原理

page-agent 的扩展点是构造期 `customTools`。每个语义工具把一段自然语言意图绑定为一个**结构化后端命令**：

```
自然语言 → LLM 选择工具（依据 description + zod schema）
        → 工具 execute(input) 构造 {type, payload}
        → POST /admin/agent/cmd（Bearer 通道密钥 / admin 会话）
        → 后端白名单分发（与 WSS 同一实现）
        → result/error 信封 → 渲染器转为人类可读文本回填 LLM
```

zod `inputSchema` 与 WSS 协议 payload **同构**（如 job.pause 的 `{job_id: uuid}`），保证语义工具与 debugctl 命令一一对照、单一事实源。

### 6.2 v1 语义工具集（与 WSS 白名单同构，10 项）

| 工具名 | 级别 | 对应 WSS 命令 | inputSchema 要点 | 用户意图示例 |
|---|---|---|---|---|
| pano_view_transcode_snapshot | L1 | snapshot | 无参 | 「现在转码队列什么情况」 |
| pano_view_queue_stats | L1 | queue.stats | 无参 | 「队列还有多少积压」 |
| pano_view_job_log | L1 | job.log.tail | job_id(uuid)、lines≤500 | 「查一下这个任务为什么失败」 |
| pano_watch_jobs | L1 | subscribe/unsubscribe | on: bool | 「有任务完成提醒我」（事件以 observation 注入） |
| pano_pause_transcode_job | L2 | job.pause | job_id(uuid) | 「暂停那个 4K 任务」 |
| pano_resume_transcode_job | L2 | job.resume | job_id(uuid) | 「恢复刚才暂停的任务」 |
| pano_cancel_transcode_job | L2 | job.cancel | job_id(uuid) | 「取消这个任务」（不可逆，强确认） |
| pano_ping | L1 | ping | 无参 | 「服务通不通」 |

（hello 为连接层内部行为，不暴露为语义工具。）

### 6.3 工具选择优先级与 GUI 兜底

`instructions.system` 声明（注入 system prompt）：
1. 任何转码/系统运维意图，**必须优先使用 pano_* 语义工具**，禁止用 DOM 操作去点设置页按钮完成同等操作（DOM 层不可审计）。
2. 无对应语义工具的页面功能（浏览相册、建相册、改设置项 UI 等），用原生 DOM 工具完成。
3. 工具返回 error 信封时，先向用户报告 code+message，不得盲目重试变更类（L2）操作。

`instructions.getPageInstructions(url)` 按路由注入页面级说明（如 `/settings` 页提示哪些卡片可操作、哪些区域只读），随 Phase 3 逐步丰富。

### 6.4 「参数下发」能力（转码设置类）

v1 白名单不含转码参数修改（设计§5.4 即无此命令）。预留扩展：在命令核心新增 `config.get/config.set`（白名单字段表驱动，仅限转码并发数、ABR 档位等运维参数），语义工具 `pano_get_transcode_config`（L1）/`pano_set_transcode_param`（L2，逐键确认）。实现落点与 §5 命令核心抽取一致，作为 Phase 3 可选项。

---

## §7 与现有「转码远程调试」的兼容与迁移

| 维度 | 兼容性结论 | 说明 |
|---|---|---|
| debug_channel 表 | **零迁移** | 凭据模型、TTL、单连接语义原样保留；Agent 浏览器会话**不占用** WSS 单连接槽（走 HTTP 面），外部 debugctl 不受影响 |
| WSS 协议（proto_ver=1） | **零改动** | ws.go/session.go/commands.go 的 WSS 路径保持原样；命令实现抽入 commands_core.go 后由会话层调用，行为逐字节等价（单测钉住） |
| debugctl CLI | 零改动 | 继续可用；README/手册标注「外部 CLI 调试」与「页面内 Agent」两种接入形态 |
| 管理端点 | 增量 | enable/rotate/disable/status 原样；Agent 面板复用同一凭据生命周期（通道开启 = Agent 可用，关闭 = Agent 语义工具面与 LLM 代理同时拒绝） |
| 前端设置卡 | 改名+增区块 | 「转码远程调试」→「Agent 语义接口」；原交互（TTL/一次性凭据/两步确认/倒计时/审计摘要）不动，新增 LLM 上游与工具开关区块 |
| 审计 | 增事件不改旧 | 既有 debug.* 八事件不变；新增 `agent.cmd`、`agent.confirm`、`agent.llm`、`agent.task`（execute 开始/结束，含 success/data 摘要）；detail 键名继续避开脱敏名单（不用 token/secret/hash 子串，密钥指纹沿用 channel_fp） |
| 迁移步骤 | 蓝绿式 | 新端点与工具层全部为**增量交付**；设置卡改名前先并行展示一个发版周期（「Agent 语义接口（含转码远程调试）」），下个周期再收拢，避免用户找不到原入口 |

---

## §8 权限边界 / 二次确认 / 回滚 / 错误处理

### 8.1 权限边界（恒假 FailClosed，沿用项目纪律）

1. Agent 整体门禁 = `RequirePerm("admin:system")`，与调试通道同一把锁；无权限时：设置卡不渲染、AgentPanel 不挂载、/admin/agent/cmd 与 /agent/llm/v1 均 403/404（404 文案与不存在同形）。
2. 语义工具能力面 = WSS 白名单同源，**不存在白名单外的命令**；HTTP 面未知 type 返回 UNKNOWN_COMMAND（与 WSS 一致）。
3. LLM 上游 key 只存服务端（列级加密），浏览器永不可得；/agent/llm/v1 的 Bearer 校验失败走既有 401+失败计数路径（Valkey fail-open 语义沿用）。
4. 出站脱敏：`transformPageContent` 对 DOM 提取文本强制过滤密码框值、凭据回显区（DebugCredsPanel 内容）、审计密钥指纹区；`enableMask` 保持开启。
5. `experimentalScriptExecutionTool` 保持默认**关**（任意 JS 执行面永不开放）；`experimentalLlmsTxt` 关（避免把站点 llms.txt 混入上下文）。
6. 数据边界：Agent 会话只在管理员当前标签页存活；不提供后台无人值守执行（无定时任务面）；页面关闭 = dispose。

### 8.2 敏感操作二次确认（L2）

- 工具注册时声明 `level: 'L2'` + `confirmText`；confirmBridge 在 execute 内先 `await confirmUI(name, params, confirmText)`，用户点击「确认」才发 POST /admin/agent/cmd；「取消」直接 reject（LLM 收到取消文本，可改走只读路径）。
- **取消不可逆操作（pano_cancel_transcode_job）加二级强化**：确认卡额外展示该任务当前状态与已耗时（执行前先行一次 L1 snapshot 取数），并要求点击文案为「确认取消任务」而非通用按钮。
- 确认事件独立审计 `agent.confirm{tool, params_digest, decision, latency_ms}`；被拒绝的 L2 调用同样入库（decision=denied）。
- 超时语义：确认卡 120s 无操作 = deny（避免挂起的 Promise 阻塞 Re-act 循环耗尽步数）。

### 8.3 失败回滚

1. **操作前基线**：每个 L2 工具执行前自动先取对应 L1 快照（pause/cancel 前先 snapshot 该 job 状态），连同结果写入 agent.task 审计 detail——回滚依据永远可查。
2. **可逆映射表**：pause ↔ resume 一一配对；执行失败（非业务拒绝）时工具层自动尝试反向恢复一次并如实报告。cancel 不可逆 → 仅靠前置强确认 + 审计，不做伪回滚。
3. **任务级回滚**：Agent 面板保留「撤销上一步 L2 操作」按钮（依据最近一次 agent.confirm+agent.cmd 审计行执行反向工具）。
4. **通道级熔断**：管理员 rotate/disable 即刻断开语义面（HTTP 面 Bearer 校验复用同一 key_digest，轮换后旧会话下一条命令即 401）。

### 8.4 错误处理策略

| 层 | 策略 |
|---|---|
| 语义工具 | error 信封原样回填 LLM（code+message 机器可读），LLM 可自我修正参数重试（仅限 L1；L2 失败后必须报告不得自动重试） |
| LLM 调用 | llms 包 retry 事件 → 面板 activity「重试中」提示；连败终止当次 execute，返回 failed ExecutionResult |
| 步数/超时 | maxSteps=40 封顶（配置可下调）；单命令 30s 超时（HTTP 面超时即 INTERNAL） |
| 会话终止 | done 正常结束 / stop() 用户中止 / TTL 到期后所有命令 401 / dispose 清理（面板隐藏+事件解绑+abort） |
| 展示原则 | 所有失败**透出 status/code**（沿用 v1.8.x 播放器修复确立的「错误透出」纪律），不静默吞错 |

---

## §9 设置项配置方式

设置页「Agent 语义接口」卡（原调试卡升级）：

| 区块 | 控件 | 存储/接口 |
|---|---|---|
| A 调试通道（原样保留） | TTL 下拉 / 开启 / 凭据一次性回显 / 重置·关闭两步确认 / 倒计时 / 审计摘要 | debug_channel + /admin/debug/*（不动） |
| B LLM 上游（新） | baseURL 输入（OpenAI 兼容，如自建 vLLM/DashScope 兼容模式）、model 名、api_key（写入后仅显尾 4 位）、连通性「测试」按钮 | agent_llm_config（v44 迁移）；POST /admin/agent/llm-config {test: true} 走服务端探活 |
| C 语义工具开关（新） | 工具清单逐项开关（默认全开；L2 项标注「变更」徽标） | system_config 键 `agent_semantic_tools`（JSON 白名单集） |
| D 会话入口（新） | 开启态显示「在页面中唤起 AI 助手」说明；指向右下角 AgentPanel | 前端本地（无后端状态） |

配置语义遵循 PUT /user/ui-prefs 教训：**全部键全量提交**，无 keep-on-absent 例外。

---

## §10 分阶段落地步骤与验证方式

### Phase 0：合规落位 + 依赖引入（0.5 天）
- 动作：§1.4 清单四项执行；`npm install page-agent@1.12.4`；构建验证 bundle 体积增量。
- 验证：`npm ls page-agent` 版本钉住；licenses.csv diff 可见四条目；`npm run build` 产物 grep 到 MIT 声明文本；无 CDN demo 引用（grep jsdelivr/npmmirror 为零）。

### Phase 1：HTTP 命令面 + 语义工具桥（1.5 天）
- 动作：命令核心抽取（commands_core.go）+ `POST /admin/agent/cmd` + 审计 agent.cmd + semanticTools.js 注册表。
- 验证：
  - Go 单测：命令核心行为与 WSS 路径逐字节等价（同输入同输出同审计字段）；未知 type/非法 uuid 矩阵。
  - e2e 判定矩阵：无凭据 401 / 非 admin 403（404 同形）/ 合法矩阵 8 命令 / 审计行数+1 断言。
  - 回归：debugctl `-once` 全命令连通（外部通道零破坏证据）。

### Phase 2：LLM 代理 + Agent 面板 + 设置卡升级（2.5 天）
- 动作：v44 迁移 + /agent/llm/v1 代理 + createPanoAgent + AgentPanel + 设置卡 B 区块。
- 验证：
  - 迁移 up/down 往返；api_key 落库密文断言（psql 直查明文不存在）。
  - headless CDP e2e（真实打开页面，项目硬规则）：admin 登录 → 唤起面板 → 输入「查看当前转码队列」→ 断言①面板渲染 snapshot 摘要 ②审计出现 agent.cmd+agent.llm ③网络层无任何第三方 LLM 域名出站。
  - L2 确认 e2e：「暂停任务 X」→ 断言确认卡出现 → 取消 → 断言无 cmd 调用且审计 agent.confirm decision=denied → 再执行并确认 → 断言 job 状态变更 + 双审计行。
  - 非 admin 会话打开系统 → 断言 AgentPanel 不存在（DOM 缺席断言，非仅不可见）。
  - rotate 密钥后旧面板下一条命令 401 断言。

### Phase 3：GUI 兜底层 + 页面语义指引 +（可选）参数下发（2 天）
- 动作：instructions.getPageInstructions 按路由注入；transformPageContent 脱敏规则；（可选）config.get/set 白名单 + pano_get/pano_set 工具。
- 验证：
  - 跨页任务 e2e：「打开设置页看看当前调试通道状态」→ 断言导航与结果回填。
  - 脱敏 e2e：开启通道后的凭据回显页执行「读一下这个页面的内容」→ 断言出站 LLM 请求体中不含明文密钥（代理侧拦包断言）。
  - 手册同步：用户操作手册新增「AI 助手」章节（操作步骤+可展开原理）。

每阶段独立 Job 登记、commit 即 push、发版走既有 19 项一致性校验链。

---

## §11 风险与开放问题

| # | 风险/开放项 | 处置 |
|---|---|---|
| 1 | page-agent 为 1.x，customTools/lifecycle hooks 标注 @experimental，API 可能变更 | 版本精确锁定 + 升级走独立 Job + 装配层（createPanoAgent）做防腐隔离，升级只动一个文件 |
| 2 | 浏览器内 LLM 走自建代理的时延/流式体验 | 代理实现 SSE 流式直通（Phase 2 范围内）；劣化时可临时降级为非流式 |
| 3 | LLM 误操作风险兜底 | L2 强确认 + 操作前基线 + 撤销按钮三重防线（§8.2/8.3）；审计全程可追 |
| 4 | 微信 H5 内 Agent 面板可用性未验证 | v1 仅面向管理后台桌面场景；H5 支持列入开放问题（微信内核 DOM 事件差异需实测） |
| 5 | 源文件 "All rights reserved" 头与 MIT 并存（§1.3-5） | 以仓库 LICENSE 为准；声明照抄 LICENSE 文本；不修改其版权头 |
| 6 | 设计方案原文档《转码远程调试功能设计方案_v1.0.md》实体文件已不在仓库（代码注释仍引用其章节号） | 本文档 §2.1 已从代码注释重建设计要点；建议随本设计实施时在登记簿补记该文档缺失事实 |
