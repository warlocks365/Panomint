# Panomint v1.9.3 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.9.3**（安全修复版：1 个可达 CVE + SSRF 防线 + CSP + worker 构建根治） |
| 发布日期 | 2026-10-03 |
| 数据库迁移 | 无 |
| 源分支 | main（`b904863`，v1.9.2 tag 之后 8 commits） |
| 性质 | **安全补丁版**，含 1 个破坏性变更（见「升级须知」） |

---

## 一、本版性质

v1.9.2 是 UI 深化大版本，本版是**安全修复版**。起因是 2026-10-02 的一次全量代码审查（`文档/代码审查报告_v1.9.2.md`，556 行，未发现严重级，高 3 / 中 6 / 低 3），以及随后在群晖上的部署实测暴露的镜像构建缺陷。

审查结论：**未发现「严重」级漏洞**，系统安全水位高于同类自托管相册项目。已逐条复核确认安全的方面包括：SQL 全参数化、子进程无 shell 拼接、XSS 已封堵、路径遍历用 `filepath.Rel` 而非字符串前缀、认证防枚举三件套齐备、AES-GCM 无 IV 复用、审计日志无删除端点、CORS 无危险组合。

---

## 二、安全修复（3 项高危）

### 1. SSRF：LLM 上游地址无任何校验（高危）

`base_url` 此前只做了去尾斜杠，**无协议白名单、无内网地址拦截**。持有 `admin:system` 的账号可让服务端代替其访问任意目标（`169.254.169.254` 云元数据、内网管理面、其他服务），构成全系统唯一「可达外网 + 可打内网」的组合通路。

修复为三层防线（`internal/agentllm/netguard.go`）：

1. **协议白名单** — 仅 `http`/`https`，拒绝 `file://`、`gopher://`、`dict://` 等
2. **地址段判定** — 环回 / 私网 / 链路本地 / 未指定 / **CGNAT `100.64.0.0/10`**
3. **拨号前解析校验** — 出站 dialer 在 `DialContext` 里先解析目标主机再判定，拦截 DNS rebinding（公网域名解析到 `127.0.0.1` 只有拨号那一刻才知道）

三个出站点全部接入：配置保存时（给可操作的 400 提示）、探活、代理转发。

> 实施过程中的实测发现：Go 标准库的 `IsPrivate()` **不覆盖 CGNAT 段**，阿里云元数据 `100.100.100.200` 因此漏网。已手工补该段判定并加边界断言（`100.63` / `100.128` 应放行）。

### 2. 可达依赖漏洞：quic-go（高危）

`govulncheck ./...` 实测输出 "Your code is affected by 1 vulnerability"：

- `GO-2026-5676` — quic-go HTTP/3 QPACK Trailer Expansion 内存耗尽（`v0.59.0` → 修复于 `v0.59.1`）
- **非纸面风险**：quic-go 是 gin 的间接依赖，但 `go list -deps ./...` 实测 **19 个 quic-go 包进入编译依赖闭包**
- 已升 `v0.59.1`（同 patch 级，无破坏性），`go build ./...` 与 `go vet ./...` 均 0 错误

另有 4 处属「模块含漏洞但本项目代码未调用」，不构成风险。`x/net`、`x/crypto`、`x/text`、`x/sys` 均无可达漏洞。供应链结构复核：无 `replace` 指令、无 `math/rand` 用于安全用途。

### 3. 依赖时效：axios（高危）

`axios ^1.7.7` → `^1.20.0`，清除 **12 个 high 级 GHSA 公告**（原型链污染、header 注入、重定向 SSRF、socket 劫持等）。同 major 升级，无破坏性。

---

## 三、安全加固（3 项中危）

### 4. Content-Security-Policy 与安全响应头（已启用）

此前 CSP 出于「怕连带失败」而刻意未启用。本版按**实测得出的资源清单**放开：

