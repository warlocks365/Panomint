# Phase 3 验收报告 · P0 功能完整化（群晖级浏览 MVP）

> 日期：2026-08-25 ｜ 状态：**全部完成并通过独立验证**
> 执行：MVP 开发专家团 5 组 Wave 制并行（W1：G1 前端骨架 + G5 后端端点；W2：G2 时间轴 / G3 空间文件 / G4 360 集成）

---

## 一、交付总览

| 任务 | 内容 | 验收 |
|------|------|------|
| T3.1 前端骨架 | Vue3+Vite+Pinia+Router、登录页、Token 刷新拦截（Promise 单例防并发）、路由守卫、App 外壳（7 项侧栏） | ✅ |
| T3.2 时间轴首页 | 年/月/日/全部四视图 + buckets 钻取 + 面包屑 + vue-virtual-scroller 虚拟滚动 + 游标无限加载 + 360/视频角标 + 类型/收藏过滤 | ✅ |
| T3.3 双空间+文件夹 | 个人/共享空间切换卡（用量统计）、目录树+网格前缀联动 | ✅ |
| T3.4 上传下载 | 拖拽+多选+队列进度/速度+失败重试+>8MB 分块续传（409 断点解析）+并发上限 2 | ✅ |
| T3.5 360 引擎集成 | 原型六子系统迁移为 360Player.vue（球面渲染/陀螺仪/校准/WebXR/ABR/错误恢复/性能降档）+ needs_transcode 一键转码轮询自动加载 + HLS Bearer 鉴权注入 | ✅ |
| T3.6 查看器 | 大图/视频播放、EXIF/视频信息/GPS、0-5 星评分、收藏、软删、键盘切换 | ✅ |
| 后端端点包 | 详情/上传分块/下载/收藏/评级/软删/回收站/空间/目录树/360信息/转码任务/HLS 静态服务（鉴权+防穿越）/缩略图服务 | ✅ |

## 二、独立验证结果（项目总监抽查）

- 后端 `go vet + go build` 全绿；前端 `vite build` 通过（PlayerView chunk 1MB 为 three.js，路由级懒加载）
- 全栈冒烟：health OK / 登录 / media total=113 / 详情 / 空间 / 目录树 / 360 端点 / 缩略图 200 image/webp
- 权限边界：viewer 上传/转码/删他人媒体 403；HLS 无鉴权 401；路径穿越 400
- 上传完整性：9 块续传合并后 sha256 与原文件一致

## 三、过程中修复的问题

1. timeline.go 既有 bug：favorites 过滤引用 `a.kind`（DDL 实为 `a.type`）— G5 修复
2. MediaRef 缺 folder_path + folder 过滤参数 — 主线补齐（G3 文件夹视图依赖）
3. 缩略图无 HTTP 服务 — 新增 `GET /media/:id/thumb?size=sm|md|lg`
4. CORS 未放行 Content-Range — 预检修复（分块上传浏览器端依赖）
5. PlayerView 普通视频分支 loading 时序 bug — G4 修复
6. fmt.Sprintf 双占位符只传单值 → SQL `%!d(MISSING)` — 主线修复

## 四、遗留事项（不阻塞）

| 项 | 说明 | 建议 |
|----|------|------|
| 新上传媒体缩略图 | 上传入队正常，但需 indexctl worker 常驻消费 | 部署阶段做常驻服务 |
| 微信陀螺仪 | 诊断面板已就绪，待用户微信内截图反馈 | 用户提供诊断数据后修复 |
| 8K60 真机验证 | 测试流已生成（streams8k），待真机播放验证 | 用户真机测试 |
| 环境临时文件 | frontend 根目录若干 .verify/.build 临时文件被安全组件锁定 | 锁释放后手动删除 |
| dist 构建锁 | vite 清空 dist 触发环境安全组件拦截 | 用 --outDir 独立目录或手动清 dist |

## 五、运行入口

- 前端 dev：`src/frontend` → `npm run dev` → http://localhost:5173（admin@pano.local / pano-admin-dev-only）
- 后端 API：http://localhost:8080（健康检查 /health /ready /metrics）
- 转码 worker：`cd src/backend && FFMPEG_PATH=C:/Users/warlocks/Tools/ffmpeg/bin go run ./cmd/transcodectl worker`
