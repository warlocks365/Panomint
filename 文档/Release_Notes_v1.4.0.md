# Panomint v1.4.0 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.4.0**（中版本 +1：新端点/新工具/新页签） |
| 发布日期 | 2026-09-26（git 标签 `v1.4.0`） |
| 分支 | main（bump 提交 `4dbf9de`） |
| 版本管理规范 | 《版本管理规范.md》；对应主 Job：Job000121、Job000122 |

---

## 一、更新摘要

v1.4.0 交付「转码远程调试通道」全量能力：当用户环境出现转码失败时，管理员可限时开启一条加密诊断通道，工程师远程只读诊断，无需远程桌面、无需交付服务器口令。

### 1.1 新增

| 模块 | 内容 |
| --- | --- |
| 远程调试通道后端 | 专用迁移（00041）；凭据管理（advisory lock 防并发）；管理端点 status/enable/rotate/disable（admin:system + 审计）；WebSocket 握手 8 步前置矩阵（鉴权/TLS 闸门/凭据校验/失败锁定/握手限流 fail-open）；会话内 8 个只读诊断命令；到期自动回收（reaper） |
| 调试设置卡（前端） | 管理后台新增调试设置卡：权限门、TTL 档位选择、一次性凭据回显 + 复制降级、到期倒计时、两步确认开启、审计摘要列表 |
| debugctl 命令行 | 配套诊断客户端：WSS 接入、快照对齐、8 命令 REPL、指数退避重连（1s→30s ±20%）、`-once` 非交互、`--key-file` 防进程列表泄露；CI 三平台矩阵钉死 |
| nginx 调试路由 | 独立 `^/debug/` location：WS Upgrade 头 + `proxy_read_timeout 3600s` |
| CI 触发优化（Job000122） | push 触发增加 paths-ignore：纯文档/记忆类推送不再烧全量 CI；pull_request 保持全量 |

### 1.2 修复（e2e 九轮逮出的产品缺陷）

| 问题 | 说明 |
| --- | --- |
| jobID 正则多一段 | UUID 正则误含 8-4-4-4-4-12 段，导致一切合法 UUID 被拒 INVALID_PARAMS |
| WSS 回退必死链 | 426 闸门已禁明文 ws，但回退拼接仍产 ws:// 链接 |
| TLS 闸门误杀反代 | 反代终结 TLS 后 `Request.TLS` 恒 nil；补看 `X-Forwarded-Proto`，否则 WSS 全量被 426 误杀 |
| scanChannel NULL 扫描 | `last_connect_ip` 列 COALESCE 兜空串，修复 Enable/Rotate 后 status/Peek/Authenticate 全 500 |
| 到期通道状态不刷新 | Status/Peek 过滤到期行，过期通道状态立即转未开启（否则轮询永久 enabled） |
| compose 环境透传 | 补接 `DEBUG_REAPER_INTERVAL`（ws.go 读取但 compose 从未透传，reaper 周期恒为默认值） |

---

## 二、部署指引

```bash
docker pull warlocks/panomint-web:1.4.0
docker pull warlocks/panomint-app:1.4.0
# 编排中 image tag 改为 1.4.0 后 up -d
```

镜像 digest（Docker Hub 实测）：web `sha256:dc0cc654…`、app `sha256:6e44e2bf…`。

---

## 三、升级注意事项

1. **含数据库迁移**（00041）：首启 api 容器自动执行。
2. **默认关闭**：调试通道默认未开启；仅在需要远程诊断转码问题时由管理员限时开启，到期自动关闭。
3. 若部署使用自定义 nginx（非本仓库模板），需同步新增 `^/debug/` location（WS Upgrade + 长超时），否则调试会话无法建立。
