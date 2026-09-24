# Docker 挂载问题 e2e 测试报告（Job000112）

> 环境：测试服 192.168.1.115 隔离栈 `pano-e2e`（`/home/warlocks/pano-e2e/compose.yml`，Web 端口 18088）。
> 构型与群晖 compose 同构：`./media`（宿主机 bind）→ 容器 `/data/media`（模拟 `/volume1/photo`），其余数据走命名卷。
> 镜像：`warlocks/panomint-app:1.0.0` + `worker:1.0.0` + `db:1.0.0` + `web:1.0.1`（与 NAS 用户升级后状态一致）。
> 测试媒体：3 JPEG + 1 MP4（ffmpeg 合成）+ 1 个不支持文件（.txt）+ 1 个 @eaDir sidecar，共 6 文件。
> 与既有 11 容器开发栈完全隔离，互不干扰。

## 测试结果汇总

| # | 测试项 | 结果 | 关键证据 |
|---|---|---|---|
| 1 | 挂载生效：容器可见宿主机媒体文件 | ✅ 通过 | api 容器 `ls /data/media` 完整列出种子目录（手机照片/相机/文档/@eaDir） |
| 2 | 挂载可写（上传前置） | ✅ 通过 | 容器内 touch/rm `.wtest` 成功 |
| 3 | 初始化向导全流程 | ✅ 通过 | `setup/status` false → POST /setup 201（owner@pano.local）→ 库内用户行就位 |
| 4 | 现象①复现：挂载生效但媒体不识别 | ✅ 复现 | 初始化后 `media` 表计数 = **0**（文件可见但零入库） |
| 5 | 已有照片/视频识别（`indexctl scan` 后） | ✅ 通过 | scan：total=4 inserted=4 failed=0；.txt 与 @eaDir 正确跳过；DB 4 行 folder_path 正确（手机照片/2023、相机/DCIM…）；API 列表返回 4 条含缩略图名 |
| 6 | 缩略图生成 | ✅ 通过 | index-worker 消费后 `/data/thumbnails` 产出 SM/MD/LG 三档 WebP |
| 7 | 上传新照片并回写映射目录 | ✅ 通过 | POST `/media/upload` 201（id 返回）；宿主机 `media/2026/09/<rand>_upload_me.jpg` 实体落盘（双向 bind 传播实证）；API 立即可见（status=indexing） |
| 8 | 文件夹新增/重命名/删除 | ⚠️ 应用内全通过；**NAS 侧不同步（设计内）** | 创建 201 / 重命名 200（moved_folders=1）/ 删除 200；宿主目录全程无物理目录——与诊断报告 R4 定性一致，非缺陷 |
| 9 | 相册创建/加图/改名/删除 | ✅ 通过 | 创建 201 → 加入 2 图（added=2）→ 详情含 items → 改名 204 → 删除 204 → 列表空 |
| 10 | 容器重启后挂载与数据 | ✅ 通过 | `compose down + up` 全重建后：media 表 5 行不变、宿主 7 文件不变、挂载可见、登录正常、5 条媒体 API 全列出、15 个缩略图文件仍在卷上、缩略图 HTTP 200 |
| 11 | 视频 HLS | ➖ 未触发（预期内） | scan 只入缩略图队列；HLS 转码需前端显式发起（产品设计），非挂载问题 |

## 结论

**挂载机制本身零缺陷**：bind mount 双向传播、读写、跨重建持久化全部正常。NAS 用户两个现象的解释均被验证：

1. **「照片无法识别」= 缺扫描入口**（产品缺口，诊断报告 R1）。复现-修复闭环：`media=0` → `indexctl scan` → 全部可见。
2. **「文件夹不出现在 NAS 目录」= 虚拟目录设计语义**（诊断报告 R4），应用内 CRUD 本身完好。

## 修复建议（承接诊断报告）

| 优先级 | 项 | 内容 |
|---|---|---|
| P1 | R1-b | `POST /admin/scan` 管理端扫描端点 + 前端「扫描导入」入口（归属真实调用者，消除种子用户语义） |
| P1 | R4-短期 | 文件夹 UI 明示「应用内分类，不在磁盘建目录」 |
| P2 | R1-c | 启动可选自动扫描（hash 幂等） |
| P2 | R4-中期 | 「物理目录模式」开关（需用户裁决产品定位） |

## 环境处置

e2e 栈保留运行供查验（宿主机目录 `/home/warlocks/pano-e2e/`，端口 18088）。处置命令：
```bash
cd /home/warlocks/pano-e2e && docker compose down      # 停栈（数据保留在命名卷+bind 目录）
cd /home/warlocks/pano-e2e && docker compose down -v   # 连同命名卷一起清（⚠️ bind 目录 ./media 不删）
```
