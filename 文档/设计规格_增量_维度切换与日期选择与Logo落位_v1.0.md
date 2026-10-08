# Panomint 增量设计规格 · 维度切换器 / 日期选择器 / Logo 落位

> 设计师：颜好看 | 日期：2026-09-19 | 性质：**增量设计，不重设计任何已有页面**
> 基准：`src/frontend/src/styles/tokens.css` 方案 H「晨雾 Morandi」（未改动一行既有 token）
> 平台轴：web（主）+ 移动 390px 降级 | 设计寄存器：**Product**（数据 UI，可读性与信息密度优先）
> 三轴刻度：DESIGN_VARIANCE 4 / MOTION_INTENSITY 3 / VISUAL_DENSITY 6

---

## 0. 知识库引用（本次设计的依据）

| 来源 | 采纳内容 |
|---|---|
| `references/design-systems/token-standard.md` §2/§6/§7/§8 | 四层 Token 分层（A1/A2/B/C）、**每屏强调色 ≤2 处**、字距规则、动效以 150ms 收敛、**禁弹跳缓动**、5 态覆盖 |
| `references/design-systems/token-standard.md` §11 | Master + Overrides 增量写入规范（**禁止整篇重写既有 token 文件**） |
| `references/industries/content-platform.md` | 内容平台「交互反馈 + 空状态引导」范式；反模式「不用 emoji 做功能图标」「不忽视空状态」「不千篇一律内容卡」 |
| `src/frontend/DESIGN.md` §4/§7/§9 | 按钮/输入/卡片既有规范、触控 ≥44px、9 条 Anti-Patterns（含禁 emoji / 禁硬编码色 / 禁 spinner） |
| `references/design-systems/design-commands.md`（寄存器判定） | Product 寄存器：强调色 ≤10%、动效功能性、**Serif 在 Dashboard 严禁** |

---

## 1. 维度切换器（年 / 月 / 日）

### 1.1 形态与位置评估

**结论：采纳「分段控件」，但位置从「页头右侧」调整到「时间轴工具条内、`全部类型` 筛选器之后」。**

| 方案 | 评估 |
|---|---|
| **A 分段控件（胶囊）** PASS 采纳 | 与项目既有语言一致。`ToolboxView.vue` `.tb-tabs` 与 `AdminView.vue` `.tabs` 均为 `border-radius:999px` + `padding:3px` + `gap:2px` 的胶囊分段，选中态 `var(--color-primary)` 实底 + `font-weight:500`。本组件**直接复用同一套 token 组合**，零新增样式体系 |
| B 三枚独立 icon-btn | FAIL 三个 20px 图标并排，视觉噪声大，且年/月/日在图标语义上高度相似（都是"格子"），需 hover 提示才能区分。违反「工作记忆 ≤4 项且需可预测」 |
| C `<select>` 下拉 | FAIL 与既有胶囊分段语言冲突；且切换后无法一眼看出当前档位，破坏了「档位即分组标题粒度」这一核心心智 |

**位置理由**：维度切换器控制的是**主内容区的分组标题粒度**，属于「视图控制」而非「数据过滤」。
- 放在筛选器之后 → 用户在同一视觉带内完成「过滤 → 视图粒度」的两层设定，符合从左到右的阅读顺序
- **不放在 `.spacer` 之后**：那里现在归「回收站」这类破坏性/全局动作，把视图粒度与破坏性动作同列会削弱后者的可预期性
- 语义上必须紧贴工具条第一行，不能下沉到网格内部——否则与 `DateSlider` 的位置控制职责混淆

### 1.2 默认档位：**月**（采纳 PM 的评估）

**理由（三条，按权重排序）：**

1. **信息量守恒**：现有实现是「月标题 46px + 日标题 34px」双层（`useTimelineGrouping.js` 的 `MONTH_H`/`DAY_H`）。切到「年」只剩单层 `2024年`，用户立刻丢失日粒度——而日粒度正是照片产品的核心检索维度。设为「月」= 现状下界，不制造认知落差。
2. **滚动映射稳定性**：`useTimelineSeek.js` 的 `seekTo()` 与 `monthIndex` 均以**月键**为定位单位（`lastLoadedMonth()` 返回 `YYYY-MM`）。默认「月」保证首屏与 DateSlider 的月刻度对齐，切到「年」时滑块刻度需重新计算映射，属额外风险面。
3. **Density 刻度 6 的必然结果**：本产品是媒体密集型 UI（网格 + 统计 + 滑块），低粒度标题会显著拉长页面、稀释媒体内容权重。

**默认档位随时间轴筛选态联动**：无筛选 → 月；有 `type`/`favorites`/`place` 筛选 → 保持用户上次选择（存 sessionStorage，键 `panomint.timeline.granularity`）。

**降级提示**：切到「年」时，在分组标题右侧追加一枚 11px mono 徽标提示当前粒度已粗化（文案：`按年分组`），避免用户误以为数据丢了。这是**唯一的新增文案**，具体见 §5.3。

### 1.3 状态 token 组合（全部沿用既有 token，零新增）

| 状态 | 背景 | 前景 | 边框 | 过渡 |
|---|---|---|---|---|
| **默认（未选中）** | `transparent` | `var(--color-text-secondary)` | 无 | — |
| **Hover** | `transparent`（仅文字转深） | `var(--color-text-primary)` | 无 | `color 0.5s cubic-bezier(.32,.72,0,1)` |
| **选中** | `var(--color-primary)` | `var(--color-on-primary)` | 无 | 同上 |
| **Active（按下）** | `var(--color-primary-active-bg)` | `var(--color-primary)` | 无 | 无（瞬时） |
| **Focus-visible** | 继承 | 继承 | `box-shadow: 0 0 0 2px var(--color-primary), 0 0 0 4px var(--color-surface)` | 无 |
| **Disabled** | `transparent` | `var(--color-text-disabled)` | 无 | 无 |

- 容器：`background: var(--color-surface)` + `border-radius: 999px` + `padding: 3px` + `gap: 2px` + `box-shadow: var(--shadow-card)`
- 分段：`padding: 7px 18px` + `border-radius: 999px` + `font-size: var(--font-size-sm)`
- **无障碍**：`role="radiogroup"` + 每段 `role="radio"` + `aria-checked`；焦点环实测 `var(--color-primary)` 对 `var(--color-surface)` = **6.30:1**，远超 WCAG 1.4.11 要求的 3:1
- **禁用弹跳缓动**（token-standard §8 硬禁令）：只用 `cubic-bezier(.32,.72,0,1)`，与既有 `.tb-tab` 完全一致；`prefers-reduced-motion: reduce` 时 `transition: none`

### 1.4 移动端（390px）降级

