# API 详细契约 (v1.16)


# 全景相册系统 · API 详细契约（OpenAPI 风格）

> 版本：v1.16 ｜ 日期：2026-09-26\ 配套：PRD v3.1 / TDD v1.1 / 数据库 DDL v1.1\ Base URL：`https://<domain>/api`\ 认证：**独立后台账户**，Bearer JWT（不对接 DSM）；SSO 走 OIDC

## v1.16 变更说明（2026-09-26，Job000126「360° 视频无 HLS 原始回退播放」）

背景：总闸门（`auto_transcode`）关闭或实时开关未开时，无 HLS 的 360° 视频此前完全无法播放（`usePanoStream` 只吃 HLS + CreateJob 409 死局）。本版打通「原始文件回退贴球面」：Three.js VideoTexture 对原始 mp4 blob 与 HLS 解码 video 元素同等可用（iOS 原生分支同型实证），回退后视角转动 / 陀螺仪 / VR 头追全部保留，仅清晰度切换与拖动起播降级。e2e：G 矩阵 + 渲染配对回归 **37/37 ALL GREEN**。

1. 前端 `usePanoStream.attachSource` 增 `blob:` 前缀分派 → 新 `attachOriginal`（video 元素直吃 blob，纹理循环照常；档位选择器随 `qualityOptions` 为空自动隐藏）
2. `usePlayerMedia`：`panoSrc` 优先级改为 `hlsUrl || panoOriginalUrl`——HLS 就绪后经 `:key=panoSrc` 重挂载自动切回 HLS；新增 `panoOriginalUrl` / `panoFallbackLoading` 状态与 `untrackBlob`（HLS 就绪即 revoke 原始 blob，防 4K 双份内存）；无 HLS 的 360 视频加载即回退，实时开+闸门开则 `startTranscode(true)` 并行发起（attach-or-create 幂等语义不变）
3. `PlayerView` 新增回退提示条 `pano-fallback-notice` 四态：转码中/已切换、发起失败保持原始播放、未转码+「立即转码」按钮（`startTranscode(false)`）、闸门关闭说明；`panoFallbackLoading` 前置守卫防 TranscodePrompt 闪现
4. **AND 语义不变**：总闸门关闭时仅回退、不发起转码（`auto_transcode AND realtime_transcode` 语义逐字节不动，409 闸门测试不放松）
5. 回归修复（e2e 截图目检逮出）：回退提示条为独立 `v-if`，其后的普通视频分支由裸 `v-else` 改 `v-else-if="mode==='video'"`——否则照片 / 全景 / HLS 就绪态会意外渲染空 video 播放器
6. **无后端改动**（零 API 变更、零迁移）；本节为前端播放器行为契约补充

## v1.15 变更说明（2026-09-26，Job000125「HLS 流媒体设置 + HTTPS/证书设置 + 访问守卫强化」）

背景：管理后台补齐流媒体与网络两块系统级配置，并强化前端访问守卫。设计方案《流媒体与访问控制设计_HLS_HTTPS_访问守卫_v1.0.md》（含 HLS 必要性论证：4K 360° 直连四矛盾 / 视线不可预知 → ABR 必须 / 微信 H5 iOS 原生 + Android MSE）。e2e：API 层 38/38、UI 层 20/21（唯一未过项为测试状态残留，SQL 归位后语义全成立）。

1. `system_transcode_config` 增三列（迁移 `00042`，`DEFAULT` = 存量行为逐字节不变）：`hls_seg_seconds INT DEFAULT 4`（分片时长，仅影响新任务，worker 读侧 60s 缓存）、`hls_cache_profile TEXT DEFAULT 'balanced'`（no_cache=调试 / balanced=m3u8 no-cache+ts 一年=历史行为 / aggressive=归档）、`stream_base_url TEXT DEFAULT ''`（空=站内相对路径）；`GET/PUT /admin/transcode-config` 响应/请求体增量三键（PUT 部分更新扩为五键、至少一项）；校验：seg 2-20、profile 枚举、url 须 http(s):// 且含主机（非法 → 400 `ErrValidation`）
2. HLS 缓存头由 `HLSCacheControl(profile, isMaster)` 纯函数输出：三档 m3u8/ts 头矩阵（no_cache 全不缓存、balanced m3u8 no-cache、aggressive m3u8 60s；ts 一年 immutable 仅 balanced/aggressive）；`ServeHLS` 热读缓存档（60s TTL，读失败沿用旧值）
3. 新表 `system_https_config`（迁移 `00042`，singleton）+ 新包 `internal/httpsconfig`：`GET /admin/https/config`（视图含 `force_https/cert_path/cert_key_path/cert_not_after` + 运行时注入 `cert_dir/request_proto`）、`PUT /admin/https/config`（当前仅 `force_https` 键）、`POST /admin/https/cert`（multipart cert+key）；全部 `admin:system`，写操作落审计 `admin.settings.patch`/`setting`/`https-config`
4. 强制 HTTPS = **应用层中间件**（挂 CORS/限流之前）热生效：判据为反代透传的 `X-Forwarded-Proto`，**XFP==http 才 301**（缺失/其他值不拦——防无反代部署 301 死局）；`/health` `/ready` 豁免（探活不跟随重定向）；开关 60s 进程内缓存 + 读失败沿用旧值（可用性优先），**PUT 成功后 `InvalidateForceCache()` 立即失效**（e2e 实测踩出：不失效则变更最长 60s 不可见）；301 Location = `https://<host><RequestURI>`，**不带原 HTTP 端口**（nginx `$host` 语义：HTTPS 走 443，保留 8088 反指向不监听 TLS 的端口）。**架构边界**：中间件在 api 层，只能拦 API 请求；SPA 页面（/timeline 等）由 nginx `location /` 直接回 index.html，页面级跳转由入口反代（Caddy auto_https）兜底——应用层为 API 面的纵深防御
5. 证书上传链：multipart 读内存（1MB 上限）→ PEM 语法预检 → `tls.X509KeyPair` 配对校验（失败一字节不落盘）→ 文件名服务端定死 `fullchain.crt`/`private.pem` → 落盘 0644/0600 → 到期日（x509 NotAfter）入库；`HTTPS_CERT_DIR` 未配置时上传端点 **503 fail-closed**（compose 为 api 服务注入，落 mediadata 卷 `certs/` 子目录搭备份车）；响应带 `apply_hint`（指向 Caddyfile.tls 的 TLS_CERT/TLS_KEY + `docker compose restart caddy`）——**不做 TLS 运行时热切换**（TLS 终止在反代，自动化重启入口容器风险大于收益）
6. 前端守卫强化：`router.beforeEach` 鉴权闸门携带 `?redirect=<原目标>`，登录成功经 `safeInternalPath()`（拒绝 `//` 协议相对 / `scheme:` 前缀 / 控制字符 / 回 login|setup 本身——开放重定向防御）回跳原页，非法回退 /timeline；setup 闸门增 15s TTL 进程内缓存（`invalidateSetupCache()` 供 SetupView 初始化成功后主动失效，防 15s 窗口内反向弹回）
7. 管理后台：新增「网络」页签（10 页签，`tab-network`）挂 `HttpsCard`；转码页签尾挂 `HlsSettingsCard`（三字段 + clientValidate 前置 + 外部地址红色警告）；testid 系 `hls-seg-input/hls-cache-select/hls-base-input/hls-save` + `https-proto/https-cert-days/https-force-toggle/https-cert-upload/https-apply-hint`

## v1.14 变更说明（2026-09-26，Job000121「转码远程调试通道」全量落地 + Job000122 CI paths-ignore）

背景：一次性、短寿命、全审计的远程排障通道。管理员在设置页开启 → 系统签发 WSS URL+密钥 → 参考 agent（`cmd/debugctl`）经白名单结构化命令操作转码控制面，**不提供任意 shell**。设计方案《转码远程调试功能设计方案_v1.0.md》（附录 A 六裁决全采纳）。e2e 全矩阵 69/69 ALL GREEN 后随 v1.4.0 发布。

1. 新增 **§20 转码远程调试通道**：管理端点 4（status/enable/rotate/disable，全部 `admin:system`）+ WS 接入端点 1（无 JWT，Bearer 密钥认证，upgrade 前 9 步前置矩阵）+ 命令面白名单 10 命令。凭证明文仅在 enable/rotate 响应出现一次，库中只存 sha256 摘要；接入 URL 恒 `wss://`（明文 ws 被 426 拒绝，回退拼 ws:// 给的是必死链）
2. 新表 `debug_channel`（迁移 `00041`，BOOLEAN PK 单行表惯用法）：`channel_id`(24 hex 公开标识)/`key_digest`(sha256 hex)/`created_by`/`expires_at`/`last_connect_at`/`last_connect_ip`（可空，查询列 COALESCE 兜空串）
3. 失效四路并行：TTL 默认 24h（1/8/24/72 四档）+ 手动关闭 + rotate 旧钥即刻作废踢连（4004）+ 认证失败锁定（5 次/10 分钟锁 15 分钟）；到期由 reaper 巡检（`DEBUG_REAPER_INTERVAL` 秒，默认 300，compose 已透传 api 服务）踢连 4001 + 审计 `reason=expired`（审计主路径）；status 与握手第③步均过滤到期行（不可探测语义：未开启/已关闭/已过期一律 404 同形）
4. 命令面 v1 三处收窄：`subscribe` 仅支持 `jobs:"*"`（任务 id 数组留 v2）；`job.cancel` 不覆盖 running（INVALID_STATE，终态后处置）；`job.log.tail` 返回**结构化档案**（任务行 + `detail->>'job_id'` 命中的最近审计事件 ≤500 条，无 follow）——产品无 per-job 日志文件，worker 输出在容器 stdout
5. 失败锁定计数/锁键 `debug:fail:<ip>`/`debug:lock:<ip>`（**无前缀**）与握手桶 `rl:debug:hs:<ip>`（RateLimiter 自带 `rl:` 前缀）三形态并存，均与全站 60 令牌桶隔离；运维清理须 `*debug:*` 模式全量扫
6. 审计八动作全落 `audit` 表（`target_type=debug_channel` 单查询收齐）：enable/disable(manual|expired)/rotate/connect/disconnect/cmd（每条命令）/auth_fail/locked；前端审计摘要复用既有 `GET /admin/audit?target_type=debug_channel&limit=8`，**零新增审计端点**
7. nginx：docker/web 内嵌 conf 独立 `^/debug/` location（Upgrade/Connection 头 + 长读超时），第 2 组纯 API 正则增 `debug`——**不并入**第 1 组导航判别前缀（WS 与 SPA 回退语义冲突）
8. 前端：设置页 `DebugSettingsCard`（权限门 `admin:system`，无权限不渲染整卡）+ 拆出 `DebugCredsPanel`（一次性凭据回显）/`DebugAuditList`（最近 8 条）；testid 系 `debug-toggle/debug-ttl/debug-url/debug-key/debug-copy/debug-status/debug-rotate/debug-off/debug-audit`

## v1.13 变更说明（2026-09-25，Job000124「播放时自动转码（实时转码）」+ 管理概览索引状态中文化）

背景：管理后台「转码」页签在 v1.11 总闸门（`auto_transcode`）之上新增第二个系统级开关 `realtime_transcode`（迁移 `00040` 增列，`DEFAULT FALSE` = 存量行为逐字节不变）。设计方案见《设计方案_播放实时转码_Job000124.md》。两开关 **AND 关系**：有效自动触发 = `auto_transcode AND realtime_transcode`——总闸门语义（409/fail-closed/CLI 例外）全部不动，新开关只管「触发方式自动化」。

1. `system_transcode_config` 增列 `realtime_transcode BOOLEAN NOT NULL DEFAULT FALSE`；`GET /transcode/config` 与 `GET/PUT /admin/transcode-config` 的响应视图增量 `realtime_transcode`（bool，缺省 false）——旧前端忽略新 key 不炸
2. `PUT /admin/transcode-config` 改为**部分更新**语义（沿用 Job000123「缺失不改」）：body 为 `{"auto_transcode"?, "realtime_transcode"?}` 两可选键、**至少一项**，全缺 → 400；map 先取一层再解指针（防误清空/并发覆盖）；审计 detail 按实际变更键落
3. `POST /transcode/job` 增可选 `auto: true`（播放器自动触发分支）——归属校验/总闸门/错误口径与手动全同，差异仅两点：**attach-or-create**（该媒体已有 `pending/running` 任务直接复用其 `job_id`，202 响应增量 `reused: true`——同一媒体全站最多一个活动任务，幂等防双击/多端并发）；档位由服务端按源分辨率 `ProfileForSource` 自动选取（与 CLI 补排同口径），请求 `profile` 被忽略。不带 `auto` = 旧语义逐字节不变（手动、profile 校验、新建任务）
4. 播放器配套：实时开关开且总闸门开时，无 HLS 视频（普通 + 360°）播放即自动发起；普通视频转码期间原始文件照播、完成后原地热切换 HLS（hls.js ABR 调优 `capLevelToPlayerSize`/`maxBufferLength:30`/`maxMaxBufferLength:60`，升降档由双 EWMA 带宽估算承担）；转码失败普通视频保持原始播放（轻提示）、360° 视频失败态可手动重试
5. 管理端「转码」页签第二个开关行（data-testid `tc-rt-toggle` 系）；总闸门关时实时开关 UI 禁用（AND 语义的界面表达）
6. 管理概览「索引状态」展示层中文化（纯前端映射，API 码值不动）：`idle→空闲 / running→运行中 / failed→失败 / unknown→未知`；最近任务 `pending→排队中 / running→运行中 / done→已完成 / failed→失败 / canceled→已取消`

