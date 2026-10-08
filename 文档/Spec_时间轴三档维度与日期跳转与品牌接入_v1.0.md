# Spec — Panomint 时间轴三档维度 + 日期跳转 + 品牌 Logo 全站接入 v1.0

> 生成日期：2026-10-08
> 状态：**待用户确认**
> 任务编号：**Job000144**（品牌 Logo 全站接入）/ **Job000145**（时间轴三维度 + 日期选择器）
> 性质：**存量项目增量**，非绿地 MVP。基线 v1.9.4，不改技术栈、不改架构分层
> 依据：PM 交互规格 + 架构师增量技术设计 + 设计师增量设计规格 + 项目总监 7 项裁定
> 本期交付边界：部署至 **115 测试服实装验证**，**暂不进入正式发版流程**，待验收后另行安排

---

## 1. 产品定义

- **一句话**：给 Panomint 时间轴加「年/月/日」三档分组维度与日历锚点跳转，并把正式 Logo 接入全站品牌位
- **目标用户**：自托管 Panomint 的个人/家庭相册使用者
- **核心问题**：现有时间轴分组标题硬编码为「月+日」两层，用户无法按年组织视图，也没有精确的日期跳转入口

---

## 2. 本期范围（锁定 —— 不在此列表的功能一律不做）

| 优先级 | 功能 | 验收摘要 | Job |
|---|---|---|---|
| P0 | 时间轴三档维度切换（年/月/日） | 切换只改分组标题粒度，不改媒体范围、不发请求、不重置分页 | 000145 |
| P0 | 锚点跳转（切换维度保持视口锚点） | 切换后锚点媒体仍在视口顶部同一像素偏移 ±2px | 000145 |
| P0 | 日期选择器（日历 popover，高亮有照片的日期） | 自建日历，非原生控件；无照片日期灰显不可选 | 000145 |
| P0 | 后端 `/media/date-histogram` 扩 `day` 粒度 | `granularity=day` 返回 `YYYY-MM-DD` 桶 | 000145 |
| P0 | 正式 Logo 全站品牌位接入 | favicon / PWA maskable / manifest / 登录页 / 侧栏 / 空状态 | 000144 |
| P1 | 键盘可达性（roving tabindex + APG Radio Group） | 方向键切档、焦点不丢回 body | 000145 |

## 3. 明确不做（Out-of-Scope —— 锁定，防范围蔓延）

| 不做 | 原因 | 何时考虑 |
|---|---|---|
| 媒体范围筛选（按日期筛出孤立网格） | 用户决策 2 锁定为锚点跳转 | 用户反馈后 |
| 维度切换引起缩略图密度变化 | 决策 1 锁定「数据流不变」，密度变化会使锚点算法失效 | 用户反馈后 |
| 维度档位持久化到 `/user/ui-prefs` | 需 DB 迁移，爆炸半径不值 | v2.0 |
| day 桶 from/to 区间参数 | 20000 桶 ≈ 54 年照片，对家庭相册是病态场景 | 库规模触顶时 |
| 时区可切换 / 跨时区日边界选项 | 本期钉死「日边界以库统一 TZ 计算」 | 用户投诉跨时区后 |
| 区间跳转 / 「回到今天」按钮 | 属筛选与新入口 | v2.0 |
| 日历缩略图预览格 | 需按日聚合缩略图的新后端查询 | v2.0 |
| 维度切换过渡动画（FLIP/Cross-fade） | 与「布局不位移」冲突，且竞品已证明动效+大数据量=卡顿 | — |
| 修改 `/media` 的 `view` 语义 | 已有 year/month/day 三档，属超范围 | — |
| 新增索引 / 数据库迁移 | `date_trunc` 走不了索引，本查询本就是聚合全扫 | — |
| **`og:image` / `twitter:image` 品牌图接入** | 需**产品决策**先确定对外分享用哪个 host。本项目三入口自托管（内网域名 `panomint.warlocks.cn` / 局域网 IP:8088 /隧道 `*.trycloudflare.com`），而 `og:image` 必须是**绝对 URL**，写死任一 host 都会在其余入口失效。正解是复用后端 `shares/og.go` 已有的 `originOf()`（按请求 Host 动态拼前缀），属后端职责、不该在品牌接入任务里顺手定。另：设计规格 §7.5 要求 OG 用「全标记版 + `#e9e4de` 合成」，而§8.2 资产表第 7 行的 `logo-full-256.png` 定义为**透明**，两处口径冲突；且合成 OG 资产不在锁定的 10 个文件内 | 先定对外分享 host，再由后端出 1200×630 |
| `frontend_org_guard` 存量欠账治理 | 避免 diff 噪音掩盖真实回归 | 独立立项 |
| `--color-text-secondary` 对比度修正（3.80:1 < AA） | 全站视觉重量变更，影响面超本次增量 | 独立立项 |

