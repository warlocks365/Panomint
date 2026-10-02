# Design System: Panomint「晨雾 Morandi」
**Skill:** stitch-design-taste（结构）× taste-skill（反模板纪律）× soft-skill（高端组件工艺）× GSAP 官方 Vue 规范（动效）
**适用:** Panomint 前端全站（Vue 3 + Vite）。单一事实源——所有页面/组件实施前必读，实施时只允许使用本文档定义的值。

---

## Configuration — Design Dials

| Dial | Level | 说明 |
|------|-------|------|
| **Creativity** | `5` | 克制但有个性：Morandi 低饱和 + 排版字距设计；不做编辑实验 |
| **Density** | `5` | Daily App Balanced：媒体网格密、设置页疏 |
| **Variance** | `5` | Subtle offsets：网格允许轻微偏移，不做艺术混乱 |
| **Motion Intent** | `5` | Fluid + GSAP spring：页面进入/列表瀑布/悬浮微动，无电影级编排 |

> 产品定位：自托管照片管理工具（多视图数据 UI）。dials 相应比 landing page 保守——**可读性与信息密度优先于视觉表演**。

---

## 1. Visual Theme & Atmosphere
晨雾中的莫兰迪画室：暖灰底上浮着米白卡片，照片盖一层薄雾「沉」进界面；无纯黑纯白，最深到炭灰 `#41403C`，最亮到 `#F4F1ED`。全站饱和度封顶 25%。层级表达顺序：**明度差 > 字距 > hairline > 双层漫射阴影**——永远不用强描边与高对比投影。细字重 300 主导，强调靠字距（0.01em–0.28em）而非加粗。总体印象：安静、温润、耐看，像清晨还没醒透的画室。

## 2. Color Palette & Roles（与 `src/styles/tokens.css` 一一对应）

| 名称 | Hex | 角色 |
|---|---|---|
| 晨雾底 Mist Ground | `#E9E4DE` | 页面背景（body）。暖灰，非冷灰 |
| 米白面 Card Face | `#F4F1ED` | 卡片/侧栏/面板填充 |
| 米白面 Hover | `#EFEBE5` | 卡片 hover / 内嵌区块 |
| 炭灰 Ink | `#41403C` | 主文字（**无纯黑**） |
| 暖灰 Secondary | `#7D7A73` | 次要文字/说明 |
| 雾淡 Disabled | `#A5A29A` | 禁用/时间戳 |
| **深雾蓝 Primary** | `#4A5A6A` | 主按钮/AI 动作/链接/激活态（全站唯一主色） |
| 雾蓝 Hover | `#3D4B58` | 主按钮 hover |
| 雾蓝 Active BG | `rgba(143,163,173,.16)` | 激活底纹/选中项 |
| 雾蓝 Light | `#8FA3AD` | AI 面板点缀/图标底 |
| 陶土红 Danger | `#B0685C` | 危险动作/错误 |
| 灰绿 Success | `#6E8A72` | 成功/在线 |
| 暖沙 Warning | `#8A6D3D`（底 `rgba(217,164,101,.14)`） | 警告 |
| Hairline | `#DDD8D0` | 全站 1px 分隔线 |

**图例四类别色（时间轴统计，颜色即数据）**：照片 `#7D95A8` 雾蓝 / 视频 `#9A8AA0` 灰紫褐 / 全景照片 `#C4A57A` 燕麦 / 全景视频 `#B0766A` 陶土。

### Banned Colors（硬禁令）
- AI 紫/紫粉渐变（P0-2）——含紫蓝 neon、紫粉 glow、紫色按钮光晕
- 纯黑 `#000000` 与纯白 `#FFFFFF`——一律用炭灰/米白
- 饱和度 > 25% 的任何色（本表内所有色均已封顶）
- 冷蓝灰与暖灰混用（全站统一暖灰体系）

## 3. Typography Rules
- **字体栈**：`Outfit`（拉丁/数字）+ `Noto Sans SC`（中文），经 index.html Google Fonts 引入，权重 **300 / 400 / 500** 三档封顶
- **Body**：300 字重，`14px`，行高 `1.65–2.1`，最大 `65ch`
- **Heading**：400–500 字重 + **字距 `0.01em–0.28em`**（中文标题用宽字距，非加粗非巨号）；禁止 700+ 大黑体铺陈
- **Mono**：`IBM Plex Mono` 400——所有元数据（日期/分辨率/版本号/Key/文件名）、表单值
- **Scale**：页面标题 `17–20px` / 区块 `15–16px` / 正文 `13–14px` / 元数据 `10.5–12px`
- **禁用**：Inter、Roboto、Arial、系统默认 serif；700+ 字重铺满标题；全大写中文

