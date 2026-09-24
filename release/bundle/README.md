# Panomint 集成运行环境包（bundle 形态）

面向**面板（宝塔类）/ NAS / 无 systemd** 环境的自包含应用包：单目录、免编译、
免 root、不碰系统目录。所有进程（API + 4 个 AI 清扫进程 + 缩略图/转码消费 +
挂载执行）由 `bundle/panoctl` 统一启停。

## 前置条件（须自带，env-check 会逐项核验）

| 依赖 | 用途 | 没有会怎样 |
|---|---|---|
| PostgreSQL 16 + PostGIS + pgvector | 数据库 | 无法启动（硬性） |
| Valkey（或兼容 Redis 协议）8.x | 任务队列/限流 | 无法启动（硬性） |
| ffmpeg | 缩略图/HLS 转码 | AI 功能正常，仅缩略图与转码不工作 |

> 这三样面板环境一般已有一键安装方式；裸机形态（baremetal）则会自动装齐它们。

## 快速开始

```bash
tar xzf panomint-<V>-linux-amd64-bundle.tar.gz
cd panomint-<V>
./bundle/install.sh            # 交互生成 .env + 数据目录 + 数据库迁移
./bundle/panoctl env-check     # 自检（PG/Valkey/ffmpeg/资产）
./bundle/panoctl start         # 启动全部
./bundle/panoctl status        # 查看状态
./bundle/panoctl logs api      # 跟踪日志
```

浏览器打开 `http://<本机IP>:8080` —— **首次访问自动进入初始化向导**（创建管理员，
一次性，完成后不再出现）。

## 目录结构

```
panomint-<V>/
├── bin/        全部二进制（版本已注入，pano-api -version 经 /version 端点可查）
├── web/dist/   前端静态产物
├── assets/     AI 运行资产（chinese-clip / 人脸模型 / ONNX Runtime x64）
├── bundle/     panoctl + install.sh + 本说明
├── data/       媒体/缩略图/HLS（**备份它**）
└── var/        日志与 pid 文件
```

## 反代

对外服务建议经 nginx/Caddy 反代（TLS + 域名）。规则见 `文档/独立部署指南_v1.0.md`
的 6 条规则；注意 API 前缀清单含 `/setup`、`/version`（v1.0.0 新增）。

## 升级

停进程 → 解新包 → 复制旧包 `.env` 与 `data/` 进新包 → `./bin/pano-migrate up`
（幂等，多数情况下 API 启动自迁移已替你完成）→ `panoctl start`。
