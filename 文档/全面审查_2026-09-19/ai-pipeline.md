# 审查分卷：AI/媒体管线域

审查基线：HEAD=61838f8 ｜ 范围：internal/{embed, faces, tags, index, transcode, ortx, ffmpeg, geo, search, phash} + cmd/{embedgen, facesgen, taggen, phashgen, indexctl, transcodectl, watchctl, genmedia, nodeagent, mapproto, migrate, migrateplan}

核查方法：先读登记簿（§一~§二十七）收敛已知项；再逐文件亲读上述范围内的全部生产源码（约 1.1 万行，仅 `*_nocgo.go` 占位文件与测试文件未逐行读），每个发现均以 文件:行号 验证；危险模式（动态 SQL 占位符错位、`go func`、子进程管理、rows 生命周期、time.Sleep、panic、TODO）已全量 Grep 扫描。

登记簿已录、本卷不重复报告的项：E1/E4（人脸召回与增量用例）、Job000041（多尺度，卡 ORT 环境）、可见性谓词收敛（Job000020，搜索侧与媒体侧口径差异已登记）、pHash 对视频为弱标识（固有局限）、`geo/cluster.go:38` 动态表名（仅 mapproto 用，已登记）、`ListMediaByTag` 畸形 id 500（已登记）、缩略图"同帧"语料属性结论。

## 统计

| P0 | P1 | P2 | 合计 |
| --- | --- | --- | --- |
| 2 | 4 | 7 | 13 |

## P0 发现（逐条）

### [P0-01] ORT 推理的互斥锁位置错误：共享输入张量在锁外写入，并发下查询结果张冠李戴

- 位置：
  - src/backend/internal/embed/clip_cgo.go:267-300（`EncodeText`：269 行 `dst := e.textIn.GetData()`、273-286 行写入，**288 行才 `e.muText.Lock()`**）
  - src/backend/internal/embed/clip_cgo.go:324-346（`EncodePixels`：332 行 `copy(dst, px)`，**334 行才 `e.muVision.Lock()`**）
  - src/backend/internal/faces/embed.go:98-119（`EmbedAligned`：106 行 `copy(dst, px)`，**108 行才 `r.mu.Lock()`**）
- 描述：三个推理入口都是「先写共享输入张量 → 再加锁 → Run」。锁只串行化了 `Run()`，输入准备却在锁外。两个 goroutine 交错时（A 写输入 → B 写输入 → A 锁并 Run），A 拿到的是 B 输入算出的向量。同包的 `Detector.Detect`（detect.go:188-199）就是**先锁后写**的正确写法，证明此处是疏漏而非设计。
- 影响：**线上可触发**。API 进程内 `embed.Encoder` 是跨请求共享单例（cmd/api/main.go:436 装配进 `search.VectorRecaller`），两个并发 `GET /search?q=...` 会让一方拿到对方查询词的语义召回结果（结果仍过可见性谓词，不构成越权，但返回错误结果）。这也是 Go 数据竞争（`-race` 可复现）。`EncodePixels`/`EmbedAligned` 目前只在单线程 worker（embedgen/facesgen）中使用，属同一根因的潜在缺陷，应一并修。
- 建议：把 `Lock()` 移到首次触碰共享张量之前（Lock → 写输入 → Run → 拷输出 → Unlock），三处同改；为 `EncodeText` 补一个并发回归测试（两个 goroutine 分别编码不同文本，断言各自输出与串行结果一致）。

### [P0-02] faces.bbox 坐标空间不统一：「原图优先」重扫时命名迁移 IoU 跨坐标系比较 ≈ 0，用户命名静默丢失

