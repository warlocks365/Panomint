# 审查分卷：需求符合性差距矩阵

审查基线：HEAD=61838f8 ｜ 对照文档：计划 v1.0 / PRD v3.1 / TDD v1.1 / API v1.1 / 进度表 0917
审查人：requirements-auditor ｜ 审查方式：只读对照（未修改任何项目源码）
端点核对口径：以 `src/backend/cmd/api/main.go` 实际注册行为准（共 105 条注册路由）；契约以《API详细契约_v1.1》17 章为准（约 97 条声明端点/策略）。

状态图例：✅ 已实现+有证据 ｜ 🔶 已实现但无独立验证证据 ｜ 🟡 部分实现（注明缺口）｜ ❌ 未实现（含「未实现-计划内」）｜ ⤳ 已实现但与契约形状有偏差（功能等价）

---

## 一、模块对照矩阵

### 1. 基础设施与合规（Phase 0：T0.1–T0.5）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| 计划 T0.2 | Docker Compose 三服务（PG16+postgis/pgvector/pg_trgm、valkey、minio），60s healthy | ✅ | `docker-compose.yml`（HEAD 已扩至 12 服务）；进度表：11 容器全 Up、health/ready 200 | DDL 全量由 goose 迁移落地；「30 表」口径与实际不符（DDL 实际 28 张 `CREATE TABLE`，迁移 26 版全覆盖 28 张）——文档口径出入，非实现缺口 |
| 计划 T0.3 / AC-01~04 | Valkey 等价替换 + 队列封装（入队/消费/ack/指数退避重试/延迟）+ 令牌桶限流原型 | ✅ | `src/backend/internal/queue/`（含测试）；valkey 8.0-alpine 在 compose | 进度表 §四：27 包测试全绿 |
| 计划 T0.4 / AC-05~07 | ffmpeg subprocess 封装（超时/取消/SIGTERM、-progress 解析、结构化错误、缩略图三档/HLS 预设） | ✅ | `src/backend/internal/ffmpeg/`（runner/parser/types + 测试）；transcode/index worker 真实调用（Job000018 `5740bb7`） | NVENC 接口+软编回退（Job000019 `39253e4`）；用户决策 Q3：本机无 N 卡，硬编留 GPU 节点验证 |
| 计划 T0.5 / AC-09 | go-licenses 扫描，无 RSALv2/SSPL/GPL/AGPL 直接依赖 | ✅ | 仓库根 `licenses.csv` | 合规矩阵见 PRD §12.2 |
| PRD §3 NFR-可维护 | Docker 化、配置即代码 | ✅ | compose + `.env.example` + Job000026 配置去耦（`be9ae1b`，生产门禁） | |

### 2. 后端骨架（Phase 1：T1.1–T1.5）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| 计划 T1.1 | API 骨架 + 五件套中间件 + /health /ready /metrics | ✅ | `cmd/api/main.go:397-399`、`internal/middleware`、`internal/health` | 进度表：health/ready 200 |
| 计划 T1.2 | golang-migrate/goose 落地 DDL 全量 + 种子权限 | ✅ | `src/backend/migrations/` 00001–00026（v26） | DDL 28 表全覆盖 |
| 计划 T1.3 / API §2 | JWT（access+refresh）、bcrypt、RBAC、会话吊销 | ✅ | `internal/auth`；进度表：refresh 轮换+重放吊销已验证 | |
| 计划 T1.4 | 媒体索引：扫描/EXIF/ffprobe/360 判定/hash 去重/缩略图入队 | ✅ | `internal/index`、`cmd/indexctl`、index-worker 常驻 | **契约形状偏差**：契约 §3 `POST /media/index` 端点未注册（grep `media/index` 后端 0 命中）；索引由上传自动触发 + `watchctl` 监听 + CLI 承担 |
| 计划 T1.5 / API §3 | GET /media 年/月/日聚合 + 游标分页 + 过滤 | 🔶 | `internal/media` timeline | **验收缺口**：AC「10 万级分页 <1s」未验证——进度表自承库仅 93 条、未做规模测试（风险表亦列） |
| API §3 | `POST /media/index` 触发索引 | ❌ | 无（两处 grep 0 命中） | 功能由 watchctl/上传自动索引等价覆盖；契约未同步 |