---

## 4. 技术架构（锁定，含版本锚定）

| 层 | 技术 | 版本 | 锁定原因 |
|---|---|---|---|
| 前端 | Vue | 3.x（既有） | 不换 |
| 前端构建 | Vite | 既有 | 不换；`publicDir` 未显式配置，默认 `public/` |
| 前端状态 | composable（`use*`） | 既有模式 | 与 `useTimelinePager/Grouping/Seek` 一致 |
| 虚拟滚动 | vue-virtual-scroller | 既有 | 不换 |
| 后端 | Go + Gin | 1.26 | 不换 |
| 数据库 | PostgreSQL + PostGIS + pgvector | 16 | 不换；**无迁移** |
| 缓存 | Valkey | 既有 | 不换 |
| 认证 | 既有 JWT | — | 不动 |

**部署要点（已实测）**：web 镜像 `COPY dist` → 静态资源**必须**放 `src/frontend/public/`（vite 复制到 dist 根）。`assets/brand/` 前端零引用，**放错目录全站 Logo 404**。

---

## 5. API 端点清单（增量）

### 5.1 变更：`GET /media/date-histogram`

| 项 | 变更前 | 变更后 |
|---|---|---|
| `granularity` | `year` \| `month` | `year` \| `month` \| **`day`**（新增） |
| 默认值 | `month` | `month`（不变） |
| 非法值响应 | 400 `INVALID_PARAMS` + `granularity 仅支持 year\|month` | 400 `INVALID_PARAMS` + **`granularity 仅支持 year\|month\|day`** |
| 响应体 | 裸数组 `[{bucket,count}]`（**非包裹对象**） | 不变 |

**向后兼容**：year/month 两档的请求与响应**逐字节不变**。

**实现收敛（3 处）**：
1. `internal/media/histogram.go:23-26` — `histogramTrunc` 加 `"day": {"day","YYYY-MM-DD"}`。SQL 骨架逐字节不动（`date_trunc`/`to_char` 参数全部来自包内 map，无注入面）
2. `internal/media/handlers.go:140` — 硬编码 `if` 改为查 `histogramTrunc` map，与 store 同源**消除双份真源**（现状双份，只改 map 会出现「day 上线即 400 而 store 层测试全绿」的沉默失效）
3. `ErrInvalidGranularity` 文案更新

> **陷阱**：后端路由**无 `/api/v1` 前缀**。写错会落到 SPA 回退返回**假 200 HTML**，验证必须查 body 不能只看状态码。
> **陷阱**：`taken_at IS NULL` 的媒体归入 `"unknown"` 桶，前端消费时必须剔除，不得当日期字符串解析。

**契约文档**：`文档/API详细契约_v1.2.md` 正文 v1.16 → **v1.17**（**文件名不改**，改名会打断既有引用）。

### 5.2 不新增端点

---

## 6. 数据库表清单

**无新增表、无新增列、无迁移。** `migrations` 停在 `00047` 不动。

---

## 7. 改动文件清单（锁定，逐文件逐行）