## v1.12 变更说明（2026-09-25，Job000123「账号扫描根目录分配 + 成员自助扫描」）

背景：管理端扫描（`POST /admin/scan`）此前仅 `admin:system` 可用，普通成员想把挂载目录里已有的照片导入自己库无任何入口。本版把扫描能力开放给普通账号，但**边界不再是整个 MEDIA_ROOT**，而是管理员按账号分配的 `users.scan_root`（迁移 `00039`，`VARCHAR(512)` 可空）：

1. **三态语义**：`NULL` = 未分配（成员侧 `POST /scan`、`GET /fs/tree`、`GET /jobs/:id` 一律 **403 `SCAN_ROOT_REQUIRED`**，fail-closed——「没有根目录」与「根目录就是媒体根」必须区分）；`""` = 已分配、根 = MEDIA_ROOT 本身；`"photos/trip"` = 已分配子目录
2. 新增 `PUT /admin/users/:id/scan-root`（需 `admin:users`）：分配/取消。请求体 `{"scan_root": "相对 MEDIA_ROOT 目录"}`；`""` = 媒体根本身；显式 `null` = 取消分配；**字段缺失 → 400**（防误清空）。写入前校验：路径穿越拒绝、必须是**真实存在的物理目录**（非虚拟目录）、`_imports` 保留段（任意层级）拒绝；存储值为归一化斜杠路径。落审计 `admin.user.update`，`detail.scan_root` = 归一化路径或 `null`
3. 新增 `GET /user/scan-root`（任意登录用户）：查自己的分配 `{assigned, scan_root}`——成员扫描面板挂载前置（`assigned=false` 直接引导，不发扫描请求）
4. 新增成员三端点（均 authed + 读/写权限位）：`POST /scan`（同管理端异步语义，202 `{job_id,status,root,dir}`，入库归属调用者）、`GET /fs/tree`（目录树懒加载，`_imports` 不下发）、`GET /jobs/:id`（只查 `index_jobs` 且归属过滤写在 SQL——别人的任务 id 404 与「不存在」逐字节同形；`id` 非 UUID 先于 DB 查询 400）。越界/保留段一律 400 `INVALID_INPUT`
5. 前端配套：管理后台用户页签「分配扫描根」列与目录树选择器（可选中媒体根本身、可清空=取消分配）；工具箱第 4 页签「扫描导入」成员面板（未分配引导/分配被收回回引导态，轮询 seq 守卫）
6. nginx 第 2 组纯 API 前缀增 `scan|fs|jobs`（第 1 组导航判别前缀不变）

## v1.11 变更说明（2026-09-25，Job000120-r2「自动转码开关上收系统级」）

用户裁决「播放与转码功能移入管理里」：v1.10 的用户级开关上收为管理员系统级开关。迁移 `00038` 建 `system_transcode_config` 单行表（singleton 主键硬约束）并 DROP `user_ui_prefs.auto_transcode`，`DEFAULT true` = 存量行为不变。

1. `GET /user/ui-prefs` 响应**移除** `auto_transcode` 键（v1.10 第 1 条作废）；`PUT /user/ui-prefs` 的 keep-on-absent 例外同步作废，恢复既有八键统一语义
2. 新增只读 `GET /transcode/config`（任何登录用户）：系统级开关视图 `{auto_transcode, updated_at}`——播放器加载视频前预判姿态（关闭时如实提示、不发起注定 409 的请求）。该值非机密，写权限由 admin 端点单独把守
3. 新增 `GET/PUT /admin/transcode-config`（需 `admin:system`）：读写同一视图；PUT 请求体必须含非 null `auto_transcode`（缺省/null → 400）；写操作落审计 `admin.settings.patch`/`setting`/`transcode-config`
4. `POST /transcode/job` 闸门改判系统级真源：开关关闭 → **409 `TRANSCODE_DISABLED`**「管理员已关闭自动转码，请联系管理员开启」（闸门位置不变：归属/类型校验之后、写任务行+入队之前；查询出错 fail-closed 500）；CLI 批量补排（`transcodectl enqueue-videos`）仍不受约束
5. 前端配套：管理后台新增「转码」页签（AdminView 第 9 页签 `TranscodeTab.vue`，data-testid `tc-toggle` 系）；设置页「播放与转码」卡移除（`TranscodeSettingsCard.vue` 删除）；播放器改读 `GET /transcode/config`

## v1.10 变更说明（2026-09-25，Job000120「自动 HLS 转码用户级开关」）

背景：超大视频转码长时间占满宿主机 CPU/内存。开关落 `user_ui_prefs.auto_transcode`（迁移 `00037`，`BOOLEAN NOT NULL DEFAULT true`，存量行为不变），关闭后播放器直接播放原始文件，缩略图/语义索引照常生成（本就不依赖转码管线）。

1. `GET /user/ui-prefs` 响应增 `auto_transcode`（bool，缺省 true）——设置页「播放与转码」卡与播放器读取同一真源
2. `PUT /user/ui-prefs` 对该键采用 **keep-on-absent** 例外语义：请求缺省此键 = **保留现值**（不回落默认）——旧客户端整行 PUT 不会把用户已关的开关清回默认；与既有 8 键「缺省=清回默认」语义不同，是刻意的向后兼容取舍（INSERT 分支缺省落 `true`）
3. `POST /transcode/job` 增闸门：调用者 `auto_transcode=false` → **409 `TRANSCODE_DISABLED`**「已关闭自动转码，可在设置页重新开启」（闸门在归属/类型校验之后、写任务行+入队之前）。CLI 批量补排（`transcodectl enqueue-videos`）属管理员工具不走本端点、不受约束
4. 前端配套：设置页 `TranscodeSettingsCard`（data-testid `tc-toggle`）；播放器每次加载现取偏好——普通视频无 HLS 维持原始文件 blob 回退，360° 视频无 HLS 且开关关闭时如实提示（360° 依赖 HLS 切片，原始文件无法全景播放）；`startTranscode` 客户端先挡（服务端 409 双保险）

## v1.9 变更说明（2026-09-25，Job000118「存储位置移除 + 挂载语义目录」）

1. **移除 §18 存储位置**：功能自 v1.7 落地后从未启用（写入侧零消费、线上零数据，评估见《存储位置评估与挂载语义改造方案_v1.0.md》），整功能删除——`internal/storage/locations.go` 与四端点移除，迁移 `00035_drop_storage_locations.sql` 回滚 `media.location_id` 与 `storage_locations` 表；前端 `StorageLocationPanel.vue` 删除
2. **网络挂载落点语义化**：`POST /storage/mounts` 新增 `landing_dir` 字段（相对媒体库根、逐段 slug 校验、拒穿越、保留前缀 `_imports`/`@eaDir` 黑名单；创建后不可改）。创建时后端在媒体库根下 `MkdirAll(landing_dir)` 并注册 `folder_dirs`——**空挂载目录在「文件夹」页签立即可见**，且天然可被扫描。存量行迁移回填 `'_imports/'||left(id::text,8)`（行为不变）。详见 §18 占位节下方存储挂载端点

## v1.8 变更说明（2026-09-24，v1.0.0 发布：首次安装引导 + 版本端点，Job000105/107）

新增 §19 首次安装引导两端点与 `GET /version`。实现见 `internal/setup/setup.go`、`internal/version/version.go`：

1. `GET /setup/status`（公开）：`{initialized: bool, version: string}`。`initialized` = users 表是否已有任何用户；查库失败 → 500（**fail-closed**：状态不明时绝不放行初始化动作）
2. `POST /setup`（公开）：一次性创建首个 owner 账号。请求 `{email, password(8~128), display_name?(≤64，缺省「管理员」)}`；成功 201 `{id, email}`；系统已有用户 → 409 `SETUP_COMPLETED`「系统已完成初始化，请直接登录」（引导不再出现的后端权威闸门）；格式错 → 400 `BAD_REQUEST`
3. 并发语义：检查与创建在同一事务内、先取 `pg_advisory_xact_lock` 固定键 —— 并发提交不可能双双通过（竞态由库层消除，不依赖"先查再改"）
4. 审计：创建成功写 `setup.complete`（actor = 被创建者本人，detail 只记邮箱）
5. `GET /version`（公开）：`{version, commit, build_date}` —— 发布产物由 `release/build.sh` 经 `-ldflags -X panoalbum/internal/version.*` 注入（`VERSION` 文件为唯一真源，规则见《版本管理规范.md》）；开发产物恒 `"dev"`
6. 生产种子语义变更（衔接 §2 登录）：`cmd/api` 的管理员种子（`admin@pano.local`）**仅在非 prod 环境或显式设置 `ADMIN_PASSWORD` 时写入**；prod 全新部署的第一个账号由本向导创建
7. 前端配套：`/setup` 公开路由 + 全局守卫（未初始化 → 全部导航重定向 `/setup`；已初始化 → `/setup` 重定向 `/login`）；nginx 反代新增两前缀（`setup` 进导航判别组、`version` 进纯 API 组）

## v1.7 变更说明（2026-09-23，Job000103「存储位置管理」）

新增 §18 存储位置四端点（命名物理存储根）。实现见 `internal/storage/locations.go`，迁移 `00034_storage_locations.sql`：

1. 存储位置 = 命名物理存储根，名称对应部署时容器内映射目录名（Docker 映射名约束 slug 化：`^[a-z][a-z0-9-]{0,31}$`，禁点号/斜杠/大写/空格——HTTP 层 400 `INVALID_NAME` 与表 CHECK 双保险，防路径穿越与非法挂载名）
2. `media.location_id` 可空 UUID FK `ON DELETE SET NULL`：NULL = 默认存储根（存量行为不变）；删除位置时其媒体自动回默认存储根
3. 名称创建后不可修改：`PATCH` 带 `name` → 400 `NAME_IMMUTABLE`（引用稳定裁决）；`PATCH` 仅改 `description`
4. 删除前置引用守卫：仍有媒体引用 → 409 `LOCATION_IN_USE`「该位置仍有 N 项媒体引用，须先迁回默认存储」；无引用物理删，回 204
5. 重名 → 409 `DUPLICATE_NAME`（表 UNIQUE 约束；HTTP 层 pg 23505 翻译）
6. 权限：四端点均要求系统管理员（`requirePrivileged`，与网络挂载同口径），非管理员 403
7. 已知文档缺口（登记不修）：Job000070 网络挂载 `/storage/mounts` 六端点未入契约，后续补录

## v1.6 变更说明（2026-09-23，Job000101「个人空间按相册分组视图」）

补录 `GET /albums/groups`（个人空间分组视图数据源）与 `GET /media` 的 `album=none` 过滤。实现见 `internal/albums/groups.go` / `internal/media/timeline.go`：

1. `GET /albums/groups`：本人 personal 空间「按相册分组」首屏聚合。分组范围 = `type IN ('manual','favorites')` 且**有可见成员**的相册（favorites 置顶序，与 `GET /albums` 一致）；smart 不入分组（成员由 criteria 动态计算、无 `album_items` 行）。响应 `{groups:[{album_id,name,kind,count,items[≤8],truncated}], ungrouped:{count,items[≤24],has_more,next_cursor?}}`；全部媒体行过 `mediascope.VisibleCondFor`（调用者可见集），fail-closed
2. 未分组桶 = 不在「本人拥有的任何相册」里的 personal 媒体（仅被 smart criteria 命中仍算未分组）；`has_more` 时给 `next_cursor`，续翻走 `GET /media?album=none&cursor=…`（同族游标契约）
3. `GET /media` 增 `album=none` 查询参数：过滤出不属于本人任何相册的 personal 媒体（谓词同未分组桶口径，`NoAlbum` 绑 `Scope.OwnerID`）；其余取值暂不识别（YAGNI）
4. `GET/PUT /user/ui-prefs` 增固定键 `spaces_group_by_album`（bool，默认 false）= 空间页个人空间默认视图模式（与地图偏好同端点同契约：固定键整体替换）
5. 前端：空间页「时间轴｜按相册」分段切换（个人空间专属；偏好 500ms 防抖落服务端）；每组前 8 项 + 「查看全部」跳相册详情页；未分组桶前 24 项 + 截断提示