## 4. Component Stylings
- **按钮（Button）**：主按钮 = 深雾蓝填充 + 米白文字，圆角 `10px`，高 `38–40px`；hover = `#3D4B58`（色彩过渡，无发光）；active = `scale(0.98)` 触压感。次按钮 = 米白底 + hairline 边框。危险 = 陶土红填充。全部 `cubic-bezier(.32,.72,0,1)` 500ms
- **卡片（Card）**：米白面 `#F4F1ED`，圆角 `10–14px`，**无边框**，双层漫射阴影 `0 1px 3px rgba(65,64,60,.05), 0 10px 34px rgba(65,64,60,.07)`；内边距 `22–24px`。hover = 阴影升至 lift 层（不位移不描边）
- **媒体卡（Media Card）**：照片盖**雾面 veil**——`linear-gradient(180deg, rgba(233,228,222,.16), transparent 34%)` 顶部薄雾，让媒体「沉」进界面；元数据行置于照片下方（mono 字体）；状态 chip = `rgba(255,255,255,.75)` 磨砂圆角胶囊 + 语义色圆点
- **输入（Input）**：label 上置；输入底 `#EFEBE5`，hairline 边框，圆角 `10px`，高 `38–40px`；focus = 深雾蓝 1px 边框；错误 = 陶土红文字置于下方
- **导航**：侧栏 `220px` 米白面，激活项 = 雾蓝 Active BG 圆角 `7px`；顶栏 `56px` 米白面 hairline 下缘
- **AI 助手面板**：米白卡 + 深雾蓝主按钮 + 确认卡（参数表 + 雾蓝确认/灰白取消）；**官方紫粉光效 canvas 已由 CSS 禁用（P0-2），不得恢复**
- **加载态**：骨架屏（形状与布局同尺寸的雾面闪烁块），禁用圆形 spinner
- **空态**：构图式引导（图标组合 + 「上传第一批照片」类行动建议），禁用「暂无数据」裸文案
- **开关（Switch）**：`40×22px`，开 = 灰绿，关 = 雾淡；白圆钮带 1px 投影

## 5. Page Header（产品 UI 的「页面封面」规范）
每个视图顶部：**区块编号式页头**——`11px mono 灰序号` + `16px 标题（字距 .12em）` + 右侧主操作按钮（深雾蓝）。标题下可选一行 `12.5px` 说明文字。**禁止**居中大 hero、装饰性大图、渐变横幅——产品页的「第一印象」由媒体内容本身承担。

## 6. Layout Principles
- **CSS Grid 优先**，禁止 flexbox 百分比 `calc()` 拼接
- 内容包含于 `max-width: 1400px` 居中；水平内边距 移动 `16px` / 桌面 `40px`
- 侧栏布局：左侧栏 `220px`（收起 `56px`）+ 内容区；顶栏 `56px`
- **「三等分卡片横排」BANNED**——用不对称网格（`4/8`、`2fr 1fr`）或瀑布流
- 全高区域用 `min-height: 100dvh`，禁 `height: 100vh`
- 列表/网格项进入视口需瀑布渐显（见 §8）

## 7. Responsive Rules
- `< 768px`：所有多列塌缩单列；侧栏折叠为图标列（`56px`）；字距降为 `0`
- 断点必须实测 `375px / 768px / 1440px`
- 触控目标 ≥ `44px`；无横向滚动（critical failure）
- 标题用 `clamp()` 缩放；正文最小 `14px`

