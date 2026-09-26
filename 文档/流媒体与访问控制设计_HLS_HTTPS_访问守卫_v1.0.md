# 流媒体与访问控制设计：HLS 设置 / HTTPS 设置 / 登录回跳 / 初始化守卫

> 版本 v1.0 ｜ 2026-09-26 ｜ 关联 Job000125（拟）
> 范围：HLS 必要性分析 + 管理后台四项功能的设计与实现口径。

---

## 一、为什么全景视频播放必须依赖 HLS

### 1.1 问题本质

一段 4K 360° 全景视频（equirectangular，10 分钟）原始体积约 4–8 GB。浏览器 `<video>` 直连源文件（progressive download）有四个不可解的矛盾：

| 矛盾 | 直连源文件的表现 | HLS 的解法 |
|------|------------------|-----------|
| **首帧延迟** | 必须等 HTTP 响应头 + moov atom 到位才能起播；大文件冷启动可达数十秒 | 服务端切成 2–20s 的 TS 分片，播放器拿到 `master.m3u8`（几 KB）+ 首个分片即可起播，**首帧时间 ≈ 一个分片时长** |
| **带宽波动** | 源文件只有一条码率曲线：带宽跌了就卡顿，带宽升了也用不满 | 多码率 ladder（本系统 480p→4K 按源分辨率裁剪），hls.js 双 EWMA 带宽估算**逐分片升降档**（ABR） |
| **浏览器兼容** | 4K/H.265 直连在 Chrome/Firefox/移动端普遍不支持或硬解失败 | HLS 播放器统一走 MSE（Media Source Extensions）喂软/硬解，`hls.js` 覆盖所有主流浏览器（Safari 原生 HLS 兜底） |
| **拖动seek** | 直连依赖 Range 请求与 mp4 索引对齐，360° 视角切换叠加变焦时缓冲命中差 | 分片即 seek 单元：拖到任意时间点 = 请求对应序号的分片，**秒级响应** |

### 1.2 全景场景的加成必要性

360° 视频与平面视频的关键差异：**用户视线方向不可预知**，任意时刻都可能看向画面任意区域：

- 360° 播放器按视角投影渲染时实际解码的是**完整帧**（本项目采用球面 UV 贴图方案，非 tile），码率需求高于同分辨率平面视频 → **必须靠 ABR 在带宽与画质间动态取舍**，这正是 HLS 的核心能力；
- 陀螺仪/头追会让用户在**视角突变**时对起播延迟极其敏感 → 分片化让缓冲窗口（`maxBufferLength`）可以做得小而精准；
- 微信 H5 分享场景（项目 P0 需求）：微信内置浏览器 iOS 走原生 HLS，Android 走 MSE+hls.js，**HLS 是唯一同时覆盖两端的协议**。

### 1.3 本系统的 HLS 实现现状（设计基线）

- ffmpeg `HLSArgsEnc` 多码率一次性转码（`-hls_time` 分片、`master.m3u8` + `<档位>/seg_NNN.ts`），硬编失败自动回退软编；
- 服务：`GET /transcode/hls/:id/*file`（鉴权+归属校验+防穿越）与分享公开端点；
- 缓存：m3u8 `no-cache`（清单常变）、ts `immutable` 一年（内容寻址不变）；
- 播放：hls.js ABR（`capLevelToPlayerSize` + 双 EWMA），URL 由前端 `API_BASE + hls_master` 拼接。

**结论：HLS 不是可选项，而是全景视频在浏览器可达性的唯一工程解**。管理后台要配置的是它的**参数面**（分片时长/缓存策略/外部地址），而非"要不要用"。

---

## 二、总体架构与相互影响

```
┌─ 浏览器 ─────────────────────────────────────────────┐
│  路由守卫（beforeEach）                               │
│   ① setup 闸门：未初始化 → /setup（含 /login 一并拦截）│
│   ② 鉴权闸门：未登录 → /login?redirect=<原路径>        │
│   ③ 登录成功 → 读 redirect 回跳原页面                 │
│  播放器：hlsUrl = (stream_base_url || API_BASE) + ... │
└──────────────┬───────────────────────────────────────┘
               │ HTTPS（Caddy 终止 TLS）
┌──────────────▼─ 服务端 ───────────────────────────────┐
│  Gin 中间件链：                                       │
│   ④ force-https：X-Forwarded-Proto=http 且开关开      │
│      → 301 https（DB 配置热生效，60s 内存缓存）        │
│   ⑤ 既有鉴权/RBAC/限流                                │
│  转码 worker：SegSeconds ← system_transcode_config    │
│  ServeHLS：Cache-Control ← hls_cache_profile          │
└───────────────────────────────────────────────────────┘
```

