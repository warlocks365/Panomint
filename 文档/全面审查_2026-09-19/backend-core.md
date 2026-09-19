# 审查分卷：后端核心域

审查基线：HEAD=61838f8 ｜ 范围：cmd/api/main.go、internal/{auth,audit,media,albums,shares,spaces,folders,health,middleware,httperr,cursor,queue,compute,watch,config,pgxutil,mediascope}、migrations/ 全部 26 个迁移文件。

核查方法：先读 main.go 路由表建立端点地图（含每条路由的中间件链），再逐包精读 handlers/store；用 Grep 全量扫 `fmt.Sprintf.*(SELECT|INSERT|UPDATE|DELETE)`、`go func`、`panic`、`time.Sleep`、`TODO|FIXME`、`REFERENCES media` 等危险模式后对命中点逐一精读验证；与《待解决问题记录.md》逐节对照防重复（§十八/二十/二十一/二十二/二十四/二十六/二十七 已收敛项均未重复报告）。

覆盖文件（精读 44 个源文件 + 26 个迁移）：
- cmd/api/main.go
- auth：handlers.go / jwt.go / middleware.go / store.go / totp.go / roles.go
- audit：audit.go / store.go / query.go / handlers.go / restore_history.go
- media：handlers.go / scope.go / mediaref.go / timeline.go / thumb.go / detail.go / write.go / write_handlers.go / upload.go / duplicates.go / histogram.go / tags.go / tag_handlers.go
- albums：handlers.go / store.go / criteria.go
- shares：handlers.go / store.go / og.go / bandwidth.go
- spaces/spaces.go、folders/folders.go、health/health.go、middleware/{middleware,ratelimit}.go、httperr/httperr.go、cursor/cursor.go、queue/{queue,ratelimit}.go、config/config.go、pgxutil/pgerr.go、mediascope/scope.go、watch/{watcher,debounce}.go
- compute：store.go / agentapi.go / token.go / handlers.go / agent.go / executor.go / offline.go
- migrations：00001–00026 全量索引/约束扫描，00004/00005/00006 精读，外键行为全量 grep

## 统计

| P0 | P1 | P2 | 合计 |
|----|----|----|------|
| 1  | 5  | 16 | 22   |

## P0 发现（逐条）

### [P0-01] 相册评论端点完全无归属校验：跨用户读评论（含他人邮箱）+ 跨用户写评论

- 位置：src/backend/internal/albums/handlers.go:273-311（ListComments / AddComment）；路由注册于 src/backend/cmd/api/main.go:199-200
- 描述：同文件的 `Get`（handlers.go:108-130）用 `getAlbumMeta + canManage` 把「无权」收敛成 404，这正是 §二十六 `0ec9a78` 修掉的越权。但**同一个修复没有覆盖评论端点**：
  - `ListComments`（:273）直接 `h.Store.ListComments(c.Param("id"))`，既不查相册存在性、也不做 `canManage`——任何持 `media:read` 的账号可读取**任意相册 id** 的全部评论，且 `ListComments` SQL（store.go:433-434）会 `COALESCE(NULLIF(u.display_name,''), u.email)` 把评论作者的**邮箱**一并返回（display_name 为空时），构成跨用户信息泄漏；
  - `AddComment`（:283）只校验相册存在（:285 `errors.Is(err, ErrNotFound)`），不校验归属——任何持 `album:write` 的账号可往**他人相册**写评论（store.go:455 的 `AddComment` 以调用者 user_id 落库）。
- 影响：跨用户读（评论内容 + 作者邮箱枚举）与跨用户写（向他人相册注入内容）双向越权。与 §二十六 同类（可见性覆盖面没被定义），属「[登记簿已录-新证据]」：§二十六 修了 `GET /albums/:id` 本体，漏掉了它下面的两条子资源路由。
- 建议：
  1. `ListComments`：开头加 `getAlbumMeta`（404 同形）+ `canManage` 判定，口径与 `Get` 完全一致（无权 404）；
  2. `AddComment`：同样加 `canManage`（无权按 `Get` 的 404 口径，避免存在性预言机）；
  3. 若要保留「共享相册成员可评论」的产品语义，则以「相册属主 ∪ 共享空间成员」为判定集，但**必须显式实现**，不能靠缺省放行；
  4. 邮箱兜底建议改成不返回邮箱（仅 display_name，空则返回空串/「用户」），评论列表不是该泄漏邮箱的地方；
  5. 在 `ownership_test.go` 补两条负对照（viewer 读/写他人相册评论 → 404）。

## P1 发现（逐条）

