# Panomint 全景相册系统

**中文** | [English](README.en.md)

> 自托管的 360° 全景智能相册系统 —— Synology Photos 的现代替代方案

![Version](https://img.shields.io/badge/version-1.7.1-blue)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20Docker%20%7C%20NAS-lightgrey)
![Backend](https://img.shields.io/badge/backend-Go%201.26-00ADD8)
![Frontend](https://img.shields.io/badge/frontend-Vue%203-42B883)

## 项目概述

Panomint 是一套 **100% 自研、可自托管** 的相册系统，核心能力围绕 **360° 全景视频与照片** 构建：球面回放、陀螺仪与 VR 头显跟随、服务端 HLS 转码与自适应码率、微信 H5 分享、时间轴与全屏地图、AI 语义搜索与自动打标。

- **媒体源位置无关**：本地目录 / SMB / NFS / 远端挂载均可作为照片源；
- **部署形态无关**：任意主流 Linux、Docker Compose、NAS（群晖 DSM Container Manager 有现成编排），或无容器的独立服务器（二进制 + systemd）；
- **存储与算力分离**：存储节点与算力节点解耦，无 NVIDIA GPU 时自动回退 CPU；
- **不绑定任何厂商**：以 Synology Photos 为功能对标基准，但部署不依赖 DSM 或任何具体机型。

## 功能亮点

- **360° 球面回放**：Three.js 贴球渲染 + WebXR 头显双眼模式，手机陀螺仪即开即用；
- **GB 级视频即时起播**：未转码大视频走 HTTP Range 流式回退（不等待转码），转码完成后自动热切换到多档 HLS 自适应流；
- **HEVC 兼容提示**：相机直出的 H.265 素材在不支持的浏览器上给出明确指引，而不是静默失败；
- **AI 语义搜索**：Chinese-CLIP 向量检索，用中文自然语言找照片（如「海边的日落」）；
- **自动打标与人物聚类**：YuNet 人脸检测 + SFace 特征，完全本地推理、无云端依赖；
- **全屏交互地图**：MapLibre GL + PostGIS 地理聚合，照片按 GPS 落图；
- **安全的账号体系**：JWT + 可选 TOTP 双因素、登录失败锁定、密码强度策略、邀请码注册、应用密码（WebDAV 专用），管理后台一站管理；
- **运维友好**：备份/恢复脚本、资产离线获取脚本、巡检脚本、转码远程调试通道（限时授权 + 一次性密钥）。

## 功能清单

**媒体管理**：照片/视频上传与导入、目录扫描（含 NAS 挂载）、时间轴（年/月/日）、虚拟文件夹、重复检测（pHash）

**播放与分享**：360° 球面回放（拖动/陀螺仪/VR）、HLS 多码率自适应、未转码回退播放（Range 流式）、HEVC 编码检测提示、微信 H5 分享链接、公开分享与下载、EXIF 展示

**智能能力**：中文语义搜索（CLIP）、自动标签、人脸检测与聚类、GPS 逆地理编码（高德，可选）、全屏地图

**账号与安全**：注册开关 + 一次性邀请码、密码强度策略、登录失败锁定、管理员重置密码（一次性临时密码 + 强制改密）、双因素认证（TOTP）、应用密码、RBAC 角色、审计日志

**管理后台**：用户与角色管理、账号策略、扫描根目录分配、转码与 HLS 设置、HTTPS 证书与端口配置、远程调试通道、版本信息

**部署与运维**：Docker Compose（通用/群晖模板 + 交互式生成脚本）、独立服务器（systemd）、三形态发布包、备份/恢复、资产离线获取

## 技术架构

| 层 | 技术 | 说明 |
|----|------|------|
| 后端 | Go 1.26（gin + pgx + goose） | 单二进制，含 API/索引/转码/鉴权/AI 全部能力 |
| AI 推理 | Go + CGO + ONNX Runtime | Chinese-CLIP（默认）/ OpenAI CLIP、YuNet + SFace 人脸；无 CGO 环境自动降级构建 |
| 数据库 | PostgreSQL 16 + PostGIS 3 + pgvector | 地理聚合 + 向量检索一体 |
| 前端 | Vue 3 + Vite + Pinia | + MapLibre GL + Three.js + hls.js + vue-virtual-scroller |
| 转码 | ffmpeg（subprocess，不链接 libav\*） | HLS 多码率；NVENC 可选加速，无卡自动回退 CPU |
| 队列 | 自研（语义对齐 BullMQ） | 基于 Valkey，waiting/delayed/processing/failed 四态 |
| 部署 | Docker Compose / systemd | 反代：内置 nginx（web 容器）或 Caddy（可选 TLS） |

**核心设计**：媒体入库仅三条受控路径（HTTP 上传 / 扫描导入 / 存储对账）；可见性判定收敛于单一真源模块（mediascope），所有面向用户的媒体查询强制走同一读口径；队列与任务生命周期自研实现，无外部消息中间件依赖；AI 模型资产全部由脚本显式获取，支持离线部署。

## 快速开始

### Docker Compose（推荐）

```bash
git clone https://github.com/warlocks365/Panomint.git
cd Panomint
bash scripts/generate-compose.sh   # 交互式生成 docker-compose.yml（可选群晖模式）
docker compose up -d
```

首次访问 `http://<host>:8088` 进入初始化向导（创建管理员账号）。

AI 模型与静态资产（CLIP / 人脸模型等）通过脚本显式获取（支持联网或离线包）：

```bash
bash scripts/fetch-all-assets.sh
```

### 群晖 DSM

使用 `release/docker/docker-compose.synology.yml`（Container Manager 导入），或运行生成脚本选择群晖模式自动产出。

### 独立服务器（无容器）

二进制 + systemd：见 [独立部署指南](文档/独立部署指南_v1.0.md) 与 `deploy/systemd/`。

### 系统要求

| 档位 | 说明 |
|------|------|
| 最低 | 2 核 CPU / 4GB 内存 / PostgreSQL 16；AI 功能走 CPU 推理 |
| 推荐 | 4 核 / 8GB+ 内存；NVIDIA GPU（可选）启用 NVENC 硬转码 |

## 文档

| 文档 | 说明 |
|------|------|
| [用户操作手册](文档/用户操作手册_v1.0.0.md) | 全功能使用说明 + 原理深入 |
| [API 详细契约 v1.2](文档/API详细契约_v1.2.md) | 接口契约（开发者向） |
| [数据库 DDL v1.1](文档/数据库DDL_v1.1.md) | 表结构与索引 |
| [技术设计文档 TDD v1.1](文档/技术设计文档_TDD_v1.1.md) | 架构与设计决策 |
| [产品需求文档 PRD v3.1](文档/相册系统详细需求文档_PRD_v3.1.md) | 功能需求全量定义 |
| [版本管理规范](文档/版本管理规范.md) | 版本号规则与发版流程（含发版一致性校验） |
| [独立部署指南](文档/独立部署指南_v1.0.md) | 无容器部署（二进制 + systemd） |
| [NAS 挂载与群晖 Photos 对照](文档/NAS挂载与群晖Photos功能对照_v1.0.md) | 与群晖 Photos 的功能对照 |

Release Notes：[v1.0.0](文档/Release_Notes_v1.0.0.md) · [v1.1.0](文档/Release_Notes_v1.1.0.md) · [v1.2.0](文档/Release_Notes_v1.2.0.md) · [v1.3.0](文档/Release_Notes_v1.3.0.md) · [v1.4.0](文档/Release_Notes_v1.4.0.md) · [v1.5.0](文档/Release_Notes_v1.5.0.md) · [v1.6.0](文档/Release_Notes_v1.6.0.md) · [v1.7.0](文档/Release_Notes_v1.7.0.md) · [v1.7.1](文档/Release_Notes_v1.7.1.md)

## 更新日志

### v1.7.1（2026-09-27）

- 大文件回退播放改 Range 流式：GB 级未转码视频即时起播、拖动秒级定位，浏览器内存不再随文件大小膨胀
- HEVC 编码视频在浏览器不支持解码时给出显式提示（建议开启 HLS 转码），不再笼统报加载失败
- 播放错误提示透出 HTTP 状态码

### v1.7.0（2026-09-26）

- 新增管理后台「账号策略」页签：注册开关与邀请码、密码强度、登录失败锁定策略集中配置
- 新增账号自助注册：开启注册开关后凭一次性邀请码注册（默认关闭）
- 新增登录失败锁定：连续输错达上限（默认 5 次）锁定 15 分钟，正确登录清零计数
- 新增密码强度策略：最低长度与字符类别要求（默认至少 8 位，可要求大小写/数字/特殊字符）
- 管理员重置密码改为一次性临时密码：仅显示一次，强制下次登录修改
- 禁用账号立即失效其全部会话；用户列表直接展示锁定与强制改密状态
- 新增 HTTP/HTTPS 访问端口配置：重定向目标按 HTTPS 端口拼接，热生效

### v1.6.0（2026-09-26）

- 360° 视频未转码回退播放：无 HLS 时以原始文件播放，拖动环视/陀螺仪/VR 头追全保留，转码完成后自动切回多档流
- 播放页新增回退提示条（转码中/失败/未转码/闸门关闭四态），闸门关闭时不再报错而是降级播放
- 修复播放页一处模板条件分支误配对导致的异常渲染问题

### v1.5.0（2026-09-26）

- 新增 HLS 流媒体设置：管理后台可调流媒体开关与参数
- 新增 HTTPS/证书设置与访问守卫强化
- 用户操作手册同步更新

### v1.4.0（2026-09-26）

- 新增转码远程调试通道：管理员可在管理后台开启限时调试授权（TTL 档位/一次性凭据/两步确认），工程师经加密 WebSocket 通道诊断转码问题
- 配套 debugctl 命令行工具（快照采集/会话诊断，凭据文件防泄露）
- CI 触发优化：纯文档类推送不再触发全量构建

### v1.3.0（2026-09-26）

- 新增播放时自动转码开关：未转码视频播放时可即时发起转码（与系统级开关 AND 组合）
- 管理概览的索引状态显示中文化

### v1.2.0（2026-09-25）

- 管理端扫描导入增强：新增目录树选择器，浏览媒体库层级并选中目录，无权限目录锁定提示
- 管理员可按账号分配扫描根目录，成员支持自助触发扫描
- 设置页新增版本信息：当前版本号与历史更新说明
- 新增应用内操作手册（使用方法/原理深入两级内容）
- 自动 HLS 转码开关上收为系统级（管理后台转码页签），关闭后新视频以原始文件播放
- 移除无实际作用的存储位置死功能，挂载导入落点语义化（landing_dir）

### v1.1.0（2026-09-25）

- 新增管理端扫描导入：指定目录批量入库，支持启动时自动扫描
- 修复复制媒体同源互覆缺陷（复制副本不再覆盖源文件）
- 修复存储页签显示串台问题

### v1.0.1（2026-09-24）

- 修复 web 镜像误打包旧前端导致首次安装向导不可见的问题

### v1.0.0（2026-09-24）

- 首次正式发布：360° 全景/陀螺仪/VR 头追、HLS 流媒体、时间轴、全屏地图、AI 语义搜索与打标、人物聚类、相册与分享、管理后台

## 目录结构

```
Panomint/
├── docker-compose.yml          # 容器编排（PG/PostGIS/pgvector + Valkey + api/web/worker + caddy；MinIO 为可选 profile）
├── docker/                     # 各服务 Dockerfile（db/web/worker/api）与 Caddy 配置
├── release/                    # 三形态发布（Docker/裸机/集成包）与群晖编排模板
├── scripts/                    # compose 生成 / 资产获取 / 备份恢复 / 巡检 / 质量门禁脚本
├── deploy/systemd/             # 独立服务器 systemd unit 模板
├── src/
│   ├── backend/                # Go 后端（cmd/ 入口 + internal/ 业务包 + migrations/）
│   └── frontend/               # Vue 3 + Vite 前端
├── 文档/                       # 产品文档（手册/API 契约/DDL/TDD/PRD/发版规范/Release Notes）
└── licenses.csv                # 第三方依赖许可证清单
```

## 开发

```bash
# 后端
cd src/backend && go build ./... && go test ./...
# 前端
cd src/frontend && npm ci && npm run build
# 数据库迁移（启动时自动执行；手动构建迁移工具：）
go build ./cmd/migrate
```

质量门禁：CI（`.github/workflows/ci.yml`）+ 前端组织规模棘轮（`scripts/frontend_org_guard.py`）+ P0 安全守卫（`scripts/p0_guard.py`）。

## License

本项目源码公开供学习、评估与自托管使用；除仓库内声明外，未授予再分发许可。第三方依赖许可证见 [licenses.csv](licenses.csv)。
