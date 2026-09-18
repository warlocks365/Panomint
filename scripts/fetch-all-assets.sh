#!/usr/bin/env bash
# 一键获取"运行本系统所需的全部模型与原生库"（P0-3 的单一入口）。
#
# 用法：
#   bash scripts/fetch-all-assets.sh [--platform <p>] [--gpu] [--dir <仓库根>] [--skip-faces]
#
#   --platform : linux-x64 | linux-arm64 | win-x64 | darwin-x64（省略则按 uname 自动识别）
#   --gpu      : 取 CUDA 版 ONNX Runtime（仅 linux-x64 / win-x64 有上游预编译包）
#   --skip-faces: 跳过人脸模型（约 37MB；不用人脸链路时可省）
#
# 为什么需要这个入口：assets/ 整个目录被 .gitignore 排除（`git ls-files assets` = 0），
# 而 docker/api/Dockerfile 会 `COPY assets/models/...`。**全新克隆必然缺资产**，
# 且缺了之后旧行为是在 build 时抛一句难懂的 Docker 错误、或在运行期变成崩溃循环。
# 所以：先跑这一条命令，再构建镜像。Dockerfile 里也有对应的构建期存在性检查。
#
# 覆盖的资产：
#   ① ONNX Runtime 原生库（按平台/架构） → assets/lib/onnxruntime-<platform>-1.29.0/
#   ② OpenAI CLIP 模型                   → assets/models/clip/
#   ③ Chinese-CLIP 模型（含单塔切分）     → assets/models/chinese-clip/
#   ④ 人脸模型 YuNet + SFace             → assets/models/faces/
#
# 离线环境：在一台联网机器上跑完本脚本，然后把
#   assets/models/                                     （整目录，约 400MB）
#   assets/lib/onnxruntime-linux-<x64|aarch64>-1.29.0/ （只这一层，约 30MB）
# 打包拷到目标机仓库的相同相对路径下即可；本脚本可重复执行且幂等。
#
# 幂等：所有下载步骤都会跳过已存在且体积达阈值的文件。

set -euo pipefail

ORT_VER="1.29.0"

ROOT=""
PLATFORM=""
USE_GPU=0
SKIP_FACES=0

while [ $# -gt 0 ]; do
  case "$1" in
    --platform) PLATFORM="$2"; shift ;;
    --gpu) USE_GPU=1 ;;
    --dir) ROOT="$2"; shift ;;
    --skip-faces) SKIP_FACES=1 ;;
    -h|--help) sed -n '2,26p' "$0"; exit 0 ;;
    *) echo "未知参数：$1" >&2; exit 2 ;;
  esac
  shift
done

if [ -z "$ROOT" ]; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi

# ---- 平台：显式优先，否则按 uname 识别 ----
# 这里自带一份与 fetch-clip-assets.sh 相同的映射，是为了让本脚本既能独立运行，
# 又能在最后一步按确定的目标平台校验 ORT 目录名（两份必须保持一致）。
detect_platform() {
  local os m
  os="$(uname -s 2>/dev/null || echo unknown)"
  m="$(uname -m 2>/dev/null || echo unknown)"
  case "$os" in
    Linux)
      case "$m" in
        x86_64|amd64) echo linux-x64 ;;
        aarch64|arm64) echo linux-arm64 ;;
        *) return 1 ;;
      esac ;;
    Darwin)
      case "$m" in
        x86_64|amd64) echo darwin-x64 ;;
        *) return 1 ;;
      esac ;;
    MINGW*|MSYS*|CYGWIN*)
      case "$m" in
        x86_64|amd64|i686|i386) echo win-x64 ;;
        *) return 1 ;;
      esac ;;
    *) return 1 ;;
  esac
}

if [ -z "$PLATFORM" ]; then
  if ! PLATFORM="$(detect_platform)"; then
    echo "无法自动识别当前平台（uname -s=$(uname -s 2>/dev/null) / uname -m=$(uname -m 2>/dev/null)）。" >&2
    echo "请显式指定：bash scripts/fetch-all-assets.sh --platform <linux-x64|linux-arm64|win-x64|darwin-x64>" >&2
    exit 2
  fi
fi

case "$PLATFORM" in
  linux-x64)   ORT_ARCH="x64";     ORT_DIR="onnxruntime-linux-x64-${ORT_VER}";     ORT_LIB="libonnxruntime.so" ;;
  linux-arm64) ORT_ARCH="aarch64"; ORT_DIR="onnxruntime-linux-aarch64-${ORT_VER}"; ORT_LIB="libonnxruntime.so" ;;
  win-x64)     ORT_ARCH="x64";     ORT_DIR="onnxruntime-win-x64-${ORT_VER}";       ORT_LIB="onnxruntime.dll" ;;
  darwin-x64)  ORT_ARCH="x86_64";  ORT_DIR="onnxruntime-osx-x86_64-${ORT_VER}";    ORT_LIB="libonnxruntime.dylib" ;;
  *) echo "未知平台：$PLATFORM（支持 linux-x64 | linux-arm64 | win-x64 | darwin-x64）" >&2; exit 2 ;;