### [P1-01] gin 默认信任全部代理：X-Forwarded-For 可伪造 ClientIP → IP 限流旁路 + 审计/日志 IP 失真

- 位置：src/backend/cmd/api/main.go:69（`r := gin.New()` 后从未调用 `SetTrustedProxies`）；消费点：src/backend/internal/middleware/ratelimit.go:16（`"api:ip:"+c.ClientIP()`）、src/backend/internal/audit/audit.go 经 FromGin（handlers.go:141 `c.ClientIP()`）、middleware.Logging（middleware.go:58）
- 描述：gin v1.12.0 默认 `TrustedProxies = ["0.0.0.0/0", "::/0"]`、`RemoteIPHeaders = ["X-Forwarded-For","X-Real-IP"]`。全部地址都被视为可信代理时，`ClientIP()` 直接取 XFF 最左端——**完全由客户端控制**。后果：
  1. 全局限流（每 IP 突发 60、持续 10/s）形同虚设：每个请求换一个伪造 XFF 即获得全新令牌桶，`/auth/login` 的口令爆破不受限（bcrypt 与 MFA 仍在，但第一道闸没了）；
  2. 审计表 `ip` 列与访问日志的 `ip` 字段可被任意伪造——审计的归因价值受损（本项目刚为 actor 归因做过 00025/00026 两个迁移）。
- 影响：安全控制（限流）可被确定性旁路；审计/日志来源字段不可信。登记簿 Job000033 只变量化了 `X-Forwarded-Proto`，XFF 信任问题未覆盖（Grep 全文无 TrustedProxies）。
- 建议：main.go 装配时 `r.SetTrustedProxies(cfg.TrustedProxies)`，默认值取部署实际反代地址（如 `127.0.0.1` / docker 网桥 `172.21.0.0/16`，由环境变量 `TRUSTED_PROXIES` 覆盖）；显式置空列表 `[]string{}` 表示不信任任何代理（ClientIP 退回 RemoteAddr）。改后需回归验证经 Caddy/nginx 链路的真实 IP 仍正确。

### [P1-02] media.Purge 撞 NO ACTION 外键：被引用为封面/配对/原件的媒体永远无法彻底清除

- 位置：src/backend/internal/media/write.go:228-236（`DELETE FROM media ... RETURNING path`）；外键定义：migrations/00005_ddl_part.sql:14（albums.cover_media_id）、00005:4（people.cover_media_id）、00004_ddl_part.sql:52,100（media.duplicate_of / live_photo_pair_id，均 NO ACTION）
- 描述：`Purge` 直接 `DELETE FROM media`。但 `albums.cover_media_id`、`people.cover_media_id`、`media.duplicate_of`、`media.live_photo_pair_id` 四处引用 `media(id)` 且都**没有** `ON DELETE SET NULL/CASCADE`。只要待 purge 的媒体是任一相册封面、人物封面、Live Photo 配对目标或去重原件，DELETE 即以 23503 失败，handler（write_handlers.go:363-365）落到通用 500「更新失败」。
- 影响：回收站出现「删不掉」的媒体，用户无法清空；错误形态是 500（伪服务故障），用户无任何可操作指引。触发路径非常日常：相册设封面 → 删照片 → 清空回收站。
- 建议：迁移把四处外键改为 `ON DELETE SET NULL`（cover/pair/duplicate_of 语义上都允许悬空为 NULL），并在 Purge 的事务里先 `UPDATE albums SET cover_media_id=NULL WHERE cover_media_id=$1` 等做显式清理 + 审计 detail 记一笔「顺带解除 N 处引用」；至少在 handler 把 23503 映射为 409 并说明原因。

### [P1-03] queue.ConsumeOnce 死信路径 LMove 移错任务：并发消费下把别的 worker 的任务送进死信

- 位置：src/backend/internal/queue/queue.go:130-132
- 描述：`BRPopLPush` 把本任务压入 `processing` 的**头部**；成功/重试路径用 `LRem(processing, 1, res)` 按载荷精确移除——正确。但死信路径用 `LMove(processing, failed, "LEFT", "RIGHT")` 移的是**当前头部元素**。多 worker 并发时，本任务入队后可能有别的 worker 的任务被压到头部，LMove 就会把**别人的任务**移进死信，而本任务永远留在 processing（可靠队列语义被破坏，且没有回收 processing 的 sweeper）。
- 影响：并发消费同一队列时任务被误判死信 / 卡死在 processing。当前部署每队列单 worker 时不可达，但队列库的设计目标（注释自称「可靠队列」）明确支持并发消费，属潜伏正确性缺陷。
- 建议：死信路径改为「先 `LRem(processing, 1, res)`，成功再 `RPush(failed, res)`」（或 Lua 原子化）；长期可加 processing 超时回收（BRPOPLPUSH 模式的既定伴生件）。