### 7.1 后端（Job000145）

| 文件 | 改动 | 行数影响 |
|---|---|---|
| `internal/media/histogram.go` | `histogramTrunc` 加 day 项 + 注释 | +2 |
| `internal/media/handlers.go:140` | 硬编码 if → 查 map | 净 0 |

**明确不改**：`/media` 的 `view` 语义、`timeline.go` 的 `viewTrunc`（已有三档）。

### 7.2 前端（Job000145）

**新建**：
- `components/timeline/timelineDimensions.js` — **单一高度真源** `HEADER_HEIGHT = { year:56, month:46, day:34 }`
- `components/timeline/useTimelineDimension.js` — 当前档位状态
- `components/timeline/DimensionTabs.vue` — 三段式分段控件
- `components/timeline/DatePickerPopover.vue` — 日历弹层
- `components/timeline/__tests__/` — vitest 单测（`resolveAnchorIndex` 抽为不依赖 Vue 的纯函数）

**修改**：
| 文件 | 改动 | 行数红线 |
|---|---|---|
| `useTimelineGrouping.js` | 参数化档位；`monthIndex` → `headerIndex`（**不保留旧名、不新增三张 map**）；三档键定长零填充 | < 300 |
| `useTimelineSeek.js` | `seekTo({dimension,key})`；锚点三段式定位 | < 300 |
| `useTimelinePager.js` | `loadHistogram` 参数化档位（现硬编码 `granularity:'month'`）+ 桶数上限保护 | < 300 |
| `DateSlider.vue` | 桶适配器 + `seek` 载荷改 `{dimension,key}` | < 300 |
| `TimelineGrid.vue` | **仅装配，不得净增**（当前 355 行，登记基线 326，**存量欠账，本次只减不增**） | **≤355** |
| `TimelineView.vue` | 引入维度控件 + 日历入口 + 内联图标 | < 300 |
| `index.html` | favicon 引用 | — |

### 7.3 前端（Job000144 品牌）

**新建 `src/frontend/public/brand/`**（10 个文件，参数见 §8.2）

**修改**：`manifest.webmanifest`（`background_color` → `#e9e4de`、`theme_color` → `#4a5a6a`、icons 数组）、`index.html`、`App.vue`/登录页/侧栏/空状态等品牌位组件。

**11 个手写 SVG 图标一律内联进 .vue，禁止混用**（`currentColor` 可用、0 额外 HTTP、与 `navIcons.js` 惯例一致）。本次无复用，**不建 `icons.js`**。

---

## 8. 设计规格（锁定）

### 8.1 三档维度

| 档位 | 唯一产出标题 | 标题高度 | 字号 | 字重 |
|---|---|---|---|---|
| `year` | `2024年` | **56px**（新增） | `--font-size-lg` 16px | 600 |
| `month` | `2024年6月` | 46px（沿用） | `--font-size-lg` 16px | 600 |
| `day` | `2024年6月14日` | 34px（沿用） | `--font-size-sm` 13px | 500 |

**不变式（最易踩坑）**：`:min-item-size` 必须 ≤ 三档最小标题高度。当前最小仍是 day 的 34px，与现状一致 ⇒ **`TimelineGrid.vue:15` 的 `:min-item-size="34"` 无需改动**。

> ⚠️ **方向陷阱**：若日后把 `HEADER_HEIGHT.day` 调到 34 以下，**必须同步下调 `min-item-size`**，否则切到 day 档时虚拟滚动算小总高、出现滚动抖动。须写入代码注释。

**默认档位 = `month`**。理由：现状双层标题 46/34 来自 `useTimelineGrouping.js`，`seekTo()`/`lastLoadedMonth()` 全以月键为单位，默认月档保证首屏与滑块月刻度对齐，且回归面最小。

**键空间（定长零填充，强制）**：

| 档位 | `anchorKey` | 示例 |
|---|---|---|
| year | `YYYY` | `2024` |
| month | `YYYY-MM` | `2024-06` |
| day | `YYYY-MM-DD` | `2024-06-14` |

