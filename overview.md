# 全景相册系统 · 续作交接与 P0 任务计划

> 生成日期：2026-08-24
> 基于：README + overview + 工作日志 + TDD v1.1 + DDL v1.1 + API v1.1
> 目标：新电脑环境无缝衔接，避免重复劳动

---

## 一、项目全貌认知

### 1.1 项目本质

**通用相册系统**：100% 自研、可自托管。**部署形态不绑定群晖** —— 可部署任意主流 Linux / Docker，或采用独立服务器安装（二进制 + systemd + 外部 PostgreSQL/Valkey）；媒体源位置无关（SMB/NFS/本地/远端）。全景 / 360° 智能视频相册是其内置的核心能力之一，功能上以**群晖 Synology Photos 为对标参照**（示例环境之一为群晖 DS1819+，Intel Atom C3538，无 GPU）。

### 1.2 核心需求（7 项用户刚需）

| # | 需求 | 现有方案覆盖情况 |
|---|------|------------------|
| 1 | 4K+ 360° 全景视频 | 群晖仅 360 照片 / PhotoPrism 仅 equirect 无交互 → **纯自研** |
| 2 | 手机/PAD 陀螺仪交互 | 无人覆盖 → **纯自研** |
| 3 | VR/AR 头显头追 | 无人覆盖 → **纯自研** |
| 4 | 自适应码率（ABR） | hls.js 可复用 |
| 5 | 微信 H5 链接分享（非整文件） | 群晖有链接分享，无 360 H5 |
| 6 | 时间轴（年/月/日） | 群晖 Photos 已有 |
| 7 | GPS 地点 / 全屏交互地图模式 | 群晖有地点，无全屏地图 → **自研扩写** |

### 1.3 关键约束（贯穿全部设计）

- **存储与算力分离（能力模型，不绑定型号）**：存储节点（任意 NAS / 服务器 / 对象存储，只要有磁盘）只负责存放原文件；**算力节点**（本机 CPU、本地 GPU、云 GPU、局域网第三方主机，经 Compute Node agent 接入）负责转码/AI。低算力主机不承担人脸索引与 4K+ 全景转码。
- **独立后台账户体系**：不对接 DSM/目录账户，自建账户 + RBAC
- **100% 自研** = 自写前后端/AI/转码，不 fork 成品，仅复用通用开源库

### 1.4 技术栈（已定稿）

| 层 | 技术选型 | 版本 |
|----|----------|------|
| 后端 API | Go | `go.mod` 声明 1.26.0 |
| AI 推理 | **Go + CGO + ONNX Runtime**（无 Python 服务） | ORT 1.29.0；`github.com/yalue/onnxruntime_go` |
| 向量与视觉模型 | Chinese-CLIP（默认）/ OpenAI CLIP；YuNet + SFace（人脸） | 512 维 / 128 维 |
| 数据库 | PostgreSQL + PostGIS + pgvector | PG 16 / PostGIS 3 / pgvector |
| 对象存储 | **当前未接入**（后端零调用 MinIO/S3；媒体走本地文件系统）；compose 中 MinIO 为可选 profile `s3` | - |
| 前端 | Vue3 + Vite + Pinia | 3.x |
| 地图 | MapLibre GL JS | v6 |
| 360/VR | Three.js / A-Frame | - |
| 自适应码率 | hls.js | - |
| 转码 | ffmpeg（subprocess）+ NVENC（可选，无卡回退 CPU） | 6.x |
| 消息队列 | **自研队列**（语义对齐 BullMQ），基于 Valkey | - |
| 部署 | Docker Compose（基线）/ 独立服务器（二进制 + systemd） | - |
| 反代 | nginx（容器内）/ 自建反代（规则清单见独立部署指南） | - |

### 1.5 文档体系（当前版本）

| 文档 | 版本 | 修正项数 | 状态 |
|------|------|----------|------|
| PRD | v3.1 | 12 项 | 已定稿 |
| TDD | v1.1 | 10 项 | 已定稿 |
| DDL | v1.1 | 16 项 | 已定稿 |
| API | v1.1 | 10 项 | 已定稿 |
| 审查修正清单 | — | 48 项总览 | 已定稿 |
| overview | — | 修正汇总 | 已定稿 |
| 工作日志 | — | 前序全部记录 | 已归档 |

---