### [P1-04] 智能相册详情/分享无任何 LIMIT：全库结果集一次序列化，且匿名可触发

- 位置：src/backend/internal/albums/store.go:243-255（smart 分支 `Get` 无 LIMIT）、store.go:256-276（manual 分支同理，受相册规模所限）；放大面：src/backend/internal/shares/store.go:224-245（`ListItems` → `Albums.Get`，经 `GET /public/shares/:token` **匿名**可达，handlers.go PublicGet）
- 描述：smart 相册的条目查询是 `SELECT MediaRefColumns FROM media m WHERE <criteria>`，无 LIMIT、无分页。一个条件为空的智能相册 = 属主全库媒体。经公开分享链路，匿名请求即可让服务端做一次全表扫描 + 把数万行 MediaRef 序列化成单个 JSON 响应（同时 shares 侧 OGCover 也要全量遍历）。
- 影响：大库下的内存/带宽放大与慢查询；匿名可触发使其成为 DoS 面。
- 建议：为 album items 引入上限与游标分页（复用 internal/cursor 的 (taken_at,id) 复合游标，与 /media 同构）；短期可先加硬上限（如 5000）+ `truncated` 标记，公开分享侧同样生效。

### [P1-05] 上传无体积上限与配额：任何 media:write 账号可写满磁盘

- 位置：src/backend/internal/media/upload.go:125-163（整文件）、309-333（ingest 的 `io.Copy` 落盘）
- 描述：整文件与分块上传都没有任何大小校验（无 `http.MaxBytesReader`、无单文件上限、无 per-user 配额、无磁盘剩余检查）。gin 的 `MaxMultipartMemory`（默认 32MiB）只影响内存缓冲，不限制落盘体积。分块协议的 `total` 由客户端自报，服务端照单全收。
- 影响：认证用户（或被打洞的 media:write 会话）可持续写满数据卷，导致媒体库与审计库整体不可用；与限流（P1-01 可被旁路）叠加后更难约束。
- 建议：配置化单文件上限（如 `UPLOAD_MAX_BYTES`，默认按产品预期取 2–10GiB），在 `parseContentRange` 校验 total、在 ingest 前校验 fh.Size；写盘前检查目标卷剩余空间（复用 health 的磁盘探测思路）；分块会话目录加 TTL 清扫（见 P2-04）。

## P2 发现（逐条）

### [P2-01] 共享空间成员「列表可见、详情/缩略图/下载 403」的口径断裂
- 位置：src/backend/internal/media/detail.go:97-99（canAccess 仅本人/owner/admin）vs src/backend/internal/mediascope/scope.go:106-109（sharedVerdict 允许成员）
- 描述：`GET /media?space=shared` 成员可见全部条目，但点进 `GET /media/:id` / `:id/thumb` / `:id/download` 全部被 `canAccess` 拒掉。若这是刻意的（列表只看元数据），应在契约与注释中写明；若是疏漏，则共享空间功能实际不可用。
- 建议：确认产品语义；若为疏漏，把 Detail/Thumb/Download 的判定扩为「属主 ∪ 该媒体 space=shared 且调用者是成员 ∪ owner/admin」，并把该判定收敛进 mediascope（单一真源，勿再各写一份）。

### [P2-02] ListMediaByTag 的 total 带游标条件，翻页时 total 逐页缩小
- 位置：src/backend/internal/media/tags.go:359-364（count 查询直接复用含游标的 where）
- 描述：`/media`（timeline.go:189-191）刻意剥掉游标再算 total，并在注释里说明理由；`/tags/:id/media` 却把游标条件一起算进 count，导致同一筛选下 total 随翻页递减。两处同构端点口径不一致。
- 建议：复用 timeline 的做法（计数用无游标的 where），或干脆把「带游标/不带游标」两种 where 的组装收敛成一个函数。

### [P2-03] ingest 去重查询吞掉真实 DB 错误，库故障时会插入重复媒体
- 位置：src/backend/internal/media/upload.go:341-348
- 描述：`err = QueryRow(...).Scan(&existID); if err == nil { 返回重复 }`——非 nil 的 err 同时包含「无重复」（ErrNoRows）与「库故障」，后者被静默当作无重复继续 INSERT。落盘后还 `os.Remove(abs)` 只在错误分支做，语义上把故障变成了脏数据。
- 建议：`if err == nil {...} else if !errors.Is(err, pgx.ErrNoRows) { return "", false, err }`。