### 3. 360 播放引擎（Phase 2 T2.1 / Phase 3 T3.5 / PRD §6.11–6.12 / TDD §4.2 / API §13）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| 计划 T2.1 | 独立 360 原型（球面渲染/交互/陀螺仪/WebXR/hls.js ABR/移动性能六子系统） | ✅ | `prototypes/360-player/`（index.html + streams + streams8k + vendor） | |
| AC-10 | 桌面拖拽/缩放 ≥30fps | 🔶 | 原型 + `360Player.vue` | 无独立帧率实测记录 |
| AC-11 | iOS+Android 陀螺仪、权限拒绝降级 | 🟡 | `360Player.vue`（含微信 UA 降级提示） | 微信内降级逻辑在码；**真机未验收**（进度表 ⌗；Phase3 报告遗留「待用户微信内截图反馈」） |
| AC-12 | 弱网 2Mbps HLS 自动降档 | 🔶 | hls.js ABR 集成 | 无限速实测记录 |
| AC-13 | VR 头显 immersive-vr 分屏头追 | 🔶 | 原型 + 播放器 WebXR 路径 | 无 Quest/Pico 真机记录（进度表 ⌗） |
| 计划 T3.5 / API §13 | 引擎迁入播放页；`GET /media/:id/360`；HLS 管线 | ✅ | `main.go:149`、`PlayerView.vue`、`components/player/360Player.vue`；Phase3 验收报告 T3.5 ✅ | |
| PRD §6.11 | 360° 照片球面查看（equirect 照片） | ✅ | `PlayerView.vue:13-24`（照片走 TextureLoader 球面贴图，视频走 HLS） | P1 已交付 |
| API §13 | `WS /media/:id/360/state` 多端视角同步（可选） | ❌ 未实现-计划内（可选） | 无 | 契约注明「可选」 |
| NFR | 4K 360 起播 <3s | 🔶 | HLS 管线在 | 缺 ≥4K 源无法验证（进度表已列） |

### 4. 地图模式（Phase 2 T2.2 / PRD §6.6 / API §7 / TDD §3.7）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| 计划 T2.2 / AC-14~16 | 地图原型：bbox 聚合 + WGS84→GCJ-02 + 双底图瓦片反代 | ✅ | `prototypes/map-mode/` + `cmd/mapproto` | |
| API §7 | `GET /map/items`（bbox+zoom 聚合） | ⤳ | `main.go:243-244` 注册为 `/geo/clusters`、`/geo/items` | **路径漂移**：契约 `/map/items` ↔ 实现 `/geo/*`；契约未同步 |
| API §7 | `GET /map/timeline`（时间跨度直方图） | ⤳ | `main.go:245` `/geo/histogram` | 同上路径漂移 |
| API §7 | `GET /map/search`（高德/Nominatim 自动选源） | ✅ | `main.go:264`；降级头 `X-Map-Search-Degraded` | 契约路径一致 |
| API §7 | `GET /map/providers` | ✅ | `main.go:265` | |
| API §7 | `GET/PUT /user/ui-prefs`（滑块位置/筛选栏侧/默认底图） | ✅ | `main.go:254-255`；进度表：布局偏好服务端持久化 | |
| API §7 | `GET/PUT /admin/map-config`（含密钥加密存储） | 🟡 | `main.go:256-257` | **缺口**：密钥写入仅 env 注入，界面填 Key 需密钥管理方案（进度表 B4 待拍板） |
| PRD §6.6 | 全屏地图 + 时间轴双向联动 + 筛选栏 | ✅ | `MapView.vue`（29KB）；进度表：双向联动+kind 四类筛选 | |
| TDD §3.7 | 高德瓦片反代（Key 服务端注入） | ✅ | `main.go:247` `/tiles/amap/:z/:x/:y` + 磁盘缓存 | |
| PRD §6.6 | OSM 国际底图 | 🟡 | 前端界面如实提示缺代理 | 进度表：OSM 底图缺瓦片代理 |
| PRD §5/§10 P2 | 3D 地图矢量/卫星增强（建筑挤出） | ❌ 未实现-计划内 | 无（grep `fill-extrusion` 仅 maplibre 库内部） | 进度表 B6：P2 范围待确认 |
| —（契约外） | `/geo/places` 地理位置罗列、`GET/PUT /preferences/map` 图标配置 | ⤳ | `main.go:246,248-249` | Job000009 依据充分，但**契约 §7 未同步收录** |

