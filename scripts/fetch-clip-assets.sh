#!/usr/bin/env bash
# 获取 CLIP 模型与 ONNX Runtime 原生库（Job000010）。
#
# 用法：
#   scripts/fetch-clip-assets.sh [platform] [--gpu] [--dir <仓库根>]
#
#   platform : 可省略 —— 省略时按 uname 自动识别
#              linux-x64 | linux-arm64 | win-x64 | darwin-x64
#   --gpu    : 改用 CUDA 版 onnxruntime（体积大得多；仅在目标部署机有 NVIDIA GPU 时需要）
#
# 产物（均被 .gitignore 忽略，不入库）：
#   assets/models/clip/                       CLIP ViT-B/32 量化 ONNX + tokenizer
#   assets/lib/onnxruntime-<platform>-<ver>/   ONNX Runtime 原生库（CPU 或 CUDA 版）
#
# 说明：模型为 Xenova/clip-vit-base-patch32（基于 openai/clip-vit-base-patch32，MIT）。
# ONNX Runtime 版本必须与 Go 绑定头文件版本一致（当前 1.29.0），否则会在
# CreateOrtEnv 处崩溃。CPU/GPU 两版可并存，由 EMBED_LIB 选择其一。
#
# ⚠️ 目录命名是接口：docker/api/Dockerfile 按 `onnxruntime-linux-<x64|aarch64>-1.29.0`
#    拼路径（linux-arm64 → onnxruntime-linux-aarch64），不要改动这里的映射。
# ⚠️ 本脚本只取 OpenAI CLIP 与 ORT。默认模型族 chinese-clip 的资产见
#    scripts/fetch-chinese-clip-assets.sh；一次取全请用 bash scripts/fetch-all-assets.sh
#    （脚本在 git 里未记录执行位，故统一用 bash 调用）。

set -euo pipefail

ORT_VER="1.29.0"
HF_BASE="https://huggingface.co/Xenova/clip-vit-base-patch32/resolve/main"
ORT_BASE="https://github.com/microsoft/onnxruntime/releases/download"

PLATFORM=""
USE_GPU=0
ROOT=""

while [ $# -gt 0 ]; do
  case "$1" in
    --gpu) USE_GPU=1 ;;
    --dir) ROOT="$2"; shift ;;
    -h|--help) sed -n '2,23p' "$0"; exit 0 ;;
    *) PLATFORM="$1" ;;
  esac
  shift
done

if [ -z "$ROOT" ]; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi

# ---- 平台自动识别（省略 platform 时）----
# ONNX Runtime 上游的资产命名不是统一后缀，故这里做一层映射；
# 映射结果同时决定了 assets/lib/ 下的目录名，必须与 Dockerfile 的约定一致。
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
        # Apple Silicon 需要 onnxruntime-osx-arm64，本工程未接入（见文档「不把 macOS 作为一等部署目标」）
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
    echo "请显式指定：scripts/fetch-clip-assets.sh <linux-x64|linux-arm64|win-x64|darwin-x64>" >&2
    exit 2
  fi
  echo "== 未指定平台，按 uname 自动识别为：$PLATFORM"
fi

MODEL_DIR="$ROOT/assets/models/clip"
LIB_ROOT="$ROOT/assets/lib"
mkdir -p "$MODEL_DIR" "$LIB_ROOT"

echo "== 仓库根：$ROOT"
echo "== 平台：$PLATFORM  GPU：$USE_GPU  ORT：$ORT_VER"

# ---- 1) CLIP 模型 ----
echo "-- 下载 CLIP 模型（约 160MB）"
for f in \
  "onnx/text_model_quantized.onnx" \
  "onnx/vision_model_quantized.onnx" \
  "tokenizer.json" "vocab.json" "merges.txt" \
  "preprocessor_config.json" "config.json"
do
  out="$MODEL_DIR/$(basename "$f")"
  if [ -s "$out" ]; then
    echo "   跳过（已存在）$(basename "$f")"
    continue
  fi
  echo "   下载 $f"
  curl -fSL --retry 3 -o "$out.tmp" "$HF_BASE/$f"
  mv "$out.tmp" "$out"
done

# ---- 2) ONNX Runtime 原生库 ----
case "$PLATFORM" in
  linux-x64)   ORT_ASSET="onnxruntime-linux-x64" ;;
  linux-arm64) ORT_ASSET="onnxruntime-linux-aarch64" ;;
  win-x64)     ORT_ASSET="onnxruntime-win-x64" ;;
  darwin-x64)  ORT_ASSET="onnxruntime-osx-x86_64" ;;
  *) echo "未知平台：$PLATFORM" >&2; exit 2 ;;
esac

if [ "$USE_GPU" = "1" ]; then
  # ORT 1.29.0 官方同时提供 cuda12 与 cuda13 两个 GPU 构建：
  # 选哪个取决于目标机的 CUDA 运行时版本。可用 ORT_CUDA 覆盖（默认 cuda12）。
  # 例：目标机只有 CUDA 13（如 torch cu130）→ ORT_CUDA=cuda13 ./fetch-clip-assets.sh linux-x64 --gpu
  CUDA_TAG="${ORT_CUDA:-cuda12}"
  case "$PLATFORM" in
    linux-x64) ORT_ASSET="${ORT_ASSET}-gpu_${CUDA_TAG}" ;;
    win-x64)   ORT_ASSET="${ORT_ASSET}-gpu_${CUDA_TAG}" ;;
    *) echo "该平台暂无 GPU 版预编译库：$PLATFORM" >&2; exit 2 ;;
  esac
fi

TARBALL="$ORT_ASSET-$ORT_VER"
DIR="$LIB_ROOT/$TARBALL"
if [ -d "$DIR" ]; then
  echo "-- ONNX Runtime 已存在：$DIR"
else
  echo "-- 下载 ONNX Runtime：$TARBALL"
  cd "$LIB_ROOT"
  case "$PLATFORM" in
    win-x64)
      curl -fSL --retry 3 -o "$TARBALL.zip" "$ORT_BASE/v$ORT_VER/$TARBALL.zip"
      command -v unzip >/dev/null 2>&1 && unzip -q "$TARBALL.zip" || python3 -c "import zipfile,sys;zipfile.ZipFile(sys.argv[1]).extractall('.')" "$TARBALL.zip"
      rm -f "$TARBALL.zip"
      ;;
    *)
      curl -fSL --retry 3 -o "$TARBALL.tgz" "$ORT_BASE/v$ORT_VER/$TARBALL.tgz"
      tar xzf "$TARBALL.tgz"
      rm -f "$TARBALL.tgz"
      ;;
  esac
fi

echo
echo "== 完成。目录结构："
ls -d "$LIB_ROOT"/*/ 2>/dev/null || true
echo "   模型：$MODEL_DIR"
ls -la "$MODEL_DIR" | sed 's/^/     /'
echo
echo "提示：CPU 与 GPU 版库可并存，运行时用 EMBED_LIB 指向其一；"
echo "      设 EMBED_DEVICE=cuda 强制走 GPU（装配失败会直接报错），"
echo "      设 EMBED_DEVICE=auto 则优先 GPU、不可用回落 CPU。"
echo "      默认模型族 chinese-clip 还需：bash scripts/fetch-chinese-clip-assets.sh"
echo "      一次取全：bash scripts/fetch-all-assets.sh"