> **零填充不可省**：锚点兜底的「字典序 == 时间序」与翻页判断的 `last <= key` 都依赖定长。为缩短键而去掉补零会直接破坏锚点定位。

**`seek` 载荷裁定**：采用 **`{ dimension, key }` 对象**（架构师方案），**PM 原定的裸字符串 `seek(dateKey)` 作废**。理由：靠字符串长度推断档位是本项目已栽过的坑（本次会话中曾因 grep 子串 `"year|month"` 命中 `"year|month|day"` 而误判测试断言）。对象载荷让不匹配**显式失败**。

**控件**：三段式分段控件，置于**工具条内筛选器之后**（不在 spacer 之后——那里是回收站等破坏性动作，视图粒度与其同列会削弱后者可预期性）。`role="radiogroup"` + roving tabindex（组内恰好一个 `tabindex="0"`），方向键/Home/End 切档并**立即切换**，切换后焦点**不得丢回 `<body>`**。禁止全局劫持方向键（WCAG 2.1.4）。

### 8.2 Logo 资产（锁定参数）

**源图裁切框**（1024 坐标，左闭右开）：
- 全标记版 `x:169, y:251, w:663, h:551`（aspect 1.203）
- 盘面版 `x:295, y:251, w:411, h:410`（aspect 1.002）

| # | 文件 | 画布 | 标记尺寸 | 偏移 | 底色 | 圆角 |
|---|---|---|---|---|---|---|
| 1 | `favicon-16.png` | 16 | 14×14 | (1,1) | `#e9e4de` | 0 |
| 2 | `favicon-32.png` | 32 | 28×28 | (2,2) | `#e9e4de` | 0 |
| 3 | `favicon-48.png` | 48 | 42×42 | (3,3) | `#e9e4de` | 0 |
| 4 | `apple-touch-icon-180.png` | 180 | 97×97 | (42,42) | `#e9e4de` | 0 |
| 5 | `maskable-192.png` | 192 | 104×104 | (44,44) | `#e9e4de` | **0** |
| 6 | `maskable-512.png` | 512 | 276×275 | (118,119) | `#e9e4de` | **0** |
| 7 | `logo-full-256.png` | 透明 | 256×213 | — | 透明 | — |
| 8 | `logo-full-128.png` | 透明 | 128×106 | — | 透明 | — |
| 9 | `logo-disc-64.png` | 透明 | 64×64 | — | 透明 | — |
| 10 | `logo-disc-32.png` | 透明 | 32×32 | — | 透明 | — |

> **7–10 号尺寸口径＝文件名承诺的标称尺寸**（Job000144 返工修正）。初版曾按裁切框原始
> 像素出图（7 号实为 663×551），导致文件名对外承诺的数字与文件真实尺寸脱节、
> manifest 被迫写 `"sizes": "663x551"`，且单个文件占全部品牌资产 43%。
> 现按标称宽等比缩放：全标记版 663:551 → 256×213 / 128×106；盘面版取正方形。
> **只缩放不裁剪**，盘面保持完整圆形不切边。
>
> 盘面版（9/10 号）**四边满铺、四角不透明**（实测源裁切框四边余量 L0 T0 R0 B0），
> 这是 artwork 本身即铺满裁切框的原图特性，不是切边缺陷。若后续要把盘面用作需要留白的
> inline 图标，须自行补 padding。

**maskable 安全区计算（设计师已自我纠正上一版错误）**：
约束 `f × 系数 ≤ 0.40`，其中 `系数 = maxR / 裁切框宽`。
- 全标记版 `系数 0.5354` ⇒ `f ≤ 0.7470` → 取 **f = 0.72**（极半径 38.6%，余量 1.4pp）
- 盘面版 `系数 0.7056` ⇒ `f ≤ 0.5669` → 取 **f = 0.54**（极半径 38.1%，余量 1.9pp）