esac

echo "================================================================"
echo " 一键获取全部资产"
echo "   仓库根   ：$ROOT"
echo "   目标平台 ：$PLATFORM"
echo "   ORT      ：$ORT_VER（目录 $ORT_DIR）"
echo "   GPU 版   ：$USE_GPU"
echo "   人脸模型 ：$([ "$SKIP_FACES" = "1" ] && echo 跳过 || echo 获取)"
echo "================================================================"
echo

# ---- ① OpenAI CLIP + ONNX Runtime ----
echo "【1/3】OpenAI CLIP 模型 + ONNX Runtime 原生库"
EXTRA=()
if [ "$USE_GPU" = "1" ]; then EXTRA+=(--gpu); fi
bash "$ROOT/scripts/fetch-clip-assets.sh" "$PLATFORM" "${EXTRA[@]+"${EXTRA[@]}"}" --dir "$ROOT"
echo

# ---- ② Chinese-CLIP（默认模型族，含单塔切分）----
echo "【2/3】Chinese-CLIP 模型（默认族；含 text_only/vision_only 切分）"
bash "$ROOT/scripts/fetch-chinese-clip-assets.sh" --dir "$ROOT"
echo

# ---- ③ 人脸模型 ----
if [ "$SKIP_FACES" = "1" ]; then
  echo "【3/3】人脸模型：已按要求跳过（--skip-faces）"
  echo "    注意：跳过后面镜像运行 facesgen/faces-worker 会失败；如需补取："
  echo "      bash scripts/fetch-face-assets.sh --dir \"$ROOT\""
else
  echo "【3/3】人脸模型（OpenCV Zoo YuNet + SFace，约 37MB）"
  bash "$ROOT/scripts/fetch-face-assets.sh" --dir "$ROOT"
fi
echo

# ---- 统一校验：逐项对着 docker/api/Dockerfile 会 COPY 的资产清单核对 ----
echo "================================================================"
echo " 资产校验（对照 docker/api/Dockerfile 的 COPY 清单）"
echo "================================================================"
bad=0
need() { # need <绝对路径> <说明>
  if [ ! -s "$1" ]; then
    echo "  缺失：$1" >&2
    echo "        （$2）" >&2
    bad=1
    return
  fi
  echo "  OK  ：$1（$(wc -c < "$1" | tr -d ' ') 字节）"
}

need "$ROOT/assets/lib/$ORT_DIR/lib/$ORT_LIB" "ONNX Runtime 原生库（$PLATFORM）"
need "$ROOT/assets/models/clip/text_model_quantized.onnx"     "OpenAI CLIP 文本塔"
need "$ROOT/assets/models/clip/vision_model_quantized.onnx"   "OpenAI CLIP 视觉塔"
need "$ROOT/assets/models/clip/tokenizer.json"                "OpenAI CLIP tokenizer"
need "$ROOT/assets/models/chinese-clip/text_only.onnx"        "Chinese-CLIP 文本塔（默认族必需）"
need "$ROOT/assets/models/chinese-clip/vision_only.onnx"      "Chinese-CLIP 视觉塔（默认族必需）"
need "$ROOT/assets/models/chinese-clip/tokenizer.json"        "Chinese-CLIP tokenizer"

faces_n=0
for f in "$ROOT"/assets/models/faces/*.onnx; do
  if [ -s "$f" ]; then
    echo "  OK  ：$f（$(wc -c < "$f" | tr -d ' ') 字节）"
    faces_n=$((faces_n + 1))
  fi
done
if [ "$faces_n" = "0" ]; then
  echo "  缺失：assets/models/faces/*.onnx（YuNet 检测 + SFace 识别）" >&2
  bad=1
fi

echo
if [ "$bad" != "0" ]; then
  echo "结果：资产不完整。请按上面的提示补齐（人脸模型：bash scripts/fetch-face-assets.sh）。" >&2
  exit 1
fi

echo "✓ 全部资产就绪。现在可以构建/启动："
echo "    docker compose build api      # 构建镜像（构建期还会再断言 ORT 架构与目标架构一致）"
echo
echo "  镜像会用到（.dockerignore 已排除的 190MB model_quantized.onnx 不需要进镜像）："
echo "    assets/models/clip/           → /opt/models/clip"
echo "    assets/models/chinese-clip/   → /opt/models/chinese-clip（只需单塔 + tokenizer）"
echo "    assets/models/faces/          → /opt/models/faces"
echo "    assets/lib/$ORT_DIR/lib/      → /opt/onnxruntime/lib"
