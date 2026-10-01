#!/usr/bin/env bash
# build-images.sh —— 构建 Docker 形态发布镜像并打版本 tag（v1.1.0-feature，测试通道）
#
# 【双发布通道】（1.9.0 起合并自 feature/agent-semantic-fusion）：
#   · TAG_PREFIX 缺省 = **正式通道**：TAG=<版本号>（如 1.9.0），镜像内版本串=版本号；
#     正式 latest 推送由发版链 push 环节负责（docker tag warlocks/panomint-app:latest && push）。
#   · TAG_PREFIX=test = **测试通道**：TAG=test<版本号>（如 test1.8.7），镜像内版本串=TAG
#     （设置页版本卡可辨识 test 前缀）；分发只走 push-images.sh（结构性禁 latest）。
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

# 通道标签：TAG_PREFIX 仅允许空（正式）或 test；其他值一律拒绝。
TAG_PREFIX="${TAG_PREFIX:-}"
case "$TAG_PREFIX" in
  ""|test) : ;;
  *) echo "[images] 拒绝：TAG_PREFIX 仅允许空（正式）或 test" >&2; exit 1 ;;
esac
TAG="${TAG_PREFIX}${V}"
if [ "$TAG_PREFIX" = "test" ]; then
  APP_VERSION_VALUE="$TAG"
  CHANNEL_NOTE='测试通道，不发布 latest'
else
  APP_VERSION_VALUE="$V"
  CHANNEL_NOTE='正式通道'
fi

echo "[images] 版本=$V → 标签=$TAG（$CHANNEL_NOTE）"
cd "$ROOT"

echo "[images] 1/4 app（api + AI 工具链，CGO/ORT，含模型资产，版本经 build-arg 注入）"
# 测试通道：APP_VERSION 注入 $TAG（test<版本号>）——/version 与设置页版本卡直接显示
# test1.8.4，让测试构建在产品 UI 上可辨识（与正式通道 1.8.4 明确区分）。
DOCKER_BUILDKIT=1 docker build \
  --build-arg "APP_VERSION=$APP_VERSION_VALUE" \
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

echo "[images] 版本注入核验（app 镜像内二进制的版本串 = $APP_VERSION_VALUE）"
docker run --rm --entrypoint sh "panomint/app:$TAG" -c \
  "grep -a -m1 -q "$APP_VERSION_VALUE" /usr/local/bin/api && echo IMAGE_VERSION_OK"

docker images | grep -E "panomint/(app|worker|db|web)" | grep "$TAG"
echo "[images] 完成（标签=$TAG，通道=$CHANNEL_NOTE）。分发：docker save panomint/app:$TAG panomint/worker:$TAG panomint/db:$TAG panomint/web:$TAG | gzip > images-$TAG.tar.gz"
echo "[images] 推送 Hub：bash release/docker/push-images.sh $V"