## 二、已完成工作清单

### 2.1 需求阶段（已完成）

- [x] 竞品调研：PhotoPrism（AGPL v3，仅设计参考不 fork）、群晖 Synology Photos（优先参考基准）
- [x] PRD v1.0 → v2.0 → v3.0 → v3.1（四轮迭代，覆盖 7 项刚需 + 地图模式 + 商业化合规）
- [x] 第二轮开放问题决策（GPU 算力多形态 / 微信分享 / 共享形态 / 网络带宽）
- [x] 地图模式大功能点完整集成四文档（11 项细分需求逐一落点）

### 2.2 设计阶段（已完成）

- [x] TDD v1.0 → v1.1（四层架构 / 360 引擎详细设计 / 算力节点 agent 协议 / 运维可观测性）
- [x] DDL v1.0 → v1.1（PG16 + PostGIS + pgvector 全量建表，16 项修正）
- [x] API v1.0 → v1.1（Bearer JWT 独立账户，全端点 + 错误码 + 分页 + 限流，10 项修正）

### 2.3 审查阶段（已完成）

- [x] 48 项全面审查修正（PRD 12 + TDD 10 + DDL 16 + API 10）
- [x] 商业化许可合规矩阵（A 类安全 17+ 库 / B 类注意 8 项 / C 类必替换 1 项）
- [x] 地图模式任务完成性复查（1 处遗留不一致已修正）

### 2.4 尚未开始的工作

- [ ] **P0 许可替换验证**：Valkey 替代 Redis + ffmpeg subprocess 封装层
- [ ] **P0 360 播放引擎原型**：Three.js + hls.js MVP
- [ ] **P0 地图模式原型**：PG+PostGIS + 高德瓦片反代 + MapLibre
- [ ] **P1 数据迁移工具**：rsync+inotify 扫描器
- [ ] 全部后端/前端/AI 代码实现
- [ ] Docker Compose 编排
- [ ] CI/CD 流水线

---

## 三、P0 续作任务：Valkey 许可替换 + ffmpeg subprocess 封装

### 3.1 任务背景

Redis 自 2024 年起改用 RSALv2/SSPL 许可，非 OSI 认证开源，不兼容商业化。**Valkey**（BSD-3-Clause）是 Linux 基金会维护的 Redis fork，BullMQ 完全兼容。此替换为 P0 级商业化前置条件。

同时，ffmpeg 必须以 subprocess 方式调用（不链接 libav* 库），避免 GPL 传染。需编写封装层确保进程管理、错误处理、输出捕获均符合生产级标准。

### 3.2 具体目标

1. **Valkey 部署验证**：Docker Compose 拉起 Valkey 容器，验证 Redis 协议兼容性
2. **BullMQ 队列验证**：通过 Valkey 实现任务入队/消费/重试/延迟调度
3. **ffmpeg subprocess 封装**：Go 包封装 ffmpeg 进程管理（启动/监控/超时/输出解析/错误恢复）
4. **健康检查集成**：/ready 端点检测 Valkey 连通性

### 3.3 涉及模块与文件范围

```
全景相册系统项目/
├── docker-compose.yml              # PG + Valkey + api/web/worker + caddy 编排（MinIO 为可选 profile s3）
├── .env.example                     # 环境变量模板
├── src/
│   └── backend/
│       ├── go.mod                   # Go 模块定义
│       ├── go.sum
│       ├── cmd/
│       │   └── api/
│       │       └── main.go          # API 服务入口
│       ├── internal/
│       │   ├── config/
│       │   │   └── config.go       # 配置加载（Valkey 连接参数）
│       │   ├── queue/
│       │   │   ├── queue.go         # BullMQ 等效：Go 端队列封装
│       │   │   └── queue_test.go    # 队列单元测试
│       │   ├── ffmpeg/
│       │   │   ├── runner.go        # ffmpeg subprocess 封装
│       │   │   ├── runner_test.go   # 封装测试
│       │   │   ├── parser.go        # ffmpeg 输出解析器
│       │   │   └── types.go         # 任务类型定义
│       │   └── health/
│       │       └── health.go        # 健康检查（PG + Valkey + 磁盘）
│       └── pkg/
│           └── valkey/
│               └── client.go        # Valkey 客户端封装
├── 文档/                            # 前序设计文档（已下载）
│   ├── 技术设计文档_TDD_v1.1.md
│   ├── 数据库DDL_v1.1.md
│   ├── API详细契约_v1.1.md
│   ├── 相册系统详细需求文档_PRD_v3.1.md
│   ├── 审查修正清单.md
│   └── overview.md
├── 工作日志/
│   ├── 2026-08-24_工作日志.md       # 前序日志
│   └── 2026-08-24_续作日志.md       # 本次续作日志
├── README.md                        # 项目入口
└── .workbuddy/
    ├── memory/                      # 工作记忆
    └── wb2md.py                     # 文档转换工具
```

