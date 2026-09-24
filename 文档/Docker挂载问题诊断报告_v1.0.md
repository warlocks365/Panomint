# Docker 挂载问题诊断报告（Job000111）

> 触发场景：群晖 NAS（DSM Container Manager）部署，compose 将 `/volume1/photo` bind mount 为容器 `/data/media`。
> 现象：① 相册无法识别映射进容器的照片/视频；② 相册「文件夹」功能新建的文件夹未出现在 NAS 映射目录。
> 结论速览：① 的主根因是**产品缺口**——应用没有任何「自动扫描 / 用户触发扫描」入口（文件在挂载目录里 ≠ 入库可见）；② 是**设计语义**——文件夹是库内虚拟目录（`folder_dirs` 纯 DB 记录），本就不创建物理目录。两者均已在测试服以代码事实 + 部署事实双向核验。

---

## 1. 原因分析（四方向）

### 方向一：挂载路径配置是否正确 —— 配置本身正确，但「路径对 ≠ 能看到」

群晖 compose（`release/docker/docker-compose.synology.yml`）将 `/volume1/photo:/data/media` 挂到 api + 4 个 AI worker + 2 个重活 worker，共 7 处，`MEDIA_ROOT=UPLOAD_DIR=/data/media`。bind mount 是覆盖式语义：容器内 `/data/media` 完全等于 `/volume1/photo`，配置层面没有路径错位。

**但关键事实：文件对容器「可见」与对应用「可见」是两回事。** 应用展示媒体只认数据库 `media` 表里的行（可见性唯一真源 `internal/mediascope`）；文件躺在挂载目录里而没有 scan 入库，时间轴就是空的。这不是挂载失效，是**索引入库从未发生**。

### 方向二：文件权限与属主 —— 容器内是 root，读一般无碍，但有两处隐患

- api / worker 镜像均未声明 `USER`（`docker/api/Dockerfile`、`docker/worker/Dockerfile` 全文无 USER 指令），容器进程 = root。群晖 Container Manager 默认不做用户命名空间映射，root 读 `/volume1/photo` 通常无障碍。
- 隐患 A（写）：上传落盘在 `UPLOAD_DIR/yyyy/mm/` 下新建目录并写文件；若群晖侧目录开了 ACL 限制或只读挂载，上传会以 500 失败（落盘报错路径有明确报错）。
- 隐患 B（扫描归属）：`indexctl scan` 把媒体归属到种子用户 `owner@pano.local`（`internal/index/index.go:19,147-153`）。恰好本人账号就是这个邮箱时无感；**其他邮箱注册的部署会出现一个无人能登录的种子用户持有全部扫描媒体**——属主错位的第二事故源。

### 方向三：文件系统兼容性 —— 无格式层障碍

- 支持的扩展名（`internal/index/scanner.go:29-39`）：`.jpg .jpeg .png .webp .insp`（照片）、`.mp4 .mov .insv .lrv`（视频），共 8 种；其余文件静默跳过。
- 扫描会跳过群晖缩略图 sidecar 目录 `@eaDir`（`scanner.go:85`，防污染媒体库）。
- NAS 存储格式（ext4/btrfs）与 Docker bind mount 无兼容性问题；BTRFS 的 `@eaDir` 已由扫描器豁免。
- 唯一兼容性风险：照片若是 `.heic` 等未支持格式，会被静默跳过（表现为「部分照片不显示」而非全部）。

### 方向四：媒体扫描机制 —— 根因所在

入库只有三条路径，全部核实如下：

| 路径 | 触发方式 | 归属 | 现状 |
|---|---|---|---|
| HTTP 上传 | 前端上传 → `ingest` 落盘即入库 | 上传者本人 | 正常（但文件落 `yyyy/mm/` 日期目录，与用户「文件夹」无关） |
| `indexctl scan -dir <dir>` | **仅 worker 镜像内的 CLI**，无任何 HTTP/API 触发 | 种子用户 owner@pano.local | 存在但用户无入口（见下） |
| F4 网络挂载（storagectl reconcile 循环） | index-worker 容器常驻轮询 `storage_mounts` 表，挂载→复制→导入 | 挂载属主 | 只服务「应用内登记的远程挂载」，对「照片本来就在本机挂载目录」的群晖场景不适用 |

