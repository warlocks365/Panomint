# Panomint 发布工程（v1.0.0 起）

本目录是三种部署形态发布包的**唯一构建入口**。版本号唯一真源 = 仓库根 `VERSION`
（规则见 `文档/版本管理规范.md`），产物命名内嵌版本号，可追溯、可复现。

## 三种形态

| 形态 | 产物 | 适用场景 |
|---|---|---|
| **裸机一键安装** | `panomint-<V>-linux-amd64-baremetal.tar.gz` | 独立 Linux 服务器：脚本自动装依赖（PG16+PostGIS+pgvector/Valkey/ffmpeg）、建库建号、装 systemd 单元并启动 |
| **集成运行环境包** | `panomint-<V>-linux-amd64-bundle.tar.gz` | 面板/NAS/无 systemd 环境：单目录自包含（二进制+web+AI 资产），`panoctl` 统一启停守护，不碰系统目录 |
| **Docker 镜像** | `release/docker/`（compose + build-images.sh） | 容器化部署：镜像 `panomint/app:<V>` / `panomint/web:<V>` / `panomint/db:<V>` |

## 构建

```bash
bash release/build.sh          # 需要 Docker；产出在 release/out/
```

- 前端构建在 `node:22-alpine` 容器、后端在 `golang:1.26-bookworm` 容器内完成，
  与宿主机工具链解耦，任何装了 Docker 的机器上构建结果一致（可复现）；
- 后端二进制经 `-ldflags -X panoalbum/internal/version.*` 注入版本三元组，
  `curl http://<host>/version` 可验证产物版本；
- 运行期 AI 资产（chinese-clip 模型 / 人脸模型 / ONNX Runtime x64 库）打包进
  前两种形态；**构建前**须先 `bash scripts/fetch-all-assets.sh`（资产不入库）。

## 验收惯例（每次发布必做）

1. `bash -n` 全部脚本 + 产物内文件清单核对；
2. 裸机/集成包在干净的 `debian:12` 容器内实装冒烟（见 Job000106 验证记录）；
3. Docker 形态 `build-images.sh` 构建 + compose 拉起 `/ready`、`/setup/status` 冒烟；
4. 三形态 `/version` 输出均须等于 `VERSION` 文件值。