> ⚠️ 上一版「69.3% < 80% 天然合规」的结论**已作废**——那是在原始 1024 画布上测的（含大量留白），裁切重排后极坐标半径会显著放大。必须在「裁切后重排」的真实工况下重算。**盘面版反而更严**，因为它近似正圆。

**圆角一律 0**：maskable 由系统按 mask 形状二次裁切，预置圆角会出现「圆角叠圆角」瑕疵。

**Logo 的「分裂性格」（实测，推翻过错误归因）**：

| 元素 | 实测色 | vs 深色底 `#14181d` | vs 暖灰底 `#e9e4de` |
|---|---|---|---|
| 字标「panomint」 | `rgb(4,33,59)` 深墨 | **1.09:1 等于没看见** | **12.92:1 优秀** |
| 胶片盘主体 | `rgb(57,156,209)` 青 | 5.81:1 良好 | 2.43:1 偏弱 |

→ **字标必须浅底、盘面宜深底。深色页（player/share）不放 Logo。**

**使用场景映射**：

| 位置 | 用哪版 | 具体资产 | 底色 |
|---|---|---|---|
| favicon 16/32/48 | 盘面版 | `favicon-16/32/48.png` | `#e9e4de` 合成 |
| apple-touch 180 | 盘面版 | `apple-touch-icon-180.png` | `#e9e4de` 合成 |
| maskable 192/512 | 盘面版 | `maskable-192/512.png` | `#e9e4de` 合成 |
| PWA icon（any） | 全标记版 | `logo-full-256.png` | 透明 |
| 登录/注册/Setup | 全标记版 | **`logo-full-256.png`**（显示高 72px，需 2x 余量） | 透明 |
| 侧栏展开态 | 全标记版 | `logo-full-128.png`（显示高 28px，128 已有 3.8x 余量） | 透明 |
| 侧栏图标态 56px | 盘面版 | `logo-disc-64.png` | 透明 |
| OG 图 | — | **本期不做**，见 §3 Out-of-Scope | — |
| 时间轴空状态 | 盘面版 | `logo-disc-64.png` | 透明（替换既有 40px 线框图标） |
| 页头 topbar | **不放** | — | — |
| **深色页 UI** | **不放** | — | — |

> **认证三页为何用 256 而非 128**（Job000144 返工裁定）：登录/注册/Setup 的 Logo 显示高
> 72px，2x 屏需≥144 设备像素。`logo-full-128`（128×106）只有 **1.47x**，欠采样发虚；
> `logo-full-256`（256×213）为 **2.96x**，2x 屏有余量。侧栏显示高仅 28px，
> 128 已达 3.8x，换 256 纯属浪费。体积代价 +65KB，且只在**未登录**的三页加载，
> 登录后走侧栏的 128，对实际流量影响可忽略。

**残差缺陷已被选底色顺带消解**：资产残留近白（242–255）对 `#e9e4de` 仅 1.13–1.26:1（肉眼不可见），对 `#14181d` 达 16.78:1（刺眼白斑）。选暖灰底**无需额外修图**即消除残留。

**manifest 裁定**：`background_color` `#14181d` → **`#e9e4de`**（消除「桌面图标浅底、开 App 深底闪屏」的割裂）；`theme_color` `#14181d` → **`#4a5a6a`**（`--color-primary`，浏览器 chrome 品牌识别）。

**硬编码 P0 豁免（批准，范围严格限定）**：仅 `src/frontend/public/manifest.webmanifest` 的 `theme_color` / `background_color` 两个 JSON 字段（PWA 规范强制，CSS 变量不可达）。白名单精确到「文件路径 + JSON 键」二元组，**不做目录级或文件级豁免**。**不得扩展到任何 .vue/.css/组件**。

`sw.js` **无需改**：新资产命中既有 `STATIC_RE`（含 png|svg|ico）走 SWR 缓存，不在 `SHELL` 预缓存清单。