```
default-src 'self'; script-src 'self';
style-src 'self' 'unsafe-inline' https://fonts.googleapis.com;
font-src 'self' https://fonts.gstatic.com data:;
img-src 'self' data: blob: https://*.autonavi.com https://*.amap.com;
connect-src 'self' https://*.autonavi.com https://*.is.autonavi.com https://dashscope.aliyuncs.com;
worker-src 'self' blob:; object-src 'none'; base-uri 'self';
form-action 'self'; frame-ancestors 'self'
```

同时补齐 `X-Content-Type-Options` / `X-Frame-Options` / `Referrer-Policy` / `Permissions-Policy` 四条响应头。

`script-src` 保持严格（项目无 inline script）；`style-src` 保留 `'unsafe-inline'` 是因为 Vue `:style` 动态绑定算样式，属设计使然。

**验证**：headless CDP e2e 真实打开 **17 条路由，17/17 PASS**，采集 security 日志 / CSP 阻断 / console error / 同源 4xx5xx 四类证据 → **CSP 违规 0 条**。资源真实性已验证（`Noto Sans SC` + `Outfit` 两族字体真实加载 309 个 font-face；地图页 maplibre canvas 真实渲染）。

> **踩坑记录（已写进 Dockerfile 注释）**：安全头最初写在 nginx `server` 块里，容器内 `grep` 到 5 处配置但 **HTTP 响应里一条都没有**。根因是 nginx 规则 —— **子 `location` 只要出现任意一条 `add_header`，就完全不再继承父级的**（不是逐条合并，是整体覆盖）。本项目 4 个 `location` 各自都带 `add_header`，故 `server` 块写法一条都不生效。正解是**逐 `location` 重复声明**。

### 5. 会话撤销窗口收紧

`access token` 是自证的纯 JWT（服务端不查库），用户被禁用或改密后，已签发的令牌在原 15 分钟内仍可使用。本版将有效期收紧至 **5 分钟**（配合前端已有的自动续期，对用户无感）。

> 彻底解决需引入 `session_ver` 并在鉴权时比对，代价是每请求多一次校验；当前自托管单实例负载下 5 分钟是性价比最高的取舍。

### 6. 错误文本回显

`agentllm` 探活失败时把 Go 原始错误回显客户端（内含目标 URL 与网络细节）。与 SSRF 叠加会给攻击者提供「内网端口是否开放」的观测通道（连接拒绝 / 超时 / DNS 失败可区分出版本拓扑）。已改为固定文案，完整错误只进服务端日志。

---

## 四、部署修复（群晖实测）

### 7. worker 容器无法启动（Dockerfile 构建残留）

**现象**：群晖 Container Manager 部署 v1.9.2 后，`panomint-index-worker` / `panomint-transcode-worker` 崩溃循环：

```
exec /usr/local/bin/worker-entrypoint.sh: no such file or directory
```

**根因**：上一版的「根治」提交（`a7bf379`）替换 Dockerfile heredoc 段时匹配错了片段，导致脚本正文被当作 Dockerfile 指令写入，构建产物**根本没有 entrypoint 文件**。

**修复**：清除残留内容（53 行 → 30 行），entrypoint 改以真实文件提交；`.gitattributes` 将 `Dockerfile` 与 compose 的行尾**钉死为 LF**（此前只覆盖 `*.sh`，Windows 工作区的 `core.autocrlf` 会把修正后的 Dockerfile 再转回 CRLF，是事故放大器）。

**Hub 镜像已验证**：`warlocks/panomint-worker@sha256:cc1383fb` 内 entrypoint 存在、权限 0755、shebang 纯 LF，实测 `docker run` 行为正确。

### 8. 群晖编排模板补 `STORAGE_CIPHER_KEY`

`v1.9.0` 起挂载凭据与 LLM API Key 采用 AES-256-GCM 列级加密，env 未配置时为 FailClosed。官方模板与本文档此前的群晖编排**均遗漏此变量**（v1.9.0 发版遗漏），已补占位行。

---

## 五、稳定性（3 项中危）