## v1.5 变更说明（2026-09-23，Job000100「移动/复制目标选择」）

补录 `POST /media/batch`（Job000066 落地时漏登记的端点）并扩展目标选择语义。实现见 `internal/media/batch.go` / `batch_store.go`：

1. 请求：`{ids[1..200], op, folder_path?, album_id?, tag_ids?, taken_at?, place?}`；`op ∈ delete|move|copy|add_tags|remove_tags|set_meta|share_space`；响应 `{succeeded, failed:[{id, reason}]}`（部分成功不回滚）
2. `folder_path`（move/copy 目录目标）：move 缺省=根目录；copy 缺省(nil)=副本继承源目录，**显式给（含空串）=置为目标目录**。目标为注册目录时消费 `folders.CanWrite`（无 write 授权 → 403 FORBIDDEN「无目标目录的写入权限」）
3. `album_id`（move/copy 相册目标，Job000100 新增）：move=加入相册成员（相册为虚拟集合，`folder_path` 不变）；copy=副本行全部生成后一次性入册 `album_items`（入册失败时副本保留在个人空间、按源 id 报失败）。守卫与 `POST /albums/:id/items` 逐字一致：404 同形「相册不存在」/ 400 `SMART_READONLY` / 403「仅相册所有者或管理员可添加媒体」，**先于归属过滤 fail-fast**；入册前做整笔可见性校验（谓词主体=相册属主，与 `internal/albums.addItemsQueries` 同形制），任何一项不在相册属主可见范围 → 整笔不写、403
4. 前端：批量栏「移动/复制」走统一对话框宿主的 `pickTarget`（`TargetPicker.vue`，目录树/相册双 tab；智能相册行为禁用态）。约定：操作成功=清空选择集+通知宿主刷新，失败=保留
5. 附带守卫：`PATCH /folders/rename` 新增「目录移入自身/子目录」拦截——`to` 落在 `from` 子树（段边界前缀）→ 400 `BAD_PATH`「不能将目录移入自身或其子目录」（防 a → a/b 自引用目录环）

## v1.4 变更说明（2026-09-23，Job000098「应用密码管理自助化」）

补录 §2 应用密码三端点。背景：WebDAV Basic 认证（§16）早已支持「邮箱+应用密码」，
但此前没有任何端点能设置它（列是 DDL 预留的、消费方 `dav.go` 是现成的，唯独写入路径缺失）。
没有它，第三方客户端只能配主密码，主密码一泄漏就是全部。实现见 `internal/auth/password.go`：

1. §2 新增 `GET /user/app-password` → `{set}`（只回是否已设置，**永不回散列**）
2. §2 新增 `PUT /user/app-password`（生成/轮换）→ `{app_password}`；格式 `pano-` + 40 位 hex
   （20 字节 crypto/rand，160 位熵；45 字符远低于 bcrypt 72 字节上限）
3. §2 新增 `POST /user/app-password/clear`（清除）→ `{ok}`
4. 安全语义：生成与清除**都必须验证当前密码**（防「捡到开着的电脑即可铸造/抹除长期凭据」）；
   轮换语义为覆盖（旧应用密码立即失效）；**不吊销任何会话**（应用密码不走登录、不产生会话，
   与 `PUT /user/password` 有意不同）；明文只在生成响应里出现一次，服务端只存 bcrypt 散列
5. 审计：`user.app_password`，detail 仅 `{operation: rotate|clear}`，明文绝不入审计
6. 前端：设置页「应用密码」卡（`AppPasswordCard.vue`），状态徽章/生成/轮换/清除 +
   一次性明文展示与逐级降级复制（局域网 http 无 Clipboard API 时退化 execCommand/手动）

## v1.3 变更说明（2026-09-20，Job000054「SSO/OIDC 登录」）

在 v1.2 基础上补录 §2 SSO/OIDC 三端点的完整语义（v1.2 仅有 `POST /auth/sso/oidc` 一行声明）。实现见 `internal/auth/sso.go`：

1. §2 新增 `GET /auth/sso/config` → `{enabled}`（公开；登录页据此决定是否渲染 SSO 按钮）
2. §2 新增 `GET /auth/sso/oidc/login` → 302 到 IdP 授权地址（带一次性 state；未配置 404 `NOT_CONFIGURED`）
3. §2 `POST /auth/sso/oidc` 补全语义：请求 `{code, state}` → 响应**与密码登录同形** `{access_token, refresh_token, expires_in, token_type}`；失败对外一律 401 `SSO_FAILED` 同形（state 错/code 无效/token 换不到/userinfo 失败/`email_verified=false` 不分层——防 IdP 探测面），claims 缺 email 单独 400 `EMAIL_REQUIRED`（管理员可诊断的 IdP scope 配置错误）
4. 接入方式：通用 OIDC（Keycloak/Authelia/任意标准 IdP），环境变量 `SSO_OIDC_ISSUER / SSO_OIDC_CLIENT_ID / SSO_OIDC_CLIENT_SECRET / SSO_OIDC_REDIRECT_URI / SSO_OIDC_SCOPES`（默认 `openid email profile`）；四项必填缺一即整体未启用（FailClosed）
5. 账户语义：**JIT 自动开通**——首次 SSO 登录按 email（大小写不敏感）自动建号（角色 `member`，`users.password_hash` 写**随机不可知口令**——schema 不变，密码登录对其自然同形失败，管理端「重置密码」可直接转为混合账号）；再次登录复用既有账号；与本地账号体系并存
6. 审计：`auth.sso.login`（成功才写，detail 记 `jit_created`；失败不写，同密码登录的取舍）

## v1.2 变更说明（2026-09-19，Job000051「文档失信收口」）

本版依据《文档/全面审查_2026-09-19/requirements.md》（审查基线 HEAD=61838f8，及其后 HEAD 实况复核）对 v1.1 做**失信收口**，使契约重新成为客户端开发的真源。全部修改点：

**A. 补录已实现但 v1.1 未收录的端点**
1. §3 `GET /media/date-histogram`（日期密度直方图，Job000005）
2. §3 `PATCH /media/:id`（备注 notes / 编辑参数 edits，Job000005）
3. §7 `GET /geo/places`（地理位置罗列，Job000009）
4. §7 `GET/PUT /preferences/map`（地图图标配置，账户级，Job000009）
5. §11 `GET /public/shares/:token/og`（OG 封面，Job000013）
6. §12 `GET /transcode/job/:id`（转码任务状态查询）
7. §8 标签管理 CRUD：`PATCH /tags/:id`、`DELETE /tags/:id`、`GET /tags/:id/media`（Job000010）
8. §8 `POST /media/:id/tags/confirm`（逐图批量确认 AI 标签）
9. §11 分享创建 body 增加 `max_views` 访问上限字段（依据 DDL `share_links.max_views` / `share_access_log`）
10. §2 `PUT /user/password`（用户自助改密，Job000034，commit c87cf21）

**B. 路径漂移改正（4 组，以实现为准）**
1. §7：`/map/items` → `/geo/items`；`/map/timeline` → `/geo/histogram`；补 `/geo/clusters`
2. §11：`POST /share/album`、`POST /share/media` → 统一 `POST /shares`（body `kind,target_id,...`）；`GET /share` → `GET /shares`；`DELETE /share/:id` → `DELETE /shares/:id`；`GET /s/:token` → `GET /public/shares/:token`（前端路由 `/share/:token`）
3. §12：`GET /ai/jobs/:id` → 实现为 `GET /admin/jobs/:id`（等价覆盖，标注）
4. §2：`POST /auth/2fa`（temp_token 两段式）未实现 → 实际为 login 内联 `totp_code` + `/auth/mfa/*`，删除该端点声明并标注

**C. 形状 / 阶段标注**
1. §3 `POST /media/index`：未注册，索引由上传自动触发 + watchctl 监听 + CLI 等价覆盖——改为如实标注
2. §11：`POST /s/:token/unlock` 无此端点；密码校验经 `X-Share-Password` 请求头（2026-09-19 起优先），媒体流保留 `?password=` 查询参数兼容（v1.1 的 `?pwd=` 与实现不一致，以实现为准）
3. §11 公开下载：`allow_download=true` 时公开侧下载当前为占位统一 403（P1 实现）——标注占位未实现
4. §3 旋转 `op:rotate|crop|auto`：rotate/crop 已实现（合并语义，写 `media.edits`）；auto 无自动增强算法，按「重置编辑参数」处理——标注实际范围
5. §5 `GET /albums/:id/items`：由 `GET /albums/:id` 内联 items 等价覆盖——标注
6. §10 `GET /search/video-moment`：代码明示「本期不实现」（`internal/search/types.go`）——补「暂不实现」注记
7. §13 `WS /media/:id/360/state`：可选，未实现——补注记


---


## 1. 通用约定


### 1.1 认证

```
Authorization: Bearer <access_token>
```

- 登录/SSO/2FA 免鉴权；其余端点需合法 JWT。

- 角色/权限经 RBAC 校验（见 DDL `roles`/`role_permissions`/`shared_space_members`）。


### 1.2 统一响应包络

```json
{ "code": 0, "message": "ok", "data": {}, "request_id": "uuid" }
```

- `code=0` 成功；非 0 见错误码。


### 1.3 分页

查询参数：`cursor`（上一页末 ID 或 offset）、`limit`（默认 50，最大 200）。\ 响应含 `next_cursor` 与 `total`。

### 1.4 错误码

| code | 含义 |
| --- | --- |
| 0 | 成功 |
| 40001 | 参数错误 |
| 40101 | 未认证 / Token 失效 |
| 40102 | 2FA  required |
| 40301 | 无权限 |
| 40401 | 资源不存在 |
| 40901 | 冲突（如重复） |
| 42901 | 限流 |
| 50001 | 服务器错误 |


### 1.5 公共字段类型

- `MediaRef`：`{ "id", "type": "photo|video|360", "thumbnail_md", "width", "height", "taken_at", "is_360" }`


---


## 2. 鉴权与账户（独立后台）


### POST /auth/login

登录签发 JWT。
- 请求：`{ "email": "u@x.com", "password": "***", "totp_code"?: "123456" }`

- 响应：`{ "access_token", "refresh_token", "mfa_required": false }`

- **2FA 为内联单段式**（v1.2 更正）：账户启用 TOTP 时，登录请求体直接携带可选 `totp_code`；未携带或错误时返回错误码 `MFA_REQUIRED` / `MFA_INVALID`（401），客户端补码后重发同一 login 请求。防 2FA 枚举：启用与未启用 2FA 的账户在口令错误时同形响应。

### ~~POST /auth/2fa~~（未实现，v1.2 删除声明）

v1.1 声明的 temp_token 两段式二次验证端点**未实现**：后端无 `/auth/2fa` 路由（grep 0 命中）。实际实现为上述 login 内联 `totp_code`（功能等价），管理端二次验证生命周期见下。

### POST /auth/mfa/setup ｜ /auth/mfa/confirm ｜ /auth/mfa/disable

当前登录者自助管理 TOTP（三端点均需 JWT，身份取自会话，不接受请求体传入 user id）。
- `setup`：生成待确认密钥 → `{ "secret", "otpauth_url" }`
- `confirm`：以一次有效口令确认 → `mfa_enabled=true`
- `disable`：需有效口令 → 关闭并清除密钥


### POST /auth/refresh

刷新令牌：`{ "refresh_token" }` → `{ "access_token" }`

### GET /auth/sso/config

SSO 启用状态（公开，无需登录）→ `{ "enabled": true|false }`。前端登录页据此决定是否渲染 SSO 按钮。

### GET /auth/sso/oidc/login

SSO 登录入口：后端 discovery（`{issuer}/.well-known/openid-configuration`）后 **302** 到 IdP 授权地址（带一次性 state，10 分钟有效）。未配置 404 `NOT_CONFIGURED`（FailClosed，同 callback 口径）。

### POST /auth/sso/oidc

OIDC 回调：`{ "code", "state" }` → `{ "access_token", "refresh_token" }`（**与密码登录完全同形**，含 `expires_in`/`token_type`）。
- 成功路径：校验 state（签名+过期+一次性）→ code 换 token（HTTP Basic 客户端认证）→ userinfo → email 必需且 `email_verified` 不为显式 false → 账号复用或 JIT 开通（member）→ 签发票据 + 建会话 + 审计 `auth.sso.login`
- 失败：401 `SSO_FAILED` 同形（一切 SSO 失败不分层）；400 `EMAIL_REQUIRED`（IdP 未返回 email，查 scope）；403 `USER_DISABLED`（账号被禁用）
- 未配置 404 `NOT_CONFIGURED`

### POST /auth/logout