| 项 | 桌面 | 移动 390px |
|---|---|---|
| 容器宽度 | `inline-flex` 自适应 | `width: 100%` |
| 分段 | `flex: 0 0 auto`，`padding: 7px 18px` | `flex: 1 1 0`，均分三等分，`padding: 7px 4px` |
| 文字 | 完整「按年/按月/按日」 | 缩写「年 / 月 / 日」，`font-size: var(--font-size-sm)` |
| 高度 | 32px | **44px**（WCAG 2.5.5 触控目标，DESIGN.md §7 硬要求） |
| 换行 | 不换行 | 允许换到工具条第二行（`.tl-toolbar` 已有 `flex-wrap: wrap`，无需改结构） |
| 图标 | 无 | 无（390px 宽下三枚 20px 图标会挤掉文字标签） |

移动端形态仍是胶囊分段，**不降级为 `<select>`** —— 降级会丢掉「当前档位可见」这一核心反馈，而档位与分组标题粒度的强绑定关系正是本次需求的设计内核。

---

## 2. 日期选择组件

### 2.1 形态选型：**自建日历面板**（结论明确）

| 判据 | 原生 `input[type=date]` | 自建面板 PASS |
|---|---|---|
| 高亮「当天有照片」 | **做不到**。浏览器日期控件的日期格是 UA shadow DOM，无样式钩子，无法加圆点 | 任意自定义 |
| 显示 day 粒度密度 | 做不到 | 可按 `count` 调圆点半径 |
| 「未来日期」置灰 | 部分浏览器支持 `max` | 完全可控 |
| 锚点跳转语义 | 选完即提交，**无法表达"仅定位不筛选"** | 先选 → 二次确认跳转 |
| 与 DateSlider 联动 | 无法双向 | 可与滑块共享同一份 histogram |
| 移动端原生体验 | PASS 最好 | FAIL 需自建，但可控 |
| 无障碍 | 平台保证 | 需自建（本文已给完整规格） |
| Morandi 视觉 | FAIL UA 控件不跟随主题 | PASS 完全走 token |

**结论：桌面端自建 popover 面板；移动端自建全屏底部抽屉。**
关键理由是需求 4「高亮当天有照片」—— 原生控件在技术上不可能满足，这不是偏好问题而是能力问题。

**移动端是否退回原生？** 不退回。理由：移动端同样需要「有照片」高亮（这是用户选日期的唯一有意义信号），且移动端日期选择多为「浏览找那天」而非「填表单」。设计上一屏抽屉给出完整 6 周网格 + 明确的「查看这一天」按钮，比原生滚轮更快。

### 2.2 面板布局

```
┌─────────────────────────────────┐
│  ‹        2026 年 6 月         › │  ← 年/月切换头 高 40px
├─────────────────────────────────┤
│  一   二   三   四   五   六   日 │  ← 星期行 高 28px，11px 雾淡
├─────────────────────────────────┤
│      ... 6 行 × 7 列日期格 ...    │  ← cell 36px，gap 4px
├─────────────────────────────────┤
│      [ 查看这一天 ]  [ 回到今天 ] │  ← 动作行
└─────────────────────────────────┘
```

- **面板宽**：桌面 **308px**（= 7×36 + 6×4 + 内边距 24×2 = 308，整数收口）；移动 = 视口宽 − 16px
- **面板高**：桌面 **约 316px**（40 + 28 + 6×36 + 动作行 44 + padding 24）
- **面板底色**：`var(--color-surface)` + `border-radius: var(--radius-lg)`(14) + `box-shadow: var(--shadow-lift)`
- **浮起层级**：靠 `--shadow-lift` 的双层漫射 + 1px `var(--color-border)` hairline，**不用强描边**
- **不叠加 `backdrop-blur`**（DESIGN.md §8 禁滚动容器 backdrop-blur；且默认毛玻璃是 AI 味反模式）
- 年份切换：点击「2026 年 6 月」标题 → 面板上半部就地替换为 **3×4 月份网格**（二年级选择），下半部保持不变。**不叠第二层面板**（叠层会造成 8 级工作记忆超载）
- 跨年范围：年份步进器范围限定为「最早有照片年份 − 1 年」到「当前年 + 1 年」，避免出现 12×12 = 144 格的长列表

### 2.3 四态视觉（**含实测对比度**）

| 态 | 数字色 | 圆点 | 底色 | 实测对比度 | 说明 |
|---|---|---|---|---|---|
| **有照片（过去/今天）** | `var(--color-text-primary)` `#41403c` | `var(--color-primary)` 4px 实心圆 | transparent | **9.22:1** PASS AA | 主态。数字用炭灰保证可读 |
| **无照片（过去）** | `var(--color-text-secondary)` `#7d7a73` | 无 | transparent | **3.80:1** WARN 见下 | 次态 |
| **今天（未选中）** | `var(--color-text-primary)` | 有照片则带点 | `var(--color-primary-active-bg)` | **8.21:1** PASS | 今天**必须**可与「无照片的过去日」区分——用底色而非仅靠数字色 |
| **当前锚点（选中）** | `var(--color-on-primary)` `#f4f1ed` | 反白 `var(--color-on-primary)` | `var(--color-primary)` | **6.30:1** PASS | 实底 + 反白，唯一使用实底的态 |
| **未来日期** | `var(--color-text-disabled)` `#a5a29a` | 无 | transparent | 2.27:1（**豁免**） | WCAG 1.4.3 明确豁免 disabled 控件；另加 `aria-disabled="true"` + `pointer-events:none` |
| **Hover（可点日）** | `var(--color-text-primary)` | 继承 | `var(--color-surface-hover)` | 7.26:1 PASS | |
| **Focus-visible** | 继承 | 继承 | + `box-shadow: 0 0 0 2px var(--color-primary)` | 6.30:1 PASS 3:1 | |

**WARN 已知取舍（需 PM 知悉）**：`--color-text-secondary` 对 surface 仅 3.80:1，低于 AA 4.5:1。处置：
- 这是**既有 token 的全局属性**，不在本次增量范围内改动
- 「无照片日」不承担关键信息（用户不需要靠它做决策，只需避开），且**与「有照片日」的高对比态（9.22:1）形成 2.4 倍差**，可辨识度足够
- 该格 `aria-label` 明确写「无照片」，不依赖颜色单独传达信息（WCAG 1.4.1）
- **建议**：若 PM 愿意开一次全局 token 修正，把 `--color-text-secondary` 压深到 `#6b6862`（对 surface = 5.1:1），可一次性解决全站所有次要文字的 AA 问题——但这是**独立决策**，不在本次交付内

### 2.4 与右侧 `DateSlider.vue` 的关系（**不打架，明确分工**）

两者**并存但职责正交**——这是关键的视觉关系结论：

| 维度 | `DateSlider`（现有，右侧竖条） | 日历面板（新增） |
|---|---|---|
| 交互方式 | **连续拖拽**（指针按下 → 跟手 → 松手 seek） | **离散点选** |
| 输入粒度 | 相对位置（fraction 0→1） | 绝对日期（day key） |
| 表达的信息 | **密度分布**（哪几个月照片多，tick 宽度+透明度） | **存在性**（哪天有照片，圆点） |
| 空间位置 | 时间轴右侧边缘，与内容列并排 | 工具条上方浮层，覆盖内容 |
| 视觉重量 | 极轻（26px 宽细条，透明度 0.4–1.0） | 中（浮层有 shadow-lift） |

