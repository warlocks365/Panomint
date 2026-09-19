# 审查分卷：前端域

审查基线：HEAD=61838f8 ｜ 范围：`src/frontend/src`（14 views + 29 components + stores/api/composables/utils/router，约 17k 行）+ `public/sw.js` + `vite.config.js`。未审 node_modules 与 dist。全程只读，未改动任何源码。

## 统计

| P0 | P1 | P2 | 合计 |
|----|----|----|------|
| 0  | 10 | 16 | 26   |

## P0 发现

无。

核查覆盖面与方法（P0 级议题的逐项排除依据）：
- **XSS**：全仓 15 处 `v-html`（AppShell/UploadItem/FoldersView/SpacesView/UploadView/MapIconPicker）逐一核对，内容全部是模块内硬编码 SVG 常量，无任何用户可控输入进入 `v-html`；用户内容（评论、备注、文件名、标签）全部走 mustache 转义插值。
- **内存泄漏/资源未 dispose**：360Player.vue:607-648（hls.destroy、陀螺仪解绑、renderer.dispose+forceContextLoss、定时器全清）、MapView.vue:690-701（map.remove、5 个定时器全清）、PlayerView.vue:279-286（plainHls.destroy + blob URL 回收）、TimelineGrid.vue:382-387（RO/IO/scroll/RAF 全清）、MediaViewer.vue:467-473 均逐一验证有对称清理；`setInterval` 全仓仅 2 处（MediaViewer 幻灯片、backupManager watcher）均有清理路径。
- **刷新令牌并发安全**：api/http.js:27-46 单例 refreshPromise 去重逻辑正确；触发条件 `INVALID_TOKEN` 与后端 internal/auth/middleware.go:23 唯一 401 码一致；重放吊销场景下 refresh 失败走 forceLogout，无死循环。
- **路由守卫/token 过期**：router/index.js:105-113 仅查 token 存在性，过期由拦截器兜底，无守卫绕过。
- P1-02/03/04/05 的四处竞态均可造成短暂或需手动刷新才能恢复的错误展示，但都不致页面崩溃或持久数据损坏，故定 P1 不定 P0。

## P1 发现（逐条）

### [P1-01] 全局搜索从非搜索页首次回车不执行搜索；`?person=` 参数无人消费
- 位置：src/frontend/src/components/search/SearchBar.vue:40-47；src/frontend/src/views/SearchResultsView.vue（全文无 onMounted/watch）；src/frontend/src/views/PeopleView.vue:205-208
- 描述：`SearchBar.submit` 在非搜索页只 `setQuery` + `router.push({name:'search'})`，不调 `search.run()`；SearchResultsView 挂载后没有任何触发搜索的逻辑（`store.searched` 仍为 false），页面停留在「输入关键词或设置筛选条件开始搜索」，用户必须在搜索页**再按一次回车**才真正搜索。同理 PeopleView.openPerson 跳 `/search?person=<id>`，但 search store 的 `buildParams`（stores/search.js:56-67）根本不读 route query、也无 person 参数，点击人物卡片落到一个什么都不会发生的搜索页。
- 影响：全局搜索框（顶栏常驻）和人物页跳转两条主链路首次使用必现"没反应"，100% 可复现。
- 建议：SearchResultsView `onMounted`（或 watch route）时：若 `store.query` 非空或 `route.query` 带筛选参数（person 等）则 `store.run()`；并把 `person` 纳入 `buildParams`（后端契约 §8 已支持 `GET /search?person=`）。

### [P1-02] TimelineGrid 切换筛选与在途分页请求竞态，旧筛选结果混入新列表
- 位置：src/frontend/src/components/timeline/TimelineGrid.vue:201-227（loadMore）、238-247（reset）、389（watch props 触发 reset）
- 描述：`reset()` 清空 items/cursor 后不调任何作废机制；若上一筛选条件下的 `loadMore()` 仍在途，其响应到达后会继续 `items.push`（旧的 type/favorites 数据）并把 `nextCursor` 覆盖为旧查询的游标，后续翻页全部错位。`seen` 去重也救不了——旧数据本来就没见过。
- 影响：加载中切换「类型/收藏」筛选 → 时间轴混入上一筛选的媒体且分页游标错乱，需手动再切一次才恢复。
- 建议：加递增 `loadSeq`，loadMore 入口快照、响应落地前比对；或 reset 时置标志让在途响应直接丢弃。

