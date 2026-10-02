# syntax=docker/dockerfile:1
# 全景相册 Worker 镜像：indexctl（缩略图）/ transcodectl（HLS 转码）常驻消费（TDD §8.1 方案 A）
# 构建上下文为 src/backend（见 docker-compose.yml 中 index-worker/transcode-worker 的 build 段）

# ---- 阶段 1：编译 ----
FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/indexctl ./cmd/indexctl \
 && CGO_ENABLED=0 go build -trimpath -o /out/transcodectl ./cmd/transcodectl \
 && CGO_ENABLED=0 go build -trimpath -o /out/storagectl ./cmd/storagectl

# ---- 阶段 2：运行时（ffmpeg 供缩略图/转码调用；rclone/fuse/nfs-utils 供挂载执行器 Job000070）----
FROM alpine:3.21
# 国内网络走阿里云镜像源，避免 dl-cdn 拉取过慢
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
 && apk add --no-cache ffmpeg ca-certificates tzdata rclone fuse3 nfs-utils
COPY --from=build /out/indexctl /out/transcodectl /out/storagectl /usr/local/bin/

# 入口分发：WORKER_KIND=index|transcode|storage 决定执行哪个 worker 子命令
# worker-entrypoint.sh 以真实文件提交（src/backend/worker-entrypoint.sh，LF）；
# 弃用 Dockerfile heredoc——群晖/Windows 构建链对 heredoc 内容的 CRLF 不稳（1.9.2 首发实测踩坑）。
COPY worker-entrypoint.sh /usr/local/bin/worker-entrypoint.sh
RUN chmod 0755 /usr/local/bin/worker-entrypoint.sh' /usr/local/bin/worker-entrypoint.sh
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
EOF
RUN chmod +x /usr/local/bin/worker-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/worker-entrypoint.sh"]
