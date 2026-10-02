#!/bin/sh
set -e
case "${WORKER_KIND:-}" in
  index)
    # Job000070 F4：挂载执行器与缩略图 worker 同容器（共享 mount 命名空间——
    # rclone mount 的 FUSE 挂载点只有同容器进程可见，跨容器需传播挂载更脆）。
    # storagectl 崩溃不拖垮 indexctl（& 后台 + 无 set -e 关联）。
    /usr/local/bin/storagectl run &
    exec /usr/local/bin/indexctl worker -thumbdir "${THUMB_DIR:-/data/thumbnails}"
    ;;
  storage)
    exec /usr/local/bin/storagectl run
    ;;
  transcode)
    exec /usr/local/bin/transcodectl worker -hlsdir "${HLS_DIR:-/data/hls}"
    ;;
  *)
    echo "WORKER_KIND 必须为 index|transcode|storage，当前: '${WORKER_KIND:-<空>}'" >&2
    exit 2
    ;;
esac
