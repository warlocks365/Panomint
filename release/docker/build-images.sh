#!/usr/bin/env bash
# build-images.sh —— 构建 Docker 形态发布镜像并打版本 tag（v1.0.0，Job000106 形态三）
#
# 在**有完整仓库 + 资产**的机器上执行（CI/打包机），产物镜像可 docker save 离线分发：
#   docker save panomint/app:1.0.0 panomint/web:1.0.0 panomint/db:1.0.0 panomint/worker:1.0.0 \
#        | gzip > panomint-images-1.0.0.tar.gz
#
# 用法：bash release/docker/build-images.sh [版本号]   （默认读仓库根 VERSION）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/../.." && pwd)"
V="${1:-$(tr -d '[:space:]' < "$ROOT/VERSION")}"
[ -n "$V" ] || { echo "版本号为空" >&2; exit 1; }

echo "[images] 版本=$V"
cd "$ROOT"

echo "[images] 1/4 app（api + AI 工具链，CGO/ORT，含模型资产，版本经 build-arg 注入）"
DOCKER_BUILDKIT=1 docker build \
  --build-arg "APP_VERSION=$V" \
  --build-arg "APP_COMMIT=$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)" \
  --build-arg "APP_BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -f docker/api/Dockerfile -t "panomint/app:$V" .

echo "[images] 2/4 worker（ffmpeg/rclone/fuse 运行时）"
DOCKER_BUILDKIT=1 docker build -f docker/worker/Dockerfile -t "panomint/worker:$V" src/backend

echo "[images] 3/4 db（PG16 + PostGIS + pgvector）"
DOCKER_BUILDKIT=1 docker build -f docker/db/Dockerfile -t "panomint/db:$V" docker/db

echo "[images] 4/4 web（nginx 托管 dist + API 反代）"
if [ ! -f src/frontend/dist/index.html ]; then
  echo "前端 dist 不存在，先构建：cd src/frontend && npm ci && npm run build" >&2
  exit 1
fi
DOCKER_BUILDKIT=1 docker build -f docker/web/Dockerfile -t "panomint/web:$V" src/frontend

echo "[images] 版本注入核验（app 镜像内二进制的版本串）"
docker run --rm --entrypoint sh "panomint/app:$V" -c \
  "strings /usr/local/bin/api | grep -m1 '^$V\$' && echo IMAGE_VERSION_OK"

docker images | grep -E "panomint/(app|worker|db|web)" | grep "$V"
echo "[images] 完成。分发：docker save panomint/app:$V panomint/worker:$V panomint/db:$V panomint/web:$V | gzip > images-$V.tar.gz"