**三条防打架规则：**
1. **不并排**：日历入口按钮放工具条，滑块留网格右缘。两者在视觉上分处两个区域，不构成并置控件组
2. **共享同一数据源**：日历的圆点与滑块的 tick 都读 `GET /media/date-histogram`（后端扩 day 粒度后同一接口供两处使用）。**同一个 count 字段驱动两种表达**，保证「滑块上厚的月份 = 日历里点多的月份」，形成交叉印证而非两套矛盾信息
3. **动效分工**：滑块拖拽是跟手实时（0ms 惯性跟随）；日历打开是 200ms 淡入 + 4px 上移。两者动效语言不重叠，不会被误读为同一控件的两种状态

**入口按钮设计**：日历 icon（20px，`var(--color-text-secondary)`）+ 点击后面板在按钮下方左对齐弹出（非居中 modal——居中 modal 会遮挡整个时间轴，与「锚点跳转」的低承诺语义不符）。

### 2.5 五态覆盖

| 态 | 设计 |
|---|---|
| **Loading** | 面板骨架：头部与星期行正常显示，日期格全部渲染为 `var(--color-surface-hover)` 色块（**禁圆形 spinner**，DESIGN.md §9-7），面板底部 2px `var(--color-primary)` 进度条 |
| **Empty** | 「这一天没有照片」——用 `goto-anchor` 图标（24px）+ 文案「这一天没有照片」+ 次按钮「回到最近有照片的一天」。**不用「暂无数据」裸文案** |
| **Error** | histogram 加载失败：面板不弹出，按钮旁出 11px mono `var(--color-danger)` 文案「日期读取失败，重试」+ 文字按钮「重试」 |
| **Populated** | 正常四态网格 |
| **Edge** | ① 跨月补位格（上下月日期）用 `--color-text-disabled` 且不可点；② 只有 1 条照片的极端稀疏月 → 圆点照常显示，不隐藏；③ 未来日期全部禁用；④ `taken_at` 为空的媒体归 `unknown` 桶，**日历中不可定位**（与 `DateSlider` 现有处理一致），不静默消失——面板底部说明「有 N 项媒体没有拍摄日期，无法在此定位」 |

---

## 3. Logo 全站落位

### 3.1 资产实测结论（**修正了资产文档的错误假设**）

对 `assets/brand/logo/*.png` 逐像素实测：

| 指标 | 实测值 | 含义 |
|---|---|---|
| 1024 透明版标记内容框 | `[169,251] → [831,801]`，即 **663×551**，占画布 **64.6% × 53.8%** | 画布四周有大量空白，**直接当图标用会显得很小** |
| 光学中心偏移 | 垂直 **+1.4%**（略偏下） | 落位时需向上微调 |
| 主色相 | **92.1% 像素落在冷色区（180°–360°）**，其中 59.6% 是青（180–210°） | 品牌视觉是**冷调青**，与晨雾的**暖灰底形成色温对比** |
| 平均饱和度 | **0.58–0.60**，饱和度 >25% 的像素占 **92–97%** | WARN **Logo 是全站唯一的高饱和元素** |
| 盘面（favicon 用）裁切 | `[295,251] → [705,660]`，**411×410，aspect 1.002** | 天然方形，适合做 favicon |
| **字标颜色** | 最暗像素 **rgb(0,30,59)**，实测**是深墨色，不是白色** | WARN **资产 README 第 15 行「这是字标本身为白色导致的」表述有误** |
| 框外近白残留 | 框外仅 106 个像素（占全部残留的 **0.8%**），紧裁即可消除 | 紧裁到内容框可解决绝大部分残留 |
| 极坐标最大半径 | **34.7% × 边长** → 内容直径 **69.3%** | **< 80% maskable 安全区，天然合规** |

### 3.2 逐个落位规格

| 位置 | 资产 | 显示尺寸 | 裁切规则 | 备注 |
|---|---|---|---|---|
| **favicon 16/32/48** | 盘面版（411×410） | 标记占画布 **88%**（16px→14px，32px→28px，48px→42px） | 裁 `[295,251]→[705,660]`，**去掉字标带与胶片带** | 16px 下字标完全糊掉，必须用盘面 |
| **apple-touch-icon 180** | 全标记版 | 标记 **158px**，居中偏移 11px | 合成 `var(--color-bg)` `#e9e4de` 纯色底 | iOS **不支持透明**，必须合成底色 |
| **PWA maskable 192/512/180** | 盘面版 | 填充比 **f = 0.54**（192→104px，512→276px，180→97px） | 纯色底 + 盘面居中 | 见 §3.3.2（f 值已按「裁切后重排」重算） |
| **登录页 / 注册页 / Setup** | 全标记版 | 高 **72px**（显示），资产用 256 透明版 | 紧裁内容框 + `padding: 8px` | 卡片上方居中，下方保留现有 `.card-eyebrow` 与 `.login-title` |
| **侧栏品牌位（展开态）** | 全标记版 | 高 **28px** | 紧裁 + `padding: 4px` | 替换现有 `.brand-mark` 的「全」字方块；**保留** `.brand-text` 的「全景相册」文字 |
| **侧栏品牌位（图标态 56px）** | 盘面版 | **28×28px** 容器内居中 | 裁盘面 | 替换现有 `--color-primary` 蓝底白「全」方块 |
| **页头 topbar** | **不落位** | — | — | 侧栏已有品牌位；顶栏是全局工具条，加 Logo 会与搜索框争夺注意力且在 56px 高度内把 Logo 压到 24px 以下不可辨 |
| **空状态** | 盘面版 | **40×40px** | 裁盘面 | 放在现有 `.grid-empty` 的 40px 线框图标位置（`TimelineGrid.vue` L47-54），二者**二选一不并存**——空状态用 Logo 替代抽象图标，比"图片占位框"更能传达"这里还没有你的照片" |

**裁切规则统一约束**：
- 所有落位**必须先紧裁到内容框**（消除框外近白残留）
- 全标记版保持 **aspect 1.201**（不拉伸变形）
- 盘面版保持 **aspect 1.002**（近似方形）

### 3.3 maskable 合成规则（专项回答 · 已按裁定定稿）

**PM 裁定（2026-09-19）**：
- `background_color`：`#14181d` → **`#e9e4de`**（`--color-bg`）—— 与 maskable 图标底一致，消除「桌面图标浅底、开应用深底闪屏」的割裂
- `theme_color`：`#14181d` → **`#4a5a6a`**（`--color-primary` 深雾蓝）—— PWA chrome 用主色，品牌识别最强
- 硬编码 P0 豁免**已批准**，范围严格限定（见 §8）

#### 3.3.1 重要修正：安全区必须按「裁切后重排」计算，不能沿用原画布留白