**守卫顺序是设计核心**：初始化检测最外层（系统没就绪时谈登录无意义）→ 鉴权 → 页面内权限。
**生效层次分两类**：可热生效的走 DB + 应用层（登录回跳、初始化守卫、force-https、缓存策略、分片时长对**新任务**生效）；必须动反代的（证书替换）走"上传落卷 + 指引重启"。

---

## 三、功能 1：HLS 设置（管理后台 · 转码页签）

### 3.1 配置项规格

| 配置项 | 存储（system_transcode_config 新列） | 默认值 | 校验规则 | 消费方 |
|--------|--------------------------------------|--------|----------|--------|
| HLS 服务开关 | 复用 `auto_transcode` + `realtime_transcode`（语义映射，不新增列） | false/false | — | UI 文案映射：自动转码=总闸，播放时自动转码=触发方式 |
| 分片时长（秒） | `hls_seg_seconds INT` | 4 | 整数 2–20；变更仅影响**新转码任务**，存量分片不变（UI 红字提示） | worker 入队/执行时读 `GetSystemConfig` 填 `SegSeconds` |
| 缓存策略 | `hls_cache_profile TEXT` | `balanced` | 枚举 `no_cache` / `balanced` / `aggressive` | `ServeHLS` 输出 Cache-Control（热生效） |
| 流媒体地址 | `stream_base_url TEXT` | 空（站内相对路径） | 空 或 `http(s)://` 开头且不以 `/` 结尾 | 前端 `usePlayerMedia` 拼 master.m3u8 前缀 |

**缓存策略三档语义**：

| 档 | m3u8 | ts 分片 | 适用 |
|----|------|---------|------|
| `no_cache` | no-cache | `max-age=300` | 调试/产物可能被覆盖重转 |
| `balanced`（默认） | no-cache | `max-age=31536000, immutable` | 常规（=现状） |
| `aggressive` | `max-age=60` | immutable | 只读归档库，m3u8 也可短缓存 |

### 3.2 关键实现逻辑

- 后端：迁移 00042 加三列；`PutSystemConfig` COALESCE 部分更新扩展 + 校验（非法值 400 带字段名）；审计 `recordAs`；
- `GET /transcode/config`（登录可读，播放器在用）同步返回新字段 → 前端零额外请求拿到 `stream_base_url`；
- worker 消费：本地转码执行器在派发任务前读一次系统配置（进程内 60s 缓存），`payload.SegSeconds<=0` 时用配置值覆盖；
- 前端：TranscodeTab 增加「HLS 流媒体」卡片（与现有开关卡并列），数据-testid 齐全。

---

## 四、功能 2：HTTPS 设置（管理后台 · 新增「网络」页签）

### 4.1 配置项规格

| 配置项 | 存储 | 默认 | 说明 |
|--------|------|------|------|
| 强制 HTTPS | 新表 `system_https_config.force_https` | false | **应用层热生效**：Gin 全局中间件判 `X-Forwarded-Proto`（Caddy 透传），=http 且开关开 → 301 `https://{host}{uri}`；60s 内存缓存避免每请求查库；`/health` `/ready` 豁免 |
| 证书上传 | 文件落 `HTTPS_CERT_DIR`（env，默认 `/certs`，宿主卷挂载） | — | multipart 上传 fullchain.crt + private.pem；服务端解析 x509 校验 PEM 合法性并自动提取 `not_after` |
| 证书路径配置 | `cert_path/cert_key_path`（记录用） | Caddyfile.tls 现值 | 纯记录+展示：TLS 终止在 Caddy，路径改动需改 env/挂载并重启 caddy |
| 证书到期日 | `cert_not_after TIMESTAMPTZ` | — | 上传时自动解析；支持手填；卡面倒计时显示，<30 天黄色、<7 天红色告警 |

### 4.2 生效边界（关键设计决定）

**[自行决策] 不做 TLS 运行时热切换**。理由：TLS 终止在 Caddy 容器（红线：不自动化重启入口容器——断 web 入口风险）；Caddy 无 reload 管理通道。因此：

- **强制 HTTPS / HTTP 自动跳转**：应用中间件实现，保存即生效（可热）；
- **证书上传**：落共享卷 + 校验 + 记录到期日，返回「下一步指引」（更新 `TLS_CERT`/`TLS_KEY` env 指向新文件 → `docker compose restart caddy`），重启动作留给用户（界面明确提示中断秒级）；
- **当前状态检测**：卡面显示当前请求协议（后端从 X-Forwarded-Proto 读）、证书到期日、force_https 生效状态。

