# Panomint vs PhotoPrism vs Immich —— 全面功能对比报告

| 项 | 内容 |
| --- | --- |
| 版本 | v1.0（研究稿，**未实施任何改动**） |
| 日期 | 2026-10-04 |
| 对标版本 | PhotoPrism `260919-28c46a116` / Immich `v3.2.x` |
| 方法 | Panomint 侧为**本机代码 + 线上容器实测**；另两侧为**官方文档 + GitHub issue/release 核实**（每条带来源） |
| 方法论说明 | 我没有靠「印象」对比 —— 先派两名研究员各自攻一侧（要求每条结论带链接、区分官方声明与社区反馈），再自己实测 Panomint。三方数据来源可追溯。 |

---

## 〇、三个必须先纠正的前提

对比前，我核实了三条**常见但已过时**的认知。它们直接影响结论方向：

| 常见认知 | 核实结果 | 影响 |
| --- | --- | --- |
| 「PhotoPrism 有 CLIP 语义搜索」 | ❌ **没有**。#1287 长期 open，实际靠 caption/label 关键词 | PhotoPrism 在 AI 上**弱于**预期，不是标杆 |
| 「Immich 已经放弃 InsightFace」 | ❌ **仍在用**（`antelopev2/buffalo_l` 等模型），维护者明确「无收益更换」 | Panomint 用 YuNet+SFace 规避授权风险 = **合规优势** |
| 「Immich 是移动优先，Web 较弱」 | ⚠️ **部分反转**。v2.5.0 Web 先有编辑器，v3.0.0 移动端才跟上统一；管理/ML 配置 Web 独占 | Immich 的 Web 能力被低估，但**部署门槛**（6GB RAM/4 容器/x86-64-v2）仍是硬伤 |

---

## 一、总体定位差异

| 维度 | Panomint | PhotoPrism | Immich |
| --- | --- | --- | --- |
| **一句话** | 自研**全景**相册（360 原生） | 通用照片管理（AI 检索见长） | 移动优先的家庭相册 |
| 技术栈 | Go 1.26 + Vue3 + PG/PostGIS/pgvector + Valkey + 11 容器 | Go + Vue3 + **MariaDB**（PG 长期未落地） | Flutter + Nuxt + PG + **VectorChord** + ML 容器 |
| 部署门槛 | **4 核纯 CPU 可跑**（实测 115） | 最低 2 核/3GB | **6GB RAM 起步**、4 容器、amd64 需 x86-64-v2 |
| 许可 | 自有 | AGPL-3.0（⚠️ 官方 `:latest` 镜像是 Plus License，非纯 AGPL） | AGPL-3.0 |
| 中文能力 | ✅ **Chinese-CLIP 原生**（本仓自研） | ⚠️ 界面 i18n 有中文，AI 模型无中文专项 | ⚠️ 需选 nllb/siglip2 才支持中文搜索 |
| 360 支持 | 🏆 **唯一原生支持**（含 360 视频工作流） | ✅ 图片级（`.insv`/`.insp`/fisheye DNG） | ❌ **完全不支持**（`.insv` 只播第一轨） |

**一句话结论**：Panomint 在 **360 全景 + 中文 AI + 轻量部署 + 许可干净** 四个维度已占位，但在 **检索广度、工程完整度、生态** 上落后。

---

## 二、能力矩阵总表

图例：✅ 有 ｜ 🟡 部分/弱 ｜ ❌ 无 ｜ — 未查到

### 2.1 媒体格式与处理