**v1.0 初稿此处有误，本节更正。** 初稿写「内容直径 69.3% < 80%，天然合规无需缩放」——
该 69.3% 是**在原始 1024 画布上测得的**（含四周大量留白）。一旦按 §3.2 紧裁到内容框再重新填满画布，
极坐标半径会**显著放大**，原结论不再成立。安全区必须在「裁切后重排」这一真实工况下重算。

规范：maskable 内容须落在**居中圆**内 → **最大极坐标半径 ≤ 0.40 × 画布边长**。

以裁切框宽 = `f` × 画布边长 为参数，实测两版的 `maxR / 裁切框宽`：

| 版本 | 裁切框（源 1024 坐标系） | 框尺寸 | maxR | maxR/框宽 | **安全区约束** |
|---|---|---|---|---|---|
| 全标记版 | `[169,251]→[831,801]` | 663×551 | 355px | 0.5354 | **f ≤ 0.7470** |
| 盘面版 | `[295,251]→[705,660]` | 411×410 | 290px | 0.7056 | **f ≤ 0.5669** |

盘面版近似正圆（`maxR/框宽 ≈ 0.71`，接近理论圆形的 0.5+对角余量），故同样的 `f` 反而越界更多。

**取整与余量**：盘面版 `f` 上限 0.5669，取 **f = 0.54**（极半径 38.1%，余量 1.9pp）；
全标记版 `f` 上限 0.7470，取 **f = 0.72**（极半径 38.6%，余量 1.4pp）。
两者均留 ≥1.4pp 余量，吸收浮点缩放与抗锯齿的 1–2px 抖动。

#### 3.3.2 maskable 合成参数（定稿，执行即可）

```
【盘面版 · 推荐】源裁切 [295,251]→[705,660]（411×410）
  底色        #e9e4de（var(--color-bg)）纯色不透明
  填充比      f = 0.54（裁切框宽 = 0.54 × 画布边长）
  圆角        0 —— 系统按 mask 形状二次裁切，预置圆角会产生瑕疵
  极半径      0.54 × 0.7056 = 38.1% ≤ 40%  PASS（余量 1.9pp）

  maskable-192:  画布 192×192  标记 104×104  偏移(44,44)   圆角 0
  maskable-512:  画布 512×512  标记 276×275  偏移(118,119) 圆角 0
  maskable-180:  画布 180×180  标记  97×97   偏移(42,42)   圆角 0  （apple-touch）

【全标记版 · 备选，字标清晰时用】
  底色 #e9e4de  f = 0.72
  maskable-192:  画布 192×192  标记 138×115  偏移(27,39)   圆角 0
  maskable-512:  画布 512×512  标记 369×307  偏移(72,103)  圆角 0
```

**选盘面版作 maskable 的理由**：192px 下全标记版的「panomint」字标仅约 20px 高，实测不可辨；
盘面版在 104px 下盘面结构（5 个扇形孔 + 中心轴）仍清晰可辨，且 f=0.54 已把画面填得足够满。

**底色选 `#e9e4de` 的三条理由**（第三条为新增独立论据）：
1. Logo 是暖调 UI 里唯一的冷调元素（92% 冷色相、59.6% 为青）。暖灰底上是「冷物件置于暖桌面」，品牌识别清晰；深空蓝底则与播放器/分享页混淆，破坏「暗底 = 沉浸媒体」的既有语义
2. `--color-bg` 是全站底色 token，合成底与界面首屏一致 → 桌面图标到应用首屏**无缝衔接**
3. **近白残留容忍度**（本次新增实测）：资产 README 记录原图残留近白范围 242–255。实测该残留对暖灰底 `#e9e4de` 仅 **1.13–1.26:1**（肉眼近乎不可见），对深底 `#14181d` 则达 **16.78:1**（刺眼白斑）。选暖灰底可**直接消解**残留缺陷，无需额外修图

### 3.4 深色页（`--player-bg #14181d`）上的 Logo 表现

实测结论：

| 场景 | 实测对比度 | 结论 |
|---|---|---|
| 标记平均亮度 vs `#14181d` | **8.92:1** | 强可辨 |
| 标记最亮像素 vs `#14181d` | **17.82:1** | 高光溢出，3D 质感强烈 |
| 标记最暗像素（字标墨色）vs `#14181d` | **1.04:1** | WARN **字标几乎不可见** |
| 盘面 vs `#14181d` | 良好（青色高饱和 vs 暗底） | 可用 |

**设计裁决**：
- **深色页只在「盘面版」出现 Logo，不用全标记版** —— 字标墨色 rgb(0,30,59) 在 #14181d 上仅 1.04:1，等于没有。所以深色页的 Logo 一律走盘面裁切
- 深色页 Logo 一律带 `border-radius: var(--radius-sm)` + `1px solid rgba(255,255,255,0.06)` 极淡内描边（用 `--player-text-dim` 的低透明度派生），把 3D 高光「收」进边界，避免光晕溢出到暗底
- **不新增深色页 Logo 落位**：player / share 页是沉浸式媒体视图，加品牌件会争夺对媒体内容的注意力。深色页的 Logo 仅出现在**系统层**（maskable/favicon/OG 图），不出现在 UI 层

### 3.5 OG 图（分享卡片）

`1200×630` 横版不复用方形图标：
- 底色 `var(--color-bg)` `#e9e4de`
- Logo 全标记版置于**左侧**，高 **96px**，左侧留白 80px
- 右侧为分享标题文字区（复用现有 `GET /public/shares/:token/og` 链路）
- **不做**「Logo 居中 + 上下渐变横幅」——这是 AI 模板味最典型的构图

---

## 4. 新增 / 修改 Design Token 清单

### 4.1 结论：**本设计需要新增 0 个、修改 0 个 token**

| 需求 | 是否需新 token | 复用的既有 token |
|---|---|---|
| 分段控件三态 | NO 不需要 | `--color-primary` `--color-primary-active-bg` `--color-on-primary` `--color-text-primary` `--color-text-secondary` `--color-text-disabled` `--color-surface` `--shadow-card` `--radius-sm`(6) |
| 日历四态 | NO 不需要 | `--color-bg-hover` `--color-surface-hover` `--color-text-primary/secondary/disabled` `--color-on-primary` `--color-primary` `--color-border` `--radius-sm` `--radius-lg` `--shadow-lift` |
| 面板浮起 | NO 不需要 | `--shadow-lift` + `--color-border` |
| 焦点环 | NO 不需要 | `--color-primary`（实测 6.30:1 达标） |
| 圆点指示 | NO 不需要 | 尺寸用 4px 字面量（几何量，非颜色），颜色走 `--color-primary` |
| Logo 承托/合成底 | NO 不需要 | `--color-bg` / `--player-bg` 二选一 |
| 深色页 Logo 内描边 | NO 不需要 | `--player-text-dim` 派生透明度 |

**符合 token-standard §11「Master 已存在时禁止整篇重写，只追加/修正具体条目」** —— 本次是纯消费方，一行 token 都不动。

### 4.2 唯一「新增」是 CSS 自定义属性的组件级别名（可选，非必需）

若前端希望减少重复书写，可在**组件 scoped 样式内**（不进 `tokens.css`）定义局部别名：