## 8. Motion Philosophy（GSAP × Vue 3 官方规范）
- **引擎**：Vue 中用**原生 `gsap` + `gsap.context()` + `onBeforeUnmount` 清理**（`@gsap/react` 的 `useGSAP()` 内部依赖 React hooks，Vue 引入即崩——实测教训）；`ScrollTrigger` 仅用于长页进入动画
- **弹簧基准**：`stiffness: 100, damping: 20`（GSAP 等效 `duration: 0.5–0.7` + `ease: "power2.out"`）；禁 `linear` / `ease-in-out` / 弹跳 `back.out` 大回弹
- **三定律**：只动 `transform` 与 `opacity`；`backdrop-blur` 只用于固定层；`will-change` 仅加在正在动画的元素
- **标准剧目**（逐组件对号入座）：
  1. **页面进入**：`gsap.from(el, { y: 24, opacity: 0, duration: 0.6, ease: 'power2.out', stagger: 0.06 })`——卡片/列表瀑布
  2. **媒体卡 hover**：`{ scale: 1.015, boxShadow 升至 lift, duration: 0.5 }`——位移不超过 1.5%
  3. **确认卡/弹层**：`{ y: -12, opacity: 0 } → { y: 0, opacity: 1, duration: 0.45, ease: 'power3.out' }` + 遮罩 opacity
  4. **数字/进度**：`gsap.to(obj, { val: target, snap: 1, duration: 1 })` 计数动画
  5. **AI 面板**：展开 `{ height: auto, opacity: 1, duration: 0.55, ease: 'power2.inOut' }`；运行态光点用 CSS pulse（非 GSAP 循环）
- **降级**：`prefers-reduced-motion: reduce` 时全部动画跳过（useGSAP 内 `gsap.matchMedia()` 判定）

## 9. Anti-Patterns（NEVER DO——含 P0 映射）
1. **无 emoji 作图标**（P0-1）——图标用统一线性 SVG（1.5px 描边）
2. **无紫/紫粉渐变与光效**（P0-2）——ai-motion 默认光效已由 CSS 禁用，不得恢复
3. **无 AI 模板味**（P0-3）：无 Lorem ipsum/Welcome 占位、无硬编码色值（全部走 tokens）、无弹跳缓动
4. 无 Inter / Roboto / Times / Georgia
5. 无纯黑纯白、无饱和度 > 25% 的色
6. 无「三等分卡片横排」、无居中 hero、无重叠层叠
7. 无 spinner 圆圈加载、无「暂无数据」裸文案、无 Scroll-to-explore 类引导废话
8. 无 `height:100vh`、无 layout 属性动画、无滚动容器上的 backdrop-blur
9. 无硬编码色值——一切经由 `tokens.css` 变量

## 10. Page Inventory & Rollout（实施账本）

| 视图 | 路由 | 效果图 | 实装 | 备注 |
|---|---|---|---|---|
| TimelineView | `/timeline` | ✅ | ✅ CDP 验证 | 门面页，首攻 |
| AlbumsView / AlbumDetailView | `/albums` | ✅ 确认 | ✅ 实装 | 卡 veil/磨砂 tag/封面头条/grid-stagger/banner-in/dgrid-enter |
| SettingsView（含 LLM 上游卡） | `/settings` | ✅ 已截 | ✅ | CDP 实拍确认 |
| AdminView | `/admin` | ✅ 确认 | ✅ 实装 | 页头 11/11 胶囊 tab/panel-fade |
| MapView / PlacesView | `/map` `/places` | ⏳ 排队 | tokens 已生效 | 地图瓦片底色需单独调和 |
| PeopleView / TagsView | `/people` `/tags` | ✅ 确认 | ✅ 实装 | 圆形头像卡体系/聚类虚线卡/磨砂云 chip/cloud-in |
| SpacesView / FoldersView | `/spaces` `/folders` | ✅ 确认 | ✅ 实装 | 2:1 空间双卡/胶囊分段/树导航 1:2.5/cards-in |
| ToolboxView / UploadView | `/toolbox` `/upload` | ✅ 确认 | ✅ 实装 | 胶囊 tabs/tabs-fade/雾蓝虚线 dropzone/queue-in |
| SearchResultsView | `/search` | ✅ 确认 | ✅ 实装 | 页头 10/磨砂 chips/筛选条面板化/results-in |
| PlayerView / SharePublicView | `/player/:id` `/share/:token` | ⏳ 排队 | 独立暗底不受影响 | 播放器保持暗底 |
| LoginView / RegisterView / BootView / SetupView | 独立路由 | ✅ 确认 | ✅ 实装 | 雾面氛围/卡片规范/card-in；Boot 含历史耗时估算进度条 |
| ManualView | `/manual` | ⏳ 排队 | tokens 已生效 | |

**实施状态**：Phase 1（tokens 全局换肤）✅ 已在 115 运行。Phase 2（逐页组件深化：veil / 页头规范 / GSAP 进入动效）⏳ 按本账本逐页「效果图 → 用户确认 → 实装 → CDP 截图」推进。