### 8.3 Design Token

**新增 0 个、修改 0 个全局 token。** `tokens.css` 一行未改。
硬编码 P0 豁免仅限 §8.2 所列两个 manifest JSON 字段。

---

## 9. 验收标准（EARS 格式，QA 据此唯一判定）

| 编号 | 验收标准 | 优先级 |
|---|---|---|
| AC-01 | While 处于 `year` 档, When 渲染任意分组, the 系统 must 只输出 `2024年` 形式标题，且网格中**不得出现任何月或日标题** | P0 |
| AC-02 | While 处于 `month` 档, When 渲染分组, the 系统 must 只输出 `2024年6月` 形式标题 | P0 |
| AC-03 | While 处于 `day` 档, When 渲染分组, the 系统 must 只输出 `2024年6月14日` 形式标题 | P0 |
| AC-04 | While 切换档位, When 执行切换, the 系统 must 不发起任何网络请求，且不改变已加载 `items`、`nextCursor` 与任何筛选参数 | P0 |
| AC-05 | While 已滚动到列表中部, When 切换档位, the 系统 must 以视口顶部完整可见的媒体为锚点，重定位后该媒体仍在视口顶部同一像素偏移（±2px） | P0 |
| AC-06 | While 锚点媒体因分页未加载无法定位, When 切换档位, the 系统 must 降级用其 `taken_at` 定位同月首项；再失败则保持 scrollTop 不变，且每次降级 must 上报埋点 | P0 |
| AC-07 | While 分页加载进行中, When 用户切换档位, the 系统 must 把切换延后到加载完成并立即执行，期间控件 `aria-busy="true"`；**must 不静默丢弃该操作** | P0 |
| AC-08 | While 媒体总数为 0, When 切换档位, the 系统 must 正常完成渲染层级切换且不报错；日期跳转按钮 `aria-disabled="true"` | P0 |
| AC-09 | While 某日期 day 计数 > 0, When 渲染日历, the 系统 must 渲染为可选态并在日期下方显示主色实心圆点 | P0 |
| AC-10 | While 某日期 day 计数为 0 或超出媒体时间范围, When 查看该日期格, the 系统 must 灰显且 `aria-disabled="true"`，`Enter`/`Space`/点击均不触发跳转 | P0 |
| AC-11 | While 存在 `taken_at IS NULL` 媒体, When 渲染日历, the 系统 must 不为 `unknown` 桶渲染任何标记，且不允许跳转到 unknown | P0 |
| AC-12 | While 选中某有照片的日期, When 跳转生效, the 系统 must 滚动定位到该日期分组首项、关闭日历、焦点还给触发按钮，且 **`/media` 查询参数与切换前逐字段相同** | P0 |
| AC-13 | While 目标日期分组尚未加载, When 执行跳转, the 系统 must 先按方向预取至覆盖目标日期后定位；预取期间 must 显示加载态 | P0 |
| AC-14 | While 分页请求在途, When 执行日历跳转, the 系统 must 不 abort 在途请求，且丢弃其响应不追加到列表，避免游标错乱导致重复或漏项 | P0 |
| AC-15 | While 正在拖拽滑块, When 点击日历, the 系统 must 立即终止拖拽与惯性，只执行最后一次跳转意图 | P0 |
| AC-16 | While 正在拖拽滑块, When 尝试切换档位, the 系统 must 拒绝该次切换（控件禁用），松手后恢复 | P0 |
| AC-17 | While 焦点在分段控件组内, When 按方向键/Home/End, the 系统 must 按 APG 语义移动焦点并立即切换档位，首尾循环 | P0 |
| AC-18 | While 档位刚切换完成, When 检视 `document.activeElement`, the 系统 must 保持焦点在分段控件上，**不丢失到 `<body>`** | P0 |
| AC-19 | While 焦点不在分段控件上, When 按方向键, the 系统 must 不响应（禁止全局劫持，WCAG 2.1.4） | P0 |
| AC-20 | While 校验响应, When 请求 `granularity=day`, the 系统 must 返回 200 + 裸数组；请求非法粒度 must 返回 400 `INVALID_PARAMS` + `granularity 仅支持 year\|month\|day` | P0 |
| AC-21 | While 校验响应用于探测, When 请求不存在的路径, the 系统 must 返回**非 HTML**（防 SPA 假 200 掩盖错误） | P0 |
| AC-22 | While 打开应用, When 浏览器加载图标, the 系统 must 从 `/brand/` 路径成功取到全部 6 个合成图标（非 404） | P0 |
| AC-23 | While 在深色页（player/share）, When 检视 UI, the 系统 must **不放**全标记版 Logo（字标对比度 1.09:1 不可见） | P0 |