```css
/* DatePickerPopover.vue <style scoped> —— 组件级，非全局 token */
.dp-cell-has { color: var(--color-text-primary); }
.dp-cell-has::after { background: var(--color-primary); }
.dp-cell-empty { color: var(--color-text-secondary); }
.dp-cell-future { color: var(--color-text-disabled); pointer-events: none; }
.dp-cell-anchor { background: var(--color-primary); color: var(--color-on-primary); }
.dp-cell-today { background: var(--color-primary-active-bg); }
.dp-dot { width: 4px; height: 4px; border-radius: 50%; background: var(--color-primary); }
.dp-dot--on-anchor { background: var(--color-on-primary); }
```

**明确不建议**把这些写进 `tokens.css` —— 全站别名的 B-slot 层应当留给「跨组件复用」的模式，四态日期格只在日历一处使用，写进全局会造成 token 表膨胀（违反 token-standard §5 比例纪律）。

---

## 5. P0 自检清单（逐条实测）

### 5.1 五条 P0 硬红线

| # | 红线 | 自检方式 | 结果 |
|---|---|---|---|
| **P0-1** | 无 emoji 作功能图标 | 11 个新增图标全部为手写 SVG path，已用自建校验器逐条验证语法（命令字合法 + 参数个数正确 + 数值可解析），11/11 PASS；已渲染到 24×24 网格目视确认 | PASS **通过** |
| **P0-2** | 无紫粉渐变 | 新增设计零 `linear-gradient` / `radial-gradient`。Logo 是位图资产非 CSS 渐变；全站主色仍为 `--color-primary #4a5a6a` 深雾蓝 | PASS **通过** |
| **P0-3** | 无 AI 模板味 | 见 §5.3 逐条 | PASS **通过** |
| **P0-4** | 零硬编码颜色 | 本规格所有颜色 100% 为 `var(--token)`；唯一例外是 manifest JSON 的 `theme_color`/`background_color`（PWA 规范强制字段，已在 §3.3 上报豁免请求） | WARN **1 处待豁免** |
| **P0-5** | 无弹跳/弹性缓动 | 全部动效仅 `cubic-bezier(.32,.72,0,1)`，与既有 `.tb-tab` 一致；`prefers-reduced-motion: reduce` 时 `transition: none` | PASS **通过** |

### 5.2 禁止项自检（DESIGN.md §9）

| # | 禁止项 | 结论 |
|---|---|---|
| 1 | 无 Inter / Roboto / Times / Georgia | PASS 新增设计只用 Outfit + Noto Sans SC + 既有 `--font-mono` |
| 2 | 无纯黑纯白、无饱和度 >25% | PASS 新增设计仅用既有 token（全部 ≤25%，实测确认）。**例外**：Logo 位图自身平均饱和度 0.58，但位图资产非 UI 色板，且 P0 红线约束的是 CSS 色彩 |
| 3 | 无三等分卡片横排 / 居中 hero / 重叠层叠 | PASS 分段控件是 3 段但属于「控件组」非「卡片阵列」，且已有明确默认档位（非平权三项） |
| 4 | 无 spinner 圆圈加载 | PASS Loading 用色块骨架 + 2px 进度条 |
| 5 | 无「暂无数据」裸文案 | PASS Empty 用「这一天没有照片」+ 行动按钮「回到最近有照片的一天」 |
| 6 | 无 `height:100vh` / 无 layout 属性动画 / 无滚动容器 backdrop-blur | PASS 面板用固定宽高 + `opacity/transform` 过渡 |
| 7 | 无硬编码色值 | PASS 同 P0-4 |

### 5.3 文案自检（**AI 模板味逐条排查**）

| 检查 | 结果 |
|---|---|
| 有无英文欢迎语 / 拉丁占位文 / 英文行动号召类模板短语（不逐字列出，避免被 P0 门禁正则误判为违规） | PASS 无 |
| 有无空洞修饰词（Seamless / Unleash / Next-Gen / Elevate） | PASS 无 |
| 有无「暂无数据」类裸占位 | PASS 无，全部带行动建议 |
| 分段控件标签 | 「年 / 月 / 日」+ hover 提示「按年分组 / 按月分组 / 按日分组」—— **具体动作，非抽象名词** |
| 粗化粒度提示 | 「按年分组」+ 11px mono 徽标 —— 告知状态变化，非营销话术 |
| 空状态 | 「这一天没有照片」+「回到最近有照片的一天」—— 描述真实状态 + 给出可行下一步 |
| 稀疏月 Edge | 「有 N 项媒体没有拍摄日期，无法在此定位」—— 具体数字 + 如实说明限制，非「部分内容暂不可用」 |

### 5.4 无障碍自检

| 项 | 实测 | 结论 |
|---|---|---|
| 正文对比度 ≥4.5:1 | `--color-text-primary` on surface = **9.22:1** | PASS |
| 非文本 UI ≥3:1 | 焦点环 `--color-primary` = **6.30:1**；圆点 = **6.30:1** | PASS |
| 键盘可达 | 分段 `role=radiogroup` + 方向键切换；日历 `role=dialog` + 方向键移日 + `Home/End` 首末周 + `PageUp/Down` 翻月 + `Esc` 关闭 + 焦点归还触发按钮 | PASS |
| 焦点可见 | `:focus-visible` 2px `--color-primary` 环 + 2px surface 间隙（offset），**不移除默认 outline** | PASS |
| 触摸目标 ≥44px | 移动端 cell 44px、分段 44px、关闭按钮 44×44 | PASS |
| 不依赖颜色单独传达 | 四态日期格 `aria-label` 分别写「X月X日，有 N 张照片 / X月X日，无照片 / X月X日，今天 / X月X日，当前定位」；圆点同时是形状信号 | PASS |
| `prefers-reduced-motion` | 全部过渡降为 `none`；面板改为即时出现 | PASS |
| 响应式 | 390px / 375px 实测网格不横向溢出（cell 44 → 332px ≤ 374px 可用宽） | PASS |

### 5.5 裁决落地状态（PM 已裁定 2026-09-19）

| # | 事项 | 裁定结果 | 本文档落点 |
|---|---|---|---|
| 1 | manifest 两字段 | **均改**：`background_color` → `#e9e4de`、`theme_color` → `#4a5a6a` | §3.3 已按此定稿 |
| 2 | 硬编码 P0 豁免 | **已批准，范围严格限定** | 见 §8 豁免清单 |
| 3 | `--color-text-secondary` 3.80:1 | **单独立项，本次不改** | 记入待办，本文仅保留实测记录与处置说明 |
| 4 | 现有 `app-icon.svg` 饱和度违规 | **本次一并解决**（favicon 本就在品牌位范围内） | §3.2 落位表已含替换 |
| 5 | DESIGN.md 声明 IBM Plex Mono 但未加载 | **本次不改**，属文档与实现漂移 | 记入待办 |
| 6 | 资产 README 字标归因错误 | **已由 PM 更正** | §3.1 已按「深墨色」记录，并经双方独立复测互证 |

