# 全景相册系统 · 续作交接与 P0 任务计划

> 生成日期：2026-08-24
> 基于：README + overview + 工作日志 + TDD v1.1 + DDL v1.1 + API v1.1
> 目标：新电脑环境无缝衔接，避免重复劳动

---

## 一、项目全貌认知

### 1.1 项目本质

100% 自研、自托管的全景相册系统，定位为**群晖 Synology Photos 增强替代方案**。用户拥有群晖 DS1819+（Intel Atom C3538，无 GPU），存有大量 H.265/H.264 全景视频。

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

- **存储与算力分离**：DS1819+ 仅做存储，转码/AI 跑在独立 GPU 节点（本地 GPU / 云 GPU / 局域网第三方 GPU 主机，通过 Compute Node agent 接入）
- **独立后台账户体系**：不对接 DSM/目录账户，自建账户 + RBAC
- **100% 自研** = 自写前后端/AI/转码，不 fork 成品，仅复用通用开源库

### 1.4 技术栈（已定稿）

| 层 | 技术选型 | 版本 |
|----|----------|------|
| 后端 API | Go | 1.22+ |
| AI 推理 | Python + FastAPI | 3.13 |
| 数据库 | PostgreSQL + PostGIS + pgvector | PG 16 / PostGIS 3.4 / pgvector 0.7 |
| 对象存储 | MinIO（或 SeaweedFS） | - |
| 前端 | Vue3 + Vite | 3.x |
| 地图 | MapLibre GL JS | - |
| 360/VR | Three.js / A-Frame | - |
| 自适应码率 | hls.js | - |
| 转码 | ffmpeg（subprocess）+ NVENC | 6.x |
| 消息队列 | BullMQ + **Valkey** | - |
| 部署 | Docker Compose → K8s | - |
| 反代 | Caddy（自动 HTTPS） | - |

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
├── docker-compose.yml              # Valkey + PG + MinIO 服务编排
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
Step 1: 创建 docker-compose.yml（Valkey + PG16+PostGIS + MinIO）
  → 启动验证：docker-compose up，确认三服务均 healthy

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

## 四-补、当前进度快照（2026-08-26 更新）

**已完成**：Phase 0-3 全部（合规前置、后端骨架、双原型、P0 完整化六任务）；Phase 4 Stage 1（相册+智能相册+两级评论）、Stage 2（微信 H5 分享）、#7（Worker 容器化）；**Stage 3 结构化搜索（Job000001：GET /search + pg_trgm 中文检索 + 地点地理降级 + 响应式搜索 UI）**；七项遗留问题全部闭环。
**进行中（Job000002 三路并行）**：① GitHub 同步 + 搜索真机联调部署；② P1 数据迁移工具（fsnotify 增量扫描器）；③ Pico4 WebXR 验证准备。
**代码基线**：GitHub `warlocks365/Panomint` main（Stage 3 已并入）。
**Token 累计**：18,478.30（含 Job000001 实测 465.45）；追加额度 5,000 已批准。
**待办**：云端勘误接受、系统联调验收、部署上线、Stage 4 AI 语义搜索（待 GPU 节点）。

---

## 五、云端资源链接

| 资源 | 链接/路径 |
|------|-----------|
| 前序成果空间 | https://www.workbuddy.cn/space/s/GYevaAWJjCPr9DmNPqESZL |
| 云端协作空间 | https://www.workbuddy.cn/space/s/GYevaAWJjCPr9DmNPqESZL |
| 本地工作区 | C:\Users\warlocks\WorkBuddy\全景相册系统项目\ |