### 5. 时间轴与媒体库（Phase 3 / PRD §6.2、§6.8、§6.10 / API §3、§9）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| 计划 T3.2 / PRD §6.2 | 时间轴首页：虚拟滚动、年月日钻取、懒加载、360 角标 | ✅ | `TimelineView.vue` + `GET /media` + `/media/date-histogram`；Phase3 验收 ✅ | 「10 万媒体首屏 <2s」未规模验证（🔶 性能项） |
| 计划 T3.3 / PRD §6.3 | 双空间切换 | ✅ | `SpacesView.vue` + `GET /spaces` | 跨用户越权修复后 60/0/1 对抗验证（进度表 §四.1） |
| PRD §6.8 / API §9 | 文件夹视图：`/folders/tree` ✅、`/folders/:id/media`、`/folders/:id/albums` | 🟡 | `main.go:183` 仅注册 `/folders/tree`；`FoldersView.vue` 已存在 | **缺口**：后两条端点未注册（grep 0 命中）；前端 nav「文件夹」仍 `ready:false`（`AppShell.vue:164`），路由已挂但入口未开 |
| 计划 T3.4 | 上传（分块续传 Content-Range）/下载/进度 | ✅ | `internal/media/upload.go`（分块状态机+测试）；`UploadView.vue`；Phase3：9 块续传 sha256 一致 | |
| 计划 T3.6 / PRD §6.10 | 查看器：EXIF/收藏/评级/软删/回收站 | ✅ | `MediaViewer.vue` + `/media` 端点群；Phase3 验收 ✅ | |
| PRD §6.10 | 幻灯片 | ✅ | `MediaViewer.vue:54-65` + `stores/viewer.js`（间隔/跳过视频 360） | |
| PRD §6.10 | 基本编辑：旋转/裁剪/自动增强（非破坏） | 🟡 | `POST /media/:id/rotate`（`main.go:151`，写 `media.edits`） | **缺口**：契约请求体 `op:rotate|crop|auto` 三操作，实现仅旋转；裁剪/自动增强无实现 |
| API §3 | `live_photo_pair_id` | 🟡 | 迁移 00004 列 + `detail.go:85` 透传 | 配对识别逻辑未见实现（仅 schema+读透传） |
| API §3 | `PATCH /media/:id`（备注） | ⤳ | `main.go:142` | Job000005 依据；契约 §3 未收录此端点 |
| API §3 | `GET /media/date-histogram` | ⤳ | `main.go:138` | Job000005 依据；契约 §3 未收录 |

### 6. 相册（PRD §6.4 / API §5）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| API §5 | 相册 CRUD + 封面 + 条目增删 | ✅ | `main.go:192-198`、`internal/albums` | 进度表 90% |
| API §5 | 智能相册（query 条件实时聚合） | ✅ | `internal/albums/criteria.go`（type/tag/place/date/favorites 条件 + 测试） | 参数名 `kind`（normal\|smart）与契约 `type`（manual\|smart）存在命名漂移 |
| API §5 / PRD §6.4 | 四类相册：普通/智能/**共享**/收藏 | 🟡 | normal/smart/favorites 已实现 | **缺口**：`shared` 类型相册未实现（`handlers.go:56` 仅允许 normal\|smart）；共享语义由「共享空间」承担，契约/PRD 的「共享相册」类型无对应 |
| API §5 | `GET /albums/:id/items` 分页 | ⤳ | 无独立路由；`GET /albums/:id` 内联返回 items（`store.go:252`） | 功能等价、形状偏差 |
| API §5 | 相册评论（含嵌套 parent_id） | ✅ | `main.go:199-201` | |
| 前端入口 | 相册页可达 | 🟡 | `AlbumsView.vue`/`AlbumDetailView.vue` 完整实现、路由已注册 | **文档失信（状态未更新）**：nav「相册」仍 `ready:false`（`AppShell.vue:160`）显示「开发中」，页面实际可用 |

### 7. 人物（PRD §6.5 / API §6 / TDD §3.2）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| API §6 | `GET /people`（named+unnamed 聚类） | ✅ | `main.go:173`、`internal/faces` | 进度表：69 张/65 簇/零误检 |
| API §6 | `POST /people` 命名/合并（cluster_ids、is_pet） | ✅ | `main.go:174` | |
| API §6 | `PATCH /people/:id` 隐藏/改名 | ✅ | `main.go:175` | |
| API §6 | `GET /people/:id/media` | ✅ | `main.go:176` | |
| TDD §3.2 | YuNet 检测 + SFace 128 维 + pgvector 聚类（Apache-2.0） | ✅ | faces-worker（`facesgen -mode watch`）；模型随镜像固化 | 聚类阈值 0.40 经真实库标定（compose 注释） |
| PRD §6.5 验收 | 设备端式聚类、自动聚合准确 | 🔶 | 同上 | **召回不足为已知项**：进度表 A6/Job000041（多尺度卡在可运行 ORT 环境）；Job000032 已改原图优先 |
| 前端 | 人物页 | ✅ | `PeopleView.vue`（nav ready） | |

