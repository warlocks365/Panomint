// API 服务入口（T1.1 骨架）。
// 装配：配置 → 日志 → PG/Valkey → gin 引擎（五件套中间件 + JWT 占位）→ 健康检查端点 → 优雅停机。
package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"go.uber.org/zap"

	"panoalbum/internal/albums"
	"panoalbum/internal/audit"
	"panoalbum/internal/auth"
	"panoalbum/internal/compute"
	"panoalbum/internal/config"
	"panoalbum/internal/embed"
	"panoalbum/internal/faces"
	"panoalbum/internal/folders"
	"panoalbum/internal/storage"
	"panoalbum/internal/geo"
	"panoalbum/internal/health"
	"panoalbum/internal/media"
	"panoalbum/internal/middleware"
	"panoalbum/internal/queue"
	"panoalbum/internal/search"
	"panoalbum/internal/setup"
	"panoalbum/internal/shares"
	"panoalbum/internal/spaces"
	"panoalbum/internal/tags"
	"panoalbum/internal/transcode"
	"panoalbum/internal/version"
	"panoalbum/migrations"
)

func main() {
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		// 生产环境仍在使用"仅适用于本机开发"的默认配置时直接拒绝启动，
		// 而不是静默连到 127.0.0.1 上的陌生库。此处只用 os，避免新引入 import。
		_, _ = os.Stderr.WriteString("配置错误: " + cfgErr.Error() + "\n")
		os.Exit(1)
	}

	var log *zap.Logger
	if cfg.Env == "prod" {
		log, _ = zap.NewProduction()
		gin.SetMode(gin.ReleaseMode)
	} else {
		log, _ = zap.NewDevelopment()
	}
	defer log.Sync()

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.PGDSN)
	if err != nil {
		log.Fatal("PG 连接池创建失败", zap.Error(err))
	}
	defer pool.Close()

	// 启动自迁移（v1.0.0 发布形态"解压即起"的根基）：迁移经编译期嵌入执行，
	// 三种发布形态（裸机/集成包/Docker）由此不再需要"先手工跑 migrate"这一步。
	// goose up 幂等；失败即拒启动（fail-closed）——绝带着半成品 schema 接流量。
	// AUTO_MIGRATE=off 仅供本地非常规排障。
	if cfg.AutoMigrate {
		if err := runMigrations(cfg.PGDSN); err != nil {
			log.Fatal("数据库自动迁移失败", zap.Error(err))
		}
		log.Info("数据库迁移已就位（goose up，幂等）")
	}

	q := queue.New("sys", queue.Config{Addr: cfg.ValkeyAddr, Password: cfg.ValkeyPass})
	defer q.Close()

	r := gin.New()
	// gin 默认信任 0.0.0.0/0（把全部来源当可信代理），此时 ClientIP() 直接取
	// X-Forwarded-For 最左端——完全由客户端伪造：IP 限流可逐请求换 XFF 旁路，
	// 审计/日志的 IP 字段也会失真。这里只信任配置的反代地址（默认本机 nginx 与
	// docker 网桥，TRUSTED_PROXIES 可覆盖），其余来源的 XFF 一律忽略。
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatal("可信代理配置错误（TRUSTED_PROXIES）", zap.Error(err))
	}
	r.Use(
		middleware.RequestID(),
		middleware.Recovery(log),
		middleware.Logging(log),
		middleware.CORS(cfg.CORSOrigins),
		middleware.RateLimit(q.NewRateLimiter(), 60, 10), // 每 IP：突发 60、持续 10/s
	)

	// ---- T1.3 鉴权装配 ----
	secret, err := auth.JWTSecret()
	if err != nil {
		log.Fatal("JWT 密钥配置错误", zap.Error(err))
	}
	authStore := &auth.Store{Pool: pool}
	// 审计写入器在鉴权装配**之前**创建：登录与二次验证（启用/关闭）都要写审计，
	// 而这几件事发生在整个 api 生命周期里最早的时刻，晚创建就漏记。
	// 它是"尽力而为"的（失败只记 warn，不影响请求），因此提前创建没有可用性风险。
	auditRec := audit.New(pool, log)
	authH := &auth.Handler{Store: authStore, Secret: secret, Audit: auditRec}

	// 种子管理员：仅开发环境自动种（admin@pano.local / ADMIN_PASSWORD 或开发默认值）。
	// 生产环境的第一个账号由首次安装向导创建（POST /setup，Job000107）——
	// 若运维显式设置 ADMIN_PASSWORD，视为其明确选择"跳过向导、直接种子"，照常种。
	if cfg.Env != config.EnvProd || os.Getenv("ADMIN_PASSWORD") != "" {
		// 种子管理员（开发默认 admin@pano.local / ADMIN_PASSWORD，生产必须经环境变量覆盖）
		adminPwd := os.Getenv("ADMIN_PASSWORD")
		if adminPwd == "" {
			adminPwd = "pano-admin-dev-only"
		}
		if err := authStore.EnsureSeedAdmin(ctx, "admin@pano.local", adminPwd); err != nil {
			log.Warn("管理员种子写入失败", zap.Error(err))
		}
	}

	// 公开端点
	r.POST("/auth/login", authH.Login)
	r.POST("/auth/refresh", authH.Refresh)
	r.POST("/auth/logout", authH.Logout)

	// ===== 版本与首次安装引导（Job000105/107）=====
	// /version：构建版本三元组（发布产物注入 ldflags；开发产物恒 "dev"），
	//   无鉴权 —— 它是部署验收与故障排查的第一现场信息，不含任何敏感数据。
	// /setup/*：一次性初始化向导（未初始化时创建首个 owner；已初始化后 POST 恒 409）。
	//   无鉴权（系统里还没有任何账号时无法鉴权）；
	//   ⚠️ nginx 前缀同步：setup 在「导航判别组」（/setup 同时是前端路由），
	//     version 在「纯 API 组」——漏配会拿到 index.html 假 200（docker/web/Dockerfile）。
	r.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"version":    version.Version,
			"commit":     version.Commit,
			"build_date": version.BuildDate,
		})
	})
	setupH := &setup.Handler{Store: &setup.PGStore{Pool: pool}, Audit: auditRec}
	r.GET("/setup/status", setupH.Status)
	r.POST("/setup", setupH.Create)

	// ===== SSO/OIDC（Job000054，契约 v1.3 补录）=====
	// 通用 OIDC（Keycloak/Authelia/任意标准 IdP），环境变量 SSO_OIDC_* 配置；
	// 未配置时 login/callback 404 同形（FailClosed），config 返回 {enabled:false}。
	// 全部公开端点（token 即凭证，与密码登录同形发 JWT）。
	ssoH := &auth.SSOHandler{Cfg: auth.SSOConfigFromEnv(), Store: authStore, Secret: secret, Audit: auditRec}
	r.GET("/auth/sso/config", ssoH.ConfigInfo)
	r.GET("/auth/sso/oidc/login", ssoH.LoginRedirect)
	r.POST("/auth/sso/oidc", ssoH.Callback)

	// 鉴权端点
	authed := r.Group("", auth.AuthRequired(secret))
	authed.GET("/auth/me", authH.Me)

	// ===== Phase 5：二次验证（TOTP，RFC 6238）=====
	// 三个端点都在鉴权后：它们操作的是"当前登录者自己"的二次验证，
	// 身份由会话给出（user_id），不接受请求体传入用户 id —— 否则就成了越权接口。
	// nginx 无需改动：`auth` 已在 docker/web/Dockerfile 的纯 API 正则组内。
	authed.POST("/auth/mfa/setup", authH.MFASetup)     // 生成待确认密钥 → {secret, otpauth_url}
	authed.POST("/auth/mfa/confirm", authH.MFAConfirm) // 用一次有效口令确认 → mfa_enabled=true
	authed.POST("/auth/mfa/disable", authH.MFADisable) // 需有效口令 → 关闭并清除密钥

	// T1.5 媒体/时间轴（需 media:read）
	mediaQ := queue.New("media", queue.Config{Addr: cfg.ValkeyAddr, Password: cfg.ValkeyPass}) // 缩略图队列（indexctl worker 消费）
	defer mediaQ.Close()
	transQ := queue.New("transcode", queue.Config{Addr: cfg.ValkeyAddr, Password: cfg.ValkeyPass}) // 转码队列（transcodectl worker 消费）
	defer transQ.Close()

	mediaH := &media.Handler{
		Store:     &media.Store{Pool: pool},
		Q:         mediaQ,
		UploadDir: cfg.UploadDir,
		UploadTmp: cfg.UploadTmp,
		MediaRoot: cfg.MediaRoot,
		Audit:     auditRec, // 写操作审计：media.delete / media.purge（后者不可恢复）
	}
	permRead := auth.RequirePerm(authStore, "media:read")
	permWrite := auth.RequirePerm(authStore, "media:write")

	// ===== WebDAV 直读直写（Job000055，契约 §16）=====
	// 树=个人空间 folder_path 目录树；Basic 认证（主密码或应用密码），不经 JWT。
	// 方法集超出 gin 的 Any（PROPFIND/MKCOL/MOVE/LOCK…），逐方法注册并整体包给 webdav.Handler 分发。
	davHandler := media.DAVHandler(mediaH, authStore)
	for _, m := range []string{"GET", "HEAD", "PUT", "DELETE", "PROPFIND", "MKCOL", "MOVE", "OPTIONS", "LOCK", "UNLOCK"} {
		r.Handle(m, "/dav", gin.WrapH(davHandler))
		r.Handle(m, "/dav/*path", gin.WrapH(davHandler))
	}

	authed.GET("/media", permRead, mediaH.List)
	// Phase 3 媒体端点（静态段 trash 优先于 :id，gin 自动处理优先级）
	authed.GET("/media/trash", permRead, mediaH.Trash)
	authed.POST("/media/trash/:id/restore", permWrite, mediaH.Restore)
	authed.DELETE("/media/trash/:id", permWrite, mediaH.Purge)
	authed.GET("/media/date-histogram", permRead, mediaH.DateHistogram) // Job000005 日期密度直方图
	// PRD §6.16 工具箱：重复项目（pHash 去重，阈值默认 10；只读，不自动删）。静态段同上优先于 /media/:id。
	authed.GET("/media/duplicates", permRead, mediaH.Duplicates)
	authed.GET("/media/:id", permRead, mediaH.Detail)
	authed.PATCH("/media/:id", permWrite, mediaH.Patch) // Job000005 备注（仅 notes 字段）
	authed.POST("/media/upload", permWrite, mediaH.Upload)
	authed.POST("/media/batch", permWrite, mediaH.Batch) // Job000066 统一批量操作
	authed.GET("/media/:id/download", permRead, mediaH.Download)
	authed.GET("/media/:id/thumb", permRead, mediaH.Thumb)
	authed.POST("/media/:id/favorite", permWrite, mediaH.Favorite)
	authed.POST("/media/:id/rate", permWrite, mediaH.Rate)
	authed.DELETE("/media/:id", permWrite, mediaH.Delete)
	authed.GET("/media/:id/360", permRead, mediaH.Pano360)
	// Job000010 Phase 4：图片旋转。写 media.edits（旋转角），缩略图由 index worker 按编辑参数重生成。
	authed.POST("/media/:id/rotate", permWrite, mediaH.Rotate)

	// Job000005 手工标签（读 media:read；写 media:write + 归属校验）
	authed.GET("/tags", permRead, mediaH.ListTags)
	authed.POST("/media/:id/tags", permWrite, mediaH.AddTag)
	authed.DELETE("/media/:id/tags/:tag_id", permWrite, mediaH.RemoveTag)

	// Job000010 Phase 4：标签管理 + AI 自动打标（零样本分类，复用 CLIP 文本塔 × 已缓存 media.embedding）。
	// 权限沿用 media:read / media:write（种子权限无独立 tag:*，与既有手工标签一致）。
	// mediaH.Tagger 为 nil 时（CLIP 不可用）读接口仍可用，预览仅返回已落库结果，触发返回 503。
	authed.POST("/tags", permWrite, mediaH.CreateTag)
	authed.PATCH("/tags/:id", permWrite, mediaH.PatchTag)
	authed.DELETE("/tags/:id", permWrite, mediaH.DeleteTag)
	authed.GET("/tags/:id/media", permRead, mediaH.ListTagMedia)
	authed.POST("/tags/:id/confirm", permWrite, mediaH.ConfirmTag)
	authed.POST("/media/:id/tags/confirm", permWrite, mediaH.ConfirmMediaTags)
	authed.GET("/ai/tags", permRead, mediaH.AITagsPreview)
	authed.POST("/ai/tags", permWrite, mediaH.AITagsTrigger)

	// Job000010 Phase 4：人物。只依赖纯 SQL 的 faces.Store，故非 CGO 构建下读接口亦可提供；
	// 实际的检测/聚类由 facesgen 清扫循环承担，此处 POST /ai/faces 仅复位扫描标记。
	facesH := &faces.Handler{Store: &faces.Store{Pool: pool}}
	authed.GET("/people", permRead, facesH.ListPeople)
	authed.POST("/people", permWrite, facesH.CreatePerson)
	authed.PATCH("/people/:id", permWrite, facesH.PatchPerson)
	authed.GET("/people/:id/media", permRead, facesH.PersonMedia)
	authed.POST("/ai/faces", permWrite, facesH.TriggerScan)

	// Phase 3 空间 / 文件夹 / 转码
	spacesH := &spaces.Handler{Pool: pool}
	authed.GET("/spaces", spacesH.Get)
	foldersH := &folders.Handler{Pool: pool}
	authed.GET("/folders/tree", permRead, foldersH.Tree)
	authed.POST("/folders", permWrite, foldersH.Create)
	authed.PATCH("/folders/rename", permWrite, foldersH.Rename)
	authed.DELETE("/folders", permWrite, foldersH.Delete)
	authed.PUT("/folders/grants", permWrite, foldersH.SetGrants)
	// Job000070 F4 网络挂载管理：写=owner/admin（handler 内 RolePrivileged 前置），
	// 列表=登录即可（shared 挂载全员可见）。
	storageH := &storage.Handler{Pool: pool}
	authed.GET("/storage/mounts", permRead, storageH.List)
	authed.POST("/storage/mounts", permWrite, storageH.Create)
	authed.PATCH("/storage/mounts/:id", permWrite, storageH.Patch)
	authed.DELETE("/storage/mounts/:id", permWrite, storageH.Delete)
	authed.POST("/storage/mounts/:id/test", permWrite, storageH.Test)
	// Job000103 存储位置（命名物理存储根，Docker 映射名约束 slug 化）
	authed.GET("/storage/locations", permRead, storageH.ListLocations)
	authed.POST("/storage/locations", permWrite, storageH.CreateLocation)
	authed.PATCH("/storage/locations/:id", permWrite, storageH.PatchLocation)
	authed.DELETE("/storage/locations/:id", permWrite, storageH.DeleteLocation)
	transH := &transcode.Handler{Pool: pool, Q: transQ, HLSDir: cfg.HLSDir}
	authed.POST("/transcode/job", permWrite, transH.CreateJob)
	authed.GET("/transcode/job/:id", permRead, transH.JobStatus)
	authed.GET("/transcode/hls/:id/*file", permRead, transH.ServeHLS)

	// Stage 1 相册 + 评论（读 media:read；写 album:write，归属校验在 handler 内）
	albumsH := &albums.Handler{Store: &albums.Store{Pool: pool}}
	permAlbumWrite := auth.RequirePerm(authStore, "album:write")
	authed.POST("/albums", permAlbumWrite, albumsH.Create)
	authed.GET("/albums", permRead, albumsH.List)
	authed.GET("/albums/groups", permRead, albumsH.Groups) // Job000101 静态段须先于 :id 注册
	authed.GET("/albums/:id", permRead, albumsH.Get)
	authed.PATCH("/albums/:id", permAlbumWrite, albumsH.Patch)
	authed.DELETE("/albums/:id", permAlbumWrite, albumsH.Delete)
	authed.POST("/albums/:id/items", permAlbumWrite, albumsH.AddItems)
	authed.DELETE("/albums/:id/items/:media_id", permAlbumWrite, albumsH.RemoveItem)
	authed.GET("/albums/:id/comments", permRead, albumsH.ListComments)
	authed.POST("/albums/:id/comments", permAlbumWrite, albumsH.AddComment)
	authed.DELETE("/albums/:id/comments/:cid", permAlbumWrite, albumsH.DeleteComment)

	// Stage 2 分享（种子无 share:write，member 角色持 share:create，owner/admin 由 share:* 通配覆盖）
	sharesH := &shares.Handler{Store: &shares.Store{Pool: pool, Albums: &albums.Store{Pool: pool}}, HLSDir: cfg.HLSDir, MediaRoot: cfg.MediaRoot, Audit: auditRec}
	permShare := auth.RequirePerm(authStore, "share:create")
	authed.POST("/shares", permShare, sharesH.Create)
	authed.GET("/shares", permShare, sharesH.List)
	authed.DELETE("/shares/:id", permShare, sharesH.Delete)

	// Phase 4 P1：带宽（契约 §15）。GET 读生效带宽（manual 优先，否则最近自测）；
	// PATCH 手动指定；probe/self-test 为登录态自测探针（与分享侧共用同一探针实现）。
	// 探针会真实占用上下行带宽，已做尺寸与总耗时上界，详见 internal/shares/bandwidth.go。
	authed.GET("/bandwidth", permRead, sharesH.GetBandwidth)
	authed.PATCH("/bandwidth", permWrite, sharesH.PatchBandwidth)
	authed.GET("/bandwidth/probe", permRead, sharesH.ProbeDown)
	authed.POST("/bandwidth/probe", permRead, sharesH.ProbeUp)
	authed.POST("/bandwidth/self-test", permWrite, sharesH.SelfTest)

	// Stage 3 结构化搜索（Job000001，API §10）；Stage 4 语义召回（Job000010）在此接入。
	// place 降级经 geo_cache 缓存的 Nominatim resolver。
	// 语义召回依赖 CLIP 模型与 onnxruntime 原生库：任一缺失则降级为占位实现（Stage 3 行为），
	// 绝不因 AI 资产缺失导致 API 无法启动。
	recaller, tagger := buildRecaller(pool, log)
	mediaH.Tagger = tagger // Phase 4 零样本打标器；nil 表示降级（标签读接口仍可用）
	searchH := &search.Handler{Store: &search.Store{
		Pool:     pool,
		Recaller: recaller,
		Resolver: &search.CachedResolver{Pool: pool, Provider: search.NominatimGeocoder{}},
	}}
	authed.GET("/search", permRead, searchH.Search)

	// Job000009 地图模式（读 media:read）：全屏地图 + 时间轴双向联动
	geoH := &geo.Handler{
		Media:    &geo.MediaStore{Pool: pool},
		Tiles:    &geo.AmapTileProxy{Key: cfg.AmapKey, CacheDir: cfg.TileCacheDir},
		Provider: "amap", // 底图为高德 → 对外输出 GCJ-02（库内仍存 WGS-84）
		// GET/PUT /admin/map-config 需要：读 system_map_config 以显示"Key 从哪来、能不能用"
		// （只暴露状态，不回显密钥），以及系统配置变更写审计。
		MapCfg:     &geo.MapConfigStore{Pool: pool},
		AmapKeyEnv: cfg.AmapKey,
		Audit:      auditRec,
	}
	authed.GET("/geo/clusters", permRead, geoH.Clusters)
	authed.GET("/geo/items", permRead, geoH.Items)
	authed.GET("/geo/histogram", permRead, geoH.Histogram)
	authed.GET("/geo/places", permRead, geoH.Places)               // Job000009 优化：地理位置罗列
	authed.GET("/geo/places-overview", permRead, geoH.PlacesOverview) // Job000062 地点页聚合
	authed.GET("/tiles/amap/:z/:x/:y", permRead, geoH.Tiles.Serve) // Key 服务端注入，前端不持 Key
	authed.GET("/preferences/map", permRead, geoH.GetMapIconPref)  // Job000009 图标配置（账户级）
	authed.PUT("/preferences/map", permRead, geoH.PutMapIconPref)

	// 契约 §7：用户地图 UI 偏好（账户级，需登录）+ 系统地图配置（管理端）。
	// ⚠️ `/user` 是**新前缀**，已同步加进 docker/web/Dockerfile 的纯 API 正则组 ——
	// 漏配的话浏览器直连该路径会拿到 index.html（本项目已因此踩坑三次）。
	authed.GET("/user/ui-prefs", permRead, geoH.GetUIPrefs) // {map_slider_pos,map_filter_side,map_default_provider,map_default_zoom}
	authed.PUT("/user/ui-prefs", permRead, geoH.PutUIPrefs)
	// Job000034 用户自助改密：只要求已登录（对自己的口令不需要额外权限），
	// handler 内部会验证旧口令 + 吊销全部会话。/user 前缀已在 nginx 反代组内，无需改 nginx。
	authed.PUT("/user/password", authH.ChangePassword)
	// Job000098 应用密码自助管理（WebDAV/第三方客户端专用凭据）：状态查询 + 生成/轮换 + 清除。
	// 三个端点都只要求已登录（对自己的凭据不需要额外权限），handler 内部验证当前密码。
	// /user 前缀已在 nginx 反代组内（Job000034 已确认），无需改 nginx。
	authed.GET("/user/app-password", authH.AppPasswordStatus)
	authed.PUT("/user/app-password", authH.AppPasswordGenerate)
	authed.POST("/user/app-password/clear", authH.AppPasswordClear)
	authed.GET("/admin/map-config", auth.RequirePerm(authStore, "admin:system"), geoH.GetMapConfig)
	authed.PUT("/admin/map-config", auth.RequirePerm(authStore, "admin:system"), geoH.PutMapConfig)

	// 契约 §7：模糊地理搜索 + 可用底图（读 media:read）。
	// ⚠️ 该端点会打上游（中国走高德 / 国际走 Nominatim）并按 q 落 geo_cache ——
	// 注意上游配额（高德按 Key 计 QPS、Nominatim 条款要求 ≥1 req/s），详见 internal/geo/mapsearch.go 文件头。
	// 无高德 Key 时中国地名优雅降级到 Nominatim，绝不 500（降级归因见响应头 X-Map-Search-Degraded）。
	mapSearchSvc := geo.NewMapSearchService(pool, cfg.AmapKey, os.Getenv("AMAP_SECRET"), log)
	authed.GET("/map/search", permRead, mapSearchSvc.Search)       // ?q= → {candidates:[{name,lon,lat,provider}]}
	authed.GET("/map/providers", permRead, mapSearchSvc.Providers) // → {china,intl,default}（不含密钥）

	// Stage 2 公开端点（无鉴权，token 即凭证；不提供原文件下载）
	r.GET("/public/shares/:token", sharesH.PublicGet)
	r.GET("/public/shares/:token/media/:id/thumb", sharesH.PublicThumb)
	r.GET("/public/shares/:token/media/:id/hls/*file", sharesH.PublicHLS)
	r.GET("/public/shares/:token/media/:id/download", sharesH.PublicDownload) // Job000053：allow_download=true 兑现原文件（含审计+访问配额）

	// Phase 4 P1：分享页 OG 封面（服务端渲染最简 HTML；社交抓取器不执行 JS，只看初始 HTML）。
	// 绝对 URL 由请求 Host 动态拼出，不写死域名；受密码保护的分享只返回中性卡片（不泄露标题与缩略图）。
	r.GET("/public/shares/:token/og", sharesH.PublicOG)
	// Phase 4 P1：分享带宽自测（契约 §11，token 鉴权、无 JWT）。
	// 三段式：下行探针(GET) → 上行探针(POST) → 无 body 的汇总(POST，落 bandwidth_profiles/tests)。
	r.GET("/public/shares/:token/bandwidth-probe", sharesH.PublicProbeDown)
	r.POST("/public/shares/:token/bandwidth-probe", sharesH.PublicProbeUp)
	r.POST("/public/shares/:token/bandwidth-test", sharesH.PublicBandwidthTest)

	// ===== T6.2 算力节点 agent（契约 §15）=====
	// 两个认证平面：管理端是人操作（JWT + admin:system）；节点侧是机器凭据（agent_token），
	// **不复用 JWT** —— 节点没有 user_id/角色，且需要"按节点吊销/轮换"的粒度。
	computeStore := &compute.Store{Pool: pool}
	// 可认领的任务类型：默认仅 noop（见 compute.DefaultClaimableKinds 的说明）。
	//
	// ⚠️ 放开 hls 是**显式的运维动作**，不是改个默认值就行：kind='hls' 的行同时被
	// cmd/transcodectl 通过 **Valkey 队列**消费（注意它消费的是队列项，不是 SQL 轮询），
	// 必须先下线那条消费（停 transcode-worker 或停止入队），否则同一条 job_id 被两边各跑一遍，
	// 产出互相覆盖。另外放开时须确认所有在轮询的节点都配了 `-executor local` 与媒体根，
	// 否则它们会领到任务再失败（执行器对不支持的 kind 是**显式失败**，不会假装成功）。
	if kinds, err := compute.ParseClaimableKinds(os.Getenv("COMPUTE_CLAIMABLE_KINDS")); err != nil {
		log.Fatal("COMPUTE_CLAIMABLE_KINDS 非法", zap.Error(err))
	} else if len(kinds) > 0 {
		computeStore.ClaimableKinds = kinds
	}
	{
		effective := computeStore.ClaimableKinds
		if len(effective) == 0 {
			effective = compute.DefaultClaimableKinds
		}
		// 明确打出生效集合：这是"节点为什么领不到 hls 任务"这类问题的第一现场证据。
		log.Info("算力节点可认领任务类型", zap.Strings("kinds", effective))
	}

	// 任务认领次数上限（迁移 00022 的 transcode_jobs.attempts）：
	// 达到上限的任务由控制端判 failed，避免"反复领了就死"的节点让同一条任务被无限重派。
	// 非法或 <=0 一律回落默认值 —— 0 会让认领条件 `attempts < 0` 恒假、任何任务都领不到。
	if v := strings.TrimSpace(os.Getenv("COMPUTE_MAX_ATTEMPTS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			computeStore.MaxAttempts = n
		} else {
			log.Warn("COMPUTE_MAX_ATTEMPTS 非法，回落默认值",
				zap.String("value", v), zap.Int("default", compute.DefaultMaxAttempts))
		}
	}
	{
		eff := computeStore.MaxAttempts
		if eff <= 0 {
			eff = compute.DefaultMaxAttempts
		}
		log.Info("算力节点任务认领上限", zap.Int("max_attempts_per_job", eff))
	}
	computeOfflineAfter := compute.OfflineAfterFromEnv() // COMPUTE_OFFLINE_AFTER_SECONDS，默认 180s（TDD §6.1：60s×3）
	computeH := &compute.Handler{Store: computeStore, OfflineAfter: computeOfflineAfter}
	computeAgentH := &compute.AgentHandler{
		Store:             computeStore,
		OfflineAfter:      computeOfflineAfter,
		HeartbeatInterval: compute.DefaultHeartbeatInterval,
	}

	// ⚠️ 通配段只出现在 PATCH/DELETE。**不要**新增 POST /compute-nodes/:id/xxx 这类路由
	// （例如 :id/rotate-token），否则与下面 /compute-nodes/agent 的静态段在同一层冲突 → gin 启动即 panic。
	// 令牌轮换已并入 PATCH 的 rotate_token 字段。
	adminNodes := authed.Group("/compute-nodes", auth.RequirePerm(authStore, "admin:system"))
	adminNodes.GET("", computeH.List)
	adminNodes.POST("", computeH.Register)
	adminNodes.PATCH("/:id", computeH.Patch)
	adminNodes.DELETE("/:id", computeH.Delete)

	// 节点侧：挂在 r（**不走 JWT**），由 AgentAuth 按 agent_token 的 sha256 校验并注入 node_id。
	agentPlane := r.Group("/compute-nodes/agent", compute.AgentAuth(computeStore, computeOfflineAfter))
	agentPlane.POST("/heartbeat", computeAgentH.Heartbeat)
	agentPlane.POST("/poll", computeAgentH.Poll)
	agentPlane.POST("/result", computeAgentH.Result)

	// 管理端点（需 admin:users 权限）
	admin := authed.Group("/admin", auth.RequirePerm(authStore, "admin:users"))
	admin.POST("/users", authH.CreateUser)
	admin.GET("/users", authH.ListUsers)

	// ===== Phase 5：管理后台完整化（用户与角色）=====
	// 契约 §2「管理端点（需 admin:users）」：PATCH 改角色/状态/重置密码；DELETE 禁用/删除；
	// GET /admin/roles 角色与权限列表。
	// ⚠️ 权限点仍取库中实际存在的 admin:users，不新造（与 /admin/audit 同级）。
	// 两条硬守卫在 handler 里（auth.CheckUserPatch，纯函数可穷举单测）：不许改自己的角色/状态、
	// 不许移除最后一个可用 owner —— 这两件事都会让系统**当场失去管理入口**且只能靠改库恢复。
	admin.PATCH("/users/:id", authH.UpdateUser)
	admin.DELETE("/users/:id", authH.DeleteUser)
	admin.GET("/roles", authH.ListRoles)
	// 契约 §2「POST /admin/roles 新建角色 + 权限」。
	// ⚠️ 本端点带**提权守卫**：只能授予调用者自己已拥有的权限 ——
	// 否则只持 admin:users 的人可建含 admin:system 的角色、再建账号用它登录，完成自我提权
	// （因为 POST /admin/users 允许按名字指定任意角色）。见 auth.PermCovered。
	admin.POST("/roles", authH.CreateRole)

	// Job000067（F1 权限体系增强）：权限元数据（中文化/自定义分类）+ 用户级增量授权。
	// 全挂 admin:users（与角色管理同级）；enforcement 不变（knownPerms 白名单把关写入）。
	admin.GET("/perms", authH.ListPermMeta)
	admin.PUT("/perms/:perm", authH.PutPermMeta)
	admin.GET("/users/:id/perms", authH.ListUserPerms)
	admin.PUT("/users/:id/perms", authH.PutUserPerm)

	// ===== Phase 5 精选第一项：审计日志 + 管理端只读端点 =====
	// 契约 §2 `GET /admin/audit`（分页）、§14 `GET /admin/stats`、§12 `GET /admin/jobs`。
	// 权限点一律取自库中**实际存在**的种子权限（实测只有 admin:system / admin:users 两个 admin 前缀），不新造权限名：
	//   /admin/audit → admin:users（契约 §2「管理端点（需 admin:users）」表头明确列出）
	//   /admin/stats、/admin/jobs → admin:system（系统级运维面，与 §15 节点管理同级）
	// 审计写入是**尽力而为**的：失败只记 warn，绝不影响业务请求（见 internal/audit 包文档）。
	// nginx 无需改动：/admin 已在 docker/web/Dockerfile 的纯 API 正则组内。
	auditStore := &audit.PGStore{Pool: pool}
	auditH := &audit.Handler{Store: auditStore, Recorder: auditRec}
	admin.GET("/audit", auditH.ListAudit)
	// 用户侧：**自己**执行过的「从回收站恢复」历史（工具箱「已恢复」标签的数据来源）。
	//
	// 挂在 `authed` 而**不是** `admin` —— 这是本端点存在的全部意义：普通成员必须能看
	// 自己的恢复历史，而 /admin/audit 在 admin:users 之下，成员够不到。
	// 权限用 media:read：它属于 /media 命名空间，与其它 /media 读端点一致。
	//
	// 注册在同一组 `authed` 上 ⇒ 与 /media/:id 落在**同一棵路由树**，
	// 静态段 restore-history 优先于参数段 :id（与 /media/trash、/media/duplicates 同型，
	// 后两者的优先级已被真实请求实测过，不是推断）。
	// 实现放在 audit 包：数据源是 audit_log，复用的也是该包的 Filter/游标/limit 约定，
	// 抄一份到 media 包就会漂（§二十一）。先例：/admin/stats、/admin/jobs 同样是
	// audit 包实现、挂在别人的路径前缀下。理由详见 internal/audit/restore_history.go。
	authed.GET("/media/restore-history", permRead, auditH.RestoreHistory)
	authed.GET("/admin/stats", auth.RequirePerm(authStore, "admin:system"), auditH.Stats)
	authed.GET("/admin/jobs", auth.RequirePerm(authStore, "admin:system"), auditH.Jobs)
	// 契约 §12 的 `GET /admin/jobs/:id`（此前只是契约里的悬空引用，本轮补上实现）
	authed.GET("/admin/jobs/:id", auth.RequirePerm(authStore, "admin:system"), auditH.GetJob)

	h := &health.Handler{Pool: pool, Queue: q, DiskCheckDir: "./data"}
	r.GET("/health", h.Live)
	r.GET("/ready", h.Ready)
	r.GET("/metrics", h.Metrics())

	// P2-04：分块上传会话残留清扫（UploadTmp 下 mtime 超 24h 的 .part/.json）。
	// 启动时扫一次，之后每 6h 周期清扫——中断的上传（网络断、用户放弃）否则永久残留。
	// 失败只记 warn：清扫是维护动作，不该阻断启动。
	sweepUploads := func() {
		n, err := media.SweepStaleUploads(cfg.UploadTmp, media.StaleUploadTTL, time.Now())
		if err != nil {
			log.Warn("分块上传残留清扫失败", zap.Error(err))
			return
		}
		if n > 0 {
			log.Info("分块上传残留清扫完成", zap.Int("removed", n))
		}
	}
	sweepUploads()
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			sweepUploads()
		}
	}()

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		log.Info("API 启动", zap.String("addr", srv.Addr), zap.String("env", cfg.Env),
			zap.String("version", version.String()), zap.String("commit", version.Commit))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("服务异常退出", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("优雅停机失败", zap.Error(err))
	}
	log.Info("已停机")
}

