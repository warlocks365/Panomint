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
#
# 前置门禁：测试门禁（后端 go test -count=1 + 前端 vitest）。默认在推送前自动运行，
#红则整个脚本退出 1，一个镜像都推不出去。紧急例外可设SKIP_TEST_GATE=1 跳过，
#但须在发版记录里写明理由。
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

# ---- 断言 3：测试门禁。测试红= 不许推送任何镜像（Job000146 固化）----
#
# 为什么门禁必须插在这里，而不是 build-images.sh 里：
#   build-images.sh 只产出**本地镜像**，不推送；push-images.sh 是发版链里
#   **唯一执行 docker push 的入库脚本**（.workbuddy/ 下的历史 push 脚本已被
#   gitignore 排除，不构成发版链）。门禁插在 push 之前，红了镜像就出不了仓库，
#   115也就部署不到这个坏版本——这才是「拦住发版」的真实含义。
#   注意：绝不能把门禁输出接进管道（`| tail` 会把失败洗成 0，本项目已栽过两次）。
if [ "${SKIP_TEST_GATE:-0}" != "1" ]; then
  echo "[push] 运行发版测试门禁（后端 go test -count=1 + 前端 vitest）..."
  PY="$(command -v python3 || command -v python || true)"
  [ -n "$PY" ] || { echo "[push] 缺 python，无法运行测试门禁（拒绝在未测的情况下推送）" >&2; exit 1; }
  "$PY" "$ROOT/scripts/release_test_gate.py" || {
    echo "[push] 测试门禁未通过 —— 阻断推送（不要改测试让门禁变绿）" >&2
    exit 1
  }
  echo "[push] 测试门禁通过"
else
  echo "[push] ⚠ SKIP_TEST_GATE=1：本次跳过测试门禁（例外通道，须在发版记录里写明理由）" >&2
fi

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