### 8. 标签（PRD §6.7 / API §8）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| API §8 | `GET /tags`（kind/confirmed 过滤） | ✅ | `main.go:154` | |
| API §8 | `POST /tags` 用户标签 | ✅ | `main.go:161` | |
| API §8 | `POST /media/:id/tags` | ✅ | `main.go:155`（另有 DELETE 移除） | |
| API §8 | `POST /tags/:id/confirm` AI 标签人工确认 | ✅ | `main.go:165`（另有 `/media/:id/tags/confirm` 批量） | PRD「confirmed 人工确认」语义落实 |
| Phase 4 概要 | AI 自动打标 | ✅ | `GET/POST /ai/tags`（`main.go:167-168`）+ tag-worker 零样本（CLIP 文本塔×embedding，108→570 词表已扩容并持久缓存） | |
| —（契约外） | 标签管理 CRUD：`PATCH/DELETE /tags/:id`、`GET /tags/:id/media` | ⤳ | `main.go:162-164` | Job000010 依据；**契约 §8 未同步** |
| 前端 | 标签页 | ✅ | `TagsView.vue`（nav ready） | |

### 9. 搜索（PRD §6.9 / API §10）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| API §10 | `GET /search`：关键词（tsvector）+ 筛选 + pgvector 语义召回 + place | ✅ | `main.go:230`、`internal/search`（结构化 + VectorRecaller + geo_cache 地名解析） | 进度表 85%；可见性谓词与媒体侧口径统一已收口（A2/MediaRef 收口已上线） |
| API §10 | `GET /search/video-moment` 视频瞬间帧级定位 | ❌ | 无 | `internal/search/types.go:8` 明确注释「本期不实现」——**实现方已声明，契约未同步标注** |
| NFR | 10 万媒体搜索 <1s | 🔶 | — | 库 93 条，未规模验证 |
| 前端 | 搜索结果页 | ✅ | `SearchResultsView.vue` | |

### 10. 分享 / 微信 H5（PRD §6.14 / API §11）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| API §11 | 创建分享（相册/单媒体、expire/password/allow_download/is_wechat） | ⤳ | `POST /shares`（`main.go:206`，body `kind,target_id,...`） | **路径漂移**：契约 `POST /share/album`、`POST /share/media` ↔ 实现统一 `POST /shares`；另有 max_views 访问上限（DDL share_access_log 依据，契约未收录） |
| API §11 | `GET /share` 列表、`DELETE /share/:id` 撤销 | ⤳ | `GET /shares`、`DELETE /shares/:id` | 复数路径漂移 |
| API §11 | `GET /s/:token` 访客免登 H5 | ⤳ | `GET /public/shares/:token` + `SharePublicView.vue`（路由 `/share/:token`） | 路径漂移；功能在 |
| API §11 | `POST /s/:token/unlock` 密码校验（cookie 会话） | 🟡 | 无 unlock 端点；密码经 `?password=` 查询参数校验（`handlers.go:222`） | 契约亦写 `?pwd=`——参数名 `pwd`↔`password` 不一致；`og.go:144` 已自述 URL 带密码的弊端，cookie 会话方案未落地 |
| API §11 | `GET /s/:token/playlist.m3u8` | ⤳ | `GET /public/shares/:token/media/:id/hls/*file` | 功能等价 |
| API §11 | 分享带宽自测（token 鉴权三段式） | ✅ | `main.go:278-280`（probe GET/POST + bandwidth-test POST） | |
| Phase 4 | OG 封面（服务端渲染中性卡片） | ⤳ | `main.go:275` `/public/shares/:token/og` | Job000013 依据；**契约 §11 未收录** |
| API §11 | 公开下载（allow_download=true 时） | 🟡 | `main.go:271` 占位统一 403，注释「P1 实现」 | **占位未实现**：契约 `allow_download` 语义在公开侧落空 |
| PRD §6.14 | 微信内观看（H5） | 🔶 | is_wechat 标记 + 微信 UA 陀螺仪降级 + OG 卡片 | **微信实机分享卡片+点开播放未验证**（进度表 C2） |

### 11. 鉴权与账户（PRD §6.1 / API §2 / TDD §7）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| API §2 | login/refresh/logout/me | ✅ | `main.go:100-106` | 登录审计 actor 修复（进度表 §四.5） |
| API §2 | `POST /auth/2fa`（temp_token 二次验证） | ⤳ | 无该端点；实现为 login 请求体内可选 `totp_code` + `MFA_REQUIRED/MFA_INVALID` 错误码；管理端 `POST /auth/mfa/setup|confirm|disable`（`main.go:112-114`） | **形状偏差**（无 temp_token 两段式）；功能等价且防 2FA 枚举（`handlers.go:111` 注释）；TOTP 经 RFC 官方向量验证（进度表） |
| API §2 | `POST /auth/sso/oidc` | ❌ 未实现-计划内 | 无（grep sso/oidc 0 命中） | P2；进度表 B5 待拍板 |
| API §2 管理端点 | users CRUD / roles 列表+新建 / audit | ✅ | `main.go:350-377` | 提权守卫 `PermCovered` + 双硬守卫（`CheckUserPatch`） |
| PRD §6.1 / 契约 | 用户自助改密 | ❌ | 无路由、/settings 无入口 | **已登记 Job000034（未开始）**——唯一途径是管理员 PATCH |
| API §17 | 登录限流 10 次/分/IP | ❌ | 无登录专属限流（grep auth 包无 RateLimit） | 仅全局 `RateLimit(60,10/s)` per-IP（`main.go:75`），登录被同等 600/分放开，未达契约 10/分 |
| API §17 | 普通 API 600/分/用户 | 🟡 | 全局令牌桶 600/分 **per-IP** | 维度偏差（IP≠用户）；量级一致 |
| API §17 | 分享 H5 1000 次/时/token | ❌ | 无 token 维度限流（shares 包无 RateLimit） | |

### 12. 管理后台（PRD §6.15 / API §14）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| API §14 | `GET /admin/stats` | ✅ | `main.go:391`（audit 包实现） | |
| API §14 | `POST /admin/index/rebuild` | ❌ | 无（grep 0 命中） | 进度表 B1：重建语义待拍板（缩略图/向量/标签/人脸？） |
| API §14 | `GET /admin/settings`、`PATCH /admin/settings` | ❌ | 无 | 进度表 A4（GET 计划内小项）/ B3（PATCH 旋钮待拍板） |
| API §12 | `GET /admin/jobs`、`GET /admin/jobs/:id` | ✅ | `main.go:392-394` | `/admin/jobs/:id` 于 8706394 后补上 |
| PRD §6.15 | 管理后台**页面**（用户/共享空间/索引/转码） | ❌ | **前端无任何 admin UI**（grep 前端 `/admin/` 0 命中；15 个 view 无 AdminView） | 后端端点齐全且经脚本验证（进度表 85% 口径为 API 级），但 PRD §6.15 的页面不存在——管理操作只能靠 API/脚本，这是 P2「管理后台完整」最大的形态缺口 |

### 13. 审计（TDD §7 / Phase 5）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| Phase 5 | 审计日志 + `GET /admin/audit` | ✅ | `internal/audit`；迁移 00020；`verify_audit.py` 69/69 PASS（总进度汇报） | |
| TDD §7 | 不可恢复操作必审计 | ✅ | media.purge/share.*/admin.user.* 已接（进度表 §四.4） | |
| — | 审计 actor 归因 | ✅ | 迁移 00025/00026（FK SET NULL + actor_email 快照，Job000022） | |
| TDD §7 | 审计事务性 | 🟡 | Recorder 独立连接池 | 已知架构上限（进度表风险表：`share.revoke` 存在丢失窗口，已定取舍「不写假记录」） |
| 进度表 A3 | 只读取证三分类（15 动作哪些真有调用点） | ❌ 未开始 | — | 计划内待办 |

### 14. 工具箱（PRD §6.16 / API §3）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| PRD §6.16 / API §3 | 重复项目（pHash 去重，只读建议） | ✅ | `GET /media/duplicates` + phash-worker；阈值 T=10 实测标定；回归资产含「证明测试真能抓住」 | 已知限制三条（O(N²)>5000 拒绝/低纹理/视频首帧）均在契约内如实记录 |
| PRD §6.16 | 最近删除（回收站复用） | ✅ | `GET /media/trash` | |
| PRD §6.16 | 已恢复（本人恢复历史） | ✅ | `GET /media/restore-history`（8706394 后新增，契约 §3 已同步收录） | 不写 audit-read 的取舍有反向自证测试 |
| 进度表 A1 | **前端工具箱页** | ✅（8706394 后完成） | `ToolboxView.vue`（19.7KB），nav「工具箱」ready:true | 基线进度表列「未做」，HEAD 已补上——进度表口径滞后于 HEAD |
| Phase 5 概要 | 去重合并入口 | 🟡 | 去重为只读建议，删除走软删确认 | 「合并」语义未做（进度表：工具箱去重合并入口） |

### 15. 转码与算力节点（TDD §6.1 / API §12、§15 / 计划 T6.2）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| API §12 | `POST /transcode/job`（thumbnail\|hls\|memories + profile） | ✅ | `main.go:185`；另有 `GET /transcode/job/:id` 状态（契约外补充） | kind=memories 仅枚举存在，无生成逻辑（见回忆影片行） |
| API §12 | `GET /transcode/hls/:id/master.m3u8` 多码率 | ✅ | `main.go:187` `/transcode/hls/:id/*file`（鉴权+防穿越） | |
| API §12 | `GET /ai/jobs/:id` | ⤳ | 无该路径；`GET /admin/jobs/:id` 等价覆盖 | 路径漂移 |
| API §15 | compute-nodes CRUD（admin:system） | ✅ | `main.go:336-340` | |
| TDD §6.1 | 节点 agent：心跳/任务拉取/断线重连/离线重派/认领上限 | ✅ | `cmd/nodeagent` + agent 平面三端点（`main.go:343-346`）；Job000015–019 全证据链（双认证平面/心跳解耦/真实 ffmpeg/NVENC 回退/attempts 上限） | 离线阈值 180s=TDD 60s×3 |
| TDD §6.1 | Win/Linux/macOS 三平台 agent | 🔶 | nodeagent 单二进制 | 三平台构建/实测矩阵未见记录 |
| API §15 | 带宽：self-test / GET / PATCH（manual 优先） | ✅ | `main.go:213-217`（另有 probe 探针两端点） | |
| 计划 | hls 任务切换节点执行 | ⏸ 挂起 | 控制端已具备，队列消费并存冲突有防护注释 | **用户决定挂起**（进度表）——非缺陷 |

### 16. AI 能力（Phase 4 / TDD §3）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| Phase 4 | CLIP 语义向量（embedding） | ✅ | embedgen + embed-worker 清扫；chinese-clip/clip 双族、CPU/CUDA 双接口 | 模型缺失降级不阻断启动（`main.go:434`） |
| Phase 4 | 零样本打标 | ✅ | taggen + tag-worker；标签向量 PG 持久缓存（启动 110s→数秒） | 阈值经真实库标定 |
| Phase 4 | 人脸检测/聚类 | ✅ | 见模块 7 | 召回优化为已知待办 |
| NFR-隐私 | AI 推理可纯本地 | ✅ | 全部本地 ONNX Runtime，无外部调用 | |
| API §12 | `POST /ai/faces`、`POST /ai/tags` 主动触发 | ✅ | `main.go:168,177`（复位扫描标记=入队等价） | 响应非契约 `{job_id}` 形状（清扫式架构无 job）——形状偏差 |

### 17. 双空间（PRD §6.3 / API §4）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| API §4 | `GET /spaces` | ✅ | `main.go:181` | |
| API §4 | `POST /spaces/shared` 建共享空间 | ❌ | 无（grep `spaces/shared` 0 命中） | 进度表 B2：`space_id` 建模待拍板；现阶段 shared 为单枚举 |
| API §4 | `POST/DELETE /spaces/shared/:id/members` | ❌ | 无 | 同上；`shared_space_members` 表已在 DDL |
| PRD §6.3 验收 | 双空间隔离 | ✅ | `internal/media/scope.go` `scopeConds` 单一真源；60/0/1 对抗验证 | |

### 18. 移动端备份 / PWA（PRD §6.13）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| PRD §6.13 | PWA（可安装、离线壳） | ✅ | `public/manifest.webmanifest` + `public/sw.js`（外壳预缓存/SWR/NetworkOnly 策略） | |
| PRD §6.13 | 移动端**自动**备份 | 🟡 | `components/upload/backupManager.js`（IndexedDB + 前台增量备份） | 实现方明示「不是后台自动备份」（PWA 平台限制），真自动备份指向桌面 Agent/原生 App——**与 PRD「自动备份」验收存在形态缺口，但取舍已在码内声明** |

### 19. 迁移工具与运维（Phase 6：T6.1/T6.3/T6.4）

| 需求/计划条目 | 要求内容 | 状态 | 实现位置 | 备注 |
|---|---|---|---|---|
| 计划 T6.1 | 迁移工具：rsync + inotify + `@eaDir` sidecar 处理 | ✅ | `internal/watch/`（watcher/rsync/debounce/state + 测试）+ `cmd/watchctl`；`@eaDir` 跳过有测试钉住 | TDD §8.8 |
| 计划 T6.3 | 运维可观测：备份/巡检/健康检查 | ✅ | `scripts/backup.sh`、`restore.sh`、`ops_check.sh`、RESTORE.md；Job000028：cron 备份+巡检 | |
| 计划 T6.3 / NFR | 监控 Prometheus+Grafana、日志 Loki、告警 | 🟡 | 仅 `/metrics` 端点暴露 | **缺口**：compose 无 Grafana/Loki/Prometheus 服务；告警（节点离线/队列积压/证书过期）未实现；CI 有 `.github/workflows/ci.yml`，灰度部署无 |
| 计划 T6.4 | 目标平台部署验证（示例环境联调+双系统并行迁移期） | 🟡 | `部署方案与兼容性_v1.0.md`（Job000025–027）+ `scripts/install-standalone.sh` + `deploy/systemd/` + HTTPS 上线（Job000029） | **验收缺口**：x86_64 有/无 GPU 两档真机矩阵未验（进度表 C1）；arm64 不承诺；install-standalone 未真跑（Job000039 未开始） |

### 20. P2 已知未做（计划内，非缺陷）

| 条目 | 状态 | 说明 |
|---|---|---|
| SSO/OIDC（API §2、Phase 5、PRD §10 P2） | ❌ 未实现-计划内 | 进度表 B5 待拍板 |
| WebDAV（API §16 七章端点） | ❌ 未实现-计划内 | grep `webdav` 0 命中；进度表 B6 待确认是否仍在 P2 |
| 回忆影片 Memories（PRD §6.18、API §12 kind=memories） | ❌ 未实现-计划内 | 仅 DDL 表 + 枚举值；无任何生成逻辑/端点 |
| 3D 地图增强（PRD §5 P2） | ❌ 未实现-计划内 | 见模块 4 |
| WS /media/:id/360/state（API §13 可选） | ❌ 未实现-计划内（可选） | |
| /search/video-moment（API §10） | ❌ 未实现 | 代码明示「本期不实现」；契约未标阶段，建议契约补注 |

---

## 二、计划外蔓延清单

> 代码存在但计划/PRD/TDD/API 四文档均无依据者。**本轮未发现恶意或无序蔓延**；以下均为「有 Job 台账依据但契约未同步」（归第三类文档失信处理）或「开发工具类」：

| # | 项 | 性质 | 处置建议 |
|---|---|---|---|
| 1 | `cmd/genmedia`（测试媒体生成）、`cmd/inject360`（360 样片注入）、`cmd/migrateplan`（迁移规划） | 开发/测试工具，四文档无依据 | 正当工具，建议在 README 工具清单登记 |
| 2 | `GET /media/date-histogram`、`PATCH /media/:id`（notes）、`GET /geo/places`、`GET/PUT /preferences/map`、`GET /public/shares/:token/og`、`GET /transcode/job/:id`、标签管理 CRUD（PATCH/DELETE/GET media）、`POST /media/:id/tags/confirm`、分享 `max_views` | 有 Job000005/000009/000010/000013 台账依据，**契约未收录** | 移入「文档失信」处理：契约 v1.2 补录 |
| 3 | `internal/mediascope`、`internal/httperr`、`internal/cursor`、`internal/pgxutil` 四个「单一真源」包 | 计划外架构治理产物（Job000020–031），无预先生效的计划条目 | 属质量治理，建议在 TDD 增补「单一真源」原则条目使其有据 |

## 三、文档失信清单

| # | 失信点 | 文档说 | 代码实况 |
|---|---|---|---|
| 1 | 契约 §3 `POST /media/index` | 有此端点 | 未注册（索引走 watchctl/上传自动）；两处 grep 0 命中 |
| 2 | 契约 §7 路径族 `/map/items`、`/map/timeline` | /map/* | 实现为 `/geo/clusters`、`/geo/items`、`/geo/histogram`；契约未同步 |
| 3 | 契约 §11 路径族 `/share/album`、`/share/media`、`GET /s/:token` | /share/*、/s/* | 实现为 `/shares`、`/public/shares/:token`；契约未同步 |
| 4 | 契约 §2 `POST /auth/2fa`（temp_token 两段式） | 有 | 实现为 login 内联 `totp_code` + `/auth/mfa/*`；无 `/auth/2fa` |
| 5 | 契约 §11 `POST /s/:token/unlock` + `?pwd=` | 有 | 无 unlock 端点；实现参数名 `?password=`（与契约 `pwd` 不一致） |
| 6 | 契约 §5 `GET /albums/:id/items`、§12 `GET /ai/jobs/:id` | 有 | 分别由 `GET /albums/:id` 内联 items、`GET /admin/jobs/:id` 等价覆盖；契约未标注 |
| 7 | 契约遗漏已实现端点 | 无记载 | `/media/date-histogram`、`PATCH /media/:id`、`/geo/places`、`/preferences/map`、`/og`、标签管理 CRUD、`/transcode/job/:id`、分享 max_views（详见二.2） |
| 8 | 计划 T0.2/T1.2「DDL 30 表」 | 30 表 | DDL 实际 28 个 `CREATE TABLE`，迁移 26 版已全覆盖——计划口径错误 |
| 9 | 前端 nav 状态 | AppShell 标注相册/文件夹「开发中」 | `AlbumsView`/`AlbumDetailView`/`FoldersView` 均已实现且路由已注册；nav `ready:false` 未更新 |
| 10 | 契约 §3 旋转 `op:rotate\|crop\|auto` | 三操作 | 仅 rotate 实现；契约未标注范围收窄 |
| 11 | 进度表（0917 基线）「前端工具箱页未做」 | 未做（A1） | HEAD 61838f8 已交付 `ToolboxView.vue` + nav 入口——基线滞后于 HEAD（本项为基线口径问题，已注明） |
| 12 | 契约 §10 `/search/video-moment` | 有（未标阶段） | 代码明示「本期不实现」（`search/types.go:8`）；契约应补「暂不实现」注记 |

## 四、验收证据缺口清单

| # | 条目 | 代码在 | 缺什么证据 |
|---|---|---|---|
| 1 | 10 万级时间轴分页 <1s / 搜索 <1s / 首屏 <2s（T1.5 AC、NFR） | ✅ | 库仅 93 条，无规模语料与压测记录（进度表风险表自承） |
| 2 | 360 AC-11/12/13：iOS/Android 陀螺仪、弱网降档、VR 头显真机 | ✅ | 无真机验收记录（进度表 ⌗；8K60 亦待真机） |
| 3 | 微信实机分享卡片+点开播放（PRD §6.14 验收） | ✅ | 进度表 C2：待用户实机 |
| 4 | 目标平台性能矩阵（x86_64 有 GPU/无 GPU 两档；T6.4） | ✅ | 进度表 C1 未做；install-standalone.sh + systemd 未真跑（Job000039） |
| 5 | 4K 360 起播 <3s（NFR） | ✅ | 缺 ≥4K 源无法验证 |
| 6 | 节点 agent 三平台（Win/Linux/macOS）构建与运行 | ✅ | 无三平台实测矩阵 |
| 7 | 人脸召回（PRD §6.5「自动聚合准确」） | ✅ | 已知不足（Job000041 多尺度卡在 ORT 环境）；现仅有零误检证据，召回侧无量化证据 |
| 8 | 队列死信/积压健康（NFR 告警） | ✅ | `media` failed=8 / `transcode` failed=11 已发现未排查（Job000035 未开始） |
| 9 | PWA 备份链路端到端（选择→上传→入库） | ✅ | 无独立 e2e 记录（前端无单测，依赖无头门禁 52/52 但未覆盖备份路径的公开记录） |
| 10 | 多用户并发/规模压力（NFR 可伸缩） | ✅ | 全部验证在 4–6 账号、93 条媒体规模（进度表风险表） |

## 五、符合性总结论

**覆盖率数字**（按 API 契约 17 章约 97 条端点/策略逐条核对）：
- ✅/⤳ 已实现（含形状偏差但功能等价）：约 **79 条 ≈ 81%**
- 其中 ⤳ 形状/路径漂移但功能等价：约 12 条
- ❌ 未实现-计划内（P2/可选/已声明不做）：约 11 条（WebDAV 7、SSO 1、WS 1、video-moment 1、3D 地图等）
- ❌ 未实现-计划外或占位落空：约 7 条（/media/index、/spaces/shared×3、/admin/index/rebuild、/admin/settings×2、公开下载占位 403、登录/分享专属限流 2 项、live_photo 配对逻辑）
- 计划 v1.0 任务维度：Phase 0/1/2/3 全绿；Phase 4 主体完成（缺共享相册类型、crop/auto 编辑）；Phase 5 审计+2FA+管理端点落地（缺 SSO、管理后台**前端页面**）；Phase 6 仅 T6.2 完成，T6.1 提前完成，T6.3 部分、T6.4 未真机验收。

**三大差距**：
1. **契约与实现的系统性路径漂移**：`/map/*↔/geo/*`、`/share/*↔/shares+/public/shares/*`、`/auth/2fa↔mfa/*`、`POST /media/index` 缺席等 12 处——功能大多等价，但 API v1.1 已不能作为客户端开发的真源，亟需契约 v1.2 全面回写（含补录 9 个未收录端点）。
2. **PRD 页面形态缺口**：管理后台（§6.15）**零前端 UI**（15 个 view 无 AdminView，前端 grep `/admin/` 0 命中），后端管理端点只能脚本触达；相册/文件夹页已做但 nav 仍标「开发中」——页面层「有而无入口」与「无而有契约」并存。
3. **验收证据集中在功能正确性，性能/真机/规模三类证据系统性缺失**：10 万级性能、360 真机、微信实机、目标平台矩阵、三平台 agent、多用户并发共 10 项无任何实测记录；现库 93 条媒体的验证规模与 NFR 承诺（10 万级）差三个数量级。

**无计划外恶性蔓延**：未发现四文档均无依据的业务功能；所有「计划外」代码均有 Job 台账或治理记录背书，问题集中在契约未同步而非无序扩张。