- 位置：
  - src/backend/internal/faces/scan_source.go:46-77（图源二选一：原图或 LG 缩略图）
  - src/backend/cmd/facesgen/main.go:287-364（`scanOne`：`det.Detect(img)` 的坐标**未做任何归一化**直接 `SaveFace`）
  - src/backend/internal/faces/match.go:54-71（`BestFaceMatch` 用 IoU 把旧库内 bbox 与新检出匹配）
  - src/backend/internal/faces/store.go:103-130（`FacesByMedia` 读出的是**上一次扫描图源**的坐标）
- 描述：Job000032 把图源改为「原图优先、LG 回退」后，`faces.bbox` 存的是**当次扫描图源的像素坐标**——喂 4K 原图就是 4K 坐标，回退 LG 就是 1280 坐标，列里没有记录用的是哪个坐标系。重扫时 `BestFaceMatch` 拿「本次图源坐标」与「上次入库坐标」直接算 IoU：只要两次图源分辨率不同（NAS 挂载状态变化、或 Job000032 之前的 LG 存量数据在升级后首次走原图重扫），IoU ≈ 0 < MatchMinIoU(0.5) → 命名迁移全部落空。
- 影响：触发条件 = 任一媒体原图宽 ≠1280（这正是 `MediaRoot` 机制存在的意义）+ 重扫（`POST /ai/faces` scope=all 或 `-force`）。后果是 `person_id` 静默丢失：已命名的人变成未命名新簇，用户命名劳动不可逆消失。附带：`MinFacePx` 在 `Detect` 内按当次图源坐标判定（detect.go:219-227），而 24px 是按 LG(1280) 标定的（face.go:136-158 注释自述"判定在 LG 缩略图坐标系"），喂原图时阈值语义同样漂移（24px@4K ≈ 8px@LG，过滤变松）。当前语料原图也是 1280 宽，故线上暂未发作——这是**潜伏**缺陷，在真正接入高清原图时爆发。
- 建议：二选一——(a) `scanOne` 入库前把 Detection 归一到固定坐标系（LG 坐标或 0..1 归一化坐标），`MinFacePx` 在同一坐标系判定；(b) faces 表增加 `src_width/src_height`（或 source 枚举）列，迁移匹配与 minpx 判定前按比例换算。配套：为「LG 旧数据 × 原图重扫」写一条命名迁移回归测试。

## P1 发现（同上格式）

### [P1-01] embedgen 失败不收敛：无"已尝试"标记，坏缩略图在每个 sweep 永久重试

- 位置：src/backend/internal/embed/store.go:46-75（`ListPending` 仅判 `embedding IS NULL`）；src/backend/cmd/embedgen/main.go:287-312（`sweepOnce` 失败只记日志）
- 描述：embedding 是唯一没有扫描标记列的管线。对照：phash 有 `phash_scanned_at`（失败也回填，phash/store.go:90-93）、faces 有 `faces_scanned_at`、tags 有 `tags_scanned_at`——三者都明确解决过"坏行永久占位"问题，embed 是漏网的那个。
- 影响：一条缩略图损坏/格式不支持的媒体，每 30s 被重试一次并刷一条失败日志，永不收敛；待办 >200 时还长期占据 `ListPending` 的 LIMIT 窗口。
- 建议：加 `media.embed_scanned_at`（迁移 + `MarkScanned`），`sweepOnce` 对确定性失败（解码失败）回填标记；或沿用 phash 语义「失败也回填、`-force` 重算」。

### [P1-02] CreateJob 入队失败留下僵尸 pending 行，且永久阻断该媒体的自动补排

- 位置：src/backend/internal/transcode/transcode.go:149-164（`CreateJob`：INSERT 后 Enqueue 失败仅返回 500）；对照 src/backend/internal/transcode/enqueue.go:82-98（`enqueueOne` 入队失败会 `DELETE` 回滚）
- 描述：同一件事两份实现，CLI 路径会回滚任务行，API 路径不会。留下的 `status='pending'` 行有两个后果：① `missingHLSQuery`（enqueue.go:14-24）的 `NOT EXISTS (... status IN ('pending','running'))` 会把该媒体永久排除在自动补排之外；② `JobStatus` 对该 job 永远返回 pending。
- 影响：Valkey 短暂故障期间用户手动发起的转码会变成永远无法恢复的死任务，需要手工清库。
- 建议：`CreateJob` 复用 `enqueueOne`（把"写行+入队、失败回滚"收敛为一份），或同事务/同事后补偿删除。