吊销当前会话（写 `sessions.revoked`）。

### GET /auth/me

返回当前用户：`{ "id","email","display_name","role","mfa_enabled" }`

### PUT /user/password

用户自助改密（Job000034，commit c87cf21；v1.2 补录）。仅需已登录（操作自己的口令不要求额外权限）。
- 请求：`{ "current_password": "…", "new_password": "…" }`

- 响应：`{ "ok": true }`

- 安全语义：**必须验证当前密码**（改密与「会话被劫持后改密」之间的唯一屏障）；**成功后吊销该用户全部会话**（refresh 不验口令，不吊销则旧 refresh 仍有效——前端契约是成功后登出并用新口令重新登录）；吊销失败不阻断响应（口令已改成功，失败只记服务端日志）。
- 防枚举同形响应：旧密码错误与「用户无口令记录」返回同一形状 400 `WRONG_PASSWORD`。
- 校验：新密码长度沿用全局下限（与 `POST /admin/users` 一致），且不得与当前密码相同（400 `INVALID_INPUT`）。
- 全程审计（`user.password.change`），口令类字段不入审计 detail。

### GET /user/app-password ｜ PUT /user/app-password ｜ POST /user/app-password/clear

应用密码自助管理（Job000098，v1.4 补录）。均需已登录（操作自己的凭据）。消费方：§16 WebDAV Basic 认证（`邮箱 + 应用密码`，邮箱仍用注册邮箱）。

- `GET /user/app-password` → `{ "set": true|false }`。只回状态，**永不回散列**（散列是「该账号开了这道凭据」的信号；明文服务端只存散列，本就不可能回）。
- `PUT /user/app-password`：生成（未设置时）或**轮换**（覆盖，旧应用密码立即失效）。请求 `{ "current_password": "…" }` → 响应 `{ "app_password": "pano-<40位hex>" }`。**明文仅此一次响应**，之后任何接口都取不回。
- `POST /user/app-password/clear`：清除。请求 `{ "current_password": "…" }` → 响应 `{ "ok": true }`。清除后 DAV 客户端回落主密码分支（`davPasswordOK`）——用应用密码配置的客户端会认证失败，需改回主密码或重新生成。
- 安全语义：生成与清除**都必须提供当前密码**（同 `PUT /user/password` 的「捡到开着的电脑」防线；否则 access token 一泄漏，攻击者即可铸造/抹除长期凭据）；两处校验的响应**逐字同形**（400 `WRONG_PASSWORD` / 500 `QUERY_FAILED`），不暴露探测面。
- **不吊销会话**：应用密码不走 `/auth/login`、不产生会话（DAV Basic 逐请求校验），轮换/清除不影响任何现有登录态——与自助改密「必须吊销全部会话」**有意不同**。
- 错误码：400 `BAD_REQUEST`（请求体非 JSON）/ `INVALID_INPUT`（缺当前密码）/ `WRONG_PASSWORD`；500 `QUERY_FAILED` / `GENERATE_FAILED` / `HASH_FAILED` / `UPDATE_FAILED`。
- 审计：`user.app_password`，detail 仅 `{ "operation": "rotate" }`（PUT）或 `{ "operation": "clear" }`（POST），明文绝不入审计。

### 管理端点（需 `admin:users`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/admin/users` | 用户列表（分页） |
| POST | `/admin/users` | 创建用户 `{email,display_name,password,role_id}` |
| PATCH | `/admin/users/:id` | 改角色/状态/重置密码 |
| DELETE | `/admin/users/:id` | 禁用/删除 |
| GET | `/admin/roles` | 角色与权限列表 |
| POST | `/admin/roles` | 新建角色 + 权限 |
| GET | `/admin/audit` | 审计日志（分页） |


---


## 3. 媒体 / 时间轴


### GET /media

时间轴分页（年/月/日/全部 + 筛选）。
- 查询：`space=personal|shared`、`view=year|month|day|all`、`date=2026-08`、`type=photo|video|360`、`favorites=true`、`tag=`、`person=`、`album=none`（Job000101：仅 personal，过滤出不属于本人任何相册的媒体，续翻消费 `GET /albums/groups` 的 `ungrouped.next_cursor`）、`cursor=`、`limit=`
- **作用域（安全不变量，2026-09-16 修复跨用户越权后确立）**：`space` **缺省 = `personal` = 仅本人**，绝不等于「全部」；`space=shared` 限「该共享空间的成员或属主」（失败关闭：两者都不是则返回**空结果**，而不是所有 shared 媒体）；`space` 取枚举外值 → **400 `INVALID_PARAMS`**（且不回显 PostgreSQL 枚举原文）；无 `user_id` 身份 → **401 `UNAUTHENTICATED`**。谓词本体见 `internal/media/scope.go` 的 `scopeConds`；`GET /media`、`GET /media/date-histogram`、`GET /media/duplicates` 三处**共用同一函数**，不接受各写一份而漂移。

- 响应：`{ "items": [MediaRef...], "next_cursor", "total", "buckets": [{"key":"2026-08","count":N}] }`


### GET /media/date-histogram

日期密度直方图（Job000005；v1.2 补录），时间轴年/月钻取的密度数据源。
- 查询：`granularity=year|month`（默认 `month`，其余取值 400 `INVALID_PARAMS`）、`space=personal|shared`

- 作用域与 `GET /media` **完全一致、共用同一谓词函数**（`scopeConds`）：`space` 缺省 = 本人个人空间；`taken_at` 为 NULL 的媒体归入 `unknown` 桶（字典序自然落末位）。

- 响应：桶数组（升序）`[{ "bucket": "2026-08", "count": N }, ...]`。


### GET /media/:id

详情 + 元数据：`{ id, type, path, taken_at, width, height, codec, gps, place, is_360, projection, thumbnail_*, hls_master, tags[], people[], albums[], exif: { camera_make, camera_model, lens_model, focal_length, aperture, iso, shutter_speed, exposure_bias }, video: { fps, bitrate, duration, hdr, color_space }, rating, live_photo_pair_id }`

### PATCH /media/:id

更新备注 / 编辑参数（Job000005；v1.2 补录）。
- 请求：`{ "notes"?: "...", "edits"?: {...} | null }`——**仅允许 `notes` / `edits` 两个字段**，其余字段一律拒收 400（白名单显式优于静默忽略）；两者至少提供其一。
- `edits` 语义为**整体提交**（整对象替换；显式 `null` 即清空/重置），不做单维度合并——单维度合并走 `POST /media/:id/rotate`。
- 响应：`{ "id", "notes"?, "edits"? }`；媒体不存在或已删除 404 `NOT_FOUND`。

### POST /media/upload

上传媒体文件（移动端备份/Web 上传）。
- Content-Type: `multipart/form-data`

- 字段：`file`(必填)、`space`(personal|shared)、`folder_path`(可选)、`taken_at`(可选，自动从EXIF提取)

- 支持分块上传：`Content-Range` header（断点续传）

- 响应：`{ "id", "status": "indexing" }`（异步索引）


### GET /media/:id/download

下载原文件（权限校验后直接返回文件流或重定向到对象存储签名URL）。

### POST /media/:id/rate

评级（0-5 星）：`{ "rating": 3 }`

### ~~POST /media/index~~（未实现，v1.2 更正）

v1.1 声明的手动触发索引端点**未注册**（后端 grep `media/index` 0 命中）。索引实际由三条等价路径覆盖：
1. **上传自动触发**（`POST /media/upload` 成功后异步索引）；
2. **`watchctl` 目录监听**（rsync + inotify 增量同步，TDD §8.8）；
3. **CLI `indexctl`**（全量/增量扫描）。
任务进度查询走 `GET /admin/jobs` / `GET /admin/jobs/:id`。


### POST /media/:id/rotate

基本编辑（非破坏，sidecar：写 `media.edits`，原文件不变，缩略图由 index worker 按编辑参数重生成）。
- 请求：`{ "op":"rotate|crop|auto", "angle"?:90, "rect"?:{...} }`

- **实际范围（v1.2 更正标注）**：`rotate`（angle ∈ 90/180/270）与 `crop`（`rect` 为归一化 `{x,y,w,h}` 且不越界）已实现，二者为「读-改-写」**合并语义**（单维度 op 只改自己那段，保留另一段）；`auto` **不含自动增强算法**，按「重置编辑参数」处理（rotate 与 crop 一并清空）。


### POST /media/:id/favorite

收藏切换：`{ "favorite": true }`

### DELETE /media/:id

软删（入回收站）。

### GET /media/trash

回收站列表；`POST /media/trash/:id/restore` 恢复；`DELETE /media/trash/:id` 永久删除。

### GET /media/duplicates

工具箱·重复项目（PRD §6.16「工具箱：重复项目 / 最近删除 / 已恢复」）。按感知哈希（pHash，`media.phash`，见迁移 00024）找出**观感相同**的副本，整组返回，由用户在 UI 上确认后删除多余的。**本端点只读**：不自动删除，也不写 `media.duplicate_of`。

- 权限：`media:read`（`permRead`）。作用域与 `GET /media` **完全一致，且是同一个函数**（`internal/media/scope.go` 的 `scopeConds`，两端点共用而非各写一份）：`deleted_at IS NULL`；`space` 缺省 = `personal` = `owner_id = 当前登录者`；`space=shared` 限共享空间成员/属主（否则空结果）；枚举外 `space` → 400 `INVALID_PARAMS`；无身份 → 401 `UNAUTHENTICATED`。
  > ⚠️ 历史修正：本行此前写作「`space=shared` 或省略 `space` 时不限属主」——那描述的正是**当时的越权实现**（省略 `space` 即可读到全站他人 personal 媒体，实测 viewer 账号一次拿到 95 条），已于 2026-09-16 修复。该端点原先把 `GET /media` 的谓词**抄了一份**，所以 List 侧改作用域时它不跟着改；现改为共用函数，从结构上消除这类漂移。

- 查询：`threshold=`（汉明距离阈值，默认 **10**，越界钳制到 0..20）、`limit=`（返回组数上限，默认 50，越界钳制到 1..200）、`space=personal|shared`

- 响应：`{ "groups": [ { "keep_id": "<uuid>", "phash": "0x1f3a…（16 位十六进制）", "items": [ MediaRef… ] } ], "total": 12, "scanned": 93, "threshold": 10, "truncated": false }`。`items` 复用 `GET /media` 的 `MediaRef` 形状（缩略图为文件名，非 URL）；`total` 为 `limit` 截断前的组数，`scanned` 为实际参与比较的媒体数（已算出指纹、未删、在作用域内），`truncated=true` 表示 `groups` 被 `limit` 截断。

- `phash` 是字符串而非数字：64 位无符号指纹在 bit 63 置位时值 ≥ 2^63，JSON 数字在 JS 里会被解析成 double 而丢精度（`0x8000000000000000` 与 `0x8000000000000001` 会变成同一个数）。

- `keep_id`（建议保留者）判据，按优先级：**像素数 `width×height` → `filesize` 更大 → `taken_at` 更早 → `id` 更小**。前三级都可能完全打平，第四级保证存在全序 —— 否则同一份数据两次调用会给出不同 keeper，UI 的「删除其余」就成了不确定操作。组内 `items` 即按该判据降序排列，`items[0]` 恒等于 `keep_id`。该判据**不看 `type`**：组内同时出现视频与照片时按像素数比较（例如 1280×720 的视频会胜过 600×969 的照片）——刻意不引入「照片优先」这类没人能解释的隐含规则，整组只是建议、由人确认。

- 分组用**并查集求传递闭包**：A≈B、B≈C 会并成一组，即使 A 与 C 的直接距离已超过阈值。这是刻意的：不做闭包时同一张图的 5 份副本会裂成若干互相重叠的小簇，用户要点很多次才清干净。代价是「桥接」（一个中间副本能把两端不太像的图拉进同一组），可接受 —— 结果只读、整组交给人判断。

- 阈值 **T=10** 是实测标定的，不是随手调参：同图重新编码（PNG / JPEG q90/q60/q30）与缩放（75% / 60% / 40%）实测全部 d=0，真实照片的容差上限 d=6；而 990 组无关图对中位数 32、p95 为 38，仅 3 对 ≤7（人工核对确为真近重复）。容差侧 ≤6 与无关侧 ≥14 之间空出 8 位，10 居中，两侧各留 4 位余量。**改这个数等于改「什么算重复」的产品定义**，须重新标定。

- **已知限制一（O(N²) 上限，刻意接受）**：两两比较天生 O(N²)。候选宇宙超过 **5000** 时直接返回 400 `TOO_MANY_MEDIA`（消息里带实际规模），而不是让请求挂住 —— N=5000 约 1250 万次比较（毫秒级），N=50000 则是十秒级且占满一个核。调用方应按 `space` 等维度缩小范围后重试。

