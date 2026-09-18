# 独立服务器部署（systemd）模板

本目录是**无容器（独立服务器）部署形态**的最小交付物，配合 `scripts/install-standalone.sh`
使用。目标平台是**通用自托管**：任意主流 Linux（systemd）+ 外部 PostgreSQL/Valkey；
**不绑定群晖、DSM 或任何具体机型**。

| 文件 | 作用 |
|---|---|
| `pano-api.service` | API 服务（前台 HTTP，`bin/pano-api`）|
| `pano-worker@.service` | Worker 模板 unit，实例 `index` / `transcode` |
| `pano-worker-dispatch.sh` | Worker 分发器（systemd 无法在 `ExecStart` 里做条件分支）|

## 占位符

三个 unit 里的占位符由安装脚本替换（**也可以手动替换后自行安装**）：

| 占位符 | 含义 | 默认 |
|---|---|---|
| `@INSTALL_DIR@` | 安装根目录（含 `bin/`、`data/`、`assets/`、`.env`） | 仓库根 |
| `@RUN_USER@` / `@RUN_GROUP@` | 运行身份，**不是 root** | 当前用户 |
| `@ENV_FILE@` | 环境变量文件（密钥与连接串） | `@INSTALL_DIR@/.env` |

## 安装步骤（需 root，故由你手动执行）

```bash
# 1) 生成并检查 unit（脚本只写到临时目录，不动 /etc）
bash scripts/install-standalone.sh --dir /opt/panomint --user pano

# 2) 建运行用户（不给登录 shell）
sudo useradd --system --home-dir /opt/panomint --shell /usr/sbin/nologin pano
sudo chown -R pano:pano /opt/panomint/data

# 3) 安装 unit（把 <临时目录> 换成脚本打印的路径）
sudo cp <临时目录>/pano-api.service          /etc/systemd/system/
sudo cp <临时目录>/pano-worker@.service      /etc/systemd/system/
sudo cp <临时目录>/pano-worker-dispatch.sh   /opt/panomint/deploy/systemd/
sudo chmod +x /opt/panomint/deploy/systemd/pano-worker-dispatch.sh

# 4) 启动
sudo systemctl daemon-reload
sudo systemctl enable --now pano-api
sudo systemctl enable --now pano-worker@index pano-worker@transcode

# 5) 看状态与日志
systemctl status pano-api
journalctl -u pano-api -f
```

## 已知前提（不会自动处理）

- **外部依赖**：PostgreSQL（含 PostGIS + pgvector）与 Valkey 需你自行提供；本系统只连过去。
- **ffmpeg** 必须在 PATH 上（缩略图与 HLS 转码），worker 与 API 都用得上。
- **APP_ENV=prod 的门禁**：`PG_DSN` / `VALKEY_ADDR` / `MEDIA_ROOT` 若仍是本机开发默认值，
  进程会**启动即失败**（`src/backend/internal/config/config.go:96`）。这是刻意设计，不是 bug。
- **反代**：`pano-api` 只监听本机端口，对外需要你自备反向代理；
  **必须自行复刻的 6 条规则**见 `文档/独立部署指南_v1.0.md`。
- **arm64 暂不承诺**（2026-09-18 决议），见 `文档/部署方案与兼容性_v1.0.md` §11。
