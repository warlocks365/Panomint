# Panomint v1.9.6 Release Notes

> 发布日期：2026-10-11
> 上一版本：v1.9.5
> 版本类型：**小版本递增**（按 `文档/版本管理规范.md` §2）
> 版本号裁决留痕：本次主体为 4 项缺陷修复；其中「AgentPano LLM 上游私网白名单」新增了一个环境变量，
> 但该变量**留空时行为与 v1.9.5 完全一致**（仍拒绝全部私网地址），属纯增量且完全向后兼容，
> 不构成契约不兼容调整、亦无表结构变更，故归入「优化 / BUG 修复」→ 小版本 +1。
> 若后续把它扩展为「按用户粒度配置的可持久化上游列表」（落库、多用户隔离），则须按 §2 就高不就低改判中版本。

## 一、本次范围

v1.9.5 之后的全部改动。3 个已提交修复 + 本轮 2 项修复与 1 项配置放宽，12 个文件。

## 二、更新摘要

### 修复

| # | 问题 | 根因 | 修复 | 提交 |
| --- | --- | --- | --- | --- |
| 1 | 地图时间轴**无法拖拽选择时间范围**（原需求要求起止区间，实际仅支持点选单点） | 范围选择的模板事件已写，但父组件漏接 `@range-select` 接线，交互完全不触发 | 补上父子接线 | `5723e91` |
| 2 | AI 悬浮球**展开输入后永久无法退出** | 面板遮罩层未监听 `keydown.esc` 与外部点击，且失焦时未回落 | 补齐 `esc` / 外点 / 失焦三条收起路径 | `62a0999` |
| 3 | web 镜像**残留历史 chunk**（Dockerfile 已 `rm -rf` 但容器内仍存在旧文件） | 两处成因：① `COPY dist` 不会删除目标层中源端已不存在的旧文件；② 部署用的远端 staging 目录历次只覆盖同名文件，历史产物累积到 693 个 | ①Dockerfile 改为「复制到独立目录后整体替换」；②`deploy_web_dist.py` 上传前轮转 staging，并新增 staging/dist/容器三层文件数断言 | `62a0999` |
| 4 | 时间轴 `ReferenceError: Cannot access '$' before initialization` | `watch(() => pager.items.length, …)` 位于 `useTimelineStream` 解构之前；Vue 建 watch 时会同步执行一次 getter，读取尚未初始化的 `const pager` → TDZ。压缩后 `pager` 被重命名为 `$`，故线上表现为 `$` | 将 `watch` 移到解构之后，并在源码中固定该位置约束 | `8570d6d` |
| 5 | 时间轴控制台常驻弃用警告 `[vue-virtual-scroller] sizeDependencies is deprecated` | `TimelineGrid.vue` 向 `DynamicScroller` 传了 `:size-dependencies="[cols]"`；该 prop 在 3.x 已无任何功能，唯一作用是打印一次警告 | 删除该 prop（动态尺寸测量本就由库内 `ResizeObserver` 负责），保留 `min-item-size`，并在源码中留防回退注释 | 本轮 |
| 6 | **AgentPano 无法配置本地大模型**：`base_url 非法：仅允许 http/https，且不得指向本机或内网地址` | SSRF 防线三层（URL 字面量校验 / 私有 IP 判定 / 拨号层校验）默认拒绝一切私网与环回地址，防止「管理员可配 URL」退化为面向内网的请求原语；但本项目有真实的本地大模型部署需求（llama.cpp / Ollama 常跑在内网某台机器），一刀切使该场景不可用 | 新增**按 host 精确匹配的白名单**，见下节 | 本轮 |

### 新增：AgentPano LLM 上游私网白名单

- **环境变量**：`AGENT_LLM_ALLOWED_HOSTS`，逗号 / 分号 / 空白分隔，可写 `host` 或 `host:port`。
- **默认留空 = FailClosed**：拒绝全部私网与环回地址，行为与 v1.9.5 完全一致，既有部署零影响。
- **精确匹配**：写 `192.168.1.42` 只放行该地址，不放行整个 `/24` 网段；写了 `:port` 则端口必须一致。
- **接入两层**：`validateUpstreamURL`（配置保存时的字面量校验）与 `guardedTransport` 的 `DialContext`（拨号那一刻解析后判定）。第二层是防 DNS rebinding 的关键——域名解析到私网也在该层被拦。
- **云元数据地址永不放行**：`169.254.169.254`（AWS / GCP / Azure / OpenStack）、`100.100.100.200`（阿里云）、`192.0.0.192`（Oracle Cloud）在加载与判定两处均被**硬性排除**，即使写进环境变量也不生效。拿到元数据即等于拿到云账号权限，这条不做让步。
- **协议白名单不变**：仍只允许 `http` / `https`。

示例（compose）：

```yaml
AGENT_LLM_ALLOWED_HOSTS: ${AGENT_LLM_ALLOWED_HOSTS:-}
# 例：192.168.1.42:8888,ollama.lan:11434
```

### 报错文案