- **已知限制二（低纹理图像两带重叠，刻意接受、不修）**：纯平图像的 AC 系数≈0，比较退化为浮点残差符号，汉明距离近乎任意，故 T=10 下**既可能漏检、也可能误合并**。实测纯色灰矩阵：`64 vs 128 → d=0`（深浅不同的两张纯色图指纹完全相同）、`64 vs 192 → d=10`；而纯色图对真实带纹理照片的最小距离 d=24、两幅渐变图 d=23，说明重叠只在「两边都平」时出现，**真实照片不受影响**。

- 该限制之所以可接受，是因为这条端点的安全属性不在阈值精度上：它**只读且只作建议**——自己不删任何东西，实际清理必须经过用户确认并走**软删（回收站可恢复）**。这也是不完美的阈值可以被接受的前提。`threshold` 参数可调，但**默认 T=10 不因该限制而修改**（纯平内容不是调阈值能解决的）。

- **已知限制三（单帧 pHash 对视频是弱标识，刻意接受、不修）**：缩略图与指纹都取自**首帧**，所以两个**开头画面相同**的不同视频会得到完全相同的指纹（d=0），在默认 T=10 下必然被并成一组，且**无法**由指纹本身区分——连最严的合法阈值 `threshold=0` 也拆不开（0 ≤ 0）。实测案例：极光-延时-001.mp4 ↔ 西湖-游船-延时-001.mp4，三层缩略图（sm / md / lg）**逐层 sha256 相同**，而源文件差异明显（2170533 B vs 1080574 B，时长 12s vs 6s），但**两条源视频抽取出的首帧字节相同**。即这是语料属性（两个视频本来就以同一画面开头），**不是缩略图管线取错了帧**。
  > **证据溯源**：上面「三层缩略图逐层 sha256 相同」与「两条源视频抽取出的首帧字节相同」这两个事实，是 **team-lead 在测试服上对源文件核查**得到的——源文件在仓库外，验证需要服务器文件访问权限 + ffmpeg，属**一次性人工核查，不是本仓库内可自动复现的断言**；复现对象为 `import-sample/videos/reykjavik/2026-02-极光-延时-001.mp4` 与 `import-sample/videos/hangzhou/2025-06-西湖-游船-延时-001.mp4`。仓库内可自动复现的只有**合并行为本身**（本小节的回归测试用夹具覆盖，不查库）；真实库上的 d=0 需要连库重算指纹，同样不是自动断言。

- 限制三曾被认真尝试修掉：给重复检测加一道 `detail`（AC 能量）门（原计划迁移 00025，规则为 `Hamming ≤ T AND (双方 detail ≥ D_min OR sha256 相同)`）。三种候选度量（`mean|AC|` / `RMS(AC)` / `meanAbsAC / |DC|`）实测**全部无法分离**——这一对恰恰是全库细节最丰富的一档，任何能拆开它的 `D_min` 都会连带排除 93 条真实媒体中的 62 条（67%）。**迁移 00025 未创建、该门控未实施**，此限制按现状接受。

- 限制三为何可接受：收紧（排除视频、或要求 sha256 相同）会一并砍掉**同一场景的 360 照片 ↔ 360 视频**那一对（实测 d=2，是**真重复**）——等于拿一个真阳性去换掉两个（被换掉的那个还只是语料首帧复用造成的误合并）；而本端点 over-inclusive 的代价只是「用户多看一眼」。同理，用 `filesize` / `duration` 相等做前置也不可取：那会废掉本端点的主要用途（重编码与缩放的副本 `filesize` 本来就不同，实测其汉明距离全为 0），对本案也无效（这两个视频的大小本来就不同）。回归保护见 `internal/media/duplicates_video_identity_test.go`：三条用例把「这两条视频必须合并」「跨媒介同场景必须合并」「分组判据不看元数据」一起钉住。

- 错误：参数非整数 → 400 `INVALID_PARAMS`；候选宇宙超限 → 400 `TOO_MANY_MEDIA`；查询失败 → 500 `QUERY_FAILED`。

### GET /media/restore-history

工具箱·「已恢复」标签的数据源（PRD §6.16）。返回**调用者自己执行过**的 `media.restore` 审计行，**倒序**。**本端点只读**。

- 权限：`media:read`（`permRead`）；路由挂在 `authed` 组内、**与 `GET /media/:id` 同一组**（`cmd/api/main.go`），故 gin「静态段优先于参数段」的规则同样保护 `/media/trash`、`/media/date-histogram`、`/media/duplicates` 与本端点。
  > ⚠️ 实现位于 `internal/audit/restore_history.go` 而非 `internal/media`：它读的是 `audit_log`，直接复用本包既有的 `Filter` / `Page` / 游标编解码 / limit 钳制；若搬到 media 包就得把这套约定**再实现一遍**，而「同一份约定抄两处 + 注释承诺同步」正是 §二十一 收敛掉的漂移模式（`MediaRef` 列清单曾 7 份副本、5 份各漂各的）。**路径命名空间 ≠ 包边界**，既有同型先例是 `GET /admin/stats` 与 `GET /admin/jobs`（同为 audit 包实现、挂在别人的前缀下）。

- 查询：`limit=`（默认 **50**；`<=0` 用默认；`>200` **钳到 200** 而非回落默认）、`cursor=`（上一页返回的 `next_cursor`，原样回传）、`?actor=` **无效**（见限制二）

- 响应：`{ "items": [ { "id": 132, "actor_user_id": "…", "action": "media.restore", "target_type": "media", "target_id": "…", "ip": "…", "detail": { "owner_id": "…", "path": "…" }, "at": "2026-09-17T14:30:30.113662+08:00" } ], "total": 2, "limit": 50, "next_cursor": "…" }`
  审计行原样透传（`internal/audit.Entry`，`omitempty` 字段缺失即省略；`next_cursor` 为空时字段省略）。`detail` 里的 `path` / `owner_id` 也是原样透传，不做二次加工。`total` 是**调用者自己**的条数，不是全站 `media.restore` 条数。

- 时间列是 **`at`**，**不是 `created_at`**（`audit_log` 的 DDL 里没有 `created_at` 这一列）。排序键 `(at DESC, id DESC)`：`at` 虽精确到微秒仍可能重复，只用 `at` 排序会漏行或重行，故与 `id` 组成复合键。

- 游标与 `GET /admin/audit` **完全同一套** `encodeCursor` / `decodeCursor`：`base64url(RFC3339Nano + "|" + id)`，谓词用复合行比较 `(at, id) < ($n, $m)`。**没有新造风格**。非法游标 → 400 `INVALID_INPUT`（消息「无效游标」）。

- 索引：复用既有 `idx_audit_actor_at ON audit_log(user_id, at DESC)`，**未新增任何索引**。

- 错误：未认证（拿不到非空 actor）→ 401 `UNAUTHORIZED`；游标非法 → 400 `INVALID_INPUT`；查询失败 → 500 `INTERNAL`。

- **本端点不写审计**：刻意**不**记 `ActionAuditRead`。先例（`GET /admin/audit` 记 `admin.audit.read`）的立论前提是「该端点暴露**全站**敏感操作史，读它本身就是一次有后果的敏感访问」；本端点只返回调用者**自己**的行，那些行本就是他自己的动作，**读它不产生任何新的知情面**。另有两条理由：前提不同就不该照搬先例；且每打开一次工具箱就写一行，只会把账本**淹没**（噪音掩盖真线索）。该取舍由 `TestRestoreHistoryDoesNotWriteAuditRead` 钉住，且该测试自带反向自证：同一个 fake 挂到 `/admin/audit` 上确实会写 1 行。

- ⚠️ **已知限制一（按 `path` 展示，不承诺稳定文件名）**：`detail` 里给的是**写入时**的 `media.path`，既不是可点击的下载地址，也**不承诺它现在还指向一个存在的文件**；`detail` 里**没有 filename**。若该媒体此后被 `DELETE /media/trash/:id` **永久删除**，那么 `target_id` 已经**解析不回任何东西**（`audit_log.target_id` 无外键、审计行会活过目标），此时 `path` 是唯一还能说明「当初恢复的是哪一条」的线索 —— 这正是 `media.restore` 必须把 `path` 写进 `detail` 的原因（§二十四 原则二：**先留证据、再销毁物证**）。要提供稳定文件名需另存一张恢复记录表，那是一条升级路径，不在本次范围。

- ⚠️ **已知限制二（只含本人「执行」的恢复，不含他人代办的）**：行按 **actor** 存储。管理员在成员的回收站里**代其恢复**时，那一行的 `user_id` 是**管理员**而不是成员，于是成员在本端点里**看不到**这条记录。这是**可见性缺口，不是越权泄漏**（方向是「该看到的人看不到」，过滤 `user_id = 调用者` 不会多返回任何一行）—— 但不写进契约的话，这个列表会**看起来完整而其实不然**。隔离由单测（`TestRestoreHistoryScopesToContextCaller`、`TestRestoreHistoryRejectsMissingActor`）与真库实测双重保证：actor **只**取自认证上下文，`?actor=` 覆盖在真实 handler 上**不生效**；拿不到非空 actor 时直接 401，而**不是**退化成「返回全站所有人的恢复历史」（`BuildWhere` 对空 `ActorUserID` 会静默**跳过** `user_id` 条件，故这一步是必需的安全检查，不是防御性冗余）。

---


## 4. 空间（双空间模型）


### GET /spaces

返回 `{ "personal": {...}, "shared": [ {id,name,role} ] }`

### POST /spaces/shared

建共享空间：`{ "name" }` → `{ "id" }`（需 `space:create`）

### POST /spaces/shared/:id/members

加成员：`{ "user_id", "role": "manager|member|viewer" }`

### DELETE /spaces/shared/:id/members/:user\_id

移除成员。

---


## 5. 相册


### GET /albums

列表（含 `type` 过滤：`manual|smart|shared|favorites`）。

### GET /albums/groups（v1.6 补录，Job000101）

个人空间「按相册分组」首屏聚合（空间页分组视图数据源）。
- 语义：分组 = 本人 personal 空间 `type IN ('manual','favorites')` 且有可见成员的相册（smart 不入分组——无 `album_items` 行）；未分组桶 = 不在本人任何相册里的 personal 媒体（仅被 smart criteria 命中仍算未分组）
- 响应：`{ "groups": [{ "album_id", "name", "kind", "count", "items": [MediaRef×≤8], "truncated" }], "ungrouped": { "count", "items": [MediaRef×≤24], "has_more", "next_cursor"? } }`（`next_cursor` 仅 `has_more` 时给出，续翻 `GET /media?album=none&cursor=`）
- 可见性：全部媒体行过 `mediascope.VisibleCondFor`（调用者可见集，与 `GET /media` 同真源）；相册行限 `owner_id=调用者`（fail-closed，无「全库」档）

### POST /albums

建相册。
- 普通：`{ "name","type":"manual","space":"personal" }`

- 智能：`{ "name","type":"smart","query":{ "person":["id"],"tag":["海滩"],"date_after":"2026-01-01","place":"冰岛" } }`

- 响应：`{ "id" }`


### GET /albums/:id

相册详情：`{ id, name, description, type, space, owner, query, cover_media_id, item_count, created_at, updated_at }`

### GET /albums/:id/items（无独立路由，v1.2 标注）

相册内媒体由 **`GET /albums/:id` 内联返回 items** 等价覆盖（`internal/albums` store 层在详情查询中一并装配），后端未注册独立的 `/albums/:id/items` 路由；功能等价、形状偏差。

### POST /albums/:id/items

加媒体：`{ "media_ids": [...] }`（manual 类型）。

### DELETE /albums/:id/items/:media\_id

移除。

### PATCH /albums/:id

改名/改封面/改智能条件。

### DELETE /albums/:id

删除相册。

### GET /albums/:id/comments

相册评论列表（分页，支持嵌套回复）。
- 响应：`{ "items": [{ "id", "user_id", "display_name", "content", "parent_id", "created_at" }] }`


### POST /albums/:id/comments

发表评论（需 `album:write` 或共享空间成员权限）。
- 请求：`{ "content": "...", "parent_id"?: "uuid" }`

- 响应：`{ "id", "created_at" }`


### DELETE /albums/:id/comments/:comment\_id

删除评论（本人或管理员）。

---


## 6. 人物


### GET /people

已知人物 + 未命名聚类：`{ "named":[...], "unnamed":[{cluster_id,count,cover}] }`

### POST /people

命名/合并：`{ "cluster_ids":[...], "name":"张三", "is_pet":false }`

### PATCH /people/:id

隐藏/改名：`{ "hidden":true }` 或 `{ "name":"李四" }`

### GET /people/:id/media

该人物全部媒体（时间轴）。

---


## 7. 地图模式


### GET /geo/clusters（v1.2 路径更正：v1.1 的 `/map/items` 聚合形态）