三条结构性缺口（代码事实，均已核验）：

1. **api 启动不扫描 MEDIA_ROOT**——`cmd/api/main.go` 除 AI 人脸扫描端点外无任何 `Indexer.Scan` 调用；
2. **初始化向导不做扫描**——`internal/setup/setup.go` 只创建 owner 账号（Job000107 范围不含导入）；
3. **api 镜像不含 `indexctl`/`storagectl`**——`docker/api/Dockerfile` 只编译 api/embedgen/taggen/facesgen/migrateplan/phashgen 六个二进制；扫描 CLI 在 worker 镜像。且《用户操作手册 v1.0.0》全文无 scan 步骤，群晖 compose 注释也未提示。

**判定：现象① = 主根因「产品缺口」（无扫描入口）+ 可能叠加次根因（HEIC 等格式不支持 / 权限写失败）。**

**文件夹不同步的定性**：`POST /folders` 只执行 `INSERT INTO folder_dirs (path, owner_id)`（`internal/folders/manage.go:75-91`），**不创建物理目录**；上传时 `folder_path` 也只是 DB 归类字段，文件物理落点恒为 `yyyy/mm/`（`internal/media/upload.go:420-498`）。因此「新建文件夹不出现在 NAS 目录」是设计内行为（与 Job000056 WebDAV 虚拟目录一脉相承），不是挂载故障；但它与用户「文件夹 = NAS 真实目录」的心智模型冲突，构成体验缺口。

---

## 2. 系统性排查思路（宿主机 → 容器 → 应用）

> 每步给出验证命令与判断标准。命令按群晖 Container Manager 场景书写（容器名以 `panomint-` 前缀为例）；测试服已实测等效命令。

### 第 0 层：宿主机（NAS）

| 步骤 | 命令 | 判断标准 |
|---|---|---|
| 0.1 源目录存在且有媒体 | `ls -la /volume1/photo \| head` | 列出照片/视频文件 |
| 0.2 源目录权限 | `ls -ld /volume1/photo` | 容器内 root 读取无忧；若计划容器内非 root 运行需 o+rx |
| 0.3 文件系统类型 | `df -T /volume1/photo` | ext4/btrfs 均可；注意到期/只读标志 |

### 第 1 层：容器（挂载是否生效）

| 步骤 | 命令 | 判断标准 |
|---|---|---|
| 1.1 挂载声明核对 | `docker inspect panomint-api --format '{{json .Mounts}}'` | 存在 `/volume1/photo → /data/media`、RW=true 的 bind 条目 |
| 1.2 容器内可见性 | `docker exec panomint-api ls /data/media` | 能看到 0.1 的同名文件——**通则挂载生效，断点直接到方向一/四** |
| 1.3 容器内可写性（上传前置） | `docker exec panomint-api touch /data/media/.wtest && docker exec panomint-api rm /data/media/.wtest` | 成功 = 上传可写；Permission denied = 权限根因（方向二） |
| 1.4 各 worker 挂载一致性 | `docker inspect panomint-index-worker --format '{{json .Mounts}}'` | 与 api 一致（缩略图 worker 要读原文件出图） |

### 第 2 层：应用（是否入库 / 为何不入库）

