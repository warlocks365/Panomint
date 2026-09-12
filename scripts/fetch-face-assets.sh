#!/usr/bin/env bash
# 获取人脸链路所需的两个 ONNX 模型（OpenCV Zoo，Apache-2.0）。
#
# 用法：
#   scripts/fetch-face-assets.sh [--dir <仓库根>]
#
# 产物（均被 .gitignore 忽略，不入库）：
#   assets/models/faces/face_detection_yunet_2023mar.onnx     YuNet 人脸检测（约 0.23MB）
#   assets/models/faces/face_recognition_sface_2021dec.onnx   SFace 人脸识别（约 37MB）
#
# 说明：模型来自 OpenCV Zoo（Apache-2.0），刻意选它规避 insightface 的商用许可风险。
# 文件名必须与 internal/faces/face.go 的候选列表精确匹配（代码不做通配、不递归）：
#   YuNet : face_detection_yunet_2023mar.onnx → face_detection_yunet_2026may.onnx → face_detection_yunet.onnx
#   SFace : face_recognition_sface_2021dec.onnx → face_recognition_sface.onnx
# 后端通过环境变量 FACE_MODEL_DIR 指向该目录，缺省为相对路径 assets/models/faces。
#
# 幂等：文件已存在且大小合理（>= 下限阈值）则跳过，可安全重复执行。

set -euo pipefail

ZOO_BASE="https://github.com/opencv/opencv_zoo/raw/main/models"

ROOT=""

while [ $# -gt 0 ]; do
  case "$1" in
    --dir) ROOT="$2"; shift ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "未知参数：$1" >&2; exit 2 ;;
  esac
  shift
done

if [ -z "$ROOT" ]; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi

MODEL_DIR="$ROOT/assets/models/faces"
mkdir -p "$MODEL_DIR"

echo "== 仓库根：$ROOT"
echo "== 模型目录：$MODEL_DIR"

# 每个文件的最小合理字节数（低于此值视为下载被截断或下成了 HTML 错误页）。
# 实际参考值：YuNet ≈ 232589 B，SFace ≈ 37MB。
min_bytes_for() {
  case "$1" in
    face_detection_yunet_2023mar.onnx)   echo 100000 ;;    # ~0.23MB
    face_recognition_sface_2021dec.onnx) echo 30000000 ;;  # ~37MB
    *) echo 1 ;;
  esac
}

# 文件名（精确）| OpenCV Zoo 子目录
FILES="
face_detection_yunet_2023mar.onnx|face_detection_yunet
face_recognition_sface_2021dec.onnx|face_recognition_sface
"

echo "$FILES" | while IFS='|' read -r fname subdir; do
  [ -z "$fname" ] && continue
  out="$MODEL_DIR/$fname"
  min="$(min_bytes_for "$fname")"

  if [ -s "$out" ]; then
    size="$(wc -c < "$out" | tr -d ' ')"
    if [ "$size" -ge "$min" ]; then
      echo "   跳过（已存在，${size} 字节）$fname"
      continue
    fi
    echo "   已存在但过小（${size} 字节 < ${min}），重新下载 $fname"
  fi

  echo "   下载 $fname"
  curl -fSL --retry 3 --retry-delay 2 -o "$out.tmp" "$ZOO_BASE/$subdir/$fname"
  mv "$out.tmp" "$out"
  echo "   完成 $fname（$(wc -c < "$out" | tr -d ' ') 字节）"
done

echo
echo "== 完成。模型："
ls -la "$MODEL_DIR" | sed 's/^/     /'
echo
echo "提示：后端用 FACE_MODEL_DIR 指向该目录；缺省为相对 assets/models/faces。"