可视区媒体聚合点。查询：`min_lng=&min_lat=&max_lng=&max_lat=`（bbox 四参数，**不是** v1.1 的 `bbox=` 单参数；取值为**地图显示坐标系**——高德底图下为 GCJ-02，服务端反变换为库内 WGS-84 后查询）、`zoom=`（0~22，默认 4，越界 400）、`kind=all|photo|video|pano`（缺省 all，枚举外 400 `INVALID_PARAMS`）、`from=&to=`（时间窗）、`provider=`。\ → `{ "clusters":[ { "center":[lon,lat], "count":N, "thumbnails":[...] } ] }`

### GET /geo/items（v1.2 路径更正：v1.1 的 `/map/items` 明细形态）

bbox 内媒体条目（点击簇/框选后展开网格用）。查询：bbox 四参数 + `kind=`、`from=&to=` 同上；`limit=`（默认 200，钳至 1000）。\ → `{ "items":[ { "id","gps","thumb","taken_at", ... } ], "total":N }`

### GET /geo/histogram（v1.2 路径更正：v1.1 的 `/map/timeline`）

当前筛选 + bbox 的时间跨度直方图（地图 viewport 变化 → 重算时间轴分布，双向联动的「地图→时间轴」方向）。查询：bbox 四参数 + `granularity=year|month|day`（默认 month，非法 400 `INVALID_PARAMS`）。\ → `{ "buckets":[ { "bucket":"2024-07", "count":N, "kind_counts":{...} } ] }`（每桶返回四类媒体分类计数）。

### GET /geo/places（v1.2 补录，Job000009）

bbox 内去重地名列表（含计数），供地图底部「地理位置罗列」横向滑动展示。查询：bbox 四参数 + `kind=`、`from=&to=`、`limit=`（默认 50，钳至 200）。\ → `{ "places":[ { "name","count":N, ... } ] }`

### GET /preferences/map ｜ PUT /preferences/map（v1.2 补录，Job000009）

当前用户地图图标偏好（账户级，需登录）。
- GET：无记录返回 200 + 默认值（前端据此用默认红点，无需处理 404）→ `{ "pref": {...} }`
- PUT：写入偏好，body 同 `pref` 形状；`shape` 必须为已知枚举、`color` 必须为合法十六进制色值（否则 400 `INVALID_PARAMS`）→ `{ "pref": {...} }`

### GET /map/search

模糊地理搜索（中国走高德 / 国际走 Nominatim，按 `system_map_config` 自动选源）：`?q=西湖` → `{ "candidates":[{ "name","lon","lat","provider" }] }`

### GET /map/providers

返回可用底图与当前配置（不含密钥）：`{ "china":"amap","intl":"osm","default":"auto" }`

### GET /user/ui-prefs

当前用户地图 UI 偏好：`{ "map_slider_pos","map_filter_side","map_default_provider","map_default_zoom" }`（需登录）；v1.6 增 `"spaces_group_by_album"`（bool，默认 false）= 空间页个人空间「按相册分组」视图模式（Job000101）；v1.10 曾增 `"auto_transcode"` 已于 v1.11 移除（开关上收系统级，真源改见 `GET /transcode/config`）

### PUT /user/ui-prefs

更新偏好（滑块位置/筛选栏侧/默认底图）；v1.6 起同步支持 `spaces_group_by_album`（固定键整体替换契约，未携带键不落库变更）。v1.10 的 `auto_transcode` keep-on-absent 例外已于 v1.11 移除——该键整体迁出用户偏好，恢复八键统一语义。

### GET /admin/map-config

系统地图配置读取（需 `admin:system`）。

### PUT /admin/map-config

更新配置（中国底图 provider、高德 API key【加密存储】、国际底图 URL）。
> 说明：聚合与直方图由后端按 `gps geometry(Point, 4326)`（PostGIS）+ `taken_at` 联合查询（`ST_MakeEnvelope` + `ST_Intersects` 做 bbox 过滤，时间桶聚合）；高德底图显示需将媒体 WGS-84 坐标转换为 GCJ-02。详见 TDD §3.7、DDL §2.5。

### GET /transcode/config（v1.11，Job000120-r2；v1.13 增量 realtime_transcode，Job000124）

系统级转码开关只读视图（任何登录用户，authed 组）：`{ "auto_transcode": bool, "realtime_transcode": bool, "updated_at": timestamp }`。无配置行时 `auto_transcode` 缺省 true、`realtime_transcode` 缺省 false（存量行为逐字节不变）。播放器加载视频前预判姿态用——该值非机密，写权限由下方 admin 端点单独把守。两开关 AND 关系：有效自动触发 = `auto_transcode AND realtime_transcode`。

### GET /admin/transcode-config（v1.11，Job000120-r2）

系统级转码开关读取（需 `admin:system`），响应形状同 `GET /transcode/config`。

### PUT /admin/transcode-config（v1.11，Job000120-r2；v1.13 改部分更新，Job000124）

更新系统级开关（v1.13 起**部分更新**语义，沿用 Job000123「缺失不改」）：body 为 `{ "auto_transcode"?: true|false, "realtime_transcode"?: true|false }`，两键可选、**至少一项**，全缺 → 400 `BAD_REQUEST`；map 先取一层再解指针（防误清空/并发覆盖）。→ 同 GET 视图。写操作落审计（`admin.settings.patch`/`setting`/`transcode-config`，detail 按实际变更键落）。`auto_transcode` 关闭后全站播放器不再发起转码（普通视频回退原始文件播放）；360° 视频例外——全景播放依赖 HLS 切片，关闭后新上传 360° 视频无法播放；CLI 批量补排（transcodectl）不受约束。


---


## 8. 标签


### GET /tags

标签列表（`kind=user|ai`、`confirmed`）。

### POST /tags

创建标签：`{ "name", "kind"?: "user|ai", "color"?: "#rrggbb" }` → `{ "id", ... }`（kind 默认 user；同名同 kind 已存在时返回既有行）

### PATCH /tags/:id（v1.2 补录，Job000010）

标签改名/改色：`{ "name"?: "...", "color"?: "..." }`

### DELETE /tags/:id（v1.2 补录，Job000010）

删除标签；带查询参数 `?into=<tag_id>` 时先把关联合并到目标标签再删（合并目标不存在 → 提前 400，不暴露 PG 外键错误原文）。

### GET /tags/:id/media（v1.2 补录，Job000010）

按标签浏览媒体（**仅已确认关联**），分页：`?cursor=&limit=&space=`。标签不存在 404（与「标签存在但无已确认媒体」的空列表区分）。可见性与 `GET /media` 同口径：`space` 缺省 = 本人个人空间，枚举外取值 400 `INVALID_PARAMS`；tags 表全局无 owner，按 **media 的可见性**收窄。

### POST /media/:id/tags

给媒体打标签：`{ "tag_ids":[...] }`；移除关联用 `DELETE /media/:id/tags/:tag_id`（v1.2 补录）。

### POST /tags/:id/confirm

确认 AI 自动标签：`{ "media_id"?: "...", "confirmed"?: true }`——带 `media_id` 时设置该媒体上这一关联的确认状态（`media_tags.confirmed`），否则设置标签本身（`tags.confirmed`，粗粒度审阅）；两条路径都支持 `confirmed=false` 取消确认。

### POST /media/:id/tags/confirm（v1.2 补录）

逐图批量接受 AI 标签：`{ "tag_ids"?: [...] }`——仅请求体为空/未提供 `tag_ids` 时走「全部确认」；显式空数组 = 确认 0 条；解析失败 400。

---


## 9. 文件夹视图（保留目录结构）


### GET /folders/tree

目录树：`{ "id","name","path","children":[...] }`

### GET /folders/:id/media

该目录媒体（分页）：`?cursor=&limit=&type=&sort=name|taken_at`

### POST /folders/:id/albums

以目录为源建相册：`{ "name" }`

---


## 10. 搜索


### GET /search

- 基础筛选：`q`（关键词，tsvector）、`person`、`tag`、`date_after/before`、`place`、`type`、`favorites`。

- 自然语言（自研）：`q="Maya 穿红衫玩滑板"` → 全文 + pgvector 语义召回 + WHERE 拼接。

- 响应：`{ "items":[MediaRef...], "next_cursor", "total" }`


### GET /search/video-moment（暂不实现，v1.2 补注）

视频瞬间定位：`{ "q":"猫出现", "media_id" }` → 帧时间戳列表（帧级索引）。
> ⚠️ **本期不实现**：实现方已在代码明示（`internal/search/types.go` 包注释「本期不实现：/search/video-moment」），后端无该路由；契约保留形状仅为规划留位，客户端不得依赖。

---


## 11. 分享 / 微信 H5


### POST /shares（v1.2 路径更正：v1.1 的 `POST /share/album` + `POST /share/media` 已统一）

创建分享（相册或单媒体，由 `kind` 区分；需 `share:create`，仅可分享本人资源，owner/admin 可代管）。
- 请求：`{ "kind": "album|media", "target_id": "<uuid>", "title"?: "...", "expire_at"?: "...", "password"?: "...", "allow_download"?: false, "is_wechat"?: false, "max_views"?: 100 }`
  - `target_id` 必须是合法 UUID（非 UUID 触库前直接 400 `INVALID_PARAMS`）；`expire_at` 必须是未来时间；`max_views` 访问次数上限（v1.2 补录，依据 DDL `share_links.max_views` + `share_access_log` 访问台账），必须为正整数。
  - **`is_wechat=true` 时强制 `allow_download=false`**（微信 H5 不给原文件，PRD 核心约束）。
- 响应：201 `{ "id", "token", "url" }`（`url` 为公开访问地址 `/public/shares/<token>`，尊重反代 `X-Forwarded-Proto`；前端页面路由为 `/share/:token`）。

### GET /shares（v1.2 路径更正：v1.1 的 `GET /share`）

我的分享列表（含 access_count / 状态）。

### DELETE /shares/:id（v1.2 路径更正：v1.1 的 `DELETE /share/:id`）

撤销分享。

### GET /public/shares/:token（v1.2 路径更正：v1.1 的 `GET /s/:token`；访客免登录 H5 数据接口）

返回分享内容（前端 SPA 路由 `/share/:token` 渲染轻量 H5，不加载后台；360 视频走 WebXR+HLS）。
- 响应：`{ "kind", "title", "items": [...], "require_password": bool }`
- **密码校验（v1.2 更正）**：受密码保护的分享经 **`X-Share-Password` 请求头**提交密码（2026-09-19 起为优先通道）；`?password=` 查询参数**保留兼容**——媒体流（缩略图 / HLS，由 `<img>` / `<video>` 标签直接拉取、无法携带自定义请求头）仍走查询参数。v1.1 所写 `?pwd=` 与实现不一致，以实现参数名 `password` 为准。缺密码 → 403 `PASSWORD_REQUIRED`，密码错误 → 403（同形）。
- 有效期过期 / `access_count >= max_views` → 拒绝访问（同形错误，不泄露存在性）。
- **无 unlock 端点**（v1.2 更正）：v1.1 声明的 `POST /s/:token/unlock`（cookie 会话方案）**未实现**，每次请求独立校验密码。

### ~~POST /s/:token/unlock~~（未实现，v1.2 删除声明）

见上。cookie 会话方案未落地；密码按请求独立校验（`X-Share-Password` 头优先，`?password=` 兼容）。

### GET /public/shares/:token/media/:id/thumb

分享内媒体缩略图：`?size=sm|md|lg`（限分享内媒体；受密码/有效期/次数约束）。

### GET /public/shares/:token/media/:id/hls/*file（v1.2 路径更正：v1.1 的 `GET /s/:token/playlist.m3u8`）

返回该分享媒体 HLS 清单与分片（限分享内媒体；受密码/有效期/次数约束）。

### GET /public/shares/:token/media/:id/download（占位未实现，v1.2 标注）

公开侧原文件下载：**当前为占位，统一返回 403**（代码注释「P1 实现」；公开路由禁止提供原文件是 PRD 核心约束）。`allow_download=true` 的语义在公开侧暂未落地，客户端不得依赖。

### GET /public/shares/:token/og（v1.2 补录，Job000013）

分享页 OG 封面（服务端渲染最简 HTML；社交抓取器不执行 JS，nginx 按 User-Agent 把抓取器分流到本端点，普通浏览器仍走 SPA）。
- **不写 `share_access_log`、不自增 `access_count`**（抓取器拉一次 OG 不是真实访问，否则 `max_views` 会被爬虫提前烧完）。
- **受密码保护的分享一律返回中性 OG**（无标题、无缩略图）——OG HTML 会被社交平台缓存/公开分发，靠 `?password=` 解锁等于把密码写进 URL 交给第三方 CDN。
- 分享不存在 / 已过期 / 次数达上限分别返回对应文案的 OG 卡片（带 `noindex`）。