---

## 10. 边界与约束

- 不支持 IE；响应式断点沿用既有（移动端 cell 44px，日历网格 332px ≤ 374px 可用宽，375/390 均不横向溢出）
- 性能目标：维度切换为纯前端计算，**O(1) 按 offset 定位，禁止比例估算或遍历**（竞品 Immich 已因滑轨估算导致大库跳转偏移并收到 App Store 差评）
- 三档标题高度不得低于 34px，否则须同步下调 `min-item-size`
- 三档键必须定长零填充

---

## 11. 内嵌已知坑（从项目源码与记忆拉取）

| # | 坑 | 硬约束 |
|---|---|---|
| K1 | 后端路由**无 `/api/v1` 前缀**，写错落 SPA 回退返回**假 200 HTML** | 验证必须查 body 的 `content-type`，不能只看状态码 |
| K2 | `TimelineGrid.vue` 的 `loadMore()` 只传 `{view:'all',limit}` **不传 `space`**，已登记为泄漏分支（`文档/待解决问题记录.md:739-740`） | 本期新增的任何请求**必须显式带 `space`**，禁止复制该模式 |
| K3 | `/media/date-histogram` 返回裸数组且含 `"unknown"` 桶 | 前端 day 粒度消费同样必须剔除 `unknown` |
| K4 | `TimelineGrid.vue` CSS 标题高度 与 JS `MONTH_H/DAY_H` 与 `:min-item-size` 是**三处重复魔数** | 本期收敛到 `timelineDimensions.js` **单一真源**，禁止留三处手工同步 |
| K5 | `TimelineGrid.vue` 当前 355 行 > 登记基线 326（存量欠账） | 本期**不得净增**，只减不增；`frontend_org_guard` 存量 FAIL 属既有债，验收判据用「新增违规数为 0」 |
| K6 | `p0_guard` R4 因基线未建立而 FAIL（既有债） | 同上，判据用「新增违规数为 0」 |
| K7 | 竞品 Immich `localDateTime` 与浏览器时区不一致导致日期标题差一天 | 日边界以**库统一 TZ** 计算，不得引入浏览器本地 TZ |
| K8 | 115 服务器无法回环访问自身公网域名 | 验证必须从本机发起，并做 local vs remote 字节比对 |
| K9 | 115 的 `pano-album/src/backend/internal/` 存在写入回滚怪机制 | 后端同步走 `sync_and_test.py`（写 `/home/warlocks/gowork` + docker 挂载跑测试），**不写运行栈那棵树** |
| K10 | `golang:1.26` 镜像 PATH 被覆写 | 必须显式 `export PATH=$PATH:/usr/local/go/bin` |
| K11 | compose 同 tag 重建不 recreate | 部署用 `up -d --force-recreate <svc>`，**禁用 `down`** |

---

## 12. 端到端验证步骤（本期最后一项）