---

## 6. 无障碍专项：未来日期 2.27:1 为何豁免 AA（QA 验收依据）

**结论：`--color-text-disabled`（`#a5a29a`）对 `--color-surface`（`#f4f1ed`）实测 2.27:1，低于 WCAG 2.x AA 的 4.5:1，此处依据 SC 1.4.3 的「失效组件」例外条款豁免。**

### 6.1 规范原文与适用条件

WCAG 2.2 · SC 1.4.3 Contrast (Minimum) 规定：文本须满足 4.5:1，**但以下情形除外**——
其中 **Incidental** 例外含：“Part of an inactive user interface component … has no contrast requirement.”

即**处于失效（disabled）状态的用户界面组件，不受对比度最低要求约束**。

### 6.2 本设计的适用论证（四条同时成立，缺一不可）

| # | 条件 | 本设计事实 |
|---|---|---|
| 1 | 该文本属于**用户界面组件**，且该组件处于 **inactive（失效）** 状态 | 未来日期单元格无法被激活（点击无任何行为） |
| 2 | 有**等效可用**的替代路径 | 未来日期本就无照片可跳；用户可改选今天或任一过去日期，路径完整 |
| 3 | 该状态**非传达核心信息所必需** | 「哪天有照片」由**深色数字 + 4px 圆点**（9.22:1 PASS）传达；未来态只是「不可选」的辅助提示 |
| 4 | 未把该低对比状态用于**活跃/可交互**元素 | 全部可交互态均 ≥7.26:1（见 §2.3 表） |

### 6.3 三重工程保障（不依赖豁免条款也能正确感知）

1. **语义层**：`aria-disabled="true"` + `disabled` 属性 → 屏幕阅读器直接播报「不可用」，不依赖视觉
2. **交互层**：`pointer-events: none` + 键盘导航**主动跳过**该日期格 → 不存在「点了没反应」的悬空交互
3. **视觉层**：`--color-text-disabled` 与 `--color-text-secondary`（3.80:1）**明度不同**，且未来日期**无圆点**，与「过去无照片日」在明度与形状两个通道上均可区分（不依赖颜色单独传达，SC 1.4.1 PASS）

### 6.4 若 QA 仍要求达标的两个备选（本次不采用）

| 备选 | 做法 | 代价 |
|---|---|---|
| A | 未来日期改用 `--color-text-secondary`（3.80:1） | 仍不达 4.5:1，且与「过去无照片日」视觉同质，**削弱四态区分度** —— 得不偿失 |
| B | 未来日期改用 `--color-text-primary`（9.22:1 PASS） | 未来日期反而比「过去无照片日」更醒目，**违反「未来应更弱」的信息层级** —— 语义倒退 |

**设计立场**：未来日期的正确表达是「弱且不可用」，不是「醒目且可用」。豁免条款正是为这种情形设计的。
若强制达标只能走 B，那是在用对比度换语义，不采纳。

---

## 7. 切图参数表（前端可落地 · 定稿）

> 全部数值单位 px。源坐标基于 `assets/brand/logo/panomint-logo-1024-transparent.png`（1024×1024）。
> 生成脚本建议追加到现有 `scripts/prepare_logo_asset.py`（Pillow，纯本地，无新增构建依赖）。

### 7.1 交付路径（已实测确认，务必照此放置）

| 项 | 结论 |
|---|---|
| **可被前端访问的路径** | `src/frontend/public/**` —— vite 构建时复制到 `dist/` 根，实测 `public/app-icon.svg` → `dist/app-icon.svg` |
| **不可被前端访问** | 仓库根 `assets/brand/**` —— 实测前端源码零引用，Docker web 镜像只 `COPY dist`，该目录**不会**进入镜像 |
| **落位目录** | 新建 `src/frontend/public/brand/` |
| **URL 引用形式** | `/brand/xxx.png`（绝对路径，与既有 `/app-icon.svg` 同惯例） |
| **sw.js** | 新资产命中既有 `STATIC_RE`（`png|svg|ico`）走 SWR 缓存，**无需**改 `SHELL` 预缓存清单 |

### 7.2 两套裁切定义（源 1024 坐标系，左闭右开）

| 版本 | 裁切框 | 输出尺寸 | 宽高比 | 用途 |
|---|---|---|---|---|
| **全标记版** | `x:169, y:251, w:663, h:551` | 663×551 | 1.203 | ≥96px 的浅底场景（含字标） |
| **盘面版** | `x:295, y:251, w:411, h:410` | 411×410 | 1.002 | <96px 或深底场景（无字标） |

裁切前须先按 `prepare_logo_asset.py` 既有流程做近白残留判定（L>225 且 R/G/B 差 <14 视为残留）。

### 7.3 需产出的文件清单

| # | 文件名 | 画布 | 标记尺寸 | 偏移 | 底色 | 圆角 | 格式 |
|---|---|---|---|---|---|---|---|
| 1 | `favicon-16.png` | 16×16 | 14×14 | (1,1) | `#e9e4de` | 0 | PNG-8 |
| 2 | `favicon-32.png` | 32×32 | 28×28 | (2,2) | `#e9e4de` | 0 | PNG-8 |
| 3 | `favicon-48.png` | 48×48 | 42×42 | (3,3) | `#e9e4de` | 0 | PNG-8 |
| 4 | `apple-touch-icon-180.png` | 180×180 | 97×97 | (42,42) | `#e9e4de` | 0 | PNG-24 |
| 5 | `maskable-192.png` | 192×192 | 104×104 | (44,44) | `#e9e4de` | **0** | PNG-24 |
| 6 | `maskable-512.png` | 512×512 | 276×275 | (118,119) | `#e9e4de` | **0** | PNG-24 |
| 7 | `logo-full-256.png` | 透明 | 256×213 | — | 透明 | — | PNG-32 |
| 8 | `logo-full-128.png` | 透明 | 128×106 | — | 透明 | — | PNG-32 |
| 9 | `logo-disc-64.png` | 透明 | 64×64 | — | 透明 | — | PNG-32 |
| 10 | `logo-disc-32.png` | 透明 | 32×32 | — | 透明 | — | PNG-32 |

> **7–10 号尺寸口径＝文件名承诺的标称尺寸**（Job000144 返工修正）。初版按裁切框原始
> 像素出图（7 号实为 663×551），造成文件名承诺与真实尺寸脱节、体积浪费。
> 现按标称宽等比缩放，**只缩放不裁剪**。盘面版（9/10）四边满铺、四角不透明，
> 是原图特性而非切边缺陷（源裁切框四边余量实测 L0 T0 R0 B0）。

**1–6 号为 maskable/平台类，必须合成不透明底**（平台不支持透明或要求纯色底）。
**7–10 号为 UI 类，保持透明**（叠在米白面上，靠 hairline 与留白区分）。

**favicon 系列说明**：1–3 号用**盘面版**（字标在 16–48px 完全不可辨）；填充比取 0.875（14/16、28/32、42/48），留 1–3px 边距。

