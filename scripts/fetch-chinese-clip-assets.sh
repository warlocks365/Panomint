#!/usr/bin/env bash
# 获取 Chinese-CLIP（默认模型族）的 ONNX 资产，并切分出单塔模型。
#
# 用法：
#   scripts/fetch-chinese-clip-assets.sh [--dir <仓库根>] [--no-split]
#
# 产物（均被 .gitignore 忽略，不入库）：
#   assets/models/chinese-clip/            merged 量化 ONNX + tokenizer + config
#   assets/models/chinese-clip/text_only.onnx    文本塔（切分产物）
#   assets/models/chinese-clip/vision_only.onnx  视觉塔（切分产物）
#
# 为什么需要这个脚本：系统的**默认模型族是 chinese-clip**（docker-compose.yml 与
# docker/api/Dockerfile 的 EMBED_FAMILY 默认值），代码读取的是切分后的
# text_only.onnx / vision_only.onnx（见 src/backend/internal/embed/clip_cgo.go:50-54），
# 而 Xenova 的导出是"合并模型"（每次文本查询都会白跑一遍视觉塔）。
# 改造前仓库里既没有下载脚本、也没有任何地方调用 scripts/split_clip_onnx.py
# —— 全新克隆按脚本走必然拿不到默认族所需的文件。
#
# 说明：模型为 Xenova/chinese-clip-vit-base-patch16（上游 OFA-Sys/chinese-clip-vit-base-patch16，
# 量化 ONNX 约 190MB）。切分只依赖 python 的 onnx 包，不影响运行时（运行时不装 python）。
#
# 幂等：已存在且体积达阈值的文件会跳过；重复执行安全。

set -euo pipefail

HF_BASE="https://huggingface.co/Xenova/chinese-clip-vit-base-patch16/resolve/main"

ROOT=""
DO_SPLIT=1

while [ $# -gt 0 ]; do
  case "$1" in
    --dir) ROOT="$2"; shift ;;
    --no-split) DO_SPLIT=0 ;;
    -h|--help) sed -n '2,25p' "$0"; exit 0 ;;
    *) echo "未知参数：$1" >&2; exit 2 ;;
  esac
  shift
done

if [ -z "$ROOT" ]; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi

MODEL_DIR="$ROOT/assets/models/chinese-clip"
mkdir -p "$MODEL_DIR"

echo "== 仓库根：$ROOT"
echo "== 模型目录：$MODEL_DIR"

# 远端路径 | 落地文件名 | 最小合理字节数（低于此值视为下载被截断或下成了 HTML 错误页）
# 参考实际大小：model_quantized.onnx ≈ 190MB、tokenizer.json ≈ 0.44MB、vocab.txt ≈ 110KB。
FILES="
onnx/model_quantized.onnx|model_quantized.onnx|150000000
tokenizer.json|tokenizer.json|100000
vocab.txt|vocab.txt|10000
tokenizer_config.json|tokenizer_config.json|100
config.json|config.json|100
preprocessor_config.json|preprocessor_config.json|100
"

echo "-- 下载 Chinese-CLIP（约 190MB）"
while IFS='|' read -r remote local min; do
  if [ -z "$local" ]; then continue; fi
  out="$MODEL_DIR/$local"
  if [ -s "$out" ]; then
    size="$(wc -c < "$out" | tr -d ' ')"
    if [ "$size" -ge "$min" ]; then
      echo "   跳过（已存在，${size} 字节）$local"
      continue
    fi
    echo "   已存在但过小（${size} < ${min}），重新下载 $local"
  fi
  echo "   下载 $remote"
  curl -fSL --retry 3 --retry-delay 2 -o "$out.tmp" "$HF_BASE/$remote"
  mv "$out.tmp" "$out"
  echo "   完成 $local（$(wc -c < "$out" | tr -d ' ') 字节）"
done <<EOF
$FILES
EOF

