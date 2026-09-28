# Panomint v1.7.3 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.7.3**（修订号 +1：功能增强） |
| 发布日期 | 2026-09-28 |
| 主 Job | Job000132（视频批处理流水线：进度实时化 / 视频位置信息 / 快速处理模式 / 断点续跑） |
| 数据库迁移 | **00044**（index_jobs 加 current_file 列；AUTO_MIGRATE 启动自动执行） |

---

## 一、更新摘要

面向**大量 4K 视频入库**的批处理流水线增强，覆盖「抽帧 → 缩略图 → 向量提取 → 标签生成」全链路：

### 1.1 扫描进度实时可见（修复 0/0 假死）

- **根因修复**：旧版扫描在遍历阶段就逐文件读取全部内容计算哈希（4K 视频数 GB/个），「总数」要等全部读完才写入——期间任务页长时间显示 0/0，观感如卡死。新版遍历只做轻量清点（秒级），总数立即可见，哈希计算移至逐文件处理阶段，**每处理一个文件进度即刷新**。
- **正在处理的文件实时显示**：任务详情透出 `current_file`，管理后台扫描面板显示「已处理 N / M · 正在处理 xxx.mp4」。

### 1.2 视频位置信息自动提取（地图可见）

- 解析视频元数据中的 GPS 载体：`location`（ISO 6709，iPhone/DJI/安卓主流）、`com.apple.quicktime.location.ISO6709`（苹果）、`comment` 内嵌的 DJI 遥测坐标；
- 配置了高德 Key 时，有 GPS 的视频**自动反查可读地名**（place）；
- 入库后**地图按视频拍摄位置展示**（与照片同口径）。

### 1.3 快速处理模式与断点续跑（低配设备适配）

- **重复扫描秒级完成**：已入库文件按路径快速跳过（不再重读文件算哈希）；跨路径的同内容文件仍走哈希去重，正确性不变；
- **断点续跑**：扫描中断（进程重启）后残留任务自动标记「已中断」，**重新扫描自动续跑**——已入库的按路径跳过、只补新文件，绝不重复；
- **跳帧解码开关**：`INDEX_THUMB_FAST=1` 时缩略图抽帧只解关键帧（`-skip_frame nokey`），纯 CPU 低配 NAS 上 4K HEVC 抽帧速度提升数倍（画质取关键帧，足够缩略图用途）。

### 1.4 质量与兼容

- 新增视频 GPS 解析单测（ISO 6709 各形态 / DJI XML / 越界拒绝 / 优先级）；全量 go test 与前端构建全绿；
- 115 测试服端到端实测：3 个测试视频（2 个带 GPS + 1 个无 GPS）扫描入库 → 进度 3/3 → 缩略图/向量/标签 100% 就绪 → `gps_b` 的坐标自动反查地名「上海市黄浦区南京东路街道…」→ 无 GPS 的正确留空。

## 二、部署指引

```bash
# 群晖：docker-compose.synology.yml 已指向 :1.7.3；通用：tag 改 1.7.3（或 latest）
docker pull warlocks/panomint-app:1.7.3
docker pull warlocks/panomint-index-worker:1.7.3   # 本版 index-worker 镜像有变更，必须重建
```

**升级注意**：
1. 无破坏性配置变更；迁移 00044 由 api 启动自动执行（仅加一列）；
2. **index-worker 镜像本版有变更**（进度与 GPS 逻辑），重建时务必一并 recreate index-worker；
3. 低配设备建议在 index-worker 环境变量加 `INDEX_THUMB_FAST=1`；
4. 从 1.7.2 回滚：tag 换回 `1.7.2` 重建即可（current_file 列冗余无害）。

## 三、镜像 digest（发布当日 Hub 实测，回填）

| 镜像 | 1.7.3 = latest digest |
| --- | --- |
| warlocks/panomint-app | 待回填 |
| warlocks/panomint-index-worker | 待回填 |
| warlocks/panomint-worker | 待回填 |
| warlocks/panomint-web | 待回填 |