| 步骤 | 命令 | 判断标准 |
|---|---|---|
| 2.1 媒体表计数 | `docker exec panomint-db psql -U pano -d pano_album -tAc "SELECT count(*) FROM media WHERE deleted_at IS NULL"` | 0 = 从未扫描（主根因坐实）；>0 但仍不可见 → 走 2.3 |
| 2.2 触发一次扫描（worker 镜像内 CLI） | `docker exec panomint-index-worker /usr/local/bin/indexctl scan -dir /data/media` | 输出 `total=N inserted=M duplicate=K failed=F`；failed>0 逐个看日志行 |
| 2.3 归属与可见性 | `psql ... -tAc "SELECT email, count(*) FROM media JOIN users ON users.id=media.owner_id GROUP BY 1"` | 出现 owner@pano.local 之外的属主 = 种子用户错位（方向二隐患 B） |
| 2.4 队列与缩略图 | `docker exec panomint-valkey valkey-cli LLEN q:media:waiting` | 扫描后应有缩略图任务；消费后 thumbnails 目录出图 |
| 2.5 浏览器复测 | 时间轴/文件夹页刷新 | 扫描入库后应可见；仍不可见抓 F12 Network 具体请求响应 |

**排查决策树**：1.2 不通 → 方向一（挂载配置）；1.3 不通 → 方向二（权限）；2.2 failed>0 → 拉单条错误（格式/元数据）；2.2 全 skipped → 方向三（扩展名不支持）；2.2 成功但前端不可见 → 可见性/归属问题（2.3）。

---

## 3. 修复方案（按根因分类）

### R1 主根因：补「本地目录导入」用户入口（产品缺口，推荐立项）

三个可选档位（按改动量从小到大）：

- **R1-a 文档/编排层热修（立即可做，零代码）**：群晖 compose 注释 + 用户手册补「部署后执行一次 `docker exec panomint-index-worker /usr/local/bin/indexctl scan -dir /data/media`」。成本最低，但入口仍是 CLI，普通 NAS 用户够不着。
- **R1-b API 加一个管理端扫描端点（推荐）**：`POST /admin/scan {dir}`（校验 dir 必须落在 MEDIA_ROOT 之内防目录穿越），内部调用 `Indexer.ScanAs(ctx, dir, callerUID)`——归属当前管理员而非种子用户，顺带修掉方向二隐患 B。前端管理后台加「媒体库 → 扫描导入」按钮 + 进度展示（index_jobs 表已有任务行，可直接读）。
- **R1-c 启动可选自动扫描**：api 启动时对 MEDIA_ROOT 做一次增量扫描（hash 去重幂等，可反复执行）。对「照片目录与应用同机」是终极体验，但启动时间变长、失败面变大，建议作为 R1-b 的后续增强而非替代。

### R2 权限根因（方向二，条件触发）

- 上传 500 且日志含 `permission denied`：统一容器运行用户并给 NAS 目录开写——要么容器保持 root（现状，简单），要么 compose 加 `user:` 指令并 `chown -R` 媒体目录；推荐后者符合最小权限。
- 种子用户错位（扫描归属非本人）：随 R1-b 一并消解（ScanAs 用真实调用者）。

### R3 格式根因（方向三，条件触发）

- 若诊断发现 HEIC/RAW 等被静默跳过：短期在扫描输出里加「跳过 N 个不支持的文件」计数与样例（现在静默），长期评估 heic 解码（libheif 引入需过许可审查）。

### R4 文件夹语义缺口（体验层，非故障）

- 短期：文件夹创建/重命名 UI 加一句明示「文件夹为应用内分类，不在磁盘创建目录」。
- 中期（可选产品决策）：给 folder_dirs 增加「物理目录模式」开关——开启后 Create/Rename 同步 mkdir/mv（仅对 MEDIA_ROOT 内的本地目录启用，网络挂载保持虚拟语义）。**此项涉及产品定位，需用户裁决后再立项。**

---

## 4. 下一步（Job000112，另见 e2e 报告）

在测试服从零重建与群晖同构的隔离环境（bind mount 宿主媒体目录），端到端验证：已有媒体识别、上传回写映射目录、文件夹/相册 CRUD、重启持久化。测试结果与通过/失败清单将单独成文。