// runMigrations 执行编译期嵌入的 goose 迁移（与 cmd/migrate 读同一批文件：
// panoalbum/migrations.FS）。失败语义由调用方决定（这里 = 拒启动）。
func runMigrations(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	goose.SetBaseFS(migrations.FS)
	return goose.Up(db, ".")
}

// buildRecaller 构造 Stage 4 语义召回器，并顺带构造 Phase 4 零样本打标器。
//
// 设备由环境变量选择（EMBED_DEVICE=cpu|cuda|auto，默认 auto）：
// 优先 CUDA，装配失败则回落 CPU —— GPU / CPU 双接口在同一二进制中保留，
// 由部署环境决定实际生效者，不因开发机能力而裁剪。
//
// CLIP 模型或 onnxruntime 原生库缺失时降级为占位实现（等价 Stage 3 行为），
// 保证 AI 资产未就绪也不会阻断 API 启动。推理在**本地** CPU / GPU 完成。
//
// 打标器复用同一文本塔把词表编码成文本向量（启动时一次成本），
// 之后对每张图直接用已缓存的 media.embedding 算余弦相似度，无需再跑视觉塔。
// 词表编码失败同样只降级、不阻断启动。
func buildRecaller(pool *pgxpool.Pool, logger *zap.Logger) (search.Recaller, *tags.Classifier) {
	cfg := embed.ConfigFromEnv()
	enc, err := embed.NewEncoder(cfg)
	if err != nil {
		logger.Warn("语义召回未启用（CLIP 初始化失败，降级为结构化检索）",
			zap.String("model_dir", cfg.ModelDir),
			zap.String("device", string(cfg.Device)),
			zap.Error(err))
		return search.SemanticRecaller{}, nil
	}
	logger.Info("语义召回已启用",
		zap.String("model_dir", cfg.ModelDir),
		zap.String("family", string(enc.Family())),
		zap.String("provider", enc.Provider()),
		zap.String("device", string(enc.Device())),
		zap.Int("context_len", enc.ContextLen()))

	// 阈值按模型族解析（chinese-clip 0.35 / clip 0.24），可用 TAG_MIN_SIM 等环境变量覆盖；
	// 最终应以 `taggen -mode calibrate` 在真实库上标定后固化。
	//
	// 注入持久缓存：二次启动直接载入标签向量，不再跑 570 次文本编码
	// （这是启动从 ~110s 回落到数秒的关键）。缓存读写失败只记警告并降级为编码，不影响启动。
	var clf *tags.Classifier
	if c, cerr := tags.NewClassifier(context.Background(), enc, nil, tags.ClassifyConfig{
		ModelDir: cfg.ModelDir,
		Cache:    &tags.PGLabelVectorCache{Pool: pool},
	}); cerr != nil {
		logger.Warn("AI 打标未启用（词表编码失败）", zap.Error(cerr))
	} else {
		minSim, topRatio, maxTags, family := c.Params()
		logger.Info("AI 打标已启用",
			zap.String("family", family),
			zap.Int("labels", c.LabelCount()),
			zap.Float64("min_sim", minSim),
			zap.Float64("top_ratio", topRatio),
			zap.Int("max_tags", maxTags))
		clf = c
	}

	return &search.VectorRecaller{
		Enc:   enc,
		Store: &embed.Store{Pool: pool},
		// TopK 同时是语义注入的上限：过大会让 total 被无关项撑高
		// （实测 72 条库上取 50 + 阈值 0.80 时 total 常达 30~50）。
		TopK: 25,
	}, clf
}
