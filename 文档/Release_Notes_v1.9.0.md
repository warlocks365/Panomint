# Panomint v1.9.0 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.9.0**（次版本 +1：新功能——Agent 语义接口） |
| 发布日期 | 2026-10-01 |
| 功能 Job | Job000140 系列（Agent 语义接口 Phase 0-3）+ Job000121 测试通道发布策略 |
| 数据库迁移 | **v46** `agent_llm_config`（单行表；api_key 列级加密）——升级自动执行 |
| 源分支 | feature/agent-semantic-fusion（9 commits，merge ff87edd） |

---

## 全新功能：AI 助手（Agent 语义接口）

### 1. 页面内 AI 助手

- 管理员登录后任意页面右下角「AI」悬浮球唤起；输入自然语言驱动相册系统
- 能力：查看转码队列与任务详情、查任务日志、暂停/恢复/取消转码任务、查看/修改转码配置（自动转码/实时转码/HLS 分片等）；无语义命令的页面功能由页面操作兜底
- **变更类操作二次确认**：暂停/恢复/取消/改配置弹出确认卡（含参数明细），取消类不可逆操作需专项确认；确认卡 2 分钟无操作自动拒绝
- 全量审计：每条命令（`agent.cmd`）、每次 LLM 调用（`agent.llm`，只记模型/耗时/token 数不含内容）、配置变更（`agent.llm_config`）均可回溯

### 2. LLM 上游配置（设置页新增卡片）

- 支持 **OpenAI 兼容上游**（DashScope 兼容模式 / vLLM / llama.cpp server 等）
- API Key **列级加密存储**（AES-256-GCM，密钥来自部署环境 `STORAGE_CIPHER_KEY`），**永不下发浏览器、永不回显**（只显尾 4 位）
- 页面内助手经同源代理 `/agent/llm/v1/chat/completions` 调用——真实上游 Key 只存在服务端
- 内置连通性测试（服务端向上游 GET /models 探活）

### 3. 安全模型

- 语义工具白名单（7 命令 + 2 REST 桥接），白名单外一律拒绝；无 shell/文件系统/SQL 直通
- 权限双层把守：`admin:system` 门禁（普通用户连悬浮球都不可见）+ 前端 403 收敛
- 出站内容脱敏：DOM 提取文本送 LLM 前抹除凭据形态字符串（sk-/Bearer/token 等）
- 任意 JS 执行工具保持关闭；LLM 请求体上限 4MB、用户维度限流

## 部署指引

```bash
docker pull warlocks/panomint-web:1.9.0     # nginx 新增 agent 前缀 + 新 dist（AI 助手）
docker pull warlocks/panomint-app:1.9.0     # 命令面/LLM 代理/迁移 v46 在后端
docker pull warlocks/panomint-worker:1.9.0
docker pull warlocks/panomint-db:1.9.0      # 本版 db 镜像含 goose 到 v46（升级自动迁移）
```

升级说明：
- 迁移 v46 在 api 启动自迁移时自动执行（goose version 46）；回滚请先确认 `agent_llm_config` 表可丢弃（DROP 后 goose version 回 45）
- 升级后到 **设置 → Agent 语义接口 · LLM 上游** 配置 OpenAI 兼容上游并启用，AI 助手才可用（不配置不影响系统其余功能）
- 测试通道（可选）：`TAG_PREFIX=test bash release/docker/build-images.sh <版本>` 产出 `test<版本>` 标签镜像（版本串可辨识 test 前缀），分发走 `release/docker/push-images.sh`（结构性禁 latest），与正式通道完全隔离

## 镜像 digest（2026-10-01 Hub 实测，115 出口独立查询，1.9.0 = latest 逐字一致）

| 镜像 | 1.9.0 = latest digest |
| --- | --- |
| warlocks/panomint-web | `sha256:9e23c784f0b69…` |
| warlocks/panomint-app | `sha256:496e715764a23…`（Agent 语义接口） |
| warlocks/panomint-worker | `sha256:716c895a24235…` |
| warlocks/panomint-db | `sha256:fdc2262f9d420…`（v46 迁移） |
