# PostgreSQL 16 + PostGIS 3.4 + pgvector
# pgvector/pgvector:pg16 基于官方 postgres:16 (Debian bookworm)，已含 pgvector
# apt 源替换为阿里云镜像（境外源在国内拉取停滞），再安装 postgis-3
FROM pgvector/pgvector:pg16

RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g; s|security.debian.org|mirrors.aliyun.com/debian-security|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || true \
 && sed -i 's|apt.postgresql.org/pub/repos/apt|mirrors.aliyun.com/postgresql/repos/apt|g' /etc/apt/sources.list.d/pgdg.list 2>/dev/null || true \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      postgresql-16-postgis-3 \
      postgresql-16-postgis-3-scripts \
 && rm -rf /var/lib/apt/lists/*
