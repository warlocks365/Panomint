# Panomint Docker 形态（v1.0.0）

三种发布形态之一：容器化部署。镜像自包含（app 镜像已固化 AI 模型与 ONNX Runtime），
`compose up -d` 即得完整系统，**首次访问自动进入初始化向导**（无需手工建库建号）。

## 快速开始

```bash
cd release/docker
cp .env.example .env          # 填 POSTGRES_PASSWORD 与 JWT_SECRET
docker compose up -d          # db/valkey/api/4 个 AI 进程/web
# 缩略图与 HLS 转码队列消费（媒体量大时建议启用）：
docker compose --profile workers up -d
# TLS 入口（自动 ACME，需公网域名）：
docker compose --profile edge up -d

curl http://<主机IP>:8088/version        # 验证版本 = .env 的 PANOMINT_VERSION
```

浏览器打开 `http://<主机IP>:8088` 完成初始化向导。

## 镜像构建（打包机/CI 上执行）

```bash
bash build-images.sh           # 需要完整仓库 + 资产（fetch-all-assets.sh 已跑）
```

| 镜像 | 内容 |
|---|---|
| `panomint/app:<V>` | api + embedgen/taggen/facesgen/phashgen/migrateplan（CGO，含 CLIP/人脸模型 + ORT） |
| `panomint/worker:<V>` | indexctl/transcodectl/storagectl + ffmpeg/rclone/fuse |
| `panomint/db:<V>` | PostgreSQL 16 + PostGIS 3.4 + pgvector |
| `panomint/web:<V>` | nginx 托管前端 dist + API 同源反代（6+2 前缀规则全内嵌） |

离线分发：`docker save panomint/app:<V> panomint/worker:<V> panomint/db:<V> panomint/web:<V> | gzip > images-<V>.tar.gz`，
目标机 `docker load` 后直接用本 compose。

## 升级

```bash
docker compose pull || true        # 自建镜像则先在打包机 build + save/load
docker compose up -d               # api 启动自迁移（幂等），滚动重建
```

数据全在命名卷（`pgdata`/`mediadata`/`valkeydata`），升级不触碰。
**严禁 `docker compose down -v`**（会销毁全部媒体，运维红线）。
