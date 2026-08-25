# Panomint

**Immersive Smart Video Album System（沉浸式智能视频相册系统）**

> 简称：**Panomint** ｜ 仓库可见性：**Private（私有）**，未经确认不更改

## 简介

Panomint 是一套 **100% 自研、自托管** 的全景 / 360° 智能视频相册系统，定位为群晖
Synology Photos 的增强替代方案。面向拥有大量 H.265 / H.264 全景视频、希望获得微信 H5
分享、自适应码率与 WebXR 球面回放能力的用户。

## 核心功能

- **360° / 全景球面回放**：Three.js / A-Frame + WebXR，支持手机陀螺仪与 VR 头显头追
- **微信 H5 链接分享**：不下载原始文件，服务端转码 + HLS / DASH 自适应码率（ABR）
- **时间轴（年 / 月 / 日）** 浏览与 **GPS 全屏交互地图模式**（MapLibre GL + PostGIS）
- **独立后台账户体系**：JWT 鉴权，不对接 DSM / 目录账户，配套 RBAC 权限
- **存储与算力分离**：存储节点 + 独立 GPU 转码节点（Compute Node agent 接入）

## 技术栈

| 层 | 技术 |
|----|------|
| 后端 API | Go 1.22+ |
| AI 推理 | Python 3.13 + FastAPI |
| 数据库 | PostgreSQL 16 + PostGIS 3.4 + pgvector 0.7 |
| 对象存储 | MinIO / S3 |
| 前端 | Vue 3 + Vite + MapLibre GL + Three.js + hls.js |
| 转码 | ffmpeg（subprocess）+ NVENC + BullMQ + Valkey |
| 部署 | Docker Compose → K8s + Caddy（自动 HTTPS） |

## 目录结构

```
Panomint/
├── docker-compose.yml          # PG + PostGIS + pgvector + Valkey + MinIO 编排
├── docker/
│   └── db/init/01-schema.sql   # DDL v1.1（PG16 + PostGIS + pgvector 全量建表）
├── src/
│   ├── backend/                # Go 后端（API / 媒体 / 索引 / 转码 / 鉴权）
│   │   ├── cmd/                # 可执行入口（api / migrate / transcodectl / ...）
│   │   ├── internal/           # 业务包（ffmpeg / media / index / auth / ...）
│   │   └── migrations/          # SQL 迁移脚本
│   └── frontend/               # Vue 3 + Vite 前端
├── prototypes/                 # 360 播放器 / 地图模式验证原型
├── docs/                       # 设计文档（PRD / TDD / DDL / API）
└── README.md
```

## 开发规范

- 主分支 `main` 保持稳定可运行；新功能在 `feature/*` 分支开发，验证通过后合并。
- 提交信息统一格式：`type: 简要描述`（`type` ∈ feat / fix / chore / docs / refactor / test）。
- 仓库当前为 **私有**，未经确认不更改可见性。

## License

私有仓库，版权所有。未经授权禁止对外分发。