| 问题 | 根因 | 修复 |
| --- | --- | --- |
| 加密密钥并发竞态 | `cipherKey` 包级变量无同步保护，并发首次调用是 Go data race | 改 `sync.Once`；测试改用可注入 env 源 |
| 9 处查询可能静默返回部分结果 | `rows.Next()` 返回 false 有「读完」与「出错」两种原因，缺 `rows.Err()` 区分 | 补齐检查（连接中断不再被当成查完了） |
| 账户级写操作挂读权限位 | `PUT /preferences/map` 与 `PUT /user/ui-prefs` 用 `media:read` 把关；`GET /spaces` 未挂权限位 | 前两者去 permRead（改的是自己的数据），`/spaces` 补挂 |

另补：`/debug/channel` 限流豁免名单加护栏注释（明确**禁止**把 `/dav` 加入豁免——Basic 认证下按 IP 限流是其唯一的暴力破解防护）。

---

## 升级须知

**本版含 1 项需要留意的行为变更**：

> **访问令牌有效期由 15 分钟缩短为 5 分钟。** 前端已有自动续期机制（`tokenStore` + `http.js` 的 refresh 链），正常使用无感。但**若你有用脚本直接持有 access token 长期调用 API**，需要改为每 5 分钟刷新一次，或改用 refresh token 换取。

其余变更向后兼容。

### 群晖升级步骤

1. Container Manager → **映像** → 重新下载 `warlocks/panomint-{app,worker,db,web}:1.9.3`（**必须重新拉取**：本地缓存的 worker 镜像是坏的）
2. 项目 → **停止** → **构建/重新启动**
3. 验证：`panomint-index-worker` / `panomint-transcode-worker` 日志应出现「worker 启动」而非报错

编排文件请使用本仓库 `release/docker/docker-compose.synology.yml`（tag 已是 1.9.3，含 `STORAGE_CIPHER_KEY` 占位行）。

---

## 六、镜像产物（Docker Hub）

| 镜像 | `:1.9.3` 与 `:latest` digest（两者一致） |
| --- | --- |
| `warlocks/panomint-app` | `sha256:b3213ccb78c4505f21df7acf6bda540412c26302defba2ee3467df593a450cf3` |
| `warlocks/panomint-worker` | `sha256:c715f10a7cd038bc4a29f8f2a2b136aa1402f67d161c824bdd4cd88a10b031b4` |
| `warlocks/panomint-db` | `sha256:53f4e33716b0803896ce1f95ecd5b59bb799de1559be8fa37a9e9a26a9791f76` |
| `warlocks/panomint-web` | `sha256:eaf0913eae6206b85abe18f4f7492c184856c86bfec6da2b121fdcd6a15c811a` |

体积：app 1.16GB / db 658MB / worker 353MB / web 52.2MB。

验证方式：**删除本地 `warlocks/*` 镜像后从 Hub 重新拉取**，比对 `RepoDigests` —— 不依赖本地 `inspect` 的推送记录，确保 Hub 侧真的可拉。

---

## 七、验证记录

| 项 | 结果 |
| --- | --- |
| `go vet ./...` | 0 错误 |
| `go test -count=1 ./...` | 全仓通过（`internal/index` 2 例经**非 root 容器复验为 ok** —— root 下 `chmod 000` 语义不成立，属环境错非产品缺陷） |
| `govulncheck ./...` | 0 个可达漏洞（修复后） |
| `npm audit` | 0 个 axios 条目（vite/esbuild 3 个 high 全作用在 `vite dev` 开发服务器，生产产物不含 dev server，故不动） |
| `npm run build` | 成功 |
| headless CDP e2e | 17/17 路由 PASS，CSP 违规 0 条 |
| 发版一致性校验 | 19 项断言（见发布记录） |

---

## 八、遗留未做

| 项 | 原因 |
| --- | --- |
| vite 8 major 升级 | 3 个 high 公告全作用在 `vite dev` 开发服务器，修复需跨 major 升级（含破坏性变更），建议单独窗口处理 |
| `script-src` 进一步收紧 | 当前 `'self'` 已是最严的实用值；如需去掉 `style-src` 的 `'unsafe-inline'`，需改造 Vue 动态样式写法 |
