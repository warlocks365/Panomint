# Panomint v1.0.0 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.0.0**（首个正式发布版，语义化版本起点） |
| 发布日期 | 2026-09-24 |
| 分支 | main |
| 版本管理规范 | 《版本管理规范.md》（大.中.小 三段式；本次=v1.0.0，后续严格按规范递增） |

---

## 一、更新摘要

v1.0.0 是全景相册系统的首个正式发布版。自项目启动以来的 100+ 项研发交付（时间轴、地图、相册、AI 搜索、人物、分享、WebDAV、管理后台等）在本版本全部收口为**可复现构建、可独立部署**的三种形态，并补齐发布闭环的最后几块拼图：

### 1.1 本次新增

| 模块 | 内容 |
| --- | --- |
| 首次安装引导 | 一次性初始化向导：首次访问完成数据库自动建表 + 创建管理员账号；完成后引导不再出现（后端 409 权威闸门 + 前端路由守卫双保险）；多人同时安装有防重入保护 |
| 版本管理 | 语义化版本规范落地：版本号唯一真源（VERSION 文件）→ 构建注入 → GET /version 公开端点；开发产物恒显示 dev，发布产物注入真实版本号 |
| 发布工程 | 三种部署形态一套源码可复现构建：Linux 裸机一键脚本、免 root 自包含集成包、Docker tag 化镜像 |
| 用户文档 | 《用户操作手册 v1.0.0》：十章覆盖安装、初始化、日常操作、管理后台、维护与排障 |

### 1.2 既有能力全量（v1.0.0 包含）

- **浏览**：时间轴（缩放/过滤/悬停预览/批量操作）、全屏地图（聚合簇/时间轴双向联动）、地点归纳、文件夹视图、空间（个人/团队）
- **播放**：360° 照片/视频（拖动/陀螺仪/VR 头显）、视频多档 HLS 自适应、微信 H5 兼容
- **AI**：中文语义搜索（Chinese-CLIP 向量）、零样本自动打标（人工确认流）、人脸检测与分组（YuNet+SFace，128 维本地向量）
- **组织**：手工相册 + 条件相册、标签、人物、重复项目清理、回收站（最近删除/已恢复）
- **协作**：多用户与 RBAC 权限、公开分享（免登链接/微信卡片/受控下载）、相册评论
- **接入**：WebDAV 完整读写、应用密码（第三方客户端）、二次验证 TOTP、存储位置管理、网络挂载（SMB/NFS/WebDAV，断连探活）
- **管理**：后台八页签（概览/用户/角色/权限/任务/审计/地图配置/存储）、审计日志、任务队列
- **上传**：三入口、批量、分块断点续传、去重、EXIF/GPS 提取、@eaDir 缩略图复用（群晖迁移）

### 1.3 质量与稳定性

- 全面代码审查 64 项收口：P0×3（修复+独立对抗验证 PASS）、P1×19 修复、P2 39 修+3 登记不修
- 发布门禁全绿：后端 vet + 28 包全量测试、CGO 构建链、前端构建、三道仓库级守卫（P0 规则/超大文件棘轮/反代前缀三方真源）
- 安装向导端到端实测：全新库 → 自迁移 → 建管理员 → 幂等 409 → 登录全链路 PASS（临时环境零残留）
- 发布产物冒烟：tarball 解包二进制版本注入核验、容器镜像版本串核验

---

## 二、部署指引

三种形态任选其一，详细步骤见《用户操作手册》第 2 章；此处为速览。

### 2.1 Docker 镜像版（推荐）

```bash
cp .env.example .env       # 填 POSTGRES_PASSWORD / JWT_SECRET（强随机）
docker compose up -d       # 首启 api 自动建表（goose up 幂等）
# 浏览器访问 http://服务器IP:8088 → 进入初始化向导
```

### 2.2 集成包（免 root）

```bash
tar xzf panomint-1.0.0-linux-amd64-bundle.tar.gz
cd panomint-1.0.0
./bundle/install.sh        # 交互生成 .env + 初始化数据库
./bundle/panoctl start
```

前置依赖（install 前有 env-check 体检）：PostgreSQL 16+（PostGIS、pgvector）、Valkey、ffmpeg。

### 2.3 裸机一键（Debian/Ubuntu，root）

```bash
tar xzf panomint-1.0.0-linux-amd64-baremetal.tar.gz
sudo bash panomint-1.0.0/baremetal/install.sh
```

自动完成：装依赖（PGDG 源）→ 建库建号 → 落盘 → systemd 7 服务开机自启 → /ready 健康确认。

### 2.4 升级与回滚

- **同大版本（1.x → 1.y）滚动升级**：替换产物后重启即可；api 启动自动跑增量迁移（幂等，失败拒启动不破坏数据）。
- **跨大版本（→ 2.0）**：按届时发布的迁移指引执行，勿直接跳。
- **回滚**：回退到上一小版本的产物 + 数据库快照（升级前务必备份，见手册 9.1）。

### 2.5 环境要求

