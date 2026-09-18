# docker/caddy —— 入口反代配置

两个配置文件，二选一（由 `.env` 的 `CADDY_CONFIG` 决定，见 `docker-compose.yml` 的 `caddy` 服务）：

| 文件 | 形态 | 需要证书文件？ | 何时用 |
| --- | --- | --- | --- |
| `Caddyfile`（默认） | HTTP，或对真实公网域名自动 ACME 签发 | **不需要** | 通用部署。全新克隆 `docker compose up -d` 即可起 |
| `Caddyfile.tls` | 自有证书 + 显式 HTTPS | 需要 | 用现成泛域名/自签证书、不让 Caddy 自动签发的环境（当前部署即此形态） |

```bash
# 选用 HTTPS 变体：在 .env 里加一行（.env 不入库）
echo 'CADDY_CONFIG=Caddyfile.tls' >> .env
docker compose up -d caddy
```

## 站点地址 / 证书路径

两个文件都写成 `{$VAR:默认值}`（Caddy 的加载期文本替换）。通用部署要改站点地址时，
在 `docker-compose.yml` 的 `caddy` 服务下加 `environment:` 显式传值（示例见该文件注释），
或直接用 `docker-compose.override.yml`：

```yaml
services:
  caddy:
    environment:
      SITE_ADDRESS: album.example.com
```

⚠️ **别传空值**：Caddy 用 `os.LookupEnv` 判定，空串算"已设置"，会覆盖配置文件里的默认值
（例如 `SITE_ADDRESS=` 会让站点地址变成空，Caddy 直接启动失败）。

## certs/ 由部署者放置，不入库

`docker/caddy/certs/` 已在 `.gitignore` 里（`*.pem` 亦被忽略）——**证书与私钥绝不入库**。
`Caddyfile.tls` 默认从容器内 `/etc/caddy/certs/` 读取：

```
certs/fullchain.crt   # 证书链
certs/private.pem     # 私钥（放好后 chmod 600）
```

该目录在仓库里不存在是**正常的**：默认形态（`Caddyfile`）根本不需要它。
`docker-compose.yml` 把整个 `./docker/caddy` 只读挂入容器 `/etc/caddy`，所以缺这个目录不会影响启动。

## 端口

`docker-compose.yml` 只映射了 `443`（HTTP 变体要用的话，自行补 `- "80:80"`；
也可只让 443 反代，另经 `web` 服务的 8088 直连）。
