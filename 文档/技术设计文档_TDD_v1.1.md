# 技术设计文档 (TDD v1.1)


# 全景相册系统 · 技术设计文档（TDD v1.0）

> 版本：v1.1 ｜ 状态：草案 ｜ 日期：2026-08-24\ 配套：PRD v3.1（群晖 Synology Photos 优先 / PhotoPrism 补位 / 360 视频自研）\ 范围：系统架构、服务模块、核心流程、360 播放引擎、安全、伸缩、运维可观测性、技术选型\ 关键决策：账户与权限采用**独立后台管理模式**（不对接 DSM）


---


## 1. 概述与追溯

本系统为 **100% 自研**的自托管相册系统，目标是作为 **Synology Photos 的增强替代品**，并补齐群晖缺失的 **360° 全景视频交互播放（陀螺仪/VR 头追/自适应码率/微信 H5 分享）**。
| 需求文档章节 | 本设计对应 |
| --- | --- |
| §1 产品概述 / §1.4 对照表 | §2 架构、§3 模块 |
| §4 总体架构 | §2 架构、§6 算力分离 |
| §5 模块总览 P0–P2 | §5 里程碑 |
| §6 前端页面 | §4 核心流程（前端调用后端） |
| §7 API 契约摘要 | 《API 详细契约.md》 |
| §8 数据模型 | 《数据库 DDL.md》 |
| §11 风险 | §9 风险与对策 |

**设计原则**：存储与算力分离；成熟库拼装 + 仅 360 引擎自研；一切外部访问 HTTPS；账户/权限独立后台。

---


## 2. 系统架构


### 2.1 部署拓扑（ASCII）

```
┌─────────────────────────────────────┐
     手机/平板/PAD         │            公网 (HTTPS)              │
     微信内置浏览器  ──────▶│   Caddy 反代 + WAF + 自动证书        │
     VR 头显  ───────────▶│   (WebXR/DeviceOrientation 强制 HTTPS)│
     浏览器 / PWA  ───────▶│        /api/*   /s/<token>(H5)        │
                           └───────────────┬─────────────────────┘
                                           │
                      ┌────────────────────┼────────────────────┐
                      ▼                    ▼                    ▼
              ┌──────────────┐   ┌──────────────┐    ┌──────────────┐
              │ API 网关/媒体 │   │  AI 推理服务  │    │ 转码管线节点  │
              │  (Go)        │   │ (FastAPI)    │    │ (ffmpeg+GPU) │
              │ 鉴权/RBAC     │   │ 人脸/标签/地图│    │ 缩略图/HLS    │
              │ 媒体库/相册    │   └──────┬───────┘    └──────┬───────┘
              │ 360 播放WebXR │          │ pgvector          │
              └──────┬───────┘          │                   │
                     │                  ▼                   │
              ┌──────┴───────┐   ┌──────────────┐   ┌──────┴───────┐
              │ PostgreSQL   │   │ 对象存储/NAS  │   │ 消息队列      │
              │ + pgvector   │   │(MinIO/S3)¹   │   │(BullMQ/Valkey)²│
              │ 元数据/向量   │   │ 原文件+缩略图 │   │ 索引/转码任务 │
              └──────────────┘   └──────────────┘   └──────────────┘
                     ▲                                         
                     │                                         
              ┌──────┴───────┐                                 
              │ DS1819+ NAS   │  仅做文件系统/对象存储（Atom C3538 无 GPU）
              │ (存储卷)      │  不参与 AI/转码
              └──────────────┘
```

> ¹ **MinIO**（AGPL v3）作为独立 S3 兼容服务通过 API 调用，不构成对应用代码的 copyleft 传染（不修改/不分发）。若需完全规避 AGPL，可替换为 **SeaweedFS**（Apache 2.0）。\ ² **Valkey**（BSD-3-Clause）是 Redis 的 Linux 基金会 fork，用于替代 Redis（2024 起改 RSALv2/SSPL，非 OSI 认证开源），BullMQ 完全兼容。


### 2.2 分层职责