### 3.4 验收标准

| 编号 | 验收项 | EARS 格式 | 优先级 |
|------|--------|-----------|--------|
| AC-01 | Valkey 容器启动 | When docker-compose up 执行，Valkey 容器**必须**在 5s 内进入 healthy 状态 | P0 |
| AC-02 | Redis 协议兼容 | When 客户端发送 PING/SET/GET/DEL/HSET/HGET 命令，Valkey **必须**返回与 Redis 协议一致的响应 | P0 |
| AC-03 | 队列入队消费 | When 生产者入队一个任务，消费者**必须**在 2s 内拉取并处理该任务 | P0 |
| AC-04 | 队列重试 | If 任务处理失败且配置了重试策略，队列**必须**按指数退避重试最多 3 次 | P0 |
| AC-05 | ffmpeg 进程管理 | When 调用 ffmpeg 封装层执行转码，系统**必须**正确启动子进程、实时捕获 stdout/stderr、在超时后发送 SIGTERM | P0 |
| AC-06 | ffmpeg 输出解析 | When ffmpeg 输出 progress 信息，解析器**必须**提取已处理时间/帧数/码率/剩余时间 | P0 |
| AC-07 | ffmpeg 错误处理 | If ffmpeg 退出码非 0，封装层**必须**返回结构化错误（含退出码、stderr 末尾 N 行、执行时长） | P0 |
| AC-08 | 健康检查 | When GET /ready 请求到达，系统**必须**检查 PG 连通 + Valkey 连通 + 磁盘可写，全部通过才返回 200 | P0 |
| AC-09 | 许可合规 | 项目**不得**在 go.mod 中引入任何 RSALv2/SSPL/GPL 许可的依赖 | P0 |

### 3.5 实施步骤

```
Step 1: 创建 docker-compose.yml（Valkey + PG16+PostGIS；MinIO 列为可选 profile s3）
  → 启动验证：docker-compose up，确认各服务 healthy

Step 2: 初始化 Go 后端项目骨架
  → go mod init + 目录结构 + 配置加载

Step 3: Valkey 客户端封装
  → 连接池 + PING/SET/GET 基础命令验证
  → 健康检查集成

Step 4: 队列封装（Go 端 BullMQ 等效）
  → 基于 Valkey 的延迟队列/重试/并发控制
  → 单元测试：入队/消费/重试/延迟

Step 5: ffmpeg subprocess 封装
  → 进程管理（启动/超时/优雅终止）
  → 输出解析（progress 解析 + 错误提取）
  → 集成测试：实际调用 ffmpeg -version 和简单转码

Step 6: 健康检查端点
  → GET /health（liveness）
  → GET /ready（readiness：PG + Valkey + 磁盘）
  → GET /metrics（Prometheus 指标暴露）

Step 7: 许可合规扫描
  → go.sum 中无 RSALv2/SSPL/GPL 依赖
  → ffmpeg 仅以 subprocess 调用
```

---

## 四、后续 P0 任务预览

### 4.1 360 播放引擎原型（P0 任务二）

- Three.js SphereGeometry + VideoTexture MVP
- DeviceOrientation 陀螺仪 + WebXR VR 头追
- hls.js ABR 码率切换
- iOS 权限请求 + 降级模式

### 4.2 地图模式原型（P0 任务三）

- Docker Compose PG + PostGIS
- 高德瓦片反代（避免前端暴露 key）
- MapLibre 前端 + bbox 聚合
- WGS-84 ↔ GCJ-02 坐标转换

---

## 四-补、当前进度快照（2026-09-11 更新，Job000009 优化完成节点）

