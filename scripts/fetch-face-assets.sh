#!/usr/bin/env bash
# 获取人脸链路所需的两个 ONNX 模型（OpenCV Zoo，Apache-2.0）。
#
# 用法：
#   scripts/fetch-face-assets.sh [--dir <仓库根>]
#
# 产物（均被 .gitignore 忽略，不入库）：
#   assets/models/faces/face_detection_yunet_2026may.onnx   YuNet 动态输入版（约 0.23MB，**首选**，Job000041）
#   assets/models/faces/face_detection_yunet_2023mar.onnx   YuNet 静态 640 版（约 0.23MB，存量兼容回退）
#   assets/models/faces/face_recognition_sface_2021dec.onnx SFace 人脸识别（约 37MB）
#
# 说明：模型来自 OpenCV Zoo（Apache-2.0），刻意选它规避 insightface 的商用许可风险。
# 文件名必须与 internal/faces/face.go 的候选列表精确匹配（代码不做通配、不递归）：
#   YuNet : face_detection_yunet_2026may.onnx → face_detection_yunet_2023mar.onnx → face_detection_yunet.onnx
#   SFace : face_recognition_sface_2021dec.onnx → face_recognition_sface.onnx
# 后端通过环境变量 FACE_MODEL_DIR 指向该目录，缺省为相对路径 assets/models/faces。
#
# 2026may 与 2023mar 权重相同、仅输入维度不同（静态 [1,3,640,640] → 动态 [1,3,h,w]）：
# 动态版让自适应输入（LG 1:1）与多尺度检测（Job000041）生效；候选顺序 2026may 在前，
# 两者同目录时自动选中动态版，2023mar 仅为旧资产的兼容回退。
# 2026may 官方 sha256 = ebafce4e3c118d6554634be5c27ab333b4c047a9a8c3faf1d7cf93101c22f0f0
#（下载后强制校验；镜像源仅在该 hash 校验通过时才落盘）。
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
    face_detection_yunet_2026may.onnx)   echo 100000 ;;    # ~0.23MB
    face_detection_yunet_2023mar.onnx)   echo 100000 ;;    # ~0.23MB
    face_recognition_sface_2021dec.onnx) echo 30000000 ;;  # ~37MB
    *) echo 1 ;;
  esac
}

# 官方 sha256（opencv_zoo README/登记簿所载）；任何来源下载后都必须吻合。
sha256_for() {
  case "$1" in
    face_detection_yunet_2026may.onnx) echo "ebafce4e3c118d6554634be5c27ab333b4c047a9a8c3faf1d7cf93101c22f0f0" ;;
    *) echo "" ;;
  esac
}

# 文件名（精确）| OpenCV Zoo 子目录
FILES="
face_detection_yunet_2026may.onnx|face_detection_yunet
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
  # 多源回落：官方 GitHub 优先；github 被墙时走 hf-mirror 上的 opencv_zoo 社区镜像。
  # 凡是有官方 sha256 的文件，下载后强制校验，hash 不合即拒（防镜像被篡改/下错）。
  want_hash="$(sha256_for "$fname")"
  got=""
  for base in "$ZOO_BASE" \
              "https://hf-mirror.com/spaces/djhui5710/reachy_mini_home_assistant_edge/resolve/main/reachy_mini_home_assistant/models"; do
    if curl -fSL --retry 2 --retry-delay 2 --connect-timeout 15 -o "$out.tmp" "$base/$subdir/$fname" 2>/dev/null; then
      if [ -n "$want_hash" ]; then
        got="$(sha256sum "$out.tmp" | awk '{print $1}')"
        if [ "$got" = "$want_hash" ]; then
          echo "   hash 校验通过（$got）"
          break
        fi
        echo "   hash 不合（得 $got，期望 $want_hash），试下一源" >&2
        rm -f "$out.tmp"
      else
        break
      fi
    fi
  done
  if [ ! -s "$out.tmp" ]; then
    echo "   下载失败（所有源）：$fname" >&2
    rm -f "$out.tmp"
    exit 1
  fi
  mv "$out.tmp" "$out"
  echo "   完成 $fname（$(wc -c < "$out" | tr -d ' ') 字节）"
done

echo
echo "== 完成。模型："
ls -la "$MODEL_DIR" | sed 's/^/     /'
echo
echo "提示：后端用 FACE_MODEL_DIR 指向该目录；缺省为相对 assets/models/faces。"
