#!/usr/bin/env bash
# release/build.sh —— Panomint 三形态发布包统一构建（v1.0.0，Job000106）。
#
# 产物（release/out/）：
#   panomint-<V>-linux-amd64-baremetal.tar.gz   裸机一键安装包
#   panomint-<V>-linux-amd64-bundle.tar.gz      集成运行环境应用包
#   sha256sums.txt                              全部产物校验和
#
# 可复现性三支柱：
#   ① 版本号唯一真源 = VERSION 文件（注入二进制 + 产物命名 + 镜像 tag 同源）；
#   ② 构建全部在容器内完成（node:22-alpine / golang:1.26-bookworm），与宿主机工具链解耦；
#   ③ 运行期资产在构建期做存在性+尺寸核验，缺失即失败（不产出"缺零件"的包）。
#
# 前置：bash scripts/fetch-all-assets.sh（AI 资产不入库，见 文档/部署方案与兼容性）。
# 用法：bash release/build.sh [--skip-frontend]   （--skip-frontend 复用现有 dist/，增量调试用）
#
# 测试门禁（Job000146 固化）：默认在构建**之前**先跑 scripts/release_test_gate.py
#（后端 go test -count=1 + 前端 vitest），红则整个脚本退出 1、不产出任何产物。
# 之所以放在构建前：构建要跑两个容器、耗时数分钟，测试红时先拦下最省。
# 真正的硬阻断仍在 release/docker/push-images.sh（唯一执行 docker push 的入库脚本）——
# 构建成功但测试红、且有人手工跳过本步时，推送那一步仍会拦住。
# 例外：SKIP_TEST_GATE=1 可跳过，须在发版记录里写明理由。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/.." && pwd)"
OUT="$ROOT/release/out"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
SKIP_FE=0
[ "${1:-}" = "--skip-frontend" ] && SKIP_FE=1

say()  { printf '\n=== %s ===\n' "$*"; }
die()  { printf '[build] 错误：%s\n' "$*" >&2; exit 1; }
# 构建本身只需要 Docker；测试门禁额外需要 python（宿主 Go/Node 由门禁脚本自行定位并
# 在缺失时报 FAIL）。SKIP_TEST_GATE=1 时不要求 python。
need() { command -v "$1" >/dev/null 2>&1 || die "缺 $1（构建本身只需要 Docker；python 仅测试门禁需要）"; }

need docker
[ -n "$VERSION" ] || die "VERSION 文件为空"
case "$VERSION" in *[!0-9.]*|.*|*..*) die "VERSION 非法：$VERSION（应为大.中.小 纯数字）";; esac
echo "[build] 版本=$VERSION commit=$COMMIT date=$BUILD_DATE"

# ---------------------------------------------------------------- 测试门禁
# 放在 0/5 之前：测试红就别浪费容器构建的数分钟。
if [ "${SKIP_TEST_GATE:-0}" != "1" ]; then
  say "门禁 测试（后端 go test -count=1 + 前端 vitest）"
  PY="$(command -v python3 || command -v python || true)"
  [ -n "$PY" ] || die "缺 python，无法运行测试门禁（拒绝在未测的情况下构建发布包）"
  # 🔴 绝不写成 "$PY" ... | tail —— 管道会把失败洗成退出码 0（本项目已栽过两次）。
  "$PY" "$ROOT/scripts/release_test_gate.py" || die "测试门禁未通过，拒绝构建发布包"
else
  echo "[build] SKIP_TEST_GATE=1：本次跳过测试门禁（例外通道，须在发版记录里写明理由）"
fi