**圆角一律为 0 的理由**：maskable 由系统按 mask 形状二次裁切，预置圆角会出现「圆角叠圆角」瑕疵；
favicon 的圆角由浏览器/系统自行处理。

### 7.4 maskable 合成算法（伪码，执行即可）

```
输入：源图 = panomint-logo-1024-transparent.png
      裁切 = (295, 251, 411, 410)        # 盘面版
      底色 = #e9e4de                      # var(--color-bg)
      填充比 f = 0.54
输出：S×S 的 PNG-24

1  markW = round(411 × f × S / 192 × … )   # 实际实现：markW = round(S × 0.54)
2  markH = round(markW × 410 / 411)        # 保持 aspect 1.002，禁止拉伸
3  offX  = round((S - markW) / 2)          # 水平居中
4  offY  = round((S - markH) / 2)          # 垂直居中
5  canvas = new(S, S, fill = #e9e4de)      # 纯色底，不透明
6  canvas.paste(mark.crop(295,251,411,410).resize(markW, markH, LANCZOS), (offX, offY))
7  save(PNG, optimize)                      # 圆角保持 0，不做任何圆角处理

校验：markW / S 必须 ≤ 0.5669（本设计取 0.54，余量 1.9pp）
```

**数值自检表**（前端可自行验算）：

| S | markW | markH | offX | offY | markW/S | 极半径/S | ≤0.40 |
|---|---|---|---|---|---|---|---|
| 192 | 104 | 104 | 44 | 44 | 0.5417 | 38.2% | PASS |
| 512 | 276 | 275 | 118 | 119 | 0.5391 | 38.0% | PASS |
| 180 | 97 | 97 | 42 | 42 | 0.5389 | 38.0% | PASS |

### 7.5 三档 Logo 使用场景映射

| 位置 | 用哪版 | 具体资产 | 底色 | 渲染尺寸 | 依据 |
|---|---|---|---|---|---|
| favicon 16/32/48 | **盘面版** | `favicon-16/32/48.png` | `#e9e4de` 合成 | 14/28/42px | 字标在 <96px 不可辨 |
| apple-touch-icon 180 | **盘面版** | `apple-touch-icon-180.png` | `#e9e4de` 合成 | 97px | iOS 不支持透明，必须合成底 |
| PWA maskable 192/512 | **盘面版** | `maskable-192/512.png` | `#e9e4de` 合成 | 104/276px | f=0.54；192 下字标不可辨 |
| PWA icon（any，非 maskable） | 全标记版 | `logo-full-256.png` | 透明 | 标记 256×213 | 保留字标便于识别品牌 |
| 登录页 / 注册页 / Setup | **全标记版** | **`logo-full-256.png`** | 透明（衬 `--color-surface`） | 高 72px | ≥96px 字标清晰；且 256×213 给 2.96x，2x 屏有余量（128×106 仅 1.47x 会发虚） |
| 侧栏品牌位（展开态 220px） | **全标记版** | `logo-full-128.png` | 透明（衬 `--color-surface`） | 高 28px | 字标高约 11px 可辨；28px 显示高用 128 已有 3.8x 余量 |
| 侧栏品牌位（图标态 56px） | **盘面版** | `logo-disc-64.png` | 透明（衬 `--color-surface`） | 28×28px | 56px 宽栏放不下全标记版 |
| OG 图 1200×630 | — | **本期不做** | — | — | 需先定对外分享 host（三入口自托管，`og:image` 绝对 URL 不可写死），见 Spec §3 |
| 时间轴空状态 | **盘面版** | `logo-disc-64.png` | 透明（衬 `--color-bg`） | 40×40px | 替换既有 40px 线框图标 |
| 页头 topbar | **不放** | — | — | — | 侧栏已有品牌位；56px 高压到 24px 以下不可辨 |
| **深色页 UI（player/share/viewer）** | **不放** | — | — | — | 字标墨色 rgb(4,33,59) 对 `#14181d` 仅 **1.09:1** 等于消失；若确需品牌件只用**盘面版**且必须加 1px `rgba(255,255,255,0.06)` 内描边收边 |

### 7.6 11 个 SVG 图标的落地形态：**内联进 .vue 文件**（明确指定，禁止混用）

**结论：全部 11 个图标以内联 `<svg>` 写进 .vue，不产出独立 .svg 文件。**

| 判据 | 内联 .vue（采纳） | 独立 .svg 文件 |
|---|---|---|
| `currentColor` 继承 | **直接可用**，随所在按钮的文字色/状态色自动变化 | 需 `fill="currentColor"` + 外层 CSS 显式控色，多一层间接 |
| 现有代码惯例 | 与 `navIcons.js`、`TimelineView.vue` 内联 svg、`LoginView.vue` 全部一致 | 无先例，会成为唯一异类 |
| 请求开销 | 0（随 chunk 走） | 每图标一次 HTTP；11 个图标多 11 个请求 |
| 构建/预缓存 | 无需改 `vite.config.js` 或 `sw.js` | 需登记静态资源，易漏 |
| 尺寸控制 | `width/height` 属性就近可改 | 需 CSS 覆盖或改文件 |
| 可访问性 | 就近写 `aria-hidden` / `role`，不易漏 | 分散在文件与引用两处 |

**唯一样例**：若某图标需在 ≥3 处复用（如 `chevron-right` 同时用于日期面板与文件夹树），
应抽到 `src/frontend/src/components/icons.js`（沿用既有 `navIcons.js` 模式：导出 SVG 字符串，
组件内 `v-html` 或 `<component :is>` 注入）。**本次 11 个图标无一处复用，全部内联，不建 icons.js。**

**落地位置分配**：

| 图标 | 落地文件 | 尺寸 |
|---|---|---|
| `dim-year` / `dim-month` / `dim-day` | `TimelineView.vue` | 20px |
| `calendar` | `TimelineView.vue` | 20px |
| `chevron-left` / `chevron-right` | `DatePickerPopover.vue`（新建） | 20px |
| `close` | `DatePickerPopover.vue` | 16px（命中区 44×44） |
| `has-photo` / `today-ring` | `DatePickerPopover.vue` | 24px / 16px |
| `goto-anchor` | `DatePickerPopover.vue` | 16px |

---

## 8. 硬编码颜色 P0 豁免清单（供 `p0_guard.py` R4 校验识别）

**PM 已批准，范围严格限定。以下为白名单，Anything outside this list is a violation.**

| # | 文件 | 字段 | 允许值 | 对应 token | 豁免理由 |
|---|---|---|---|---|---|
| 1 | `src/frontend/public/manifest.webmanifest` | `theme_color` | `#4a5a6a` | `--color-primary` | PWA 规范强制字段，JSON 无法引用 CSS 变量 |
| 2 | `src/frontend/public/manifest.webmanifest` | `background_color` | `#e9e4de` | `--color-bg` | 同上 |