`base_url 非法：` 后的文案改为透传具体原因，并给出可操作提示：
> 禁止指向本机或内网地址（如需访问本地模型，请将主机加入环境变量 AGENT_LLM_ALLOWED_HOSTS）

原先的「仅允许 http/https，且不得指向本机或内网地址」把两件不同的事混在一句里，且不给出路。

### 规范与规划（文档）

- `文档/二期规划_v2.0_完全档.md` §十一 新增 AgentPano 语义接口设计一节（本期不实现，并入二期）。
- `文档/用户操作手册_v1.0.0.md` 尾注追加 v1.9.6 说明。

## 三、测试

| 范围 | 结果 |
| --- | --- |
| 前端全量 vitest | **255 / 255 通过**（19 个文件），含新增 `timelineGridDeprecatedProps.spec.js` 5 条 |
| 前端 `vite build` | 两次通过 |
| 后端 `go test ./internal/agentllm/` | ok，2.610s，含新增 `netguard_allowlist_test.go` **28 条子用例**全通过 |
| 反向验证（mutation） | 前端：手工加回 `size-dependencies` → 1 条转红；后端：①拨号层忽略白名单 → FAIL ②移除元数据排除 → 4 条转红。均已还原 |

## 四、实机验证（测试服 192.168.1.115，公网入口 `https://photo.warlocks.cn:11543/`）

| 项 | 方法 | 结果 |
| --- | --- | --- |
| 弃用警告已消除 | browser-skill 重载后读控制台，`grep -ci "virtual-scroller\|sizeDependencies\|deprecated"` | **0** |
| 列数仍随宽度重算（该 prop 声称要做的事） | browser-skill `emulate` 逐档改变视口宽 | 1600px → 8 列；760px → 4 列 |
| 白名单内私网可用 | 管理后台「Agent 语义接口 · LLM 上游」填 `http://192.168.1.42:8888/v1` → 保存配置 + 测试连通 | 「上游连通正常。」配置落库 |
| 白名单外私网仍拒 | 改填 `http://10.0.0.5:11434/v1` → 保存 | 拒绝，文案含 `AGENT_LLM_ALLOWED_HOSTS` 提示 |
| 元数据地址永不放行 | `.env` 临时把 `169.254.169.254,100.100.100.200,192.0.0.192` 一并写入白名单并重启 → 填 `http://169.254.169.254/v1` → 保存 | 仍拒绝（白名单未能放行） |
| 端口精确匹配 | 填 `http://192.168.1.42:11434/v1`（同 IP、异端口）→ 保存 | 拒绝 |
| 恢复态复测 | 填回 `http://192.168.1.42:8888/v1` → 保存 + 测试连通 | 「上游连通正常。」 |

## 五、升级注意事项

- **默认无需任何操作**：不设 `AGENT_LLM_ALLOWED_HOSTS` 时行为与 v1.9.5 完全一致。
- 需要接入本地大模型时，在 compose 的 `api` 服务下新增该环境变量并 `docker compose up -d --force-recreate api`。
- **无数据库迁移、无 API 契约变更**；前端为纯替换产物。
- 升级后 `docker compose restart web`（nginx upstream IP 缓存）。
- 新增白名单变量后建议同步核验：`docker exec pano-api printenv AGENT_LLM_ALLOWED_HOSTS`。

## 六、影响范围

| 面 | 影响 |
| --- | --- |
| 数据库 | 无 |
| API 契约 | 无（仅报错文案变化，前端透传展示） |
| 前端 | `TimelineGrid.vue` 移除一个无效 prop；`versionHistory.js` 新增版本条目 |
| 后端 | `internal/agentllm`：`netguard.go` 新增白名单能力，`agentllm.go` 报错文案透传 |
| 部署 | `docker-compose.yml` 与 `release/docker/docker-compose.synology.yml` 新增一个**可选**环境变量 |
| 默认行为 | **完全不变**（白名单默认空 = 拒绝全部私网） |

## 七、回滚方式

小版本升级，**回滚即重新部署 v1.9.5 产物**，无数据迁移、无还原步骤。

```bash
# 1) 回滚运行栈（使用 v1.9.5 镜像）
docker compose down
git checkout v1.9.5 && docker compose up -d
# 2) 若曾设置过白名单，回滚后该变量不识别即被忽略，可保留也可删除
```

若只想临时关掉白名单而不回滚版本：把 `AGENT_LLM_ALLOWED_HOSTS` 置空并 `--force-recreate api` 即可恢复 v1.9.5 的拒绝行为。

## 八、遗留与已知限制

- 白名单为**全局粒度**（按进程环境变量），不支持按用户 / 按空间差异化配置。留待二期与 AgentPano 语义接口一并设计。
- `release/docker/docker-compose.synology.yml` 中该变量默认为空字符串，群晖用户需手工填写，未做安装向导提示。
- `.workbuddy/v196_probe.js`（已废弃的 puppeteer 验证脚本）与测试服上的临时 token 文件尚未清理，留待下次一并处理。
- `scripts/deploy.sh` 仍只覆盖测试通道，正式通道 `latest` 推送为手工，门禁尚未覆盖。