# ---------------------------------------------------------------- 资产核验
say "0/5 运行期资产核验"
ASSETS="$ROOT/assets"
ORT_DIR="$ASSETS/lib/onnxruntime-linux-x64-1.29.0"
check_asset() { [ -s "$1" ] || die "资产缺失：$1（先跑：bash scripts/fetch-all-assets.sh）"; }
check_asset "$ORT_DIR/lib/libonnxruntime.so"
check_asset "$ASSETS/models/chinese-clip/text_only.onnx"
check_asset "$ASSETS/models/chinese-clip/vision_only.onnx"
check_asset "$ASSETS/models/chinese-clip/tokenizer.json"
n=0; for f in "$ASSETS"/models/faces/*.onnx; do [ -s "$f" ] && n=$((n+1)); done
[ "$n" -gt 0 ] || die "assets/models/faces 下无人脸模型"
echo "[build] 资产 OK（ORT x64 + chinese-clip + faces x$n）"

say "1/5 前端构建（node:22-alpine 容器）"
if [ "$SKIP_FE" = "1" ] && [ -d "$ROOT/src/frontend/dist" ]; then
  echo "[build] --skip-frontend：复用现有 dist/"
else
  docker run --rm -v "$ROOT/src/frontend":/app -w /app node:22-alpine \
    sh -c "npm ci --no-audit --no-fund && npm run build" \
    || die "前端构建失败"
fi
[ -f "$ROOT/src/frontend/dist/index.html" ] || die "dist/index.html 不存在"

# ---------------------------------------------------------------- 后端
say "2/5 后端构建（golang:1.26-bookworm 容器，CGO=api 系）"
BINHOST="$OUT/.binhost"
rm -rf "$BINHOST"; mkdir -p "$BINHOST"
LDFLAGS="-X panoalbum/internal/version.Version=$VERSION -X panoalbum/internal/version.Commit=$COMMIT -X panoalbum/internal/version.BuildDate=$BUILD_DATE"
docker run --rm -v "$ROOT/src/backend":/src -w /src -v "$BINHOST":/out \
  -e GOPROXY=https://goproxy.cn,direct \
  golang:1.26-bookworm \
  sh -c "
    set -e
    apt-get update -qq >/dev/null 2>&1 || true
    apt-get install -y -qq gcc libc6-dev >/dev/null 2>&1 || true
    for b in api embedgen taggen facesgen; do
      CGO_ENABLED=1 go build -trimpath -ldflags '$LDFLAGS' -o /out/pano-\$b ./cmd/\$b
    done
    for b in migrate indexctl transcodectl phashgen storagectl; do
      CGO_ENABLED=0 go build -trimpath -ldflags '$LDFLAGS' -o /out/pano-\$b ./cmd/\$b
    done
    ls -la /out
  " || die "后端构建失败"
for b in api migrate indexctl transcodectl embedgen taggen facesgen phashgen storagectl; do
  [ -s "$BINHOST/pano-$b" ] || die "二进制缺失：pano-$b"
done
# 入口分发器（裸机/集成包共用；语义与 docker/worker/Dockerfile entrypoint 对齐）
cp "$ROOT/release/baremetal/pano-worker-run" "$ROOT/release/baremetal/pano-ai-run" "$BINHOST/"
chmod +x "$BINHOST/pano-worker-run" "$BINHOST/pano-ai-run"
echo "[build] 9 个二进制 + 2 个分发器 OK（api 系 CGO / 工具系纯 Go，版本已注入）"

# ---------------------------------------------------------------- 装配
say "3/5 装配两种 tarball"
STAGE="$OUT/.stage"
rm -rf "$STAGE"; mkdir -p "$STAGE"

stage_common() { # stage_common <目标子目录>
  local d="$1"
  mkdir -p "$d/bin" "$d/web" "$d/assets/models" "$d/assets/lib"
  cp -a "$BINHOST"/. "$d/bin/"
  cp -a "$ROOT/src/frontend/dist" "$d/web/dist"
  cp -a "$ASSETS/models/chinese-clip" "$d/assets/models/chinese-clip"
  cp -a "$ASSETS/models/faces" "$d/assets/models/faces"
  mkdir -p "$d/assets/lib/onnxruntime-linux-x64-1.29.0"
  cp -a "$ORT_DIR/lib" "$d/assets/lib/onnxruntime-linux-x64-1.29.0/lib"
}

BM="$STAGE/baremetal/panomint-$VERSION"
stage_common "$BM"
mkdir -p "$BM/baremetal"
cp -a "$ROOT/release/baremetal/install.sh" "$ROOT/release/baremetal/"*.service "$BM/baremetal/"
cp "$ROOT/release/baremetal/README.md" "$BM/README.md"
chmod +x "$BM/baremetal/install.sh"

BD="$STAGE/bundle/panomint-$VERSION"
stage_common "$BD"
mkdir -p "$BD/bundle"
cp -a "$ROOT/release/bundle/panoctl" "$ROOT/release/bundle/install.sh" "$BD/bundle/"
cp "$ROOT/release/bundle/README.md" "$BD/README.md"
chmod +x "$BD/bundle/panoctl" "$BD/bundle/install.sh"

# ---------------------------------------------------------------- 打包
say "4/5 打包 + 校验和"
mkdir -p "$OUT"
( cd "$STAGE/baremetal" && tar czf "$OUT/panomint-$VERSION-linux-amd64-baremetal.tar.gz" "panomint-$VERSION" )
( cd "$STAGE/bundle"    && tar czf "$OUT/panomint-$VERSION-linux-amd64-bundle.tar.gz"    "panomint-$VERSION" )
( cd "$OUT" && sha256sum "panomint-$VERSION-linux-amd64-baremetal.tar.gz" "panomint-$VERSION-linux-amd64-bundle.tar.gz" > sha256sums.txt )

say "5/5 产物清单"
ls -la "$OUT"/panomint-*.tar.gz "$OUT/sha256sums.txt"
# 冒烟：tarball 内版本注入核验（解出二进制字符串查版本）
SMOKE="$(docker run --rm -v "$OUT":/out debian:bookworm-slim sh -c \
  "tar xzf /out/panomint-$VERSION-linux-amd64-bundle.tar.gz -C /tmp panomint-$VERSION/bin/pano-api && grep -a -m1 -q '$VERSION' && echo SMOKE_VERSION_OK")"
echo "$SMOKE"
echo "$SMOKE" | grep -q SMOKE_VERSION_OK || die "tarball 内版本注入核验失败"
rm -rf "$STAGE" "$BINHOST"
echo "[build] 完成。Docker 形态镜像：cd release/docker && bash build-images.sh $VERSION"
