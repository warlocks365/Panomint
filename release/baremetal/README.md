# Panomint 裸机一键安装包（baremetal 形态）

面向独立 Linux 服务器（Debian 12 / Ubuntu 22.04+，x86_64，有 systemd）的一键安装。

```bash
tar xzf panomint-<V>-linux-amd64-baremetal.tar.gz
cd panomint-<V>
sudo bash baremetal/install.sh        # 自动装依赖/建库/装 systemd 单元并启动
# 或使用外部数据库：
sudo bash baremetal/install.sh --db external
```

装完浏览器打开 `http://<本机IP>:8080` —— **首次访问自动进入初始化向导**。

- 数据目录：`/opt/panomint/data`（`--prefix` 可改），备份它 = 备份全部媒体；
- 日志：`journalctl -u pano-api -f`；
- 反代/TLS：按 `文档/独立部署指南_v1.0.md` 6 条规则（前缀含 `/setup`、`/version`）；
- 幂等：脚本可安全重跑（系统包 skip、`.env` 与数据目录保留）。