### 4.3 关键实现逻辑

- 迁移 00042 建表 `system_https_config`（singleton 模式，与 transcode config 同构）；
- 新包 `internal/httpscfg`：Store（Get/Upsert）+ Handler（GET/PUT /admin/https/config、POST /admin/https/cert）+ `ForceHTTPS()` 中间件；
- 权限一律 `admin:system`；上传文件名服务端定死（防路径注入）；私钥落盘 0600。

---

## 五、功能 3：鉴权拦截强化（登录回跳）

### 5.1 行为规格

| 场景 | 现行为 | 新行为 |
|------|--------|--------|
| 未登录访问 `/albums/xxx` | → /login（登录后回 /timeline，**丢失上下文**） | → `/login?redirect=/albums/xxx`，登录成功**回原页** |
| 登录后访问 /login | → / | → 尊重 `redirect`（合法时） |
| 401 强制登出 | → /login | → `/login?redirect=<当前路径>`（同一次会话上下文保留） |
| redirect 参数安全 | — | **防 open redirect**：必须匹配 `/^(?!\/\/)[/?#]?\/.*/` 的站内路径（以单个 `/` 开头、非 `//`、非外部协议）；非法值一律回退 /timeline |

### 5.2 关键实现逻辑

- `router/index.js` 守卫：`return { name: 'login', query: { redirect: to.fullPath } }`；
- `LoginView.onSubmit` 成功分支：`safeRedirect(route.query.redirect) || '/timeline'`；
- `utils/url.js` 新增 `safeInternalPath()` 供守卫与登录页共用（单点实现+单测）。

---

## 六、功能 4：初始化检测强化（setup 守卫）

### 6.1 行为规格

| 场景 | 行为 |
|------|------|
| 未初始化 + 访问任意页（**含 /login**） | → /setup（**已实现**，Job000107） |
| 已初始化 + 访问 /setup | → /login（已实现） |
| 初始化完成后 | 闸门自动失效（后端 initialized=true 驱动，前端无残留） |
| setup 状态不可达 | 不拦（可用性优先，后端 SetupView 提交时 fail-closed 兜底，已实现） |

### 6.2 本轮增量

- **状态缓存**：`getSetupStatus` 结果模块级缓存 15s TTL——之前**每次路由导航**都发一次 `GET /setup/status`（时间轴缩放/地图拖动等高频导航场景白耗请求）；SetupView 初始化成功后主动清缓存，保证闸门即时失效；
- **公开分享路由豁免核对**：`/share/:token` 在未初始化时同样被拦（正确：未初始化的库不存在分享链接），行为保持。

---

## 七、相互影响矩阵

| 交互点 | 影响与处置 |
|--------|-----------|
| setup 闸门 × 登录回跳 | 未初始化时 redirect 无意义（闸门先行）；初始化完成后从 /setup 跳 /login **不带 redirect**（setup 页本身不是用户目标页） |
| force-https 中间件 × 守卫 | HTTPS 重定向发生在**服务端最外层**，先于一切应用逻辑；前端守卫只在最终协议上运行 |
| stream_base_url × 鉴权 | 外部地址必须与站点同源或自行解决回源鉴权（HLS 端点带 token 鉴权；CDN 直连会 401）——卡面红字提示 |
| hls_seg_seconds × 存量产物 | 只影响新任务；UI 明示"变更不重转存量视频" |
| 缓存策略 × 分享端点 | 公开分享 HLS 复用同一 ServeHLS 逻辑，策略三端一致 |
| 证书上传 × Caddy | 落卷不重启；到期日告警驱动用户手动续期（呼应登记簿遗留项：证书 2026-10-13 到期） |

---

## 八、验收清单

1. 未登录直访 `/albums` → 落登录页且 URL 带 `redirect=/albums`；登录后回到 `/albums`；
2. 未初始化库（新部署）直访 `/login` → 强制落 `/setup`；初始化完成后 `/setup` 再访问落 `/login`；
3. 管理后台改分片时长 4→6 → 新转码任务 ffmpeg 参数含 `-hls_time 6`（存量不变）；
4. 缓存策略切 `no_cache` → ts 响应头 `max-age=300`；
5. force_https 开启 → 以 http 请求任意 API → 301 到 https；关闭 → 直达；
6. 证书上传非法 PEM → 400；合法 → 落卷 + not_after 自动解析展示。
