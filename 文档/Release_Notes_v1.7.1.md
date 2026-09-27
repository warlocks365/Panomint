# Panomint v1.7.1 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.7.1**（修订号 +1：缺陷修复） |
| 发布日期 | 2026-09-27 |
| 分支 | main（VERSION bump 与 git 标签 `v1.7.1` 随本文件同批提交推送） |
| 版本管理规范 | 《版本管理规范.md》；对应主 Job：Job000129 |
| 前置诊断 | 《6GB 全景视频播放失败诊断与 Range 改造评估_v1.0.md》（复现实锤：全量 blob 请求 8ms 即缓冲预分配失败） |

---

## 一、更新摘要

v1.7.1 修复「关闭 HLS 转码后，GB 级大视频回退播放必失败」的缺陷：此前回退链路用 XHR 全量下载原始文件转 blob，5.3GB 测试视频在 Chromium 缓冲预分配阶段直接失败（`net::ERR_FAILED`，传输未开始）。本版本将回退播放改为 **Range 流式**——视频数据按段拉取，GB 级视频即时起播、拖动秒级定位，浏览器内存占用从 GB 级降至几十 MB。

### 1.1 修复

| 模块 | 内容 |
| --- | --- |
| 回退播放 Range 流式 | 无 HLS 的 360° 视频与普通视频回退均由全量 blob 改为直链流式：`video.src` 直指 `GET /media/:id/download?at=<短时令牌>`，浏览器原生 `Range: bytes=…` 分段拉取（服务端 `Accept-Ranges: bytes` 能力原已在位，下载端点零改动） |
| 鉴权 query 通道 | `AuthRequired` 新增白名单收窄的 query token 分支：仅 `GET /media/` 读路径接受 `?at=`（与 Bearer 等权校验）；写路径与全部非媒体端点照旧只认 Authorization 头，防 URL 泄露重放 |
| HEVC 编码检测 | 回退前经 `canPlayType` 检测浏览器解码能力：HEVC 源文件在不支持的浏览器（如默认 Chrome）给出显式提示「请开启 HLS 转码或换用支持的浏览器」，不再笼统报「加载失败」 |
| 错误透出 | 播放加载失败文案透出 HTTP 状态码（如「加载失败（401）」），排查更快 |
| 播放错误兜底 | 直链模式下 video 元素加载失败（断流/令牌过期）弹出明确错误层，不再静默黑屏 |

### 1.2 兼容性与质量

- **零数据库迁移、零 API 契约变更**（纯行为修复 + 鉴权通道扩展）
- 后端单测 7/7、API e2e 7/7、UI e2e 11/11、HLS 回归 4/4 全绿（详见登记簿 §二十七 Job000129）
- 验证实录：1.2GB H.264 测试视频 `bytes=0-` 起播 206 分段、seek 50% 触发 `bytes=580157440-` 精确分段、120 秒播放内存峰值 10MB

---

## 二、部署指引

### 2.1 Docker 镜像版（Docker Hub 公开仓库）

```bash
# 群晖 DSM：Container Manager 使用 release/docker/docker-compose.synology.yml（已指向 :1.7.1）
# 通用 Docker：
docker pull warlocks/panomint-web:1.7.1
docker pull warlocks/panomint-app:1.7.1
# worker / db 同名同理；编排中 image tag 改为 1.7.1（或 latest）后 up -d
```

镜像 digest 核验（2026-09-27 Hub 实测，本机 Registry API 独立查询与推送日志逐字一致，每镜像 1.7.1 = latest）：

运行版本自检：登录后设置页「版本信息」卡显示 `v1.7.1`；或 `curl http://<host>:8088/version` 返回 `{version: "1.7.1", ...}`。

### 2.2 裸机 / 集成包

需要时按《版本管理规范.md》§4.6 由源码复现构建：`release/build.sh` 读取仓库根 `VERSION`（当前 1.7.1）产出三形态发布包。

### 2.3 镜像 digest 三方核验（发布当日 Hub 实测）

| 镜像 | 1.7.1 = latest digest |
| --- | --- |
| warlocks/panomint-app | `sha256:d368589e…`（异于 1.7.0 `d4a1c241…`） |
| warlocks/panomint-web | `sha256:acae7da2…`（异于 1.7.0 `509ed338…`） |
| warlocks/panomint-worker | `sha256:5d96d37c…`（异于 1.7.0 `7191e6ed…`） |
| warlocks/panomint-db | `sha256:fdc2262f…`（与 1.7.0 相同——db 层本版未变更） |

---

## 三、升级注意事项

1. **从 1.7.0 升级**：拉取 `:1.7.1`（或 `:latest`）镜像重建 web 与 app 容器即可；无迁移、无配置变更。api + 常驻 worker（embed/tag/faces/phash）共用 app 镜像，需一并 recreate；index/transcode worker 镜像独立，本版未变更可不重建。
2. **HEVC 源文件**：若相机直出 HEVC（H.265）视频且在 Chrome 播放提示编码不支持，属浏览器能力边界——开启「自动 HLS 转码」或点播放页「立即转码」即可（转码产物为 H.264 多档流）；Safari 与具备硬解的 Edge 可直接播放。
3. **降级回滚**：image tag 换回 `1.7.0` 重建即可；无数据结构差异。