| 能力 | Panomint | PhotoPrism | Immich |
| --- | --- | --- | --- |
| 常规照片（JPG/PNG/WebP） | ✅ | ✅ | ✅ |
| 视频（MP4/MOV） | ✅ | ✅（容器/编码覆盖更广：MKV/AVI/MXF/AV1/VVC…） | ✅ |
| **360 视频（equirect）** | 🏆 ✅ **原生工作流**（转码→HLS→three.js 贴球） | ❌ 不支持（会畸变，按普通图显示） | ❌ 不支持（`.insv` 只播第一轨） |
| **360 原片（`.insv`/`.insp`/fisheye DNG）** | 🟡 识别为 360 但**未做去畸变拼接**（见专节） | ✅ **v360 去畸变已实机验证**（X3/X4 单文件未做） | ❌ 无 |
| RAW（DNG/CR2/NEF/ARW…） | ❌ | ✅ 30+ 厂商格式（Darktable/RawTherapee） | 🟡 可存可看，**无显影** |
| HEIC / HEIF | 🟡 待核实 | ✅ 原生 reader（260523 起） | ✅（v3.1 修了 HEIF Orientation） |
| Live Photo | ❌ | ✅ | ✅ |
| HDR 播放 | ❌ | ❌ | ❌（v3.0 标注 not implemented） |
| PDF / ZIP | ❌ | ✅（内联 PDF 阅读器 + ZIP 导入） | ❌ |
| 缩略图层级 | 🟡 sm/md/lg 三档 | ✅ **20+ 档，最大 16K（15360×8640）** | ✅ 多档 |

### 2.2 AI / 机器学习

| 能力 | Panomint | PhotoPrism | Immich |
| --- | --- | --- | --- |
| **语义搜索（CLIP 向量）** | ✅ **已实现**（`VectorRecaller`），支持 **Chinese-CLIP ViT-B/16** | ❌ **不支持**（#1287 open） | ✅ 支持（nllb/xlm/siglip2，需选型） |
| 物体/场景标签 | ✅ tag-worker | ✅ TensorFlow NASNet + **Ollama/OpenAI 多模态 LLM**（`vision.yml` 声明式编排） | 🟡 CLIP **不生成可见标签**（FAQ 明确） |
| 人脸检测 | ✅ YuNet（ONNX） | ✅ YuNet（720px，抗遮挡/侧脸） | ✅ SCRFD |
| 人脸识别/聚类 | ✅ **SFace**（128 维）+ DBSCAN | ✅ SFace + 完整 CLI（index/audit/migrate/status） | ✅ 识别 + 增量聚类，**跨用户 cluster group**（v3.2） |
| 人脸管理 UI | 🟡 有 PeopleView | ✅ Private/Hidden 人物、合并拆分 | ✅ 设生日（显示年龄）、隐藏、置顶 |
| **OCR** | ❌ | ❌（#907，维护者明确不在 roadmap） | ✅ **PP-OCRv5**（Web 搜索 + 移动端高亮复制） |
| 去重 | ✅ pHash | ✅ SHA1 + **stacks**（同 basename/序号/同秒同位置/XMP InstanceID） | ✅ **两套**：SHA-1 字节级 + CLIP 相似度分组 |
| 去重时的元数据合并 | ❌ | 🟡 stacks 切 primary | ✅ **自动合并**名称/描述/相册/收藏/评分/位置/标签 |
| NSFW 检测 | ❌ | ✅ | ❌ 官方承认无 |
| 画质评分 | ❌ | 🟡 **规则式**（非美学模型） | ❌ |
| AI 编排可配置性 | 🟡 硬编码 | 🟢 **声明式 `vision.yml`**（Type×Engine×RunMode×阈值） | 🟡 Workflows（预览版） |

### 2.3 检索与浏览

| 能力 | Panomint | PhotoPrism | Immich |
| --- | --- | --- | --- |
| 关键词搜索 | ✅ pg_trgm 模糊 | ✅ | ✅ |
| 过滤维度数 | 🟡 **约 12**（q/tag/person/date/place/type/favorite + geo 半径/直距） | 🏆 **约 70+**（含 `panorama`/`fisheye`/`diff:`感知哈希/`near:`/S2/OLC/包围盒/18 色/chroma/相机镜头…） | 🟡 约 15（人脸/地点/OCR/标签/文件名/路径/描述/相机/时段/类型/归档/收藏/评分） |
| 查询语言 | 🟡 空格 AND | 🟢 完整（`&`/`|`/`!`/通配/转义） | 🟡 多过滤器 AND/OR |
| 语义 + 文本混合排序 | ✅ `buildWhere` 有 scoreExpr | ❌ | ✅ |
| 时间线 | ✅ | ✅ Calendar + **Moments**（地点+标签+人脸自动聚合）+ `near:` | ✅ 虚拟滚动 |
| 地图 | ✅ 高德瓦片 + cluster | 🟢 6 套地图 + cluster + **3D Earth** + terrain 3D | ✅ 视口内资产（v3.2） |
| 地图 heatmap | ❌ | — 未证实 | — 未证实 |
| 360 球面查看器 | 🏆 ✅ 照片贴图 + **视频自由视角** | ✅ 拖拽/缩放/视频内视角 | 🟡 仅 360 图片（Web），视频无 |
| 原图 vs 编辑后对比 | ❌ | 🟡 下载原图 | 🟡 下载可选原图 |
| 存储配额 | ❌ | — | ✅ per-user GiB |