### [P2-04] 分块上传会话无过期与清扫，`.part`/`.json` 永久残留
- 位置：src/backend/internal/media/upload.go:183-229（只在成功合并后 :273-276 删除）
- 描述：中断的上传（网络断、用户放弃、首块后不再续传）在 `UploadTmp` 留下完整残留，无任何 TTL/启动清扫。
- 建议：启动时或周期任务清扫 `UploadTmp` 下 mtime 超过阈值（如 24h）的 `.part/.json`；meta 里记创建时间便于判定。

### [P2-05] 分享访问密码走 `?password=` 查询串，会进入代理访问日志与浏览器历史
- 位置：src/backend/internal/shares/handlers.go:222（`c.Query("password")`）
- 描述：查询串会被 nginx/Caddy 访问日志、浏览器历史、Referer 记录。应用自身日志只记 path 不记 query（middleware.go:55），但链路其它层不保证。
- 建议：改 POST body 或专用头（如 `X-Share-Password`），公开端点各 handler 统一从该处取；契约同步更新。

### [P2-06] shares.Create 对畸形 target_id 返回 500（PG 22P02 原文进日志）而非 400
- 位置：src/backend/internal/shares/store.go:198-216（TargetOwnedBy 未做格式预判）+ handlers.go:91-99
- 描述：非 UUID 的 target_id 让 PG 抛 22P02，被当作 QUERY_FAILED 500。与项目既有的 IsMalformedID→404 约定（pgxutil）不一致。
- 建议：handler 层先用 UUID 正则校验 target_id（400 INVALID_PARAMS），或复用 pgxutil.IsMalformedID 映射。

### [P2-07] 删除相册后 share_links 悬空，公开分享页 500 而非 404
- 位置：src/backend/internal/shares/store.go:224-233（ListItems album 分支：Albums.Get → ErrNotFound → ErrTargetLost）+ handlers.go:249-253（ErrTargetLost 落到 QUERY_FAILED 500）
- 描述：albums.Delete 不触碰 share_links（target_id 无 FK）。吊销残留分享前，任何访客点开链接都是 500。
- 建议：PublicGet 把 ErrTargetLost 映射为 404「分享目标已删除」；或 albums 删除时级联清理指向它的 share_links（同事务 + 审计）。

### [P2-08] albums.List 每相册 2–3 次串行查询（计数 + 首图 + 封面缩略图）
- 位置：src/backend/internal/albums/store.go:155-200
- 描述：N 个相册 = 1 + 3N 次往返。相册数大时列表端点明显变慢。
- 建议：用两条聚合查询（按 album_id GROUP BY 计数、按 sort_key 取首图）+ 一次封面批量查询替代循环；或至少把三次合并成一条带窗口函数的 SQL。

### [P2-09] 相册属主不能删除自己相册下他人的评论
- 位置：src/backend/internal/albums/handlers.go:315-336（仅作者本人或 owner/admin 角色）
- 描述：属主对自己的相册没有内容治理权，结合 P0-01（任何人可写入）会更难受。
- 建议：删除判定扩为「评论作者 ∪ 相册属主 ∪ owner/admin」（相册属主身份经 getAlbumMeta 取得）。

### [P2-10] RotateSession 非原子：同一 refresh token 并发换发可双双成功，不触发重放检测
- 位置：src/backend/internal/auth/store.go:229-251
- 描述：先 `SELECT ... revoked` 再 `UPDATE ... revoked=true`，两个并发请求可都读到 revoked=false、各自签出新会话。重放检测因此存在竞态窗口。
- 建议：改为单条 `UPDATE sessions SET revoked=true WHERE refresh_token_hash=$1 AND revoked=false AND expires_at>now() RETURNING user_id`；0 行时再区分「不存在」与「已吊销（重放）」。

### [P2-11] 登录路径把 DB 故障映射成 401 BAD_CREDENTIALS
- 位置：src/backend/internal/auth/handlers.go:98-101
- 描述：`err != nil || !VerifyPassword(...)` 短链让「库连不上」与「密码错」同形 401。库故障时客户端会反复重试密码，监控也看不到 500。
- 建议：`if err != nil { if errors.Is(err, ErrBadCredentials) {401} else {500} }` 拆开（注意保持「用户不存在」与「密码错」同形，不引入枚举面）。

### [P2-12] RequirePerm 每请求一次 HasPerm 库查询，无任何缓存
- 位置：src/backend/internal/auth/middleware.go:33-42
- 描述：每个受保护端点至少 1 次（部分 2 次）额外 DB 往返；权限是低频变更数据。
- 建议：进程内带短 TTL（如 30s）的 role→perms 缓存，变更路径（CreateRole/UpdateUser）主动失效；或接受现状但在文档中写明每次请求的固定开销。