### [P1-03] ffmpeg.Task 的 Done 帧在持锁状态下阻塞发送：订阅者失联 → 整个任务死锁

- 位置：src/backend/internal/ffmpeg/runner.go:271-284（`broadcast`：`t.mu.Lock()` 内对 Done 帧执行 `ch <- p` 阻塞发送）
- 描述：中间帧用非阻塞发送（丢帧保活），Done 帧为"终帧必达"改为阻塞发送——但仍持有 `t.mu`。若某订阅者缓冲满且不再读取（HTTP 客户端断开、消费 goroutine 退出），`scanStdout` 永远卡在 broadcast 持锁状态 → `finish()` 拿不到 mu → `t.done` 永不关闭 → `Wait()` 永不返回、`Cancel()` 阻塞、4 个 goroutine 泄漏。进程被杀也解不开（scanStdout 仍卡在锁内）。
- 影响：当前生产路径无人订阅（仅测试与预留 API），但 `SubscribeProgress` 是导出接口，是 nodeagent 进度上报的自然落点——一旦接上就可能触发任务挂死。
- 建议：发送前先在 mu 内快照 subs 切片、解锁后再发送（Done 帧可接受短暂的锁外阻塞），或为 Done 帧发送加超时兜底。

### [P1-04] facesgen 多实例并发扫描同一媒体：DELETE+INSERT 非原子 → 人脸行翻倍、簇分裂

- 位置：src/backend/cmd/facesgen/main.go:326-372（`scanOne`：`DeleteFacesByMedia` → 逐张 `SaveFace` → `MarkScanned`，无事务、无跨实例互斥）
- 描述：单实例内"先删后插"保证幂等，但两个 facesgen 并发（常驻 watch + 运维手动 `-mode scan`，或误起两副本）处理同一媒体时，交错「A删→B删→A插×n→B插×n」会得到 2n 张人脸；且两实例的 `NearestClusters` 互相看不见对方正在建的簇，同一人会被分成两个簇。对照：embed（UPDATE）、tags（upsert）、phash（UPDATE）天然幂等不受此影响，faces 是四条清扫管线里唯一 DELETE+INSERT 的。
- 影响：人脸数据重复累积（正是 scanOne 注释自述"已修复"的那类事故在并发维度的回归），且簇划分被污染后需要全量重扫才能恢复。
- 建议：`scanOne` 的 delete+inserts+mark 包进单事务，并对 `media_id` 取 `pg_advisory_xact_lock`（或部署层硬约束单实例 + 文档明示）；补一个并发扫描的集成测试。

## P2 发现（同上格式）

### [P2-01] 推理完全串行 batch=1，无真批量

- 位置：src/backend/internal/embed/clip_cgo.go:205（`visIn` 固定 `[1,3,224,224]`）、src/backend/cmd/embedgen/main.go:199-220（逐张 `EncodeImage`）
- 描述：CLIP 视觉塔张量与会话按 batch=1 绑死，encode/watch 逐张推理。GPU 场景吞吐损失一个数量级；CPU 场景影响有限。
- 建议：如需提速，编码路径支持 batch=N 张量（reshape 输入 + 循环取输出切片）；当前家庭相册规模可不动，建议仅登记。

### [P2-02] 三档缩略图三次独立 ffmpeg 调用，源解码 ×3

- 位置：src/backend/internal/index/worker.go:93-99（SM/MD/LG 循环各跑一次 `ffmpeg.New(args).Run`）
- 描述：每次调用都重新解码源文件（视频还要重新 seek 到 1s 处）。可单进程多输出（一条命令三个 `-map` + 三个输出）。
- 建议：缩略图参数构造支持多输出；收益随源文件增大而增大，当前可接受。