### [P1-03] search store 的 run() 与在途 loadMore() 竞态，结果被旧查询回填
- 位置：src/frontend/src/stores/search.js:69-103
- 描述：`run()` 清空 results/cursor 后调用 `loadMore()`，但 `loadMore` 开头 `if (this.loading ...) return`——若上一次搜索仍在途，本次 loadMore 立即返回，而在途的旧查询响应随后 push 进刚清空的 results，并把 cursor 设成旧查询的游标。后果：显示的是上一个关键词的结果，「加载更多」用新关键词 + 旧游标，两批数据混杂。
- 影响：慢网环境下连续修改关键词/筛选（FilterControls 每次 patch 都会触发 run）即可复现结果串味。
- 建议：store 内维护 `reqSeq`，`run()` 时递增，loadMore 响应落地前校验；或引入 AbortController 取消在途请求。

### [P1-04] PlayerView 连续切换媒体时 load() 无序号守卫，旧响应覆盖新媒体
- 位置：src/frontend/src/views/PlayerView.vue:179-220（load）、316-322（watch mediaId immediate）
- 描述：播放集内按 ←/→ 快速切图时，每次路由变化触发 `load()`，但 load 没有任何代次校验：两个在途 load 谁慢谁后落地，`detail/pano/mode/hlsUrl/photoUrl` 以**完成顺序**而非**请求顺序**覆盖。更糟的是第二次 load 开头的 `cleanup()` 会 revoke 第一次已建的 blob URL，第一次的响应随后可能把已 revoke 的 URL 写回 `photoUrl/panoPhotoUrl` → 黑屏/破图。同文件 MediaViewer.loadCurrent 已有 `loadSeq` 范式（MediaViewer.vue:170-203）可对照。
- 影响：快速翻页时播放器显示上一个媒体的元数据甚至画面，360/HLS 源错配。
- 建议：照搬 MediaViewer 的 `++loadSeq` 守卫，落地前比对。

### [P1-05] MapView 主数据通道 reload() 无过期请求丢弃（同文件搜索/悬停都有守卫）
- 位置：src/frontend/src/views/MapView.vue:434-459（reload）、405（watch kind 直调 reload）、466-475（onRangeChange/onZoomChange 直调 reload）
- 描述：moveend/zoomend 经 250ms 防抖（scheduleReload），但 kind 切换、时间轴框选、粒度切换都**直调** reload；在途 reload 不被取消也无 seq 校验，慢响应后到会覆盖新视野的 clusters/buckets/places 并 `setData` 到地图。讽刺的是同文件地名搜索（searchReqId，:221-251）与悬停卡片（hoverRequestId，:555-569）都做了过期丢弃，唯独数据主通道没有。
- 影响：拖图过程中切类型/框选时间 → 地图点云与统计条短暂回到旧视野数据，下一次 moveend 才自愈。
- 建议：给 reload 加 `reloadSeq`；直调入口统一改走 scheduleReload 或先取消在途。

### [P1-06] iOS/Safari 原生 HLS 路径鉴权缺口：主站 360 视频 401、密码分享 ts 切片 403
- 位置：src/frontend/src/components/player/360Player.vue:410-413；src/frontend/src/views/SharePublicView.vue:344-346
- 描述：iPhone Safari 无 MSE，`Hls.isSupported()` 为 false 走原生 HLS 分支 `video.src = url`。该分支无法自定义请求头/改写切片 URL：①主站 360 视频（auth='bearer'）master 请求不带 Authorization → 401；②密码分享（360Player auth='none' 与 SharePublicView 普通视频同理）master URL 虽带 `?password=`，但 m3u8 内相对路径的 ts 切片请求不继承查询串（代码注释自己也承认这一点，360Player.vue:341-344 的 xhrSetup 就是为补它，但原生路径没有 xhrSetup）→ 切片 403。即：**iOS 上主站 360 视频必败、密码分享的所有视频必败**。PlayerView 普通视频有 blob 回退（PlayerView.vue:234-238）不受影响但触发 P1-07 的超时问题。
- 影响：iOS/微信内置浏览器（微信 iOS 也无 MSE）上三类播放路径失效；登记簿 U2 遗留「360Player 运行时选档」真机复验正好覆盖不到这个静态可判的缺口。
- 建议：原生分支检测 auth='bearer' 或 password 非空时，回退 blob 播放（小文件）或提示用桌面/Android；长期方案是后端为切片签发一次性查询令牌。

