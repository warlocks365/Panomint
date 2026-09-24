# syntax=docker/dockerfile:1
# 全景相册 API 镜像：cmd/api（gin 服务，TDD §8.1）+ cmd/embedgen（CLIP 向量工具，Job000010）
#                  + cmd/taggen（零样本 AI 打标，Phase 4）+ cmd/facesgen（人脸检测/聚类，Job000011）
#                  + cmd/phashgen（DCT 感知哈希，TDD §381）
# 构建上下文 = 仓库根（见 docker-compose.yml 中 api.build.context）
#
# 为什么不是 alpine：CLIP 向量能力经 CGO 绑定 ONNX Runtime，而官方 Linux 预编译库是
# **glibc** 链接的；alpine 用 musl，无法加载。故编译/运行阶段均改用 Debian(glibc)。
# 设备选择（CPU / CUDA）由运行时环境变量决定，镜像本身不裁剪 GPU 接口。
#
# 多架构（P0-2）：镜像不绑定 CPU 架构。ONNX Runtime 原生库按 buildx 注入的
# `TARGETARCH` 挑选（amd64→x64 / arm64→aarch64），并在构建期断言"库的 ELF 架构 == 目标架构"。
#   amd64：docker compose build api                              （默认，行为与改造前一致）
#   arm64：docker buildx build --platform linux/arm64 -f docker/api/Dockerfile .
# 资产获取（P0-3）：本 Dockerfile 只消费 `assets/`，不负责下载。全新克隆请先跑
#   bash scripts/fetch-all-assets.sh   （细节见 文档/部署方案与兼容性_v1.0.md「资产获取」）

# ---- 阶段 1：编译 ----
FROM golang:1.26-bookworm AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
# 版本三元组注入（Job000105）：compose 开发/测试构建不传参 = 默认 dev（行为与引入前一致）；
# 发布构建由 release/docker/build-images.sh 传 --build-arg 注入真实版本。
ARG APP_VERSION=dev
ARG APP_COMMIT=""
ARG APP_BUILD_DATE=""
ENV LDFLAGS="-X panoalbum/internal/version.Version=${APP_VERSION} -X panoalbum/internal/version.Commit=${APP_COMMIT} -X panoalbum/internal/version.BuildDate=${APP_BUILD_DATE}"
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
# 编译期嵌入的 goose 迁移（Job000107：api 启动自迁移 + pano-migrate 共用同一 FS）。
# ⚠️ 漏拷 = 构建期即报 "package panoalbum/migrations is not in std"（2026-09-24 部署实测踩坑）
COPY src/backend/migrations ./migrations
# 挂载 Go 构建缓存：CGO 全量编译较慢（4 核 VM 上首次约 20 分钟），
# 缓存后增量重建只需编译变更部分
# phashgen 与 migrateplan 都是纯 Go，用 CGO_ENABLED=0 编译：不必等 C 工具链，
# 产物也不依赖 libonnxruntime（pHash 只用标准库 image + golang.org/x/image）。
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -trimpath -ldflags "$LDFLAGS" -o /out/api ./cmd/api \
 && CGO_ENABLED=1 go build -trimpath -ldflags "$LDFLAGS" -o /out/embedgen ./cmd/embedgen \
 && CGO_ENABLED=1 go build -trimpath -ldflags "$LDFLAGS" -o /out/taggen ./cmd/taggen \
 && CGO_ENABLED=1 go build -trimpath -ldflags "$LDFLAGS" -o /out/facesgen ./cmd/facesgen \
 && CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o /out/migrateplan ./cmd/migrateplan \
 && CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o /out/phashgen ./cmd/phashgen