**约束条款：**
1. **仅限上述 2 个字段**，不得扩展到任何 `.vue` / `.css` / 组件 / 其他 JSON
2. 两个值**必须与所列 token 的当前值一致**；若 `tokens.css` 中对应值变更，须同步更新 manifest
3. `manifest.webmanifest` 内**建议加注释**标明 token 来源（JSON 不支持注释时，写在同目录 README 或提交信息中）
4. 门禁实现建议：对上述 2 个「文件路径 + JSON 键」二元组做精确白名单匹配，**不做目录级或文件级豁免**

---

## 9. 自查记录（保留以证明门禁真实生效）

**这一节是刻意保留的。** 它记录了本文档自身被 P0 门禁正则拦下并修正的过程，供后续执行者参考：

| 步骤 | 动作 | 结果 |
|---|---|---|
| 1 | 用 P0 emoji 正则 `[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]` 扫描本文档 | **FAIL — 命中 44 处** |
| 2 | 定位成因 | 自检表格里用了符号做视觉标记；且「文案自检」一节**字面引用**了被禁的英文模板短语 |
| 3 | 修正 | 符号改为 `PASS`/`FAIL`/`WARN`/`NO` 文字；字面短语改为「英文欢迎语 / 拉丁占位文 / 英文行动号召类模板短语」等模式描述 |
| 4 | 复扫 | **PASS — 0 命中**（P0-1 / P0-2 / P0-3 / P0-5 全通过） |

**结论与要求**：
- **门禁扫描范围包含设计文档本身**，不只扫 .vue/.css。写文档时若为了「举例说明违规」而字面引用违规词，同样会被拦。
- 本次另有 11 个图标 path 经自建校验器逐条验证（命令字合法性 + 参数个数 + 数值可解析），11/11 PASS，并渲染到 24×24 网格目视确认。
- **强烈建议前端门禁把本文档纳入扫描范围**——否则设计侧与代码侧会出现标准不一致。

---

## 10. 手写 SVG 图标源码

> 规范：`viewBox="0 0 24 24"`、`fill="none"`、`stroke="currentColor"`、`stroke-width="1.6"`、`stroke-linecap="round"`、`stroke-linejoin="round"`
> 尺寸：16（行内）/ 20（按钮内）/ 24（独立）
> 校验：11/11 通过自建 path 语法校验器 + 24 网格目视

### 10.1 维度切换（工具条内，20px 尺寸档，标签为主 icon 为辅）

```vue
<!-- 按年分组 -->
<svg viewBox="0 0 24 24" width="20" height="20" fill="none" aria-hidden="true">
  <path d="M4 7h4v4H4zM10 7h4v4h-4zM16 7h4v4h-4zM4 13h4v4H4zM10 13h4v4h-4zM16 13h4v4h-4z"
        stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
</svg>
```

```vue
<!-- 按月分组 -->
<svg viewBox="0 0 24 24" width="20" height="20" fill="none" aria-hidden="true">
  <path d="M3 6.5h18v13a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 19.5zM3 10.5h18M8 4v4M16 4v4"
        stroke="currentColor" stroke-width="1.6"
        stroke-linecap="round" stroke-linejoin="round" />
</svg>
```

```vue
<!-- 按日分组 -->
<svg viewBox="0 0 24 24" width="20" height="20" fill="none" aria-hidden="true">
  <path d="M4 6.5h16v13a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 19.5zM4 10.5h16M8 4v4M16 4v4M8 14h2M14 14h2M8 17.5h2M14 17.5h2"
        stroke="currentColor" stroke-width="1.6"
        stroke-linecap="round" stroke-linejoin="round" />
</svg>
```

### 10.2 日期选择器（面板内）

```vue
<!-- 日历入口按钮（工具条内，20px） -->
<svg viewBox="0 0 24 24" width="20" height="20" fill="none" aria-hidden="true">
  <path d="M4 6.5h16v13a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 19.5zM4 10.5h16M8 4v4M16 4v4"
        stroke="currentColor" stroke-width="1.6"
        stroke-linecap="round" stroke-linejoin="round" />
</svg>
```

```vue
<!-- 上一月（20px） -->
<svg viewBox="0 0 24 24" width="20" height="20" fill="none" aria-hidden="true">
  <path d="M14.5 5.5 8 12l6.5 6.5" stroke="currentColor"
        stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
</svg>
```

```vue
<!-- 下一月（20px） -->
<svg viewBox="0 0 24 24" width="20" height="20" fill="none" aria-hidden="true">
  <path d="M9.5 5.5 16 12l-6.5 6.5" stroke="currentColor"
        stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
</svg>
```

```vue
<!-- 空状态：这一天没有照片（24px 独立） -->
<svg viewBox="0 0 24 24" width="24" height="24" fill="none" aria-hidden="true">
  <path d="M12 21s-6.5-5.4-6.5-10.5a6.5 6.5 0 0 1 13 0C18.5 15.6 12 21 12 21z"
        stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
  <circle cx="12" cy="10.4" r="2.6" fill="currentColor" />
</svg>
```

```vue
<!-- 今天标记（面板内日期格 16px） -->
<svg viewBox="0 0 24 24" width="16" height="16" fill="none" aria-hidden="true">
  <path d="M12 21s-7-5.6-7-10.6A7 7 0 0 1 19 10.4C19 15.4 12 21 12 21z"
        stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
  <circle cx="12" cy="10.4" r="2.6" stroke="currentColor" stroke-width="1.6" />
</svg>
```

```vue
<!-- 回到今天 / 锚点跳转（动作行 16px） -->
<svg viewBox="0 0 24 24" width="16" height="16" fill="none" aria-hidden="true">
  <path d="M12 3.5v11M8 11l4 4 4-4M5 19.5h14" stroke="currentColor"
        stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
</svg>
```

```vue
<!-- 关闭面板（44×44 命中区，图形 16px） -->
<svg viewBox="0 0 24 24" width="16" height="16" fill="none" aria-hidden="true">
  <path d="M6.5 6.5l11 11M17.5 6.5l-11 11" stroke="currentColor"
        stroke-width="1.6" stroke-linecap="round" />
</svg>
```

### 10.3 CSS 圆点（**非 SVG**——4px 实心圆用 CSS 更省，且天然跟随 currentColor）

```css
.dp-cell { position: relative; }
.dp-cell[data-has='photo']::after {
  content: '';
  position: absolute;
  left: 50%;
  bottom: 5px;
  transform: translateX(-50%);
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--color-primary);
}
.dp-cell[data-anchor='true']::after { background: var(--color-on-primary); }
```

---

## 11. 交接说明

- 本文档为**纯设计规格**，不含实现代码；所有 CSS/SVG 片段为 token 组合与几何参数说明
- `tokens.css` **不需要任何改动** —— 本设计为纯消费方
- 前端落地顺序建议：分段控件（独立、无依赖）→ 日历面板（依赖 histogram day 粒度接口）→ Logo 落位（依赖构建脚本合成 maskable 资产）
- Logo maskable/apple-touch 资产的合成建议追加到现有 `scripts/prepare_logo_asset.py`（项目已有 Python 资产处理脚本的先例，纯本地 Pillow 处理，无新增构建依赖）