### POST /public/shares/:token/bandwidth-test（v1.2 路径更正：v1.1 的 `POST /s/:token/bandwidth-test`）

分享场景专用带宽自测（访客无 JWT，以 token 鉴权）。三段式：下行探针 `GET /public/shares/:token/bandwidth-probe` → 上行探针 `POST …/bandwidth-probe` → 汇总 `POST …/bandwidth-test`。
- 请求：无 body（服务端上传/下载探针）

- 响应：`{ "up_kbps", "down_kbps", "latency_ms" }` → 写入 `bandwidth_tests`（scope=share\_token, source=self\_test）

- 360 播放页据此在 HLS 多档中选取初始档。


---


## 12. AI / 转码


### POST /ai/faces

触发人脸检测/聚类（通常索引时自动，此为主动重算）：`{ "scope":"all|media_id" }` → `{ "job_id" }`

### POST /ai/tags

触发自动标签：`{ "scope":"all|media_id" }` → `{ "job_id" }`

### GET /ai/jobs/:id（无此路径，v1.2 标注）

v1.1 声明的 `/ai/jobs/:id` 未注册；任务状态由 **`GET /admin/jobs/:id`**（需 `admin:system`，见 §14）**等价覆盖**。另：AI 触发类端点（`POST /ai/faces`、`POST /ai/tags`）为清扫式架构（复位扫描标记即入队等价），响应**非** `{job_id}` 形状——无独立 job 可查。

### POST /transcode/job

提交转码：`{ "media_id", "profile": "1080p|2k|4k", "auto"?: bool }` → `{ "job_id", "reused"?: true }`。v1.11 起闸门判**系统级**真源：管理员关闭自动转码（`GET /transcode/config`.auto_transcode=false）→ **409 `TRANSCODE_DISABLED`**「管理员已关闭自动转码，请联系管理员开启」（v1.10 曾判调用者个人偏好，r2 上收后作废）。

v1.13 增 `auto: true` 自动触发分支（Job000124，归属/闸门/错误口径与手动全同）：
- **attach-or-create**：该媒体已有 `pending/running` 任务 → 直接复用其 `job_id` 返回（202，`reused: true`）——同一媒体全站最多一个活动任务，幂等防双击/多端并发
- 无活动任务 → 档位由服务端按源分辨率 `ProfileForSource` 自动选取（与 CLI 补排同口径），请求 `profile` 被忽略（可不填）
- 不带 `auto`（或 `auto: false`）= 旧语义逐字节不变：手动、`profile` 必填校验（缺省 1080p）、每次新建任务

### GET /transcode/job/:id（v1.2 补录）

转码任务状态查询（前端轮询用）→ `{ "id", "media_id", "status", "profile"?, "result_path"? }`。归属校验 JOIN `media.owner_id`；无权时返回 404 而非 403（与「任务不存在」同形，不做存在性探测）。

### GET /transcode/hls/:id/master.m3u8

返回 HLS 清单（自适应码率；边缘缓存）。

### GET /admin/jobs

索引/转码任务列表与进度（查 `index_jobs`/`transcode_jobs`）。

### POST /admin/scan  （Job000113 / R1-b，需 `admin:system`）

管理端扫描导入：把已在媒体库挂载目录里的照片/视频入库，**归属真实调用者**（告别种子 owner 语义）。

请求：`{ "dir": "相对 MEDIA_ROOT 的子目录" }`（`""`/`"."` = 整个媒体根；也接受绝对路径，但解析后必须仍位于 MEDIA_ROOT 之内，越界即路径穿越拒绝）。

响应：`202 Accepted { "job_id","status":"running","root","dir" }`——异步受理：先建 `index_jobs` 行立即返回，扫描本体后台 goroutine 执行。进度/终态轮询 `GET /admin/jobs/:id`（`status`：`running`→`done`|`failed`；`processed`/`total` 看进度）。

错误：`400 INVALID_INPUT`（请求体非 JSON / dir 越界 / 目录不存在 / 非目录）、`401 UNAUTHORIZED`、`409 SCAN_RUNNING`（已有扫描进行中）、`500 INTERNAL`。

> 面向场景：NAS bind mount 下照片已进挂载目录但系统"不识别"——此前唯一入库路径是 worker 镜像内的 `indexctl` CLI，api 无入口、启动也不扫 MEDIA_ROOT。配套 R1-c：`MEDIA_SCAN_ON_BOOT=1` 时 api 迁移完成即后台自扫 MEDIA_ROOT（归属种子 owner，哈希幂等）。两者都依赖 api 镜像内置 ffmpeg（视频元数据经 ffprobe 提取）。

### GET /admin/fs/tree  （Job000117，需 `admin:system`）

目录树选择器数据源：扫描面板「浏览」按钮逐级展开文件系统层级。**懒加载单层列举**——每次只返回目标目录的直接子目录，前端展开到哪层才请求哪层，层级再深也只付当前层成本。

请求：`?dir=<相对 MEDIA_ROOT 的目录>`（缺省/`"."` = 媒体根本身）。

响应：`200 OK { "root": "<MEDIA_ROOT>", "dir": "<相对路径>", "unreadable": false, "items": [ { "name","rel","readable" } ] }`
- `items` 只含**目录**，按名排序；文件不上树。符号链接（含指向目录的）经 `os.ReadDir` 的 lstat 语义天然排除——树不会越出 MEDIA_ROOT。
- `readable=false` 表示 api 进程对该子目录无权限（打开+读 1 条探测失败）：前端以锁定态展示、禁止展开，**不报错**。
- 目标目录本身无权限时**同样不报错**：`200 { "unreadable": true, "items": [] }`，前端就地显示「无权限访问」占位行。空目录 → `items: []`，前端显示「（无子目录）」占位行。

错误：`400 INVALID_INPUT`（dir 越界=路径穿越 / 目录不存在 / 非目录——与 `POST /admin/scan` 同一 `resolveScanDir` 边界）、`401 UNAUTHORIZED`、`500 INTERNAL`（非权限类底层列举错误）。

> 选中目录后的联动在前端完成：回填扫描输入框并自动触发 `POST /admin/scan`（扫描进行中则只回填不抢跑）。权限语义提示：进程以 root 运行时（CAP_DAC_OVERRIDE）chmod 权限位不构成无权限，锁定态只在非 root/降权部署（或 NFS root-squash）下出现——属部署形态差异，非缺陷。

### PUT /admin/users/:id/scan-root  （Job000123，需 `admin:users`）

为普通账号分配/取消扫描根目录（`users.scan_root`，详见文首 v1.12 变更说明三态语义）。管理端扫描入口（`POST /admin/scan`）本就不看 `scan_root`，owner/admin 不受本分配限制；本端点也不改角色/状态，不受自锁/最后 owner 守卫约束。

请求：`{"scan_root": <string|null>}`——`""` = 媒体根本身；相对路径 = 该子目录；显式 `null` = 取消分配；**字段缺失 → 400**（与显式 null 区分，防误清空）。

响应：`200 OK {"user": {...更新后用户全行（含 scan_root）}}`。错误：`400 INVALID_INPUT`（请求体非 JSON / 越界路径穿越 / 非真实存在物理目录 / 目录而非文件 / `_imports` 保留段）、`401 UNAUTHORIZED`、`404 USER_NOT_FOUND`、`500 INTERNAL`。审计：`admin.user.update`，`detail.scan_root` = 归一化路径或 `null`。

### GET /user/scan-root  （Job000123，authed）

任意登录用户查自己的扫描根分配：`200 {"assigned": bool, "scan_root": <string|null>}`——`assigned=false` 时 `scan_root` 恒 `null`；`assigned=true` 时 `""` = 媒体根本身，其余 = 相对路径。成员扫描面板挂载前先打本端点：未分配直接展示「联系管理员」引导，不发扫描请求。错误：`401 UNAUTHORIZED`、`500 INTERNAL`。

### POST /scan  （Job000123，authed + 写权限）

成员扫描导入：把**自己 scan_root 范围内**的目录入库，归属真实调用者。语义与 `POST /admin/scan` 同构（异步 202、同一 SCAN_RUNNING 互斥），差别仅在边界 = 调用者 `scan_root` 而非 MEDIA_ROOT。

请求：`{"dir": "相对 scan_root 的子目录"}`（`""`/`"."` = 整个扫描根）。响应：`202 Accepted {"job_id","status":"running","root","dir"}`——`root` = scan_root 展示值（`"."` = 扫描根本身，与 `dir` 展示口径一致；成员响应不回显绝对路径）；`dir` = 相对 scan_root 的展示路径。进度/终态轮询 `GET /jobs/:id`。

错误：`400 INVALID_INPUT`（dir 越界=路径穿越 / `_imports` 保留段任意层级 / 目录不存在 / 非目录）、`401 UNAUTHORIZED`、**`403 SCAN_ROOT_REQUIRED`（未分配扫描根）**、`409 SCAN_RUNNING`、`500 INTERNAL`。

### GET /fs/tree  （Job000123，authed + 读权限）

成员目录树选择器数据源：与 `GET /admin/fs/tree` 同构的懒加载单层列举，差别在边界与保留段过滤——根 = 调用者 `scan_root`；名为 `_imports` 的子目录**不下发**（保留区对成员不可见，树里不会出现）。

请求：`?dir=<相对 scan_root 的目录>`（缺省/`"."` = 扫描根本身）。响应：`200 {"root","dir","unreadable","items":[{"name","rel","readable"}]}`（`root` = scan_root 展示值；`rel` 相对 scan_root）。`unreadable=true` / `readable=false` 锁定态语义与管理端一致。

错误：`400 INVALID_INPUT`（dir 越界 / `_imports` 保留段——**先于存在性检查** / 目录不存在 / 非目录）、`401 UNAUTHORIZED`、**`403 SCAN_ROOT_REQUIRED`**、`500 INTERNAL`。

### GET /jobs/:id  （Job000123，authed + 读权限）

成员查**自己触发**的扫描任务进度/终态。只覆盖 `index_jobs`（成员触发的扫描只进这张表；`transcode_jobs` 是系统任务，成员无权按 id 窥探）。

响应：`200 {"id","kind","status","total","processed","progress","started_at","finished_at","created_at"}`——`kind` = `full`（扫描任务）；`progress` = `processed/total`，`total` 缺失或为 0 时 `null`（「未知」≠「0%」）；`status`：`running`→`done`|`failed`。

错误：`400 INVALID_INPUT`（id 非 UUID——格式校验**先于** DB 查询，防 PG 类型错误伪装成 500）、`401 UNAUTHORIZED`、**`403 SCAN_ROOT_REQUIRED`**、`404 JOB_NOT_FOUND`（任务不存在或不属于你——归属过滤 `user_id = 调用者` 写在 SQL 里，别人的 id 与「不存在」逐字节同形，不做存在性预言）。

---


## 13. 360 播放（自研核心）


### GET /media/:id/360

返回 360 播放元数据：`{ "projection":"equirectangular", "hls_master":"/transcode/hls/:id/master.m3u8", "width","height","gyro_supported":true, "vr_supported":true }`

### WS /media/:id/360/state  （可选，未实现——v1.2 补注）

同步多端视角（如投屏到头显时手机作控制器）。**当前未实现**（计划内可选项），契约保留仅为规划留位。
> 说明：360 播放逻辑在**前端引擎**（Three.js 内翻球体 + VideoTexture；DeviceOrientation 陀螺仪；WebXR `immersive-vr` 头追；hls.js 自适应）。后端仅提供 HLS 流与元数据。详见 TDD §4.2。


---


## 14. 管理后台


### GET /admin/stats

系统概览：`{ "media_total","users","storage_used","index_status" }`

### POST /admin/index/rebuild

重建索引（低峰；PRD §11 风险对策）。

### GET /admin/settings

系统配置（DNS/域名/转码节点/SSO）读取。

### PATCH /admin/settings

更新配置。

---


## 15. 算力节点与带宽自测


### GET /compute-nodes

节点列表（含 `kind`/`status`/`codecs`/`has_nvenc`/心跳）。需 `admin:system`。

### POST /compute-nodes

登记节点：`{ "name","kind":"local_gpu|cloud_gpu|lan_agent","host"?,"codecs","has_nvenc","vram_mb","concurrency" }` → `{ "id","agent_token"? }`（lan\_agent 返回 agent 接入令牌）。

### PATCH /compute-nodes/:id

更新状态/能力/上下线。

### DELETE /compute-nodes/:id

移除节点。

### POST /bandwidth/self-test

触发带宽自测（上传/下载探针）→ `{ "up_kbps","down_kbps","latency_ms" }`，并写入 `bandwidth_tests` 与 `bandwidth_profiles`（source=self\_test）。

### GET /bandwidth

返回当前生效的上下行带宽（手动指定优先，否则最近自测值）：`{ "up_kbps","down_kbps","source" }`。