# ---- 阶段 2：资产选定与校验（按目标架构挑 ONNX Runtime，并把"缺资产"变成可读错误）----
# 为什么单独一个阶段，而不是直接在运行阶段 `COPY assets/lib/onnxruntime-linux-${ORT_ARCH}-…`：
#   ① `COPY` 的源路径虽然支持 `$ARG` 展开，但 Dockerfile 没有条件分支/字符串映射能力，
#      无法把 TARGETARCH=amd64 映射成目录名 x64 —— 这个映射只能交给 shell；
#   ② 选定后由运行阶段 `COPY --from` 取用，**运行镜像里不会多出任何文件或工具**（不装 file/binutils）；
#   ③ 存在性检查 + ELF 架构断言都落在这一阶段，失败信息可控、可读，且几乎零耗时。
FROM debian:bookworm-slim AS ortassets
ARG TARGETARCH
ARG ORT_VERSION=1.29.0
# TARGETARCH 由 buildx 按 --platform 自动注入（同时声明在阶段内才可用）。
# ORT 目录名沿用 scripts/fetch-clip-assets.sh 既有命名：amd64→x64、arm64→aarch64。
# `--mount=type=bind` 直接挂构建上下文做"只看不拷"的检查，不产生任何镜像层，构建开销≈0。
RUN --mount=type=bind,target=/ctx,ro \
    set -eu; \
    arch="${TARGETARCH:-$(dpkg --print-architecture)}"; \
    case "$arch" in \
      amd64) ort_arch=x64;     elf_machine=3e00; elf_name="x86-64" ;; \
      arm64) ort_arch=aarch64; elf_machine=b700; elf_name="AArch64" ;; \
      *) echo "错误：不支持的 TARGETARCH=${arch}（本工程只承诺 amd64 与 arm64）" >&2; exit 1 ;; \
    esac; \
    ort_dir="assets/lib/onnxruntime-linux-${ort_arch}-${ORT_VERSION}"; \
    echo "== 目标架构 ${arch} → ONNX Runtime 资产目录 ${ort_dir}"; \
    missing=""; \
    need() { [ -s "/ctx/$1" ] || missing="$missing $1"; }; \
    need "${ort_dir}/lib/libonnxruntime.so"; \
    need "assets/models/clip/text_model_quantized.onnx"; \
    need "assets/models/clip/vision_model_quantized.onnx"; \
    need "assets/models/clip/tokenizer.json"; \
    need "assets/models/chinese-clip/text_only.onnx"; \
    need "assets/models/chinese-clip/vision_only.onnx"; \
    need "assets/models/chinese-clip/tokenizer.json"; \
    if [ -n "$missing" ]; then \
      echo "" >&2; \
      echo "错误：以下运行期资产缺失，镜像无法构建：" >&2; \
      for f in $missing; do echo "    - $f" >&2; done; \
      echo "" >&2; \
      echo "  这些文件被 .gitignore 排除，全新克隆必然没有；镜像缺它们只会在运行期崩溃循环。" >&2; \
      echo "  请在仓库根执行一键获取：  bash scripts/fetch-all-assets.sh" >&2; \
      echo "  离线环境做法见：文档/部署方案与兼容性_v1.0.md「资产获取（P0-3 已处理）」" >&2; \
      exit 1; \
    fi; \
    n=0; for f in /ctx/assets/models/faces/*.onnx; do [ -s "$f" ] || continue; n=$((n+1)); done; \
    if [ "$n" = 0 ]; then \
      echo "" >&2; \
      echo "错误：assets/models/faces 下没有任何 .onnx 人脸模型（YuNet/SFace）。" >&2; \
      echo "  请在仓库根执行：  bash scripts/fetch-all-assets.sh" >&2; \
      exit 1; \
    fi; \
    mkdir -p /ort/lib; \
    cp -a "/ctx/${ort_dir}"/lib/libonnxruntime.so* /ort/lib/; \
    real="$(readlink -f /ort/lib/libonnxruntime.so)"; \
    if [ ! -f "$real" ]; then \
      echo "错误：/ort/lib/libonnxruntime.so 不是有效的符号链接（断链）" >&2; exit 1; \
    fi; \
    magic="$(od -An -tx1 -j 0 -N 4 "$real" | tr -d ' \n')"; \
    if [ "$magic" != "7f454c46" ]; then \
      echo "错误：$real 不是 ELF 文件（magic=$magic）" >&2; exit 1; \
    fi; \
    got="$(od -An -tx1 -j 18 -N 2 "$real" | tr -d ' \n')"; \
    if [ "$got" != "$elf_machine" ]; then \
      echo "错误：ONNX Runtime 原生库的架构与目标架构不符 —— 这会导致运行期 dlopen 失败且极难定位。" >&2; \
      echo "      TARGETARCH=${arch}，期望 e_machine=0x${elf_machine}（${elf_name}）" >&2; \
      echo "      实际 $(basename "$real") 的 e_machine=0x${got}" >&2; \
      echo "      多半是 ${ort_dir}/lib/ 里放了别的架构的库。" >&2; \
      echo "      请重新执行：bash scripts/fetch-all-assets.sh --platform <linux-x64|linux-arm64>" >&2; \
      exit 1; \
    fi; \
    echo "== 架构自检通过：TARGETARCH=${arch} 使用 $(basename "$real")，ELF e_machine=0x${got}（${elf_name}）"; \
    ls -la /ort/lib/

# ---- 阶段 3：运行时 ----
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
COPY --from=build /out/phashgen /usr/local/bin/phashgen

# ONNX Runtime 原生库（1.29.0，须与 Go 绑定头文件版本一致）
# 由 ortassets 阶段按 TARGETARCH 选定；amd64 下与多架构改造前**逐字节一致**
# （同版本、同 /opt/onnxruntime/lib/ 路径、同三个文件：.so / .so.1 / .so.1.29.0）
COPY --from=ortassets /ort/lib/libonnxruntime.so* /opt/onnxruntime/lib/
# CLIP 模型（Xenova/clip-vit-base-patch32 量化版，512 维，英文 BPE 分词）
COPY assets/models/clip /opt/models/clip
# Chinese-CLIP（Xenova/chinese-clip-vit-base-patch16 量化版，512 维，中文 BERT 分词）
# 镜像内只保留切分后的单塔模型；未切分的 model_quantized.onnx 由 .dockerignore 排除
COPY assets/models/chinese-clip /opt/models/chinese-clip
# 人脸模型（OpenCV Zoo 的 YuNet 检测 + SFace 识别，均 Apache-2.0 —— 刻意避开 insightface 的商用许可风险）
# 由 scripts/fetch-face-assets.sh 下载到 assets/models/faces/（约 37MB，不入库）
# ⚠️ 文件名必须精确命中 internal/faces/face.go 的候选列表，否则启动即报「未找到模型文件」
COPY assets/models/faces /opt/models/faces

# 向量能力的默认路径/设备/模型族（均可在 compose 或 .env 覆盖）
#   EMBED_FAMILY=chinese-clip 面向中文检索（默认）；=clip 则用 OpenAI CLIP（英文更强）
# ⚠️ FACE_MODEL_DIR 必须显式设置：运行阶段无 WORKDIR（工作目录为 /），
#    而 internal/faces 的缺省值是相对路径 assets/models/faces，会解析成不存在的 /assets/models/faces。
ENV EMBED_LIB=/opt/onnxruntime/lib/libonnxruntime.so \
    EMBED_MODEL_DIR=/opt/models/chinese-clip \
    EMBED_FAMILY=chinese-clip \
    EMBED_DEVICE=auto \
    FACE_MODEL_DIR=/opt/models/faces

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