### [P1-07] 查看器大图/大视频 blob 全量下载 + 继承 15s 默认超时 + 与缩略图共用 600 条 LRU
- 位置：src/frontend/src/components/timeline/mediaLoader.js:12-15,51-54；src/frontend/src/components/viewer/MediaViewer.vue:181；src/frontend/src/views/PlayerView.vue:174-177,236；src/frontend/src/api/http.js:15
- 描述：`loadFullUrl`/`fetchBlobUrl` 把**原始文件整支**拉成 blob 入内存（视频可达数百 MB），且未覆盖 axios 默认 `timeout:15000`——慢网（<约 1MB/s 对 20MB 文件）必在 15s 处 abort，查看器显示「媒体加载失败」。此外 mediaLoader 的 LRU `MAX_CACHE=600` 对缩略图合理，但 `id:full` 条目与缩略图共用同一配额：连续浏览几十张原图即可占数百 MB 内存且挤掉缩略图缓存。
- 影响：大视频在查看器/Safari 回退路径慢网必失败；长浏览会话内存膨胀。
- 建议：①大图/视频请求显式 `timeout:0`；②`:full` 单独小容量缓存（如 3~5 条）或与缩略图分池；③视频优先走 HLS，blob 仅作最终回退。

### [P1-08] 缩略图加载的回退条件过宽：任何错误（不止 404）都回退下载原文件
- 位置：src/frontend/src/components/timeline/mediaLoader.js:40-47
- 描述：`loadThumbUrl` 的 catch 不区分错误类型——注释写「缩略图未生成（404）等情况：回退原文件」，实现却是**所有**失败（500、网络中断、429 限流——登记簿已记录时间轴首屏 55 并发缩略图触发全局限流）都回退 `/media/:id/download` 拉原文件。一次缩略图 429/超时换来几十 MB 原文件下载，进一步加剧限流，形成正反馈。
- 影响：故障放大器；慢/抖网络下时间轴流量与内存消耗数量级上升。
- 建议：仅 `e.response?.status === 404` 时回退原文件；其余错误向上抛出由 ThumbItem 显示占位（ThumbItem 本来就有失败占位路径）。

### [P1-09] 地图悬停卡片/条目列表缩略图无缓存无去重，每次悬停最多重拉 60 张
- 位置：src/frontend/src/api/map.js:87-91（thumbBlobUrl）；src/frontend/src/components/map/MapHoverCard.vue:63-78；src/frontend/src/components/map/MapItemList.vue:42-56
- 描述：`thumbBlobUrl` 每次调用都发新请求并新建 objectURL，无缓存无 pending 去重；MapHoverCard/MapItemList 又以 `v-if="hoverOpen/listOpen"` 挂载（MapView.vue:83-100），**每次悬停簇点都是全新组件实例** → 对该簇最多 60 个媒体各发一次 thumb 请求，悬停 N 次就 N×60。mediaLoader.js 已有现成的缓存+去重+回退实现（loadThumbUrl）却未被复用，且 MapHoverCard 的 watch 也无组件卸载后的写入守卫（thumbBlobUrl 慢响应到达时组件可能已卸载，仍写 `thumbs`/`objectUrls`）。
- 影响：地图核心交互（hover 预览）产生重复请求风暴与 objectURL 抖动；移动端长按预览同样命中。
- 建议：删除 `api/map.js::thumbBlobUrl`，两处组件改用 `loadThumbUrl({id},'sm')`（顺带获得 404 回退与 600 条 LRU）。

### [P1-10] 登录态启动不恢复用户信息：刷新后顶栏恒显示「用户/用」
- 位置：src/frontend/src/App.vue（仅 router-view）；src/frontend/src/main.js:8-11；src/frontend/src/router/index.js:105-113；src/frontend/src/layout/AppShell.vue:131-134
- 描述：`auth.user` 只在 login/mfa/SettingsView/AlbumComments 里被填充。刷新页面后 token 仍在、路由守卫放行，但全应用没有任何地方在启动时 `fetchMe()`——顶栏用户名/头像首字母永远停留在兜底值「用户/用」，直到用户恰好打开设置页或相册评论区（这两处各自补了 fetchMe，说明作者意识到问题但只在局部打补丁）。
- 影响：每次 F5 后身份信息错误常驻；mfaEnabled/mfaPending 等依赖 user 的 getter 在其他页面也不可用。
- 建议：在 router 守卫命中受保护路由且 `getAccessToken()` 存在而 `auth.user` 为空时拉一次 `/auth/me`（失败不阻塞导航），或在 App.vue onMounted 统一引导。

## P2 发现（逐条）

