# syntax=docker/dockerfile:1
# 全景相册 API 镜像：cmd/api（gin 服务，TDD §8.1）
# 构建上下文为 src/backend（见 docker-compose.yml 中 api 的 build 段）

# ---- 阶段 1：编译 ----
FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api

# ---- 阶段 2：运行时 ----
# 工作目录保持 /：config 默认 ./data/* 解析到挂载卷 /data（健康检查 DiskCheckDir=./data）
FROM alpine:3.21
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.aliyun.com|g' /etc/apk/repositories \
 && apk add --no-cache ca-certificates tzdata
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
