#!/usr/bin/env sh
# Panomint worker 分发器（独立部署用）
#
# 为什么需要它：容器形态里靠 docker/worker/Dockerfile 的 worker-entrypoint.sh 按
# WORKER_KIND 选进程；systemd 的 ExecStart 不能直接做条件分支，故把同样的分发逻辑
# 放在这里，由 pano-worker@.service 以 %i 作为第一个参数调用：
#
#   systemctl enable --now pano-worker@index        # 缩略图/索引消费
#   systemctl enable --now pano-worker@transcode    # HLS 转码消费
#
# ⚠️ 本脚本必须可执行（部署时 chmod +x；仓库内统一为 100644，因为 core.fileMode=false）。
set -eu

kind="${1:-}"
: "${PANO_BIN_DIR:?未设置 PANO_BIN_DIR（二进制所在目录，由 unit 的 Environment= 提供）}"

case "$kind" in
  index)
    exec "$PANO_BIN_DIR/pano-indexctl" worker -thumbdir "${THUMB_DIR:-./data/thumbnails}"
    ;;
  transcode)
    exec "$PANO_BIN_DIR/pano-transcodectl" worker -hlsdir "${HLS_DIR:-./data/hls}"
    ;;
  *)
    echo "用法：$0 index|transcode（当前：'${kind}'）" >&2
    echo "  对应 systemctl 实例名：pano-worker@index / pano-worker@transcode" >&2
    exit 2
    ;;
esac
