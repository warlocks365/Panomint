# API 详细契约 (v1.3)


# 全景相册系统 · API 详细契约（OpenAPI 风格）

> 版本：v1.3 ｜ 日期：2026-09-20\ 配套：PRD v3.1 / TDD v1.1 / 数据库 DDL v1.1\ Base URL：`https://<domain>/api`\ 认证：**独立后台账户**，Bearer JWT（不对接 DSM）；SSO 走 OIDC

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
- 查询：`space=personal|shared`、`view=year|month|day|all`、`date=2026-08`、`type=photo|video|360`、`favorites=true`、`tag=`、`person=`、`cursor=`、`limit=`
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

当前用户地图 UI 偏好：`{ "map_slider_pos","map_filter_side","map_default_provider","map_default_zoom" }`（需登录）

### PUT /user/ui-prefs

更新偏好（滑块位置/筛选栏侧/默认底图）。

### GET /admin/map-config

系统地图配置读取（需 `admin:system`）。

### PUT /admin/map-config

更新配置（中国底图 provider、高德 API key【加密存储】、国际底图 URL）。
> 说明：聚合与直方图由后端按 `gps geometry(Point, 4326)`（PostGIS）+ `taken_at` 联合查询（`ST_MakeEnvelope` + `ST_Intersects` 做 bbox 过滤，时间桶聚合）；高德底图显示需将媒体 WGS-84 坐标转换为 GCJ-02。详见 TDD §3.7、DDL §2.5。


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

提交转码：`{ "media_id","kind":"thumbnail|hls|memories","profile":"1080p|2k|4k" }` → `{ "job_id" }`

### GET /transcode/job/:id（v1.2 补录）

转码任务状态查询（前端轮询用）→ `{ "id", "media_id", "status", "profile"?, "result_path"? }`。归属校验 JOIN `media.owner_id`；无权时返回 404 而非 403（与「任务不存在」同形，不做存在性探测）。

### GET /transcode/hls/:id/master.m3u8

返回 HLS 清单（自适应码率；边缘缓存）。

### GET /admin/jobs

索引/转码任务列表与进度（查 `index_jobs`/`transcode_jobs`）。

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


## 16. WebDAV（媒体直读直写）

> 对应 PRD §1.4「WebDAV 直出媒体」。支持通过 WebDAV 协议直接挂载为文件系统，用于 PC 端 SMB 替代或第三方工具直读。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/dav/` | 列目录（按 folder\_path 映射） |
| GET | `/dav/:path` | 下载原文件（权限校验） |
| PUT | `/dav/:path` | 上传文件（触发索引流水线） |
| PROPFIND | `/dav/:path` | 获取属性（EXIF/大小/时间） |
| MOVE | `/dav/:path` | 移动/重命名（更新 folder\_path） |
| DELETE | `/dav/:path` | 删除（软删入回收站） |
| MKCOL | `/dav/:path` | 创建目录 |

- 认证：HTTP Basic Auth（独立后台账户或应用密码）

- 中间件：Go 标准库 `golang.org/x/net/webdav` 或 `sablier`

- 限制：不支持随机写（仅整文件 PUT）；大文件断点续传用 `Content-Range`


---


## 17. 速率限制

- 登录：同一 IP 10 次/分钟。

- 普通 API：每用户 600 次/分钟（令牌桶，Redis）。

- 分享 H5 播放：每 token 1000 次/小时。


---

文档结束（API v1.2）。与《技术设计文档.md》《数据库 DDL.md》共同构成实现基线。商业化许可证合规矩阵见 PRD §12.2。