### [P2-01] 「相册」全链路已实现但导航仍标「开发中」；/spaces 有路由无导航入口
- 位置：src/frontend/src/layout/AppShell.vue:157-167
- 描述：`navItems` 中「相册」`ready:false`，但 AlbumsView/AlbumDetailView/评论/分享/封面的实现与路由（router/index.js:62-71）均已就绪；「文件夹」ready:false 属登记簿已录项（此处不再重复报告），「相册」未见登记。另 `/spaces` 路由存在（router/index.js:36-39）但导航列表根本没有「空间」入口，只能深链进入。
- 影响：已交付功能对用户不可见；导航状态与实现状态不一致会持续误导排期判断。
- 建议：确认相册验收状态后置 `ready:true`；/spaces 或补导航或下线路由。[文件夹 ready:false 为登记簿已录，本条仅指相册与空间]

### [P2-02] 网格尺寸计算、缩略图加载、格式化、errMsg、剪贴板降级五类逻辑多处复制
- 位置：cols/cellSize/rowHeight 计算 TimelineGrid.vue:94-104 与 SearchGrid.vue:58-68 逐行雷同；缩略图「watch item.id + alive 守卫 + loadThumbUrl」模式在 ThumbItem.vue:56-77、MediaThumb.vue:53-72、AlbumCard.vue:49-74、TrashThumb.vue:28-36 重复 4 次；formatBytes 在 uploadManager.js:178-188 与 SpacesView.vue:101-111 重复；formatDuration 在 ThumbItem.vue:79-85、MediaInfoPanel.vue:532-537、SharePublicView.vue:227-233 重复 3 次；errMsg 在 shareApi.js:25-27、albumApi.js:65-67、TagsView.vue:96-98、PeopleView.vue:162-164、ToolboxView.vue:212-214 重复 5 份；剪贴板复制+降级在 ShareCreateDialog.vue:134-148、ShareManageList.vue:106-121、SettingsView.vue:238-260 重复 3 份。
- 影响：行为漂移风险（登记簿 §二十一 正是同类失效模式的后端版本）。
- 建议：抽 `utils/format.js`、`utils/errMsg.js`、`composables/useThumbUrl.js`、`composables/useClipboard.js`；网格计算抽 `useGridMetrics(wrapRef)`。

### [P2-03] API 层不一致：7 个组件绕过 api/ 直接发 http 请求
- 位置：TagsView.vue、PeopleView.vue、SpacesView.vue、FoldersView.vue、TrashPanel.vue、MediaInfoPanel.vue、MediaPickerDialog.vue 均直接 `import http from '../api/http'`；而 map/media/search/bandwidth 四个域有独立封装。
- 影响：端点路径、响应兼容分支（`Array.isArray(data) ? data : data.tags` 这类）散在各组件，契约变更时要改一圈。
- 建议：按域补齐 api/tags.js、api/people.js 等，组件只调封装。

### [P2-04] MediaTooltip 每个实例都注册全局 scroll/resize 监听
- 位置：src/frontend/src/components/timeline/MediaTooltip.vue:130-131
- 描述：tooltip 监听器在 setup 顶层注册（每个 ThumbItem 一个实例），一屏上百卡片即数百个 window scroll(capture) 监听器，每次滚动触发同等次数的 hide() 闭包调用。
- 影响：滚动性能浪费；实例数随网格规模线性增长。
- 建议：tooltip 改为网格级单例（事件委托到卡片容器），或至少把 scroll/resize 监听提升到 TimelineGrid 一层。

### [P2-05] backupManager：文件选择器取消时 Promise 悬挂且 input 残留；watcher 仅靠 map 清空停止
- 位置：src/frontend/src/components/upload/backupManager.js:113-125,135-149
- 描述：`pickAndBackup` 只在 change 时 resolve 并移除 input；用户取消系统选择器时 Promise 永远不 settle、input 永远挂 body 上。watcher setInterval 只在 `hashByItemId.size===0` 时停，上传长期挂起（如后台标签页被冻结）期间每秒空转。
- 建议：加取消/超时路径（如 focus 回归后校验 input.files）；watcher 在队列无 active 时自停。

### [P2-06] PlayerView 转码轮询出错即无限重试，无退避无上限
- 位置：src/frontend/src/views/PlayerView.vue:273-275
- 描述：`catch { pollJob() }`——轮询请求持续失败（如服务下线、401 外的 5xx）时每 2s 一次永不停，直到离开页面。
- 建议：连续失败计数，≥N 次置 failed 并停止。