# ---- 切分：merged → text_only.onnx / vision_only.onnx ----
if [ "$DO_SPLIT" = "1" ]; then
  PY="${PYTHON:-}"
  if [ -z "$PY" ]; then
    for c in python3 python; do
      if command -v "$c" >/dev/null 2>&1; then PY="$c"; break; fi
    done
  fi
  if [ -z "$PY" ]; then
    echo "错误：未找到 Python 解释器，无法切分单塔模型。" >&2
    echo "      可用环境变量指定：PYTHON=/path/to/python scripts/fetch-chinese-clip-assets.sh" >&2
    echo "      或只下载不切分：scripts/fetch-chinese-clip-assets.sh --no-split" >&2
    exit 1
  fi

  if ! "$PY" -c "import onnx" >/dev/null 2>&1; then
    echo "-- 本机 $PY 缺少 onnx 包（仅切分需要，不进运行时镜像），尝试自动安装"
    if ! "$PY" -m pip install --user --disable-pip-version-check onnx; then
      echo "" >&2
      echo "错误：无法自动安装 onnx，切分未执行。" >&2
      echo "      已下载的模型文件**保留**在 $MODEL_DIR（未做任何删除）。" >&2
      echo "      三选一：" >&2
      echo "        ① 装好 onnx 后重跑：\"$PY\" -m pip install onnx && scripts/fetch-chinese-clip-assets.sh" >&2
      echo "        ② 在 Linux 容器内切分（本项目既有做法）：" >&2
      echo "           docker run --rm -v \"$MODEL_DIR:/m\" -v \"$ROOT/scripts:/w\" python:3.13-slim \\" >&2
      echo "             bash -lc 'pip install onnx && python /w/split_clip_onnx.py /m'" >&2
      echo "        ③ 只下载不切分：scripts/fetch-chinese-clip-assets.sh --no-split" >&2
      echo "      注意：缺少 text_only.onnx / vision_only.onnx 时，api 镜像会在构建期直接失败。" >&2
      exit 1
    fi
  fi

  # Windows(Git Bash/MSYS)：原生 python.exe 不认 `/c/...` 形式的路径，会拼成 `C:\c\...`。
  # 有 cygpath 时统一转成 Windows 路径（Linux/macOS 上无 cygpath，原样传递）。
  SPLIT_SCRIPT="$ROOT/scripts/split_clip_onnx.py"
  SPLIT_DIR="$MODEL_DIR"
  if command -v cygpath >/dev/null 2>&1; then
    SPLIT_SCRIPT="$(cygpath -w "$SPLIT_SCRIPT")"
    SPLIT_DIR="$(cygpath -w "$SPLIT_DIR")"
  fi

  echo "-- 切分合并模型为单塔（text_only.onnx / vision_only.onnx）"
  if ! "$PY" "$SPLIT_SCRIPT" "$SPLIT_DIR"; then
    echo "" >&2
    echo "错误：切分失败（split_clip_onnx.py 退出码非 0，常见原因：内存不足或源模型不完整）。" >&2
    echo "      $MODEL_DIR" >&2
    echo "      已下载的文件未做任何删除；可重试或改在 Linux 容器内切分（见上方指引）。" >&2
    exit 1
  fi
else
  echo "-- --no-split：跳过切分"
fi

# ---- 校验 ----
echo
echo "== 产物校验"
bad=0
check() { # check <文件名> <最小字节> <说明>
  local p="$MODEL_DIR/$1" min="$2" desc="$3" size
  if [ ! -s "$p" ]; then
    echo "   缺失：$1（$desc）" >&2
    bad=1
    return
  fi
  size="$(wc -c < "$p" | tr -d ' ')"
  if [ "$size" -lt "$min" ]; then
    echo "   过小（疑似切分/下载失败）：$1 = ${size} 字节 < ${min}（$desc）" >&2
    bad=1
    return
  fi
  echo "   OK：$1（${size} 字节）"
}
check text_only.onnx 1000000 "chinese-clip 文本塔，运行时必需"
check vision_only.onnx 1000000 "chinese-clip 视觉塔，运行时必需"
check tokenizer.json 100000 "中文 BERT tokenizer，运行时必需"

if [ "$bad" != "0" ]; then
  echo "" >&2
  echo "错误：Chinese-CLIP 资产不完整。" >&2
  if [ "$DO_SPLIT" != "1" ]; then
    echo "      本次是 --no-split，切分未执行；请去掉该参数重跑以生成单塔模型。" >&2
  else
    echo "      切分似乎没有产出预期文件，请检查上方 python 输出。" >&2
  fi
  echo "      缺这两个文件时 docker/api/Dockerfile 会在构建期直接报错（这是刻意设计，
      避免把问题推迟到运行期的崩溃循环）。" >&2
  exit 1
fi

echo
echo "== 完成。模型目录："
ls -la "$MODEL_DIR" | sed 's/^/     /'
echo
echo "提示：镜像里只需要 text_only.onnx / vision_only.onnx / tokenizer.json 等小文件，"
echo "      190MB 的 model_quantized.onnx 已被 .dockerignore 排除（它只是切分原料）。"
