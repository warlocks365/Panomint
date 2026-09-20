# Panomint

**Immersive Smart Video Album System（沉浸式智能视频相册系统）**

> 简称：**Panomint** ｜ 仓库可见性：**Private（私有）**，未经确认不更改

## 简介

Panomint 是一套 **通用相册系统**：**100% 自研、可自托管**，全景 / 360° 智能视频相册是其内置能力之一。面向拥有大量 H.265 / H.264 全景视频、希望获得微信 H5 分享、自适应码率与 WebXR 球面回放能力的用户。

## 部署形态（正式口径）

> **项目定位**：独立相册系统，可部署任意主流 Linux / Docker，**不绑定群晖**；媒体源位置无关（SMB/NFS/本地/远端）。任何功能不得表述为「群晖专用」。
>
> —— 该口径原载于 `文档/阶段性快照_Job000005节点.md:223`，自 2026-09-18 起提升为本仓库的**正式口径**。

因此：

- **目标平台是"通用自托管"**：任意具备主流 Linux 或 Docker 的环境（普通服务器 / 云主机 / 虚拟机 / NAS 均可），以及**无容器的独立服务器安装**（二进制 + systemd + 外部 PostgreSQL/Valkey）。
- **群晖 Synology Photos 是功能参照，不是部署目标**。本系统的部分功能以它为对标基准，但**部署形态不绑定群晖、DSM 或任何具体机型**。
- **DS1819+ / Atom C3538 只是"当前已有环境条件"的一个代表**，在本仓库中一律作为**示例环境**看待，不作为设计约束。
- 架构的可移植性依赖"能力模型"而非"型号"：存储节点（有磁盘）与算力节点（有 CPU/GPU）分离，不假设用户必有 NAS、必有 NVIDIA GPU。

## 核心功能

- **360° / 全景球面回放**：Three.js / A-Frame + WebXR，支持手机陀螺仪与 VR 头显头追
- **微信 H5 链接分享**：不下载原始文件，服务端转码 + HLS / DASH 自适应码率（ABR）
- **时间轴（年 / 月 / 日）** 浏览与 **GPS 全屏交互地图模式**（MapLibre GL + PostGIS）
- **独立后台账户体系**：JWT 鉴权，不对接 DSM / 目录账户，配套 RBAC 权限
- **存储与算力分离**：存储节点 + 独立 GPU 转码节点（Compute Node agent 接入）

## 技术栈

> 本表已按**实际代码**校正（2026-09-18）。此前版本把 AI 写成 Python/FastAPI、把队列写成 BullMQ，两者均与实现不符。

| 层 | 技术 | 版本 / 说明 |
|----|------|------|
| 后端 API | Go | `go.mod` 声明 `go 1.26.0`（gin + pgx + goose） |
| AI 推理 | **Go + CGO + ONNX Runtime**（`github.com/yalue/onnxruntime_go`） | 无 Python 服务。模型：Chinese-CLIP（默认）/ OpenAI CLIP（512 维向量）、YuNet + SFace（人脸）；缺库时以 `_nocgo.go` 构建标签降级 |
| 数据库 | PostgreSQL + PostGIS + pgvector | PG 16 / PostGIS 3 / pgvector（`docker/db/Dockerfile`） |
| 对象存储 | — | **当前未接入**：`src/backend` 内**零调用** MinIO/S3（`grep -rin minio src/backend` 仅命中人脸 IoU 常量 `MatchMinIoU`）；媒体实际走本地文件系统。compose 中 `minio` 为**可选**服务（profile `s3`，Job000036），需要对象存储时 `docker compose --profile s3 up -d minio` 显式启用 |
| 前端 | Vue 3 + Vite + Pinia | + MapLibre GL（地图）+ Three.js（360/VR）+ hls.js（ABR）+ vue-virtual-scroller |
| 转码 | ffmpeg（subprocess 调用，不链接 libav*） | NVENC 为可选加速，无 NVIDIA 卡时自动回退 CPU |
| 消息队列 | **自研队列**（语义对齐 BullMQ） | 基于 Valkey（Redis 协议，`github.com/redis/go-redis/v9`）；waiting / delayed / processing / failed 四态为自行实现，**未引入 BullMQ** |
| 部署 | Docker Compose（基线）｜独立服务器（二进制 + systemd） | 反代：容器内 nginx（或自建反代，规则见 `文档/独立部署指南_v1.0.md`） |

## 目录结构

```
Panomint/
├── docker-compose.yml          # 容器编排（PG/PostGIS/pgvector + Valkey + api/web/worker + caddy；MinIO 为可选 profile s3）
├── docker/
│   ├── db/Dockerfile           # PG16 + PostGIS 3 + pgvector 镜像
│   └── web/Dockerfile          # nginx：静态托管 + SPA 回退 + API 反代（规则清单见独立部署指南）
├── src/
│   ├── backend/                # Go 后端（API / 媒体 / 索引 / 转码 / 鉴权 / AI）
│   │   ├── cmd/                # 可执行入口（api / migrate / transcodectl / embedgen / facesgen / ...）
│   │   ├── internal/           # 业务包（ffmpeg / media / index / auth / ortx / faces / embed / ...）
│   │   └── migrations/         # SQL 迁移脚本（goose）
│   └── frontend/               # Vue 3 + Vite 前端
├── deploy/
│   └── systemd/                # 独立服务器部署的 systemd unit 模板（非容器形态）
├── scripts/                    # 资产获取 / 备份恢复 / 巡检 / 独立部署安装脚本
├── prototypes/                 # 360 播放器 / 地图模式验证原型
├── 文档/                       # 设计文档（PRD / TDD / DDL / API / 部署方案与兼容性 / 独立部署指南）
└── README.md
```

> ⚠️ 设计文档目录名为中文 **`文档/`**（非 `docs/`）。

## 开发工具（`src/backend/cmd/`）

| 工具 | 用途 |
|------|------|
| `cmd/genmedia` | 测试媒体生成器：合成 100 张 JPEG（不同尺寸、文件名含日期前缀）与 5 段不同时长 mp4，供 T1.4 索引/时间轴集成验证 |
| `cmd/inject360` | 360 样片注入：给既有照片/视频补注入 360 全景元数据（`-photo`/`-video`/`-verify`），供索引的 360 判定逻辑做端到端验证；绝不覆盖已存在文件 |
| `cmd/migrateplan` | 迁移规划扫描器：不写库扫描既有媒体库并与当前库比对，产出「导入计划」（新增/重复/路径冲突统计），供人工确认后再执行导入（TDD §8.8 / PRD §11.2） |

## 开发规范

- 主分支 `main` 保持稳定可运行；新功能在 `feature/*` 分支开发，验证通过后合并。
- 提交信息统一格式：`type: 简要描述`（`type` ∈ feat / fix / chore / docs / refactor / test）。
- 仓库当前为 **私有**，未经确认不更改可见性。

## License

私有仓库，版权所有。未经授权禁止对外分发。
