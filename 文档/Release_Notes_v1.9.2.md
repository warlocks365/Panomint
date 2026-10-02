# Panomint v1.9.2 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.9.2**（修订号 +1：全站 UI 深化 + 两处修复 + 限流豁免） |
| 发布日期 | 2026-10-02 |
| 数据库迁移 | 无 |
| 源分支 | main（深化流程 12 commits + 发版前验收 2 commits） |

---

## 一、全站 UI 深化（「晨雾 Morandi」逐页落地）

v1.9.1 完成 tokens 层换肤后，本版按「效果图 → 用户确认 → 实装 → headless CDP 验证」流程对 **11 大页面**逐页深化，每页均有 CDP 断言与实拍存证：

- **页头规范**：各视图统一「mono 灰序号（01-12）+ 字距 0.12em 标题 + mono 副题」
- **时间轴/相册/空间/上传**：雾面卡片体系（无边框 + 双层漫射阴影，hover 升 lift）、胶囊分段切换统一交互语言
- **相册**：封面雾面 veil、磨砂语义 tag、hover 封面 scale + 操作钮浮现；详情封面头条（底部 82% 雾面承托）
- **人物/标签**：圆形头像卡体系（聚类虚线区分）、标签云磨砂 chip（AI 待确认燕麦点）
- **文件夹**：树导航 1:2.5 不对称布局、mono 目录计数
- **搜索**：筛选条面板化、磨砂筛选 chips、结果瀑布（虚拟滚动安全版）
- **登录/注册/Boot/Setup**：mist-wash 雾面氛围、wordmark/版本水印、MFA 按需提示块、**启动进度估算**（历史启动时长中位数 + 分项动态进度条 + 预计剩余）
- **管理后台**：11 页签胶囊化、页签切换动效
- **手册**：章节导航卡（mono 序号）、步骤 mono 计数胶囊、原理深入左线体
- **GSAP 动效**：原生 gsap.context + 生命周期清理，prefers-reduced-motion 全跳过

## 二、修复

| 问题 | 根因 | 修复 |
| --- | --- | --- |
| 未登录页面发出管理端请求（401×2 并触发刷新链） | AgentPanel 顶层无条件挂载，权限试探在公开路由变 401 | 公开路由（/login /register /boot /setup）不发试探，进入 authed 路由再试探 |
| 管理后台 → 扫描导入面板运行时错误（ReferenceError） | StorageScanPanel import 漏 `onMounted` | 补 import |

## 三、限流调优

浏览类缩略图（`GET /media/:id/thumb`）**豁免全局限流**——地图/网格页单屏并发拉数十张缩略图会耗尽 60 令牌桶（实测 429×43）。thumb 仍有 JWT 鉴权与 mediascope 可见性校验，豁免仅放宽频控、不降低鉴权强度（2026-10-02 用户裁决「按推荐来」）。

## 部署指引

```bash
docker pull warlocks/panomint-web:1.9.2     # 本版 web（深化 dist）与 app（限流豁免 + 版本串）变更
docker pull warlocks/panomint-app:1.9.2
# worker 内容同 1.9.1（tag 递增）；db 零变化
```

## 镜像 digest（2026-10-02 Hub 实测后回填）

| 镜像 | 1.9.2 = latest digest |
| --- | --- |
| warlocks/panomint-web | `sha256:c3092cda9627…`（深化 dist + 限流豁免前端） |
| warlocks/panomint-app | `sha256:4cec9097e1c5…`（版本串 1.9.2 + 缩略图限流豁免） |
| warlocks/panomint-worker | `sha256:cc1383fbbfdd…`（entrypoint 修复版：弃 heredoc 改真实文件，修群晖部署崩溃 · 2026-10-02 深夜重推） |
| warlocks/panomint-db | `sha256:53f4e33716b0…`（同 1.9.1 内容） |
