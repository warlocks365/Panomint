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
 && CGO_ENABLED=0 go build -trimpath -o /out/transcodectl ./cmd/transcodectl

# ---- 阶段 2：运行时（ffmpeg 供缩略图/转码调用）----
FROM alpine:3.21
# 国内网络走阿里云镜像源，避免 dl-cdn 拉取过慢
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
 && apk add --no-cache ffmpeg ca-certificates tzdata
COPY --from=build /out/indexctl /out/transcodectl /usr/local/bin/

# 入口分发：WORKER_KIND=index|transcode 决定执行哪个 worker 子命令
COPY <<'EOF' /usr/local/bin/worker-entrypoint.sh
#!/bin/sh
set -e
case "${WORKER_KIND:-}" in
  index)
    exec /usr/local/bin/indexctl worker -thumbdir "${THUMB_DIR:-/data/thumbnails}"
    ;;
  transcode)
    exec /usr/local/bin/transcodectl worker -hlsdir "${HLS_DIR:-/data/hls}"
    ;;
  *)
    echo "WORKER_KIND 必须为 index|transcode，当前: '${WORKER_KIND:-<空>}'" >&2
    exit 2
    ;;
esac
EOF
RUN chmod +x /usr/local/bin/worker-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/worker-entrypoint.sh"]