### [P2-13] compute agent 任务 goroutine 无 recover，执行器 panic 直接打挂进程
- 位置：src/backend/internal/compute/agent.go:280-287（`go func(j JobSpec){ ... a.runOne(ctx, j) }`）
- 描述：节点侧所有在跑任务都在裸 goroutine 里，LocalExecutor 未来接入更多 kind 后 panic 面增大；进程崩溃会让全部 running 任务进入回收等待（默认 180s+）。
- 建议：`runOne` 外包一层 `defer recover`，把 panic 转成 failed 结果回传。

### [P2-14] queue.PromoteDelayed 的 ZRem→LPush 非原子，崩溃窗口丢延迟任务
- 位置：src/backend/internal/queue/queue.go:146-163
- 描述：逐条 ZRem 成功、LPush 前进程退出，该延迟任务即丢失。
- 建议：Lua 脚本原子完成「ZRANGEBYSCORE + ZREM + LPUSH」。

### [P2-15] Recovery 中间件不记录堆栈，panic 现场只剩错误值
- 位置：src/backend/internal/middleware/middleware.go:34-43
- 描述：`zap.Any("error", err)` 没有 stack trace，线上 panic 定位只能靠猜。
- 建议：加 `zap.Stack("stack")`（或 `debug.Stack()`）。

### [P2-16] CORS 缺 `Vary: Origin`，中间缓存可能串源
- 位置：src/backend/internal/middleware/middleware.go:64-83
- 描述：响应按 Origin 动态输出 `Access-Control-Allow-Origin`，但不声明 `Vary: Origin`，共享缓存（CDN/反代）可能把 A 源的响应回给 B 源请求。
- 建议：命中白名单分支同时 `c.Header("Vary", "Origin")`。

### [P2-17] /metrics 无鉴权暴露进程级指标
- 位置：src/backend/cmd/api/main.go:399 + internal/health/health.go:76-78
- 描述：promhttp 默认暴露 go_* / process_*（内存、goroutine、FD、GC 节奏），无鉴权。敏感度低但属信息暴露面；若未来加入业务指标（队列长度、端点计数）敏感度会上升。
- 建议：部署层限制来源（nginx allow/deny），或挂到 authed + admin:system 之下；至少写进部署文档。

### [P2-18] THUMB_DIR 默认值跨二进制不一致
- 位置：src/backend/internal/media/thumb.go:46-49 与 internal/shares/handlers.go:291-294（默认 `./testdata/thumbnails`）vs cmd/embedgen/facesgen/phashgen（默认 `./data/thumbnails`）；cmd/indexctl 默认 `./testdata/thumbnails`
- 描述：生产若依赖默认值，api 与三个 gen 工具读写的不是同一目录，表现为缩略图 404 且难以排查。
- 建议：默认值统一为 `./data/thumbnails`（`./testdata` 只应出现在测试里），并把 THUMB_DIR 纳入 config 集中管理。

### [P2-19] PublicThumb 缺 os.Stat 前置，文件缺失时回落 net/http 纯文本 404
- 位置：src/backend/internal/shares/handlers.go:295-301
- 描述：media.Thumb（thumb.go:56-59）刻意先 Stat 以返回统一 JSON `FILE_MISSING`；公开侧同型路径没做，响应形状不一致（且不经 httperr 封套）。
- 建议：与 media.Thumb 对齐（Stat + errResp FILE_MISSING）。

## 附：已核对但确认无问题的重点项（防止误报回潮）

- TOTP 实现有 RFC 6238 附录 B 官方向量单测、常量时间比较、±1 窗口、无填充 base32，未发现实现错误。
- refresh token 只存 sha256、禁用即吊销、refresh 路径查用户状态——会话生命周期闭环成立。
- mediascope 的 fail-closed 与别名参数化正确；audit 的占位符不变量（add 闭包同源编号）在各 SQL 构造函数中一致成立，未发现 $n 错位。
- shares 公开端点：token 256bit、bcrypt 密码、MediaInShare 用相册属主可见集兜历史脏行、HLS 路径穿越防护完整。
- compute 双认证平面（JWT/admin:system 与 agent_token sha256）边界清晰；submitResult 双守卫、回收先行的顺序正确。
- migrations 00020/00025/00026 的审计索引与外键放宽与代码注释一致；media 常用过滤列（owner+space、taken_at、folder、hash、phash、deleted_at）均有对应索引。
