#!/usr/bin/env bash
# build-images.sh —— 构建 Docker 形态发布镜像并打版本 tag（v1.1.0-feature，测试通道）
#
# 【feature/agent-semantic-fusion 发布通道】本分支镜像标签一律为 `test<版本号>`
# （VERSION=1.8.4 → panomint/app:test1.8.4），且**永不打/推 latest**——正式通道
# （无前缀 + latest）只在 main 分支维护，本文件随分支合并时不得带回 main。
#
# 在**有完整仓库 + 资产**的机器上执行（CI/打包机），产物镜像可 docker save 离线分发：
#   docker save panomint/app:test1.8.4 panomint/web:test1.8.4 panomint/db:test1.8.4 panomint/worker:test1.8.4 \
#        | gzip > panomint-images-test1.8.4.tar.gz
#
# 用法：bash release/docker/build-images.sh [版本号]   （默认读仓库根 VERSION）
# 推送 Docker Hub：bash release/docker/push-images.sh（只推 test<版本>，脚本内双断言禁 latest）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/../.." && pwd)"
V="${1:-$(tr -d '[:space:]' < "$ROOT/VERSION")}"
[ -n "$V" ] || { echo "版本号为空" >&2; exit 1; }

# 测试通道标签（feature 分支策略）：固定 test 前缀，TAG=test<版本号>，如 test1.8.4。
# TAG_PREFIX 仅允许 'test'：空串/latest/其他值一律拒绝——本分支永不产出 latest 标签。
TAG_PREFIX="${TAG_PREFIX:-test}"
case "$TAG_PREFIX" in
  test) : ;;
  *) echo "[images] 拒绝：TAG_PREFIX 仅允许 'test'（本分支禁发 latest/正式标签）" >&2; exit 1 ;;
esac
TAG="${TAG_PREFIX}${V}"

echo "[images] 版本=$V → 标签=$TAG（测试通道，不发布 latest）"
cd "$ROOT"

echo "[images] 1/4 app（api + AI 工具链，CGO/ORT，含模型资产，版本经 build-arg 注入）"
DOCKER_BUILDKIT=1 docker build \
  --build-arg "APP_VERSION=$V" \
  --build-arg "APP_COMMIT=${APP_COMMIT:-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)}" \
  --build-arg "APP_BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -f docker/api/Dockerfile -t "panomint/app:$TAG" .

echo "[images] 2/4 worker（ffmpeg/rclone/fuse 运行时）"
DOCKER_BUILDKIT=1 docker build -f docker/worker/Dockerfile -t "panomint/worker:$TAG" src/backend

echo "[images] 3/4 db（PG16 + PostGIS + pgvector）"
DOCKER_BUILDKIT=1 docker build -f docker/db/Dockerfile -t "panomint/db:$TAG" docker/db

echo "[images] 4/4 web（nginx 托管 dist + API 反代）"
if [ ! -f src/frontend/dist/index.html ]; then
  echo "前端 dist 不存在，先构建：cd src/frontend && npm ci && npm run build" >&2
  exit 1
fi
# dist 新鲜度守卫（Job000110 教训）：v1.0.0 发布时把向导合入前的旧 dist COPY 进了 web
# 镜像，用户端向导永久缺失只见到登录页。源码比 dist 新 → 拒绝静默使用旧产物。
STALE=$(find src/frontend/src src/frontend/index.html src/frontend/vite.config.* \
  -type f -newer src/frontend/dist/index.html 2>/dev/null | head -1)
if [ -n "$STALE" ]; then
  if [ "${SKIP_FRONTEND_BUILD:-0}" = "1" ]; then
    echo "[images] ⚠️ dist 早于前端源码（如 $STALE）；SKIP_FRONTEND_BUILD=1 已强制继续，后果自负" >&2
  else
    echo "[images] dist 落后于前端源码（如 $STALE）→ 自动重建 dist ..." >&2
    (cd src/frontend && npm ci && npm run build)
  fi
fi
DOCKER_BUILDKIT=1 docker build -f docker/web/Dockerfile -t "panomint/web:$TAG" src/frontend

echo "[images] 版本注入核验（app 镜像内二进制的版本串）"
docker run --rm --entrypoint sh "panomint/app:$TAG" -c \
  "grep -a -m1 -q "$V" /usr/local/bin/api && echo IMAGE_VERSION_OK"

docker images | grep -E "panomint/(app|worker|db|web)" | grep "$TAG"
echo "[images] 完成（标签=$TAG，未打 latest）。分发：docker save panomint/app:$TAG panomint/worker:$TAG panomint/db:$TAG panomint/web:$TAG | gzip > images-$TAG.tar.gz"
echo "[images] 推送 Hub：bash release/docker/push-images.sh $V"
