# Panomint v1.8.4 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.8.4**（修订号 +1：缺陷修复） |
| 发布日期 | 2026-09-29 |
| 修复 Job | Job000138（手机端播放三问题）+ Job000139（分享视频播放失败） |
| 数据库迁移 | 无 |

---

## 修复内容

### 1. 分享链接打开视频黑屏 / 播放失败（核心修复，Job000139）

**现象**：分享链接打开页面正常，点播放后黑屏/失败；同一视频登录后播放正常。

**根因**（三层叠加）：
1. 未转码的视频没有 HLS 产物（master.m3u8 404），而分享端播放器只连 HLS、无回退；
2. 原片 `download` 端点对微信分享（`allow_download=false`）按设计拒绝 403；
3. 实现回退过程中误删 `computed` import，导致点击播放无反应（运行时 ReferenceError）。

**修复**：
- **新增** `GET /public/shares/:token/media/:id/stream` 原片在线播放端点——inline 服务、**原生 Range**（进度拖动）、计访问配额、审计 `share.play`；不受下载开关限制（在线播放 ≠ 下载）
- 分享端播放器（360 全景 + 普通视频）**HLS 探测 404 自动回退原片**——与登录端播放体验对齐
- 恢复误删的 import，点击无反应问题消除

**验证**：端点 `206`/`Content-Range`/`inline`/`video/mp4` 实测 ✓；Chrome 实机 Console 见「HEAD 404 → 回退 → 球面渲染播放」全链路 ✓

### 2. 移动端播放器控制按钮被系统返回浮标遮挡（Job000138）

**原因**：悬浮控件带 `backdrop-filter` 毛玻璃——移动端 WebKit/Blink 在兄弟层移除后不重绘（按钮消失）；且贴顶位置与系统返回手势条重叠。

**修复**：移除播放器全部悬浮控件的 backdrop-filter；移动端按钮组下移至安全区（全屏/码率 top 52px，信息卡联动）。

### 3. 微信内陀螺仪 / VR 无效（引导完善，Job000138）

**原因**：微信内核限制——iOS WKWebView 静默拒绝动作权限申请；安卓 X5 常不派发陀螺仪事件；VR 依赖 WebXR（微信不支持）。属浏览器环境限制而非代码缺陷。

**修复**：开启陀螺仪/VR 失败时给出**明确的受限原因与替代方案**（「点右上角 ··· 选『在浏览器打开』」/「请用 Chrome/Edge」），不再只笼统提示降级。**根治需 https 域名 + 系统浏览器打开**。

## 部署指引

```bash
docker pull warlocks/panomint-web:1.8.4   # 本版 web+app 均有变更
docker pull warlocks/panomint-app:1.8.4   # stream 端点在后端
# Worker/DB 与 1.8.3 相同，可按需拉齐
```

回滚：换回 `1.8.3` 重建 web + app。

## 镜像 digest（2026-09-29 Hub 实测，115 出口独立查询，1.8.4 = latest 逐字一致）

| 镜像 | 1.8.4 = latest digest |
| --- | --- |
| warlocks/panomint-web | `sha256:74fb5a77…` |
| warlocks/panomint-app | `sha256:a0020860…`（stream 端点） |
| warlocks/panomint-worker | `sha256:aba058b9…` |
| warlocks/panomint-db | `sha256:fdc2262f…` |