**已完成**：Phase 0-3 全部（合规前置、后端骨架、双原型、P0 完整化六任务）；Phase 4 Stage 1（相册+智能相册+两级评论）、Stage 2（微信 H5 分享）、#7（Worker 容器化）；Stage 3 结构化搜索；七项遗留问题全部闭环。

**Job 流水线**：Job000001 搜索后端+UI ｜ Job000002 GitHub 同步+watchctl+全栈容器化 ｜ Job000003 搜索三连修复+悬停浮窗 ｜ Job000004 孤儿清理+72 样本重建 ｜ Job000005 统一播放器+时间轴滑块+备注标签+搜索排序 ｜ Job000006 媒体处理流水线补齐 ｜ Job000007 360 播放引擎补全 ｜ Job000008 内网 HTTPS 打通 ｜ **Job000009 全屏地图模式（MapLibre + 高德瓦片反代 + 时间轴双向联动）**。**全部完成并推送**。

**Job000007 要点（9/03，60d543c）**：
- **360 照片球面渲染**：360Player 扩展 `mode=video|photo`（TextureLoader 贴球，与视频共用球体/拖拽/捏合/陀螺仪/VR）；PlayerView 按 is_360 优先路由，360 照片直接进 pano 模式
- **分享页内嵌全景**：SharePublicView 点击 360 媒体直接球面渲染（`auth=none` 免鉴权）；密码分享的 HLS ts 切片逐请求补挂 `?password=`（hls.js 不继承 master 查询串的潜伏缺陷，普通视频播放器同类问题一并修复）；`is360` 判定改 `m.is_360`（修 type==='360' 失效契约）
- **flipY 朝向修正**：SphereGeometry 顶部 UV v=1=图片顶行 → flipY 统一 true（原视频路径 false 是合成素材期未暴露的潜伏缺陷）；截图实证天顶朝上
- **验证**：测试服 headless Chrome（CfT linux64）+ CDP 脚本四场景截图全过：主站 360 照片/视频、分享 360 照片（免密）、分享 360 视频（密码全链）
- **工程教训**：同文件多 Edit 并行竞态已两次丢编辑（vite build 无感）——必须串行+grep 核验；web 镜像只 COPY dist，须先本机 build 再同步服务器重建

**Job000008 要点（9/04，74c1b7c）**：
- **TLS 架构**：新增 `pano-caddy` 容器（caddy:2-alpine）静态挂载 `*.warlocks.cn` 泛域名证书（Let's Encrypt，来此加密渠道手动签发），443 反代 `web:80`；80 端口宝塔 nginx 不动，:8088 直连保留
- **域名**：`panomint.warlocks.cn`（主站）/ `photo.warlocks.cn`（分享页）均 A 记录 → 192.168.1.115；本机 curl 双域名 https=200、证书链校验通过
- **服务器 DNS 坑（本次超时根因）**：服务器首选 DNS 192.168.1.1 无响应、114DNS 对该记录 SERVFAIL → Chrome `DNS_PROBE_FINISHED_NXDOMAIN`，四场景全挂同一错误页且 ssh_exec 5 分钟读取超时；修法：`/etc/hosts` 固定两条记录（内网域名内网直连，最小侵入）
- **验证**：HTTPS 通道四场景 headless Chrome 回归全绿——`secure: true`（isSecureContext 解锁）×4、canvas 全 true、密码分享全链通过、截图与 HTTP 基线一致；后台 nohup + 日志轮询规避 ssh 读取超时
- **证书运维**：2026-10-13 到期；换新后替换 `docker/caddy/certs/` 内文件并 `docker compose restart caddy`；certs 目录已 gitignore

