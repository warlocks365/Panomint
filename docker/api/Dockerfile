# syntax=docker/dockerfile:1
# 全景相册 API 镜像：cmd/api（gin 服务，TDD §8.1）+ cmd/embedgen（CLIP 向量工具，Job000010）
#                  + cmd/taggen（零样本 AI 打标，Phase 4）+ cmd/facesgen（人脸检测/聚类，Job000011）
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
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY src/backend/cmd ./cmd
COPY src/backend/internal ./internal
# 挂载 Go 构建缓存：CGO 全量编译较慢（4 核 VM 上首次约 20 分钟），
# 缓存后增量重建只需编译变更部分
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -trimpath -o /out/api ./cmd/api \
 && CGO_ENABLED=1 go build -trimpath -o /out/embedgen ./cmd/embedgen \
 && CGO_ENABLED=1 go build -trimpath -o /out/taggen ./cmd/taggen \
 && CGO_ENABLED=1 go build -trimpath -o /out/facesgen ./cmd/facesgen \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrateplan ./cmd/migrateplan

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
COPY --from=build /out/taggen /usr/local/bin/taggen
COPY --from=build /out/facesgen /usr/local/bin/facesgen
COPY --from=build /out/migrateplan /usr/local/bin/migrateplan

# ONNX Runtime 原生库（1.29.0，须与 Go 绑定头文件版本一致）
COPY assets/lib/onnxruntime-linux-x64-1.29.0/lib/libonnxruntime.so* /opt/onnxruntime/lib/
# CLIP 模型（Xenova/clip-vit-base-patch32 量化版，512 维，英文 BPE 分词）
COPY assets/models/clip /opt/models/clip
# Chinese-CLIP（Xenova/chinese-clip-vit-base-patch16 量化版，512 维，中文 BERT 分词）
# 镜像内只保留切分后的单塔模型；未切分的 model_quantized.onnx 由 .dockerignore 排除
COPY assets/models/chinese-clip /opt/models/chinese-clip

# 向量能力的默认路径/设备/模型族（均可在 compose 或 .env 覆盖）
#   EMBED_FAMILY=chinese-clip 面向中文检索（默认）；=clip 则用 OpenAI CLIP（英文更强）
ENV EMBED_LIB=/opt/onnxruntime/lib/libonnxruntime.so \
    EMBED_MODEL_DIR=/opt/models/chinese-clip \
    EMBED_FAMILY=chinese-clip \
    EMBED_DEVICE=auto

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