| 层 | 组件 | 职责 |
| --- | --- | --- |
| 客户端 | Web/PWA、移动端 PWA、VR 头显、微信 H5 | 浏览/上传/360 交互/分享 |
| 边缘 | Caddy 反代 + CDN | HTTPS、ABR 边缘缓存、WAF、限流 |
| 应用 | API 网关（Go） | 鉴权/RBAC、媒体库、相册、人物、地点、分享、360 播放页渲染 |
| 地图/地理 | API 网关（Go）+ 瓦片代理 | 空间聚合(supercluster)、时间轴直方图、模糊筛选、地理搜索、中外国界切换、GCJ-02 转换 |
| AI | FastAPI 微服务 | 人脸检测/聚类、自动标签、地理编码、Clean Up |
| 转码 | ffmpeg + GPU 节点 | 缩略图、HLS 多码率、回忆影片混剪 |
| 存储 | PostgreSQL+pgvector / 对象存储 / NAS | 元数据、向量、原文件、缩略图 |


---


## 3. 服务模块设计


### 3.1 媒体索引服务（Go）






### 3.2 AI 推理服务（Python FastAPI）

- 人脸：OpenCV Zoo `YuNet`（检测）+ `SFace`（识别，128 维特征，Apache-2.0）→ pgvector 近邻聚类生成 `cluster_id` → 映射 `people`。

- 宠物：复用同类模型 `is_pet=true`。

- 自动标签：`media_tags` + `tags(kind=ai)`（分类/检测模型）；需人工确认 `confirmed`。

- 地理编码：国际用 **Nominatim** 反编码；中国用 **高德(Amap) 地理编码**（API key 可配置，见 DDL `system_map_config`）。照片 GPS 多为 **WGS-84**，叠加高德底图需做 **WGS-84 ↔ GCJ-02** 转换（GCJ-02 偏移）后再写 `media.place`/显示；结果缓存于 `geo_cache`。

- 所有模型可纯本地运行（无外发）。


### 3.3 转码管线（ffmpeg + GPU 节点）

- 缩略图：多尺寸（SM/MD/LG）WebP。



- 回忆影片：ffmpeg 拼接 + 转场滤镜（可选模块）。


### 3.4 360 播放引擎（自研，核心差异）

详见 §4.2。Three.js 球面渲染 + DeviceOrientation 陀螺仪 + WebXR `immersive-vr` + hls.js 自适应（档位由 `/api/bandwidth` 测得的上下行带宽推荐，见 §6 算力节点与 API §16）。

### 3.5 鉴权与权限（独立后台）

- JWT（Access + Refresh）；密码 bcrypt；可选 SSO(OIDC) 经 Keycloak；2FA 经 `otplib`(TOTP)。

- RBAC：`users.role_id` → `roles` → `role_permissions`；共享空间成员经 `shared_space_members.role`。

- 会话入 `sessions` 可监控/吊销；审计写 `audit_log`。



### 3.6 分享与微信 H5

- `share_links` 生成 token；`/s/<token>` 由反代渲染轻量 H5（不加载后台）；支持有效期/密码/下载开关。

- 微信内：复制链接 + OG 封面图；360 视频以 H5 + HLS 播放（非整文件下载）。


### 3.7 地图与地理模块（地图模式核心）







- 详见 API §7（地图模式）、DDL §2.5（`user_ui_prefs`/`system_map_config`/`geo_cache`）。


---


## 4. 核心流程


### 4.1 导入流水线

```
SMB/App/WebDAV 写入 → 监听事件/定时扫描
    → 提取元数据(exif/ffprobe) + 360 判定
    → 写 media(space, folder_path, hash 去重)
    → 入队: [缩略图生成] [AI:人脸/标签/地理]
    → 索引进度更新(index_jobs)
    → 完成 → 时间轴/相册可见
```


### 4.2 360 视频播放流程（自研核心）

```
H5 打开 /s/<token> 或 播放页
    → 加载 hls.js → 拉 master.m3u8 → LEVEL_SWITCHED 自适应码率
    → Three.js 内翻 SphereGeometry + VideoTexture(等距圆柱帧)
    → 控制:
        拖拽/触摸 → yaw/pitch 视角
        双指捏合 → FOV 缩放
        DeviceOrientation(需 iOS 授权) → 陀螺仪跟随倾斜
        WebXR requestSession('immersive-vr') → VR 头显左右分屏+头追
    → 全部交互强制 HTTPS(微信/头显要求)
```