### [P2-07] SharePublicView：缩略图串行加载；大图 viewerUrl blob 不 revoke
- 位置：src/frontend/src/views/SharePublicView.vue:269-279,305-318
- 描述：loadThumbs 用 `for...await` 串行拉全部缩略图（N 张 = N 个 RTT）；`openItem` 的 lg 大图 objectURL 在 closeViewer 与组件卸载时都不 revoke（thumbs map 的在卸载时统一回收，viewerUrl 不在任何回收清单里）。
- 建议：缩略图改有限并发（如 6）；closeViewer/unmount 时 revoke viewerUrl。

### [P2-08] MediaInfoPanel 评分/收藏失败静默吞错
- 位置：src/frontend/src/components/player/MediaInfoPanel.vue:488-507
- 描述：rate/toggleFavorite 的 catch 注释「静默失败，保持界面原状」——用户点了没任何反馈，会以为没点上而重复操作；与同文件 tagError 有提示的做法不一致。
- 建议：复用 tagError 或加轻提示。

### [P2-09] 原生 prompt/alert/confirm 与设计化弹窗混用
- 位置：TagsView.vue:160-231（prompt×4/alert×5/confirm×2）、TrashPanel.vue:98（alert）、MediaInfoPanel.vue:511,517（confirm/alert）、ToolboxView.vue:299（alert）
- 影响：原生弹窗无样式、阻塞主线程，与项目已有的 confirm-dlg 组件风格割裂。
- 建议：抽一个轻量 ConfirmDialog/Toast，逐步替换原生调用。

### [P2-10] 调试残留 `window.__map` 未清理
- 位置：src/frontend/src/views/MapView.vue:651
- 描述：load 后挂全局且 onBeforeUnmount 不置空，地图销毁后全局仍引用死对象。
- 建议：删除或包在 `import.meta.env.DEV` 里，卸载时置 null。

### [P2-11] vite proxy 与 sw.js 的 API 前缀名单互相不一致且各有缺口
- 位置：src/frontend/vite.config.js:30；src/frontend/public/sw.js:25
- 描述：proxy 缺 `user|preferences|map|people|tags`（dev 下 /user/ui-prefs、/preferences/map、/map/search、/people、/tags 会落到 SPA 回退返回 index.html → JSON 解析错）；sw.js 的 NetworkOnly 名单缺 `user|map|people`（当前恰好因"非导航且无静态扩展名自然放行"而无害，但名单语义已失真）；两份名单本身也是重复维护（登记簿 §二十一 的失效模式）。
- 建议：两处共用一份常量（构建期生成或注释互引），并补齐缺口。

### [P2-12] 360Player 死变量 hevcSupported
- 位置：src/frontend/src/components/player/360Player.vue:85,565-567
- 描述：计算后从未使用（无任何读取点），疑似选档逻辑删减后的残留。
- 建议：删除，或接上 HEVC 档位过滤。

### [P2-13] SpacesView.loadMore 无 catch，失败成 unhandled rejection
- 位置：src/frontend/src/views/SpacesView.vue:147-157
- 描述：只有 finally 复位 loadingMore，错误直接逃逸。
- 建议：catch 后复用 loadError 提示。

### [P2-14] http 默认 15s 超时对慢端点无区分
- 位置：src/frontend/src/api/http.js:15；调用点 api/media.js:9-13（getDuplicates，后端 pHash O(N²)）、FoldersView.vue:156-169（最多连拉 20 页全量媒体）
- 描述：上传分块已显式 `timeout:0`，但重复检测、全量拉取这类天然慢的调用仍吃 15s 上限。
- 建议：按调用点显式声明超时，而非全局一刀切。

### [P2-15] AlbumsView.fillFirstCovers 为取首图 id 逐个拉相册详情（N+1）
- 位置：src/frontend/src/views/AlbumsView.vue:98-108
- 描述：每个无封面相册都 `getAlbum(id)` 拉全量 items 只为取 `items[0].id`；相册多则列表页一串大响应。
- 建议：后端列表接口直接下发热字段 `first_media_id`；前端暂可并发限制。

### [P2-16] 相册详情页媒体不可点开查看
- 位置：src/frontend/src/views/AlbumDetailView.vue:77-85
- 描述：MediaThumb 的 click 事件未绑定，相册里的照片只能看缩略图，不能进 MediaViewer/播放器（TagsView.vue:148-150 同类网格都有 openMedia 跳转）。
- 建议：绑定 click → router player 或复用 MediaViewer 播放集。