### PATCH /bandwidth

手动指定：`{ "up_kbps"?,"down_kbps"?,"scope":"global|share_token","ref_id"? }` → 写入 `bandwidth_profiles`（source=manual）。360 播放页据此向 `/transcode/hls/:id/master.m3u8` 选取 HLS 档位。

---


## 16. WebDAV（媒体直读直写，Job000055 落地）

> 对应 PRD §1.4「WebDAV 直出媒体」。支持通过 WebDAV 协议直接挂载为文件系统，用于 PC 端 SMB 替代或第三方工具直读。**2026-09-20 落地（用户裁决：完整读写）**，实现 `internal/media/dav.go`（`x/net/webdav`）。

**树形与语义（v1）**：
- 挂载点 `/dav/`；树 = **个人空间媒体库的 folder_path 目录树**（相册/标签不进 DAV——它们是虚拟组织，DAV 是文件夹语义）。根 = 个人库根。
- `GET /dav/:path` 下载原文件（owner + 未删除 + mediascope 读口径双保险）
- `PUT /dav/:path` 整文件上传 → 与 HTTP 上传**同一 ingest 管线**（去重/缩略图队列/EXIF）；`folder_path` 取自 URL 目录；**不支持覆盖**（已存在 → 405，先 DELETE 再 PUT）
- `DELETE /dav/:path` 软删入回收站（与网页端同语义，可恢复；purge 走网页端）
- `MOVE /dav/:path` 文件=改 `folder_path`/`filename`（纯组织变更不动存储实体）；目录=前缀批量改；目标已存在 → 405
- `MKCOL /dav/:path` **虚拟目录**：folder_path 由 media 行派生、无独立目录表 → 恒 201，首次 PUT 进该目录时显形（已知限制，登记簿）
- `PROPFIND` 标准属性（size/mtime/isplaycollection）；EXIF 自定义属性 v1 不提供
- 不支持：随机写（仅整文件 PUT，契约明示）、`COPY`、跨用户/共享空间（v1 仅个人库）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/dav/` | 列目录（按 folder\_path 映射） |
| GET | `/dav/:path` | 下载原文件（权限校验） |
| PUT | `/dav/:path` | 上传文件（触发索引流水线） |
| PROPFIND | `/dav/:path` | 获取属性 |
| MOVE | `/dav/:path` | 移动/重命名（更新 folder\_path） |
| DELETE | `/dav/:path` | 删除（软删入回收站） |
| MKCOL | `/dav/:path` | 创建目录（虚拟，见上） |

- 认证：HTTP Basic Auth（邮箱 + 主密码 或 **应用密码** `users.app_password_hash`——DDL 预留列的首个消费方）；不经 JWT
- 前端无独立 UI；挂载地址 = `https://<domain>/dav/`（设置页可后续加展示）


---


## 17. 速率限制

- 登录：同一 IP 10 次/分钟。

- 普通 API：每用户 600 次/分钟（令牌桶，Redis）。

- 分享 H5 播放：每 token 1000 次/小时。


---


## 18. ~~存储位置~~（v1.9 移除，Job000118）

存储位置功能自 v1.7 落地后从未启用（写入侧零消费、线上零数据），Job000118 整功能移除：
`internal/storage/locations.go` 及四端点（GET/POST/PATCH/DELETE `/storage/locations`）删除，
迁移 `00035_drop_storage_locations.sql` 回滚 `media.location_id` 与 `storage_locations` 表。
挂载导入落点由 `_imports/<id8>` 技术前缀改为 `landing_dir` 语义目录（见存储挂载端点与 §9 文件夹）。

---

## 19. 首次安装引导与版本（v1.8，Job000105/107）

### GET /setup/status

公开，无鉴权。响应 `{ "initialized": true|false, "version": "v1.0.0" }`。`initialized=false` 时前端把全部导航重定向到 `/setup` 向导页。状态查询失败 → 500。

### POST /setup

公开，无鉴权；**仅系统内尚无任何用户时可用**（一次性）。请求 `{ "email", "password", "display_name"? }`（密码 8~128 位；显示名缺省「管理员」，首个账号固定 role=owner 不接受指定）。成功 201 `{ "id", "email" }`；已有用户 → 409 `SETUP_COMPLETED`；格式错 → 400 `BAD_REQUEST`。审计动作 `setup.complete`。

### GET /version

公开，无鉴权。响应 `{ "version": "1.0.0", "commit": "<short-sha>", "build_date": "<RFC3339>" }`；未注入版本信息的开发构建 `version` 恒为 `"dev"`。部署验收：`curl -s http://<host>/version` 应与发布版本一致。

---

## 20. 转码远程调试通道（v1.14，Job000121）

**定位**：一次性、短寿命、全审计的远程排障通道——管理员开启 → 系统签发 URL+密钥 → agent 经 WSS 接入，对转码控制面执行白名单内结构化命令，**不提供任意 shell**。全局单通道单行表（第二个连接 409，换人走 rotate）。参考 agent：`cmd/debugctl`。

### 数据模型（迁移 `00041_debug_channel.sql`）

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `id` | BOOLEAN PK DEFAULT true CHECK(id) | 单行表惯用法 |
| `enabled` | BOOLEAN NOT NULL DEFAULT false | 开关态；disable=置 false 保留行（幂等 204） |
| `channel_id` | TEXT NOT NULL UNIQUE | 24 hex 公开标识（入 URL） |
| `key_digest` | TEXT NOT NULL UNIQUE | sha256(access_key) hex；明文永不入库 |
| `created_by` | UUID → users(id) | 开启者 |
| `created_at`/`expires_at` | TIMESTAMPTZ | TTL 跨度=档位 |
| `last_connect_at`/`last_connect_ip` | TIMESTAMPTZ / TEXT 可空 | 最近接入信息；查询列 `COALESCE(last_connect_ip,'')` 兜空串 |

### 管理端点（全部 `RequirePerm("admin:system")`，JWT 面）

#### GET /admin/debug/status

- 未开启（或已过期）：200 `{enabled:false, connected:false}`
- 已开启：200 `{enabled:true, connected, channel_id, created_at, expires_at, last_connect_at?, last_connect_ip?}`——**无明钥**（密钥只在 enable/rotate 响应出现一次）

#### POST /admin/debug/enable `{ttl_hours?}`

- `ttl_hours ∈ {1,8,24,72}`，缺省 24；非法 → 400 `BAD_TTL`
- 成功 201 `{url, key, expires_at}`（**key 唯一出口**）；`url` 恒 `wss://<host>/debug/channel/<channel_id>`
- 已开启 → 409 `DEBUG_ALREADY_ENABLED`（不轮换在用密钥；防误轮换，换钥走 rotate）

#### POST /admin/debug/rotate `{ttl_hours?}`

- 未开启 → 409 `DEBUG_NOT_ENABLED`
- 成功 200 `{url, key, expires_at}`；旧连接即刻被踢（关闭码 4004，reason「密钥已轮换」）
- `ttl_hours` 缺省沿用原档位 span（created_at→expires_at），显式传则按新档位

#### POST /admin/debug/disable → 204

- 幂等：未开启也 204。由开启转关闭时踢活动连接（关闭码 4003）+ 审计 `reason=manual`

### WS 接入端点（无 JWT，Bearer 密钥认证）

`GET /debug/channel/:channel_id` → 升级 WSS。握手前置矩阵（按序，全部在 upgrade 前完成）：

| 步 | 判定 | 响应 |
| --- | --- | --- |
| ① | channel_id 非 24 hex | 400 |
| ② | 握手专用限流桶超限（fail-open） | 429 `DEBUG_RATE_LIMITED` |
| ③ | 未开启 / 已关闭 / 已过期 | 404（三态同形，**不可探测**） |
| ④ | 进程内 Hub 槽已占（单连接策略 Q3） | 409 `DEBUG_ALREADY_CONNECTED` |
| ⑤ | 缺 `Authorization: Bearer <key>` | 401 `DEBUG_AUTH_REQUIRED` |
| ⑥ | 失败锁定中（fail-open） | 429 `DEBUG_LOCKED` + 审计 `debug.locked` |
| ⑦ | 密钥摘要比对失败 | 401 `DEBUG_AUTH_FAILED` + 失败计数；未开启/过期走 404 同形 |
| ⑧ | 非 WSS（`DEBUG_ALLOW_INSECURE_WS` 未开且 `X-Forwarded-Proto`≠https；反代终结 TLS 后 `c.Request.TLS` 恒 nil，只看后者会误杀 WSS——须兼看前者） | 426 `DEBUG_TLS_REQUIRED` |
| ⑨ | 通过 → upgrade + 登记槽位 + 审计 `debug.connect` | 101 |

- 失败锁定：5 次 / 10 分钟（固定窗口，非滑动）→ 锁 15 分钟。计数/锁键 `debug:fail:<ip>`、`debug:lock:<ip>`（**无前缀**），握手桶键 `rl:debug:hs:<ip>`（RateLimiter 自带 `rl:` 前缀）——三形态并存，均与全站 60 令牌桶隔离；运维清理 valkey 调试残留须 `--pattern '*debug:*'` 全量扫并复扫核验
- 关闭码：`4001`=通道到期（reaper）/ `4003`=管理员关闭 / `4004`=密钥轮换；正常关断与心跳（`DEBUG_WS_PING_INTERVAL` 秒，默认 30）由协议层承担

**协议帧**（JSON 文本帧）：请求 `{"seq":N,"type":T,"data":{...}}`；应答 `{"seq":N,"type":"result"|"event"|"error","data":{...}}`。首消息必须 `hello`（否则 1003 断开）；`hello` 应答 `{"server":版本串,"proto_ver":1,"server_time":RFC3339}`。

### 命令面（白名单 10 命令，v1 收窄三处）

| 命令 | 载荷 | 应答 |
| --- | --- | --- |
| `ping` | — | `{server_time}` |
| `snapshot` | — | `{jobs,queue,server_time}` 活动任务 + 队列深度（worker 心跳以 processing 深度代替说明） |
| `queue.stats` | — | 队列统计 |
| `subscribe` | `{"jobs":"*"}`（**v1 仅支持 "*"**，任务 id 数组留 v2，其他值 400 `INVALID_PARAMS`） | `{subscribed:"jobs"}` |
| `unsubscribe` | — | `{unsubscribed:"jobs"}` |
| `job.pause` / `job.resume` / `job.cancel` | `{"job_id":"<UUID>"}`（正则强校验，非 UUID 400 `INVALID_PARAMS`——防 PG 22P02 原文路径） | `{job_id,status}`；订阅中推送 `job.state` 事件 |
| `job.log.tail` | `{"job_id":"<UUID>","lines"?}`（≤500，缺省 100） | `{job,audit_tail}` 结构化档案 |

- `job.*` 错误口径：`JOB_NOT_FOUND`（不存在）/ `INVALID_STATE`（当前状态不允许；**cancel v1 不含 running**，请在终态后处置）/ `INTERNAL`（底层故障不回显原文）
- `job.log.tail` 返回**结构化档案**而非日志流：任务行（`status/profile?/result_path?/created_at`）+ 该任务相关最近审计事件（`audit_log WHERE detail->>'job_id'=$1` 倒序 ≤500 条）——产品无 per-job 日志文件（worker 输出在容器 stdout）；不提供 follow
- 每条命令落审计 `debug.cmd`（含 `cmd`/`job_id` 等白名单键）；未知命令 → `UNKNOWN_COMMAND`

### 审计八动作（`target_type=debug_channel` 单查询收齐）

`debug.enable` / `debug.disable`（detail `reason=manual|expired`）/ `debug.rotate`（`old_channel_fp`+`new_channel_fp`）/ `debug.connect` / `debug.disconnect` / `debug.cmd`（每条命令）/ `debug.auth_fail` / `debug.locked`。前端摘要：既有 `GET /admin/audit?target_type=debug_channel&limit=8`，**零新增审计端点**。detail 键名全走白名单（避开 `RedactDetail` 敏感子串）。

### env 与部署集成

| env | 默认 | 语义 |
| --- | --- | --- |
| `DEBUG_ALLOW_INSECURE_WS` | 关 | 仅开发：放行 ws:// 且仍限 127.0.0.1 回环来源 |
| `DEBUG_REAPER_INTERVAL` | 300s | 到期巡检周期（compose 已透传 api 服务） |
| `DEBUG_WS_PING_INTERVAL` | 30s | WS 心跳 |

nginx：docker/web 内嵌 conf 独立 `^/debug/` location（Upgrade/Connection 头 + 长读超时），第 2 组纯 API 正则增 `debug`——**不并入**第 1 组（WS 与 SPA 回退语义冲突）。

---

文档结束（API v1.16）。与《技术设计文档.md》《数据库 DDL.md》共同构成实现基线。商业化许可证合规矩阵见 PRD §12.2。
