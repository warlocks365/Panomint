#!/usr/bin/env bash
# push-images.sh —— 测试通道镜像推送 Docker Hub（feature/agent-semantic-fusion 专用）。
#
# 发布策略（本分支红线）：
#   * 仅推送 `warlocks/panomint-*:test<版本号>`（VERSION=1.8.4 → warlocks/panomint-app:test1.8.4）；
#   * **绝不打/推 latest**——脚本内双断言硬拒绝，任何 latest 企图直接失败退出；
#   * 正式通道（无前缀 + latest）只在 main 分支维护，本脚本不得随分支合并回 main。
#
# 前置：先执行 build-images.sh 产出本地 panomint/*:test<版本号> 四镜像。
# 用法：bash release/docker/push-images.sh [版本号]   （默认读仓库根 VERSION）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/../.." && pwd)"
V="${1:-$(tr -d '[:space:]' < "$ROOT/VERSION")}"
[ -n "$V" ] || { echo "[push] 版本号为空" >&2; exit 1; }
TAG="test${V}"

# ---- 断言 1：标签必须严格符合 test<版本号> 格式（杜绝空前缀/正式版本号直推）。 ----
case "$TAG" in
  test[0-9]*.[0-9]*.[0-9]*) : ;;
  *) echo "[push] 拒绝：标签 '$TAG' 不符合 test<主版本号> 格式（如 test1.8.4）" >&2; exit 1 ;;
esac
# ---- 断言 2：显式 latest 防线（双保险，任何输入形态的 latest 都拒绝）。 ----
case "$TAG|$V" in
  *latest*) echo "[push] 拒绝：本分支禁止发布 latest 标签" >&2; exit 1 ;;
esac

echo "[push] 通道标签=$TAG（仅推送该标签，不推 latest）"

# local:hub 对——本地构建名 → Docker Hub 仓库名
PAIRS=(
  "panomint/app:warlocks/panomint-app"
  "panomint/worker:warlocks/panomint-worker"
  "panomint/db:warlocks/panomint-db"
  "panomint/web:warlocks/panomint-web"
)

for pair in "${PAIRS[@]}"; do
  local_name="${pair%%:*}"
  hub_name="${pair#*:}"
  docker image inspect "$local_name:$TAG" > /dev/null 2>&1 \
    || { echo "[push] 本地镜像缺失：$local_name:$TAG（先跑 bash release/docker/build-images.sh）" >&2; exit 1; }
  docker tag "$local_name:$TAG" "$hub_name:$TAG"
  echo "[push] 推送 $hub_name:$TAG ..."
  docker push "$hub_name:$TAG"
done

echo "[push] 推送完成：warlocks/panomint-{app,worker,db,web}:$TAG"
echo "[push] 未推送 latest（本脚本结构性禁止）。"

# ---- 本地中转别名：根 docker-compose.yml（测试服部署编排）引用无命名空间名
#      panomint-app:test<版本号>，此处一并打好桥接（替代旧行为 docker tag → :latest）。
docker tag "panomint/app:$TAG" "panomint-app:$TAG"
echo "[push] 本地别名已就绪：panomint-app:$TAG（根 compose 引用）"

echo "[push] Hub digest 核验："
for s in app worker db web; do
  echo "  curl -s https://hub.docker.com/v2/repositories/warlocks/panomint-$s/tags/$TAG | grep -o '\"digest\":\"[^\"]*\"'"
done
echo "[push] latest 应为 404：curl -s -o /dev/null -w '%{http_code}' https://hub.docker.com/v2/repositories/warlocks/panomint-app/tags/latest"
