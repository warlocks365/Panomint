# Panomint v1.5.0 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.5.0**（中版本 +1：新设置面 + 访问守卫强化） |
| 发布日期 | 2026-09-26（git 标签 `v1.5.0`，2026-09-26 补打归档） |
| 分支 | main（功能提交 `d1d2075`、文档 `fd81110`） |
| 版本管理规范 | 《版本管理规范.md》；对应主 Job：Job000125 |
| 设计文档 | 《流媒体与访问控制设计_HLS_HTTPS_访问守卫_v1.0.md》 |

---

## 一、更新摘要

v1.5.0 把「流媒体质量」与「访问入口安全」两端设置面补齐：HLS 分片与缓存策略可调、HTTPS 强制跳转与证书上传可视化管理、登录后回跳原页面、初始化向导守卫加固。

### 1.1 新增

| 模块 | 内容 |
| --- | --- |
| HLS 流媒体设置 | 管理后台转码页签新增设置卡：分片时长 `hls_seg_seconds`（默认 4s，仅影响新转码任务）、缓存策略 `hls_cache_profile` 三档（balanced=历史行为/以 HLSCacheControl 纯函数输出）、`stream_base_url` 自定义（http(s) 校验）；运行期 60s 缓存热读，改配置即生效 |
| HTTPS 强制跳转 | 管理后台新增「网络」页签（第 10 页签）：ForceHTTPS 中间件挂 CORS/限流前，仅对 `X-Forwarded-Proto==http` 的 API 面 301（缺失不拦，防无反代死局）；`/health` `/ready` 豁免；变更后缓存立即失效生效 |
| 证书上传 | 管理后台上传 PEM 证书/私钥：1MB 上限 → PEM 预检 → X509KeyPair 配对校验（失败一字节不落盘）→ 服务端定死文件名、0644/0600 权限落 `HTTPS_CERT_DIR`（落 mediadata 卷随备份车）；NotAfter 到期日入库展示；按提示 restart caddy 生效 |
| 登录回跳 | 鉴权闸门携带 `?redirect=`，登录成功后回跳原页面；`safeInternalPath` 校验（拒 `//`、scheme:、控制字符、回 login/setup）——开放重定向防御 |
| 向导守卫优化 | 初始化闸门 15s TTL 缓存 + 主动失效，减少每请求探测 |

### 1.2 内部质量

- 迁移 00042：`system_transcode_config` 增三列（默认值=存量行为）+ 新表 `system_https_config`（singleton）
- pgx nil 指针 42804 同族修复（COALESCE 显式 cast 锚定）
- API e2e 38/38 + UI e2e 20/21 全语义成立；服务器源码树第 4 次漂移由发版纪律比对逮住并补同步（188/188）

---

## 二、部署指引

```bash
docker pull warlocks/panomint-web:1.5.0
docker pull warlocks/panomint-app:1.5.0
# 编排中 image tag 改为 1.5.0 后 up -d
```

镜像 digest（Docker Hub 实测）：web `sha256:15afcade…`、app `sha256:51a356d2…`。

---

## 三、升级注意事项

1. **含数据库迁移**（00042）：首启 api 容器自动执行（新增列默认值=存量行为，零人工动作）。
2. HTTPS 强制跳转默认关闭；开启前请确认反代链路（Caddy/nginx）已正确传递 `X-Forwarded-Proto`，否则 API 请求会被 301 到 https 而本地无 TLS 终结时不可达。
3. 证书上传后需按页面提示手动 restart caddy 生效（本版不做 TLS 运行时热切换）；证书目录已纳入 mediadata 备份车。
4. HLS 分片时长变更仅对新转码任务生效，存量流保持原分片时长。