### 2.4 管理与协作

| 能力 | Panomint | PhotoPrism | Immich |
| --- | --- | --- | --- |
| 相册 | ✅ | ✅ album/folder/month/state/moment 五类 | ✅ + Folder View |
| 收藏/归档 | ✅ 收藏 | ✅ favorite + private + archived | ✅ |
| 回收站 | ✅ 软删 + 恢复 | ✅ Archive→Delete 两段式 | ✅ 默认 30 天可调 |
| 公开分享 | ✅ 密码 + max_views | ✅ 密码/过期/次数/评论/编辑/**签名下载** | ✅ 密码/过期/EXIF 开关/下载开关/上传开关 |
| **共享相册（多人协作）** | ❌ | ❌ | ✅ **Owner/Editor/Viewer 三级** |
| Activity（评论/点赞） | ❌ | 🟡 仅分享链接可开评论 | ✅ 相册+单图 |
| 伴侣分享 | ❌ | ❌ | ✅ |
| 标签管理 | ✅ | ✅ 加/删/改名/收藏/按大类搜 | 🟡 **仅 Web 可创建**（移动端 No） |
| 批量操作 | ✅ | ✅ 批量编辑对话框 | ✅ 批量元数据端点 |
| 多用户/角色 | ✅ | 🟡 **部分付费**（User/Viewer 需 Essentials，LDAP 需 Team） | ✅ |
| **WebDAV 服务端** | ❌ | ✅ 挂 `/originals` 与 `/import` | ❌ |
| 远程 WebDAV 同步 | ❌ | ✅（ownCloud 互同步） | ❌ |
| **外部库（只读挂 NAS）** | 🟡 有 `dirscope`/`watch` 但非 Immich 式的只读外部库 | — | ✅ glob 排除 + 定时扫描 + **每日自动** |
| 存储模板 | ❌ | — | ✅ make/model/lens 变量 |
| XMP sidecar | ❌（元数据在 DB） | ✅ JSON + YAML + XMP 三种 | ✅ `.xmp` + DB |
| 完整性检查 | ❌ | ✅ `index --cleanup` | ✅ **三类报告**（untracked/missing/checksum） |

### 2.5 编辑

| 能力 | Panomint | PhotoPrism | Immich |
| --- | --- | --- | --- |
| 旋转/裁剪 | ✅ 非破坏（`media.edits` JSONB） | ❌ **完全无像素编辑** | ✅ 非破坏 + 跨端统一 |
| 镜像 | ❌ | ❌ | ✅ |
| 调色/滤镜 | ❌ | ❌ | ❌（v3.0 **移除**了重着色） |
| RAW 显影 | ❌ | 🟡 经 Darktable/RawTherapee 后**可看** | ❌ |
| 视频编辑 | ❌ | ❌ | ❌ |
| **元数据编辑** | ✅ **刚上线**（Job000143：时间/拍摄地/详细地址/GPS） | ✅ 极广（title/caption/copyright/keywords/labels/people/files/quality/private…） | ✅ |

### 2.6 API 与工程

| 能力 | Panomint | PhotoPrism | Immich |
| --- | --- | --- | --- |
| REST API | ✅ 136 路由 + OpenAPI 契约 v1.2 | ✅ `/api/v1` + Swagger | ✅ OpenAPI + `@immich/sdk` + 端点状态标记 |
| **API 稳定性** | ✅ **契约锁定 v1.2**，变更需同步文档 | ⚠️ **无弃用政策**（官方自述「路由与参数可能变化」） | ⚠️ **同 minor 内回退都不支持**，每次 major 几十处破坏性变更 |
| WebSocket 实时 | ❌ | ✅ 按 ACL 授权的 topic 订阅 | ✅ |
| CLI | ✅ 一批 `cmd/*ctl` | ✅ 20+ 子命令 | ✅ `immich upload` |
| MCP（Agent 友好） | ✅ **已实现**（`/agent/llm/v1` + `/admin/agent/cmd`） | 🟡 极窄（2 个只读工具） | ❌ |
| i18n | 🟡 单一中文 | ✅ 多语言（部分靠机器翻译） | ✅ 多语言 |
| 无障碍 | 🟡 部分 | 🟡 部分（260728 起有独立分区） | 🟡 部分 |
| 主题/暗色 | ✅ 晨雾 Morandi 设计系统 | ✅ 16+ 预置主题 | ✅ |

---

## 三、Panomint 的真实差距（按严重度排序）

### 🔴 P0 —— 会被用户直接感知的缺失

| # | 差距 | 现状 | 影响 |
| --- | --- | --- | --- |
| **1** | **360 原片无法正确播放** | 识别为 360 但**不做去畸变拼接**，直接贴球 → 画面是鱼眼畸变的 | 这是 Panomint 的**核心定位**。PhotoPrism 已实机验证可行（`v360` 滤镜）；X 系列双流布局 PhotoPrism 也没做，但**我们可以做**（Immich 明确拒绝跟进） |
| **2** | **无 OCR** | 完全缺失 | 搜「pizzeria」能命中没写过 caption 的店招照片。**Immich 已有（PP-OCRv5，Apache-2.0 无授权风险）**，PhotoPrism 明确不做 → 这是**可独占的差异化** |
| **3** | **无移动端** | 纯 Web | Immich 的最大护城河。但 Panomint 定位是 Web/桌面全景，**这是取舍不是缺陷**（需明确产品决策） |
| **4** | **RAW 完全不支持** | 无 | 摄影用户的核心诉求。PhotoPrism 支持 30+ 厂商格式 |

### 🟡 P1 —— 明显影响竞争力的差距

| # | 差距 | 现状 | 借鉴对象 |
| --- | --- | --- | --- |
| 5 | **检索维度太窄**（12 vs PhotoPrism 70+） | 缺感知哈希相似（`diff:`）、邻近（`near:`）、相机/镜头、S2/OLC 空间索引、颜色 | PhotoPrism 的**无界手输查询语言**性价比极高（低代码量、高功能密度） |
| 6 | **无存储配额** | 多用户无配额概念 | Immich |
| 7 | **无共享相册与 Activity** | 分享只有公开链接 | Immich（Owner/Editor/Viewer） |
| 8 | **无 WebDAV** | NAS 用户无法把原图挂载为网络盘 | PhotoPrism |
| 9 | **无完整性检查** | 文件系统与 DB 可能漂移 | Immich（三类报告） |
| 10 | **无存储模板 / Storage Label** | — | Immich |
| 11 | **无数据库备份恢复界面** | 只有定时 pg_dump | Immich v2.5.0 才补上（说明是真需求） |

### 🟢 P2 —— 锦上添花

| # | 差距 | 备注 |
| --- | --- | --- |
| 12 | 去重无元数据合并 | Immich 的自动合并规则（偏大文件+EXIF 丰富）值得抄 |
| 13 | 无 Memories（年度回顾） | 纯查询逻辑，无需新模型，成本低感知强 |
| 14 | 无智能相册/智能回忆 | **Immich 也在 roadmap**（标 Soon），跟进即可领先 |
| 15 | 人脸管理 CLI 弱 | PhotoPrism 有完整 `faces` CLI（index/audit/migrate/status） |
| 16 | 无 16K 缩略图档位 | 360 深度缩放需要 |
| 17 | 无 AI 编排声明式配置 | PhotoPrism 的 `vision.yml` 思路 |

---

## 四、Panomint 的差异化优势（应重点保持/强化）

| 优势 | 说明 | 竞品对照 |
| --- | --- | --- |
| 🏆 **360 视频原生工作流** | 照片贴图 + **视频内自由视角**（three.js 贴球 + HLS） | Immich ❌ 完全不支持；PhotoPrism ❌ 图片级 |
| 🏆 **中文 AI 原生** | Chinese-CLIP ViT-B/16 自研集成 | 两者都需额外选型/无专项 |
| 🏆 **许可干净** | YuNet+SFace（无 InsightFace 授权风险） | Immich 仍在用 InsightFace（授权风险未解除） |
| 🏆 **API 契约锁定** | OpenAPI v1.2，变更需同步文档 | 两者均无弃用政策，Immich 破坏性变更频繁 |
| 🏆 **轻量部署** | **4 核纯 CPU 可跑**（本项目红线：不依赖 GPU 节点） | Immich 6GB RAM + 4 容器 + x86-64-v2 |
| 🏆 **Agent 友好** | MCP + `/agent/llm/v1` + `/admin/agent/cmd` | PhotoPrism 仅 2 个只读工具；Immich 无 |
| 🏆 **安全纵深** | 审计表 + 可见性单一真源 + 7 项代码审查基线 | 两者均无同等审计深度 |

---

## 五、建议的优先级路线

### 第一梯队（差异化护城河，强烈建议）

| 项 | 理由 | 依赖 |
| --- | --- | --- |
| **① 360 原片去畸变（`v360`）** | 核心定位缺口；PhotoPrism 已验证可行；Immich 明确拒绝跟进 → **可独占** | FFmpeg v360（115 实测可用）；异步转码队列（已有） |
| **② OCR 检索（PP-OCRv5）** | 两者都缺或不做；**Apache-2.0 无授权风险**；实现成本最低的高价值 AI | 引入 PP-OCRv5 模型（需评估体积） |
| **③ 360 深缩放（16K 缩略图档位）** | 配合 ①，360 深度缩放必须 | 存储成本随尺寸平方增长 |

### 第二梯队（补齐竞争力）

| 项 | 理由 |
| --- | --- |
| ④ 检索维度扩展（优先 `diff:` 感知哈希 + `near:` + 相机/镜头） | 低代码量、高功能密度，直接对齐 PhotoPrism |
| ⑤ 存储配额 + 存储模板 | 多用户/合规部署的必要件 |
| ⑥ 完整性检查 | 运维差异化，代码量小 |
| ⑦ 数据库备份恢复界面 | Immich v2.5.0 才补，说明是真需求 |

### 第三梯队（视产品定位决定）

| 项 | 判断 |
| --- | --- |
| ⑧ RAW 支持 | **投入大**（Darktable 绑定 + 存储 + 耗时）。需先确认目标用户是否有摄影 RAW 需求 |
| ⑨ 移动端 | **不建议**。与 Panomint 的 Web/桌面全景定位冲突，且是 Immich 的主场 |
| ⑩ 共享相册 + Activity | 家庭场景才需要；先确认是否有此类用户 |
| ⑪ WebDAV | NAS 用户强需求，实现成本中等（`golang.org/x/net/webdav`） |

### 明确不跟进

| 项 | 理由 |
| --- | --- |
| GPU 加速 v360 | 违反本项目「算力节点不写入软件功能」红线；且 PhotoPrism 也明确不做 |
| 陀螺仪防抖（flowstate） | 需影石 MediaSDK 商业授权；v360 做不到 |
| 滤镜/调色 | PhotoPrism 无、Immich 刚移除；非全景相册核心 |
| 迁移到 VectorChord | pgvector 足够通用，降低运维与升级风险 |
| 引入 InsightFace | 授权风险；现有 YuNet+SFace 已覆盖 |

---

## 六、许可与借鉴红线（重要）

PhotoPrism 与 Immich **均为 AGPL-3.0**。Panomint 若非 AGPL：

- ❌ **不可**复制/链接其 Go、Vue、Dart 源码（含 `internal/ffmpeg/v360.go`、搜索过滤器、缩略图命名规则、Worker 清单）
- ✅ **可**自由借鉴**事实性工程知识**：
  - FOV 标定方法论（按存储鱼眼圆盘实际张角取值、用接缝连续性跨角度扫描定最优值）
  - FFmpeg `v360` 滤镜参数语法（FFmpeg 是 LGPL/GPL，独立于两个 AGPL 项目）
  - 投影类型命名约定（equirectangular/fisheye/dual-fisheye 是通用词汇）
  - 设计思路（投影值脱敏的双字段设计、静态/动态双尺寸上限、异步降级策略）
- ⚠️ **PhotoPrism 的 `:latest` Docker 镜像是「Plus License」而非纯 AGPL** —— 想研究源码要用 `:ce` tag
- ⚠️ **两家均有不可自建的专有外部依赖**：PhotoPrism 的 `geocoding.photoprism.app` + MapTiler 瓦片（官方承担费用）；Immich 的 `tiles.immich.cloud`
- ✅ **不受限的外部组件**：FFmpeg、Darktable、RawTherapee、ExifTool、libvips、libheif、YuNet/SFace、**PP-OCRv5（Apache-2.0）**、Ollama（MIT）、CLIP/SigLIP（各自许可需逐一确认）

**建议**：独立实现 + 自用实拍样本标定，避免任何代码来源污染。

---

## 七、需要注意的三个数据时效问题

1. **Immich 每月发版**，能力表变化很快。本报告基准是 `v3.2.x`（2026-09-10），若要对标需固定版本快照。
2. **InsightFace 授权现状未能证实**。官方无任何风险缓解声明，但这影响 Panomint 的合规叙事价值，建议直接向 Immich 官方渠道确认而非依赖二手资料。
3. **PhotoPrism 社区有对标签质量与人脸聚类的负面评测**（非官方，需谨慎引用）；官方 260919 的人脸模型升级正是针对聚类问题，实际效果需自行验证。

---

## 附：数据来源

**Panomint 侧**（本机实测）：
```
后端包 34 个 · HTTP 路由 136 条 · 前端视图 16 个
AI worker 4 个全部运行：embed / faces / phash / tag
线上数据：faces 92 · tags 434 · people 1
media 表 AI 列：embedding(向量) / faces_scanned_at / tags_scanned_at / phash / embed_scanned_at
语义检索：VectorRecaller（已接线）· 模型 clip(OpenAI ViT-B/32) + chinese-clip(Chinese-CLIP ViT-B/16)
人脸：YuNet 检测 + SFace 128 维识别
格式支持（索引器）：.jpg .mp4 .mov .insv .insp .lrv
编辑：rotate + crop（非破坏，media.edits JSONB）
分享：password + max_views
OCR / WebDAV / RAW / 移动端 / 共享相册：无
```

**PhotoPrism 侧**（官方文档 + GitHub）：
- [file-formats](https://www.photoprism.app/kb/file-formats/) · [video](https://docs.photoprism.app/user-guide/organize/video/) · [panoramas](https://docs.photoprism.app/user-guide/organize/panoramas/)
- [AI 文档](https://docs.photoprism.app/user-guide/ai/) · [filters 完整参考](https://docs.photoprism.app/user-guide/search/filters/)
- [#5711 360 原片去畸变](https://github.com/photoprism/photoprism/issues/5711) · [#5771 X 系列未做](https://github.com/photoprism/photoprism/issues/5771) · [#1287 CLIP 未做](https://github.com/photoprism/photoprism/issues/1287) · [#907 OCR 不在 roadmap](https://github.com/photoprism/photoprism/issues/907)
- [advanced 设置（缩略图/动态缩放）](https://docs.photoprism.app/user-guide/settings/advanced/) · [webdav](https://docs.photoprism.app/user-guide/sync/webdav/) · [API 弃用政策](https://docs.photoprism.app/developer-guide/api/#deprecation-policy)

**Immich 侧**（官方文档 + GitHub）：
- [支持格式](https://docs.immich.app/features/supported-formats) · [searching](https://docs.immich.app/features/searching) · [facial-recognition](https://docs.immich.app/features/facial-recognition)
- [v3.0.0 release](https://immich.app/blog/v3.0.0-release) · [v3.2.0 release](https://immich.app/blog/v3.2.0-release) · [v2.5.0 release（编辑器）](https://immich.app/blog/v2.5.0-release)
- [discussion #5130 .insv 只播第一轨](https://github.com/immich-app/immich/discussions/5130) · [discussion #25851 InsightFace 未更换](https://github.com/immich-app/immich/discussions/25851)
- [部署要求](https://docs.immich.app/install/requirements) · [升级指南（破坏性变更）](https://docs.immich.app/install/upgrading) · [API 文档](https://api.immich.app/introduction) · [roadmap](https://immich.app/roadmap/)