x86_64 Linux（或 Docker 可用的 NAS）；最低 2C/4G/20G（不含媒体）；生产环境必须显式设置 PG_DSN / VALKEY_ADDR / MEDIA_ROOT（未设会被 fail-closed 门禁拒绝启动）。

---

## 三、产物清单与校验

### 3.1 发布包

| 产物 | 说明 |
| --- | --- |
| panomint-1.0.0-linux-amd64-bundle.tar.gz | 免 root 集成包（bin/web/assets + bundle/ 管理脚本） |
| panomint-1.0.0-linux-amd64-baremetal.tar.gz | 裸机一键包（含 install.sh 与 3 个 systemd 单元模板） |
| sha256sums.txt | 上述 tarball 的校验和 |

### 3.2 容器镜像

| 镜像 | 说明 |
| --- | --- |
| panomint/app:1.0.0 | api + AI 工具（embedgen/taggen/facesgen/phashgen/migrate 等） |
| panomint/worker:1.0.0 | 后台 worker（index/transcode，独立镜像） |
| panomint/db:1.0.0 | PostgreSQL 16 + PostGIS + pgvector 预装 |
| panomint/web:1.0.0 | nginx 前端 + API 同源反代（**已缺陷，见下方勘误，请改用 1.0.1**） |
| panomint/web:1.0.1 | web 修复版（唯一变更：前端产物重建，含初始化向导；其余镜像维持 1.0.0 不变） |

离线分发：docker save 四个镜像 | gzip 打包，目标机 docker load 后 compose 直接可用。

> **⚠️ 勘误（2026-09-24，Job000110）：web:1.0.0 镜像内前端产物为向导合入前的旧构建**——首次安装向导（/setup）缺失、路由守卫缺位，全新部署时浏览器只会看到登录页、无法创建管理员（后端 /setup/status 均正常，`docker exec` 验证 `initialized:false` 可确认未初始化）。**修复版 `web:1.0.1` 已发布**（digest `sha256:4ba897b14117…`，与 latest 同指；缺陷版 1.0.0 保留可追溯）。已部署 1.0.0 的用户只需升级 web 一个容器：
>
> ```bash
> # compose 中 web 服务的 image 改为 warlocks/panomint-web:1.0.1（镜像加速版加 docker.1ms.run/ 前缀），然后：
> docker compose pull web && docker compose up -d web
> # 强刷浏览器（Ctrl+F5）后访问 http://IP:8088 → 应见「欢迎使用全景相册」初始化向导
> ```

### 3.3 校验值

发布 tarball SHA-256（与 `sha256sums.txt` 一致，下载后务必校验）：

```
4ca339ba184b661cf0078fd79eb974b7a9bf9e9aa8bbeb43716ef9042f7fa144  panomint-1.0.0-linux-amd64-baremetal.tar.gz
33a97c7995405362d10bfe90441e8eda72ee790b4ec9091acf4c6eeabf52781f  panomint-1.0.0-linux-amd64-bundle.tar.gz
```

容器镜像版本串核验：app 镜像内 api 二进制 grep 版本号 1.0.0 命中（构建日志 `IMAGE_VERSION_OK`）。四镜像构建产物：

| 镜像 | Image ID | 体积 |
| --- | --- | --- |
| panomint/app:1.0.0 | e683a4767660 | 694MB |
| panomint/worker:1.0.0 | 0e195f9a34fc | 352MB |
| panomint/db:1.0.0 | ef7c886cd92d | 651MB |
| panomint/web:1.0.0 | 6cfd31b0019c | 51.6MB |

离线分发：`docker save panomint/app:1.0.0 panomint/worker:1.0.0 panomint/db:1.0.0 panomint/web:1.0.0 | gzip > images-1.0.0.tar.gz`，目标机 `docker load` 后 compose 直接可用。

---

## 四、已知限制与遗留（诚实口径）

| 项 | 说明 | 处置 |
| --- | --- | --- |
| 硬编 | 任何环境均无 GPU 硬编依赖；CPU 软编为默认路径 | 设计如此（ffmpeg subprocess）；GPU 为可选加速 |
| arm64 | 镜像构建已按多架构设计，但 v1.0.0 仅实测 x86_64 | 用户已裁决 arm64 暂不支持 |
| @eaDir sidecar | 缩略图复用已实现；人物/标签 sidecar 因无真机样本未实现 | 登记遗留，有样本后补 |
| CI 远端 run | workflow 本机预演全绿，远端 run 待 gh auth 后复核 | 挂起（需用户 gh auth login） |
| 复制同源自覆盖 | 同一媒体重复复制存在文件互覆风险 | 已登记（E8），修复方向待用户裁决 |
| 真机回归 | 陀螺仪/VR/触摸手势需真机窗口验证 | 待用户安排 |

---

## 五、相关文档

- 《用户操作手册 v1.0.0》——最终用户读本
- 《版本管理规范》——版本号规则与发布流程
- 《部署方案与兼容性》——环境细节与离线部署
- 《研发进度报告 v1.0》——100+ Job 全口径复盘
- release/README.md——构建复现说明

---

*Panomint v1.0.0 · 2026-09-24 · 自托管全景相册*