```bash
# 0) 基线（部署前必做，用于区分存量问题与本次回归）
curl -sS -k -o /dev/null -w '%{http_code}\n' https://photo.warlocks.cn:11543/
# 已知基线：panomint.warlocks.cn 为 502（存量，勿当回归）

# 1) 后端同步 + vet + test（不得写入运行栈源码树）
python .workbuddy/sync_and_test.py --all

# 2) 契约一致性
go vet ./... && go test -count=1 ./internal/media/... ./internal/httperr/... ./internal/mediascope/...

# 3) 前端构建（dist 全量轮转，核验看 entry 实际 chunk）
cd src/frontend && npm run build

# 4) 断言 day 粒度真实生效（查 content-type，防 SPA 假 200）
curl -sS -k 'https://photo.warlocks.cn:11543/media/date-histogram?granularity=day' -D- -o /tmp/h.json | grep -i content-type
# 期望 application/json（非 text/html）
python -c "import json;d=json.load(open('/tmp/h.json'));print('bucket样例',d[0]['bucket'],'类型',len(d[0]['bucket']))"
# 期望 bucket 形如 2024-06-14（10 字符）

# 5) 断言非法粒度文案已更新
curl -sS -k 'https://photo.warlocks.cn:11543/media/date-histogram?granularity=hour'
# 期望 400 + "granularity 仅支持 year|month|day"

# 6) 断言 Logo 资产可达（6 个合成图标）
for f in favicon-16.png favicon-32.png favicon-48.png apple-touch-icon-180.png maskable-192.png maskable-512.png; do
  printf '%s %s\n' "$f" "$(curl -sS -k -o /dev/null -w '%{http_code}' https://photo.warlocks.cn:11543/brand/$f)"
done   # 期望全 200

# 7) 断言 transparent 资产（4 个）
for f in logo-full-256.png logo-full-128.png logo-disc-64.png logo-disc-32.png; do
  printf '%s %s\n' "$f" "$(curl -sS -k -o /dev/null -w '%{http_code}' https://photo.warlocks.cn:11543/brand/$f)"
done

# 8) 门禁（判据：新增违规数为 0，存量欠账不计入）
python scripts/p0_guard.py
python scripts/frontend_org_guard.py

# 9) 浏览器实测（headless CDP 真实打开，非仅 curl）
#    - 三档切换，年档下断言无月/日标题节点
#    - 断言 HEADER_HEIGHT.year(56) > month(46) > day(34)
#    - 断言 min-item-size 未被改动
#    - 滚动到中部切档，断言锚点媒体仍在视口顶部 ±2px
#    - 切档前后比对 /media 请求体逐字段相同、nextCursor 未变、零新增请求
#    - 日历：有照片日期有圆点、无照片 aria-disabled、点击无反应
#    - 键盘全流程：Tab 进入 → 方向键切三档 → 焦点不丢 → Esc 关日历焦点回触发按钮
#    - 375/390 视口目视侧栏展开态字标是否可辨（不可辨则切盘面版）
#    - 深色页确认未出现全标记版 Logo
```

---

## 13. 变更记录

| 日期 | 变更 | 原因 | 影响范围 |
|---|---|---|---|
| 2026-10-08 | 初版生成 | 用户锁定 4 项决策 | — |
| 2026-10-08 | 裁定 `seek` 载荷为 `{dimension,key}` 对象，PM 裸字符串方案作废 | 靠字符串长度推断档位是本项目已栽过的坑 | 前端 3 文件 |
| 2026-10-08 | 驳回架构师「三档标题全渲染、三行等高」初稿 | 与用户锁定决策「各档只显示该档标题」冲突 | 架构设计 + ADR |
| 2026-10-08 | 更正资产文档「字标为白色」的错误归因 | 实测字标为深墨 `rgb(4,33,59)` | `assets/brand/logo/README.md` |
| 2026-10-08 | 设计师自我纠正 maskable `f` 值 | 原 69.3% 在 1024 原画布测得，裁切重排后失效 | 10 个资产文件参数 |
| 2026-10-08 | 裁定 manifest 两字段改色 + 硬编码 P0 豁免（限两个 JSON 键） | 消除色温断层；PWA 规范强制字段 CSS 变量不可达 | manifest |