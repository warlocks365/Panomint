# API 详细契约 (v1.1)


# 全景相册系统 · API 详细契约（OpenAPI 风格）

> 版本：v1.1 ｜ 日期：2026-08-24\ 配套：PRD v3.1 / TDD v1.1 / 数据库 DDL v1.1\ Base URL：`https://<domain>/api`\ 认证：**独立后台账户**，Bearer JWT（不对接 DSM）；SSO 走 OIDC


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
- 请求：`{ "email": "u@x.com", "password": "***" }`

- 响应：`{ "access_token", "refresh_token", "mfa_required": false }`

- 若账户启用 2FA：`mfa_required=true`，需再调用 `/auth/2fa`。


### POST /auth/2fa

TOTP 验证（启用 2FA 时）。
- 请求：`{ "temp_token": "...", "code": "123456" }`

- 响应：`{ "access_token", "refresh_token" }`


### POST /auth/refresh

刷新令牌：`{ "refresh_token" }` → `{ "access_token" }`

### POST /auth/sso/oidc

OIDC 回调：`{ "code", "state" }` → `{ "access_token", "refresh_token" }`

### POST /auth/logout

吊销当前会话（写 `sessions.revoked`）。

### GET /auth/me

返回当前用户：`{ "id","email","display_name","role","mfa_enabled" }`

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

- 响应：`{ "items": [MediaRef...], "next_cursor", "total", "buckets": [{"key":"2026-08","count":N}] }`


### GET /media/:id

详情 + 元数据：`{ id, type, path, taken_at, width, height, codec, gps, place, is_360, projection, thumbnail_*, hls_master, tags[], people[], albums[], exif: { camera_make, camera_model, lens_model, focal_length, aperture, iso, shutter_speed, exposure_bias }, video: { fps, bitrate, duration, hdr, color_space }, rating, live_photo_pair_id }`

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

### POST /media/index

触发索引。
- 请求：`{ "kind": "full|incremental", "root": "/volume1/photo" }`

- 响应：`{ "job_id" }` → 查 `/admin/jobs/:id`


### POST /media/:id/rotate

基本编辑：旋转/裁剪/自动增强（非破坏，sidecar）。
- 请求：`{ "op":"rotate|crop|auto", "angle"?:90, "rect"?:{...} }`


### POST /media/:id/favorite

收藏切换：`{ "favorite": true }`

### DELETE /media/:id

软删（入回收站）。

### GET /media/trash

回收站列表；`POST /media/trash/:id/restore` 恢复；`DELETE /media/trash/:id` 永久删除。

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

### GET /albums/:id/items

相册内媒体（分页）。

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


### GET /map/items

可视区媒体聚合点（支持时间窗 + 模糊筛选）：`?bbox=minlon,minlat,maxlon,maxlat&zoom=6&time_from=&time_to=&type=&person=&tag=&q=`\ → `{ "type":"FeatureCollection", "features":[ { "cluster":true,"center":[lon,lat],"count":N,"thumbnails":[...] } | { "cluster":false,"id","gps","thumb","taken_at" } ] }`

### GET /map/timeline

当前筛选 + bbox 的时间跨度直方图：`?bbox=&filter=` → `{ "min":"2019-01","max":"2026-08","buckets":[{ "bucket":"2024-07","count":N }] }`

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

用户标签：`{ "name" }` → `{ "id" }`

### POST /media/:id/tags

给媒体打标签：`{ "tag_ids":[...] }`

### POST /tags/:id/confirm

确认 AI 自动标签：`{ "confirmed":true }`

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


### GET /search/video-moment

视频瞬间定位：`{ "q":"猫出现", "media_id" }` → 帧时间戳列表（帧级索引）。

---


## 11. 分享 / 微信 H5


### POST /share/album

生成相册链接：`{ "album_id","expire_at"?,"password"?,"allow_download":false,"is_wechat":true }` → `{ "token","url":"/s/<token>" }`

### POST /share/media

生成单媒体链接（参数同上，`media_id`）。

### GET /share

我的分享列表（分页）。

### DELETE /share/:id

撤销。

### GET /s/:token  （访客免登录 H5）

渲染轻量 H5 页面（不加载后台）；360 视频走 WebXR+HLS。支持 `?pwd=` 或先 `POST /s/:token/unlock` 校验密码。

### POST /s/:token/unlock

`{ "password" }` → `{ "ok":true }`（设置 cookie 会话）。

### GET /s/:token/playlist.m3u8

返回该分享媒体 HLS（限分享内媒体；受密码/有效期约束）。



### POST /s/:token/bandwidth-test

分享场景专用带宽自测（访客无 JWT，以 token 鉴权）。
- 请求：无 body（服务端上传/下载探针）

- 响应：`{ "up_kbps", "down_kbps", "latency_ms" }` → 写入 `bandwidth_tests`（scope=share\_token, source=self\_test）

- 360 播放页据此在 HLS 多档中选取初始档。


---


## 12. AI / 转码


### POST /ai/faces

触发人脸检测/聚类（通常索引时自动，此为主动重算）：`{ "scope":"all|media_id" }` → `{ "job_id" }`

### POST /ai/tags

触发自动标签：`{ "scope":"all|media_id" }` → `{ "job_id" }`

### GET /ai/jobs/:id

任务状态。

### POST /transcode/job

提交转码：`{ "media_id","kind":"thumbnail|hls|memories","profile":"1080p|2k|4k" }` → `{ "job_id" }`

### GET /transcode/hls/:id/master.m3u8

返回 HLS 清单（自适应码率；边缘缓存）。

### GET /admin/jobs

索引/转码任务列表与进度（查 `index_jobs`/`transcode_jobs`）。

---


## 13. 360 播放（自研核心）


### GET /media/:id/360

返回 360 播放元数据：`{ "projection":"equirectangular", "hls_master":"/transcode/hls/:id/master.m3u8", "width","height","gyro_supported":true, "vr_supported":true }`

### WS /media/:id/360/state  （可选）

同步多端视角（如投屏到头显时手机作控制器）。
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

文档结束（API v1.1）。与《技术设计文档.md》《数据库 DDL.md》共同构成实现基线。商业化许可证合规矩阵见 PRD §12.2。