**360 引擎详细设计补充**：
| 子系统 | 设计要点 |
| --- | --- |
| **球面渲染（WebGL Shader）** | `SphereGeometry(radius=500, widthSeg=64, heightSeg=32)` 内翻（`scale.z = -1`）；自定义 `ShaderMaterial` 翻转 UV.y（`vec2(1.0, uv.y)` 适配 equirectangular 帧）；`VideoTexture` 设 `flipY=false`；球心相机 `fov=75°, near=0.1, far=1000` |
| **iOS DeviceOrientation 权限** | iOS 13+ 需用户手势触发 `DeviceOrientationEvent.requestPermission()`；播放页加"开启陀螺仪"按钮，点击后请求权限并绑定 `deviceorientation` 事件；权限拒绝后降级为纯拖拽模式 |
| **WebXR 会话生命周期** | `navigator.xr.isSessionSupported('immersive-vr')` 检测 → 用户点击"VR 模式"按钮 → `requestSession('immersive-vr')` → `session.requestAnimationFrame(renderLoop)` → 退出时 `session.end()`；`sessionend` 事件清理资源并恢复标准渲染；会话中断（电话/切换App）自动暂停视频 |
| **hls.js 错误恢复** | `Hls.Events.ERROR` 监听；`fatal=true` 时：网络错误→指数退避重试（3次）；媒体解析错误→降级到下一档或恢复到最低档；不可恢复→显示"请检查网络"提示并保留最后帧 |
| **移动端性能优化** | 纹理尺寸上限：移动端 `maxTextureSize=4096`（4K equirect 需分块或降采样）；帧率自适应：`requestAnimationFrame` + 性能监测，低于30fps时自动降档；`powerPreference='high-performance'`；VR 模式锁定 `75fps` |
| **视角同步（多端）** | 可选 WebSocket `/media/:id/360/state` 同步 yaw/pitch/timestamp，用于投屏到头显时手机作控制器 |


### 4.3 分享流程

```
选相册/媒体 → POST /api/share → 生成 token + 可选密码/有效期
    → 返回 /s/<token>
    → 微信: 复制链接 + 右上角菜单分享 + OG 封面
    → 访客打开 H5 → 免登录观看(360 走 4.2)
    → 统计写 share_access_log
```


### 4.4 鉴权流程

```
登录 → POST /api/auth/login → JWT(Access+Refresh)
    → 后续请求 Authorization: Bearer <JWT>
    → 网关校验 + RBAC 判定(角色/共享空间成员)
    → 2FA 首次/敏感操作需 TOTP
    → SSO: OIDC 回调换取 JWT
```


---


## 5. 里程碑（与 PRD §5/§10 对齐）

| 阶段 | 交付 | 验收 |
| --- | --- | --- |
| P0 | 时间轴+年日月+索引+缩略图+双空间+文件夹视图 | 群晖级浏览 |
| P0 | ★ 360 播放引擎（陀螺仪/VR/ABR） | iOS/Android/Quest 可交互 |
| P1 | 相册/人物/地点/标签/文件夹/查看器/幻灯片/基本编辑 | 自动聚合准确 |
| P1 | HLS 转码/搜索/微信H5/移动端备份/360照片 | 编辑可用、远程自适应 |
| P2 | 多用户/SSO/2FA/审计/3D地图/工具箱去重/回忆影片 | 企业级权限 |


---


## 6. 存储与算力分离（关键约束）