**Job000009 要点（9/11，ad971c9）**：
- **后端**：`/geo/clusters`（bbox+zoom 网格聚合+时间范围过滤，provider=amap 输出 GCJ-02）、`/geo/items`（bbox 内媒体摘要）、`/geo/histogram`（bbox 内时间分布）、`/tiles/amap/{z}/{x}/{y}`（Key 服务端注入+**磁盘缓存**治高德 429）；**视口 bbox 是 GCJ-02，查询前在后端反变换回 WGS-84**（否则偏移数百米）
- **前端**：MapLibre GL v6 全屏地图（`MapView.vue`）+ `MapTimeline.vue`（直方图+拖拽框选时间范围）+ `MapItemList.vue`（点击簇展开媒体卡片）；左侧导航新增「地图」入口（/map）；双向联动：视口变化→重算聚合+时间轴，框选时间→过滤聚合点
- **验证**：headless Chrome 三场景全绿——地图渲染（canvas+统计+12 bars）/ 点击簇下钻展开（"7 个位置 · 9 项"→列表 2 项）/ 时间轴框选过滤（33→10 项）；瓦片缓存实测 miss→hit
- **工程教训（maplibre v6 三连坑）**：① v6 GeoJSON worker 靠 import.meta.url 相对路径，vite 打包后缺失→图层**静默不渲染**（vite plugin 复制 worker 到 public + `setWorkerUrl` 修复）；② nginx .mjs 默认 octet-stream 被 module worker 拒绝（`default_type application/javascript` 修复）；③ v6 纯 ESM 无 default 导出，用具名导入
- **代码基线**：`main` @ `ad971c9`；**测试服**：八容器全 Up + tilecache 卷；库内 72 媒体（60 带 GPS）

**Job000009 优化（9/11，1064aba，用户追加四需求）**：
- **时间轴可缩放滑块**：年/月/日三档粒度切换（−/＋控件），直方图平滑重排，框选区间随粒度重算
- **四类媒体实时统计**：照片/视频/全景照片/全景视频四张统计卡片，随缩放粒度+框选+视口动态更新；后端 histogram 单查询返回四类计数（`type×is_360` 条件聚合）
- **图标可配置**：默认红点；矢量形状（圆/三角/菱形/五角星）+ 色卡选色 + 内置 PNG（图钉/倒三角）+ 上传自定义 PNG；**账户级持久化**（新增 user_preferences 表 + GET/PUT /preferences/map，跨设备同步）
- **悬停/长按即时预览**：鼠标悬停（120ms 防抖）即弹本层级媒体预览卡（地名+3缩略图+四类计数）；触摸长按（500ms，移动>10px 判定平移取消）；预览窗按触点四向翻转防溢出
- **验证**：headless 四场景全绿——粒度缩放（月13→日21→年3 bars）/ 四类统计（照片24·视频8·全景照片3·全景视频1，与 DB 吻合）/ 图标面板（symbol 图层+icon-image）/ 悬停预览（hover-card）；后端偏好 PUT→GET 持久化通过
- **数据增强**：给 8 视频+3 全景照片+1 全景视频补北京周边 GPS（原 60 条带 GPS 全是普通照片），四类齐全便于演示

**代码基线**：`main` @ `a06a387`，GitHub `warlocks365/Panomint` 同源；DB 迁移 **v13**。
**测试服**：192.168.1.115 **八容器**全 Up（+pano-caddy），web 绑 :8088 / caddy 绑 :443；库内 72 媒体（360 共 4 已带元数据，**HLS 9/9**，全部 72 带 GPS）；CfT headless 验证环境 + verify360.py / verifyMap.py / verifyMapOpt.py / verifyMapOpt2.py / verifyMapOpt3.py / verifyMapOpt4.py / verifyMapOpt5.py。
**Token 累计**：**25,745.96**（Job000007+000008 = 2,794.55 + Job000009 五轮追加 = 2,603.85 已入账；本轮回退待用户报数）。

**⚠️ 大模型切换**：2026-09-02 用户由 Kimi-K3 切至 Hy4 preview。已出**阶段性快照**存档：
`文档/阶段性快照_Job000005节点.md`（含回滚指引、部署拓扑、数据状态、契约事实、坑位红线、Token 台账）。后续 Job 如需回滚，读该文件即可从 Job000006 精确续做。

**待办**：Stage 4 AI 语义搜索（GPU 就绪，候选 Job000010）/ 数据迁移扫描器（P1，注意 scan -dir 路径坑）；地图模式真机抽查（左侧「地图」入口，等用户手机实测）；云端 DDL 勘误三项待接受；真机验证三项未回收 + 360 球面真机抽查（两条测试分享保留中）。

---

## 五、云端资源链接

| 资源 | 链接/路径 |
|------|-----------|
| 前序成果空间 | https://www.workbuddy.cn/space/s/GYevaAWJjCPr9DmNPqESZL |
| 云端协作空间 | https://www.workbuddy.cn/space/s/GYevaAWJjCPr9DmNPqESZL |
| 本地工作区 | C:\Users\warlocks\WorkBuddy\全景相册系统项目\ |
