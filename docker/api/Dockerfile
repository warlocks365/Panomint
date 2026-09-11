# syntax=docker/dockerfile:1
# 全景相册 API 镜像：cmd/api（gin 服务，TDD §8.1）+ cmd/embedgen（CLIP 向量工具，Job000010）
# 构建上下文 = 仓库根（见 docker-compose.yml 中 api.build.context）
#
# 为什么不是 alpine：CLIP 向量能力经 CGO 绑定 ONNX Runtime，而官方 Linux 预编译库是
# **glibc** 链接的；alpine 用 musl，无法加载。故编译/运行阶段均改用 Debian(glibc)。
# 设备选择（CPU / CUDA）由运行时环境变量决定，镜像本身不裁剪 GPU 接口。

# ---- 阶段 1：编译 ----
FROM golang:1.26-bookworm AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
# CGO 需要 C 工具链
RUN set -eux; \
    if [ -f /etc/apt/sources.list.d/debian.sources ]; then \
      sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources; \
    fi; \
    apt-get update; \
    apt-get install -y --no-install-recommends gcc libc6-dev; \
    rm -rf /var/lib/apt/lists/*
COPY src/backend/go.mod src/backend/go.sum ./
RUN go mod download
COPY src/backend/cmd ./cmd
COPY src/backend/internal ./internal
RUN CGO_ENABLED=1 go build -trimpath -o /out/api ./cmd/api \
 && CGO_ENABLED=1 go build -trimpath -o /out/embedgen ./cmd/embedgen

# ---- 阶段 2：运行时 ----
# 工作目录保持 /：config 默认 ./data/* 解析到挂载卷 /data（健康检查 DiskCheckDir=./data）
FROM debian:bookworm-slim
RUN set -eux; \
    if [ -f /etc/apt/sources.list.d/debian.sources ]; then \
      sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources; \
    fi; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates tzdata; \
    rm -rf /var/lib/apt/lists/*
COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/embedgen /usr/local/bin/embedgen

# ONNX Runtime 原生库（1.29.0，须与 Go 绑定头文件版本一致）
COPY assets/lib/onnxruntime-linux-x64-1.29.0/lib/libonnxruntime.so* /opt/onnxruntime/lib/
# CLIP 模型（Xenova/clip-vit-base-patch32 量化版，512 维）
COPY assets/models/clip /opt/models/clip

# 向量能力的默认路径与设备（EMBED_DEVICE 可在 compose/.env 覆盖：cpu|cuda|auto）
ENV EMBED_LIB=/opt/onnxruntime/lib/libonnxruntime.so \
    EMBED_MODEL_DIR=/opt/models/clip \
    EMBED_DEVICE=auto

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
