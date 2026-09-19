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

## 透传给后端的协议（`FORWARDED_PROTO`）

反代会把 `X-Forwarded-Proto` 传给后端，而后端**据此拼分享链接与 OG 图片的绝对 URL**
（`src/backend/internal/shares/og.go` 的 `originOf`）。这个值必须**与事实相符**，否则
分享出去的是打不开的链接。

两个形态的默认值是**各自形态的真实情况**：

| 文件 | 默认值 | 为什么 |
| --- | --- | --- |
| `Caddyfile`（HTTP 形态） | **`http`** | 该形态只做明文反代，客户端就是走 http 进来的 |
| `Caddyfile.tls`（HTTPS 形态） | **`https`** | 该形态由 Caddy 终止 TLS |

> 历史坑：`Caddyfile` 里曾**硬编码 `https`**（注释还写着"若以明文对外请手动改成 http"）。
> 后果是：明文部署下后端会拼出 `https://…` 的分享链接，而站点根本没有 TLS 监听 ——
> 分享出去点开就是连不上。现已变量化，默认值就是实话，不必再手改文件。

需要覆盖时（例如 Caddy 前面还有一层终止 TLS 的代理、或某个形态要强制生成 https 链接）。
⚠️ **不要只往 `.env` 里加 `FORWARDED_PROTO`** —— 它不会被传进 caddy 容器
（`.env` 只自动供 compose 做变量插值，容器里的环境变量必须由 `environment:` 段显式给出，
这正是本项目踩过的"环境变量传递链"坑）。用 override 文件显式传值：

```yaml
# docker-compose.override.yml
services:
  caddy:
    environment:
      FORWARDED_PROTO: https
```

⚠️ **也别用 `${FORWARDED_PROTO:-}` 这种空默认值透传**：compose 会把**空串**传进容器，
而 Caddy 的 `os.LookupEnv` 把空串当成"已设置"，于是 Caddyfile 里的 `{$FORWARDED_PROTO:http}`
默认值**不会**生效 —— 结果是 `header_up X-Forwarded-Proto `（空值），比不传更糟。
所以本项目**刻意不在** `docker-compose.yml` 的 caddy 服务里透传这个变量：
不传时容器内根本没这个环境变量，各形态的默认值才按设计生效。

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

---

## 本部署的证书与域名（Job000029，2026-09-19）

**先把域名结构搞清楚（这里最容易搞错）**：

- `warlocks.cn` 是**一级域名（apex）** —— 它是**证书覆盖范围**里的名字，**不是**本站要服务的站点。
- **`panomint.warlocks.cn` 与 `photo.warlocks.cn` 才是本站实际服务的域名。**
- 提供的证书是 apex 的**泛域名证书**（SAN = `*.warlocks.cn` + `warlocks.cn`），
  所以 `panomint.` / `photo.` 这类子域名被它覆盖 —— **这正是这张证书要解决的问题**。

⇒ 因此 `SITE_ADDRESS` **只列实际站点**（`Caddyfile.tls` 的默认值就是这两个）。
不要因为"证书里有 apex"就顺手把 `warlocks.cn` 也加进站点列表：那会让本站去接管一个
它本来不负责的名字，既无必要，也会掩盖真正的域名指向关系。

**证书来源**：`D:\ssl`（来此加密 / Let's Encrypt 渠道，随包带 `detail.txt`）。

| 文件 | 用途 | 是否必需 |
| --- | --- | --- |
| `fullchain.crt` | 证书 + 证书链 | ✅ **Caddy 读它** |
| `private.pem` | 私钥（**ECDSA P-256**） | ✅ **Caddy 读它** |
| `certificate.crt` / `chain.crt` / `public.pem` / `detail.txt` | 续期比对与核对用 | 可选（Caddy 不读） |

- **SAN**：`*.warlocks.cn` + `warlocks.cn` —— **子域名与 apex 都在覆盖范围内**，这正是
  `panomint.` / `photo.` 这两个站点域名能用它的原因。
- **到期**：2026-10-13。续期后替换 `fullchain.crt` / `private.pem`，再
  `docker compose up -d --force-recreate caddy`。
- 安装位置：`docker/caddy/certs/`（全部 `chmod 600`；该目录被 gitignore，**证书私钥绝不入库**）。

**服务的站点**由 `.env` 的 `SITE_ADDRESS` 给出（逗号分隔多个域名）。`docker-compose.yml` 用
`${SITE_ADDRESS:-…}` 透传，**空值会回落到默认**，因此不会踩到上面那个 `os.LookupEnv` 空串坑。
⚠️ 证书必须覆盖 `SITE_ADDRESS` 里的**所有**名字，否则那个域名的握手会失败。

### ⚠️ 验证 HTTPS 必须带 SNI —— 否则会得到**假故障**

`curl https://127.0.0.1/` 的 SNI 是 `127.0.0.1`，与证书不匹配 ⇒ Caddy 选不到证书 ⇒
握手报 `tlsv1 alert internal error`。**这不是服务坏了，是测法错了**（本项目真实踩过，并一度误判为"证书与私钥不匹配"）。正确做法：

```bash
curl -ksS --resolve warlocks.cn:443:127.0.0.1 -o /dev/null \
     -w 'HTTP=%{http_code} bytes=%{size_download}\n' https://warlocks.cn/
# 期望 200 且 bytes > 0
openssl s_client -connect 127.0.0.1:443 -servername warlocks.cn </dev/null 2>/dev/null \
  | openssl x509 -noout -subject -dates -ext subjectAltName
```

⚠️ **判"是否真的在服务"要看响应体大小**：站点块没接住该域名时，Caddy 走 NOP —— 状态码看着像 200、**`size=0`**。
只看状态码会把"只握手、不服务"误判为正常。

### 域名解析

- 局域网 DNS 已把 `panomint.` / `photo.` 指向 `192.168.1.115`，**与站点列表一致，无需改动**。
- apex `warlocks.cn` 在 DNS 里指向别处（`192.168.1.55`）—— **这与本站无关，不需要改**：
  本站不服务 apex，它只是证书的覆盖条目。
- 服务器自身 `/etc/hosts` 含这两条站点域名（`panomint.` / `photo.`）。