### [P2-03] NearestClusters 每张脸一次全表 GROUP BY + avg(embedding)，总量 O(F²)

- 位置：src/backend/internal/faces/store.go:180-213
- 描述：每张新检出人脸都对 faces 全表按 cluster_id 分组现算质心并排序取 top5，无法走向量索引。F 张脸的全量重扫是 F 次全表扫描。当前 93 媒体/数十脸无感；万级人脸时成为瓶颈。
- 建议：规模化时物化簇质心表（clusters(cluster_id, centroid, updated_at)，触发器或写入侧维护），质心检索走 HNSW；当前规模仅登记。

### [P2-04] 瓦片磁盘缓存对一切 200 响应永久生效，无 TTL 无内容校验

- 位置：src/backend/internal/geo/tile.go:127-148（非 200 不缓存，但 200 即写盘且永久命中）
- 描述：上游在 Key 失效/配额异常时可能以 200 返回错误占位图或 XML 错误体，会被当作瓦片永久缓存（`X-Tile-Cache: hit` 后不再回源）。
- 建议：写缓存前校验 `Content-Type` 为图片（且可 sniff PNG/JPEG 魔数），或给缓存加 TTL/失败标记。

### [P2-05] 搜索排序的 taken_at NULL 口径与游标不闭合（潜伏）

- 位置：src/backend/internal/search/store.go:161（`orderBy := "m.taken_at DESC, m.id DESC"`）、:143-150（游标行比较 `(m.taken_at, m.id) < ($n,$m)`）
- 描述：PG 的 DESC 默认 NULLS FIRST，而游标行比较遇到 NULL 求值为 NULL（即"不成立"）——taken_at 为 NULL 的行只在第 1 页可能出现，翻页后被永久排除（超过一页 NULL 行即丢数据）。同包 ListPending 系列均已写 `NULLS LAST`，此处口径不一致。当前 insertMedia 有 mtime 兜底（index/index.go:83-86），taken_at 实际非空，故为潜伏缺陷。
- 建议：与 faces/embed/tags 的 ListPending 对齐显式写 `NULLS LAST`，并在 DDL 层考虑 `taken_at` 设 NOT NULL 默认值（治本）。

### [P2-06] 工具函数与 worker 骨架重复

- 位置：
  - `L2Normalize`/`CosineSimilarity`：src/backend/internal/embed/embed.go:127-158 与 src/backend/internal/faces/face.go:309-341（逐字相同两份）
  - `vectorLiteral`/`parseVector`：embed/store.go:127-139、faces/face.go:344-376、tags/labelcache.go:256-268（三份，精度策略不同是有意的，但可共享一个带精度参数的实现）
  - `atoi`/`atof`/`firstNonEmpty`：faces/face.go:378-395 与 index/metadata.go:288-296 等
  - 清扫式 watch 主循环骨架：cmd/{embedgen,facesgen,phashgen,taggen}/main.go 四份近同构（ticker + sweepOnce + 信号退出）
- 建议：向量小工具收敛到一个 `internal/vecutil`（或并入 ortx）；watch 骨架抽一个 `sweep.Loop(interval, fn)` helper。优先级低，仅在下次改动这些文件时顺手收敛。

### [P2-07] 命令行默认值指向测试数据目录

- 位置：src/backend/cmd/transcodectl/main.go:50-52（`-mediaroot` 默认 `./testdata/media`）、src/backend/cmd/indexctl/main.go:91（`-thumbdir` 默认 `./testdata/thumbnails`）
- 描述：生产侧工具把测试数据路径作为默认值，忘传参时会静默读写错误目录（与 nodeagent「不含任何默认路径」的红线精神相悖，nodeagent/main.go:20-23）。
- 建议：改为必填或默认空 + 启动校验。