- 
- 
- 6.1 算力节点 Agent 协议
- 
- `lan_agent` 类型节点（本地网络第三方 GPU 主机）通过 agent 守护进程接入，本地 GPU / 云 GPU 也可使用 agent 统一管理。
- 
- 协议要素
- 
- 设计
- 
- 
- WebSocket（wss://）长连接；备选 gRPC 双向流
- 
- 
- 每 60s 发送 `{type:"heartbeat", gpu_util, vram_used, active_tasks}`；3 次未收到 → 标记 offline → 重派其队列任务
- 
- 
- 首次连接：`POST /api/compute-nodes` 获取 `agent_token` → 连接时 `Authorization: Bearer <agent_token>`
- 
- 
- 注册时上报 `{codecs:["h264","hevc"], has_nvenc:true, vram_mb:24576, concurrency:2, cuda_cores, tensor_cores}`
- 
- 
- agent 发送 `{type:"poll"}` → 服务端返回 `[{job_id, kind, media_id, profile, input_path, output_spec}]` 或空
- 
- 
- 任务完成：\`{type:"result", job\_id, status:"done
- 
- 
- 指数退避：1s→2s→4s→8s→30s 上限；重连后重新注册能力 + 拉取未完成任务
- 
- 
- wss 强制 TLS；agent\_token 轮换（管理员触发）；内网可配 IP 白名单
- 
- 
- agent 二进制覆盖 Windows（.exe）/Linux（ELF）/macOS（Mach-O）；配置文件 `agent.yaml`（node\_name, server\_url, token）
- 
- 
- 7. 安全设计
- 
- 项
- 
- 方案
- 
- 传输
- 
- 全链路 HTTPS；**支持自定义自用域名 + IP 直接访问 + 非标端口 + 路由器端口转发映射**（Caddy 监听非标端口、透传 `X-Forwarded-*`；分享基址 `external_base_url` 可配置含端口）
- 
- 认证
- 
- JWT + Refresh；bcrypt 口令；TOTP 2FA；OIDC SSO
- 
- 授权
- 
- RBAC（角色/权限表）+ 共享空间成员角色
- 
- 限额
- 
- Redis 令牌桶（登录/IP/API）
- 
- 会话
- 
- `sessions` 可监控/吊销
- 
- 审计
- 
- `audit_log`（用户/动作/时间/IP）
- 
- 隐私
- 
- AI 推理可纯本地；数据不出域
- 
- 
- 8. 运维与可观测性
- 
- 8.1 监控与告警
- 
- 8.2 日志体系
- 
- 日志类型
- 
- 格式
- 
- 存储
- 
- 保留
- 
- 应用日志
- 
- 结构化 JSON（`level, msg, request_id, user_id, module, latency`）
- 
- 文件 + Loki
- 
- 30 天
- 
- 访问日志
- 
- Caddy `access_log`（含 IP, UA, path, status, latency）
- 
- 文件 + Loki
- 
- 30 天
- 
- 审计日志
- 
- `audit_log` 表（DDL §2）
- 
- PostgreSQL
- 
- 永久
- 
- 错误日志
- 
- Sentry（可选 SaaS 或 self-hosted GlitchTip）
- 
- Sentry/GlitchTip
- 
- 90 天
- 
- 8.3 备份与恢复
- 
- 备份对象
- 
- 方案
- 
- 频率
- 
- 保留
- 
- PostgreSQL
- 
- `pg_dump` + WAL 归档（PITR）
- 
- 全量日1 + WAL 实时
- 
- 30 天
- 
- 对象存储（原文件/缩略图）
- 
- MinIO 版本化 / S3 生命周期
- 
- 实时版本
- 
- 90 天
- 
- 系统配置
- 
- `dsm config export` + `docker-compose.yml` + `.env`
- 
- 变更即备份
- 
- 永久
- 
- DDL Schema
- 
- `pg_dump --schema-only`
- 
- 每次迁移前
- 
- 永久
- 
- 8.4 CI/CD 流水线
- 
- ```
- Git Push → GitHub Actions / GitLab CI
- → lint(go vet, golangci-lint, eslint) + test(go test, pytest, vitest)
- → build(docker build, multi-arch: amd64 + arm64)
- → scan(Trivy 镜像漏洞扫描)
- → push(registry)
- → deploy-staging(自动) → deploy-prod(手动审批, 灰度)
- ```
- 
- 8.5 健康检查与就绪探针
- 
- 端点
- 
- 用途
- 
- 检查内容
- 
- `GET /health`
- 
- liveness
- 
- 进程存活（返回 `200`）
- 
- `GET /ready`
- 
- readiness
- 
- PG 连通 + Redis 连通 + 磁盘可写（全通才返回 `200`）
- 
- `GET /metrics`
- 
- Prometheus
- 
- 指标暴露
- 
- 8.6 配置与密钥管理
- 
- 配置层
- 
- 方案
- 
- 环境变量
- 
- `.env` 文件（开发）/ Docker Compose `environment`（生产）
- 
- 敏感密钥
- 
- SOPS + age 加密 `.env.sops` → 运行时解密注入
- 
- 高德 API key
- 
- 存 `system_map_config.china_api_key_enc`（应用层 AES 加密）
- 
- JWT 签名密钥
- 
- 环境变量 `JWT_SECRET`，轮换周期 90 天
- 
- agent\_token
- 
- `compute_nodes.agent_token`，管理员触发轮换
- 
- 8.7 CDN 与边缘缓存
- 
- 8.8 数据迁移工具（从群晖 Photos 迁移）
- 
- ```
- DS1819+ Synology Photos 导出
- → 扫描 /volume1/photo/ + /volume1/photo/@eaDir/（缩略图/sidecar）
- → 保留 EXIF + 原始目录结构（folder_path 映射）
- → 读取 Synology sidecar（.info, .extoolkit）提取人物/标签/收藏
- → 映射到本系统 media/people/tags/albums
- → 增量同步：rsync + inotify 监听新文件
- → 进度可视化：迁移进度条 + 冲突处理（重复/路径冲突）
- ```
- 
- 
- 9. 性能与伸缩
- 
- 
- 10. 风险与对策
- 
- 风险
- 
- 对策
- 
- NAS 无 GPU
- 
- 算力分离（§6）
- 
- 微信/WebXR 强制 HTTPS
- 
- Caddy 证书 + 公网域名（支持非标端口/端口转发/IP 直连）
- 
- 大库索引耗时
- 
- 增量 + 低峰 + 进度可视化
- 
- 360 元数据缺失
- 
- 导入补全 equirectangular
- 
- SMB 写入与索引冲突
- 
- 监听去重 + 入队限速
- 
- 维护成本
- 
- 仅 360 引擎自研，其余拼装成熟库
- 
- 
- 11. 技术选型与版本
- 
- 
- KS_DOC_REVIEWS	O75CxGLezEUVDRGMVMt7qM	58725	https://www.workbuddy.cn/space/d/O75CxGLezEUVDRGMVMt7qM
- 
  - ① **本地 GPU**：本机/同机容器（含 NVENC）；
  - ② **云 GPU**：按需云实例 / Serverless GPU（弹性、用完释放）；
  - ③ **本地网络第三方 GPU 主机**：局域网内 Windows / Linux 主机，经 **agent（长连接心跳 + 任务拉取）** 接入。
  - 节点能力声明：`codec` 支持（h264/hevc/av1）、是否 `nvenc`、显存、`concurrency`。
  - 调度：注册→心跳→能力匹配→任务入队（`transcode_jobs`/`index_jobs`）；节点掉线自动剔除、任务重派。
  - 日志轮转：`logrotate` 或 Docker `json-file` max-size=50m max-files=5。
  - 链路追踪：`request_id` 贯穿 Caddy → API → AI/转码 → 日志关联。
  - 恢复演练：每季度执行一次全量恢复测试（模拟磁盘损坏→从备份恢复→验证数据完整性）。
  - 灰度策略：先部署 1 个 API 副本，观察错误率/延迟 10min，无异常后全量。
  - 回滚：保留前 2 版镜像，`docker compose up -d --no-deps --force-recreate api` 一键回滚。
  - Docker: `HEALTHCHECK CMD curl -f http://localhost:8080/health || exit 1`
  - K8s: `livenessProbe` / `readinessProbe` 指向上述端点。
  - 迁移期间双系统并行运行（群晖只读，本系统增量写入）。
  - 虚拟滚动 + CDN 缩略图 → 10 万媒体首屏 \< 2s。
  - PG `tsvector` + pgvector IVFFlat 索引 → 搜索 \< 1s。
  - 转码/AI 经队列横向扩展（GPU 节点可增删）。
  - HLS 边缘缓存 → 4K 360 起播 \< 3s。
  - 后端：Go 1.22+（API/媒体）；Python 3.13（FastAPI AI）
  - 存储：PostgreSQL 16 + pgvector 0.7；MinIO/S3 对象存储
  - 前端：Vue 3 + Vite；MapLibre GL JS；Three.js / A-Frame；hls.js
  - 转码：ffmpeg 6（subprocess 调用，不链接 libav\*）+ NVENC；BullMQ + **Valkey**（Redis 的 BSD 许可 fork，避免 RSALv2/SSPL 商业化风险）
  - 部署：Docker Compose → K8s；Caddy 反代
  - 鉴权：Keycloak（OIDC）/ Authelia；otplib（2FA）

