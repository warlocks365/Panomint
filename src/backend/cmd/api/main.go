// API 服务入口（T1.1 骨架）。
// 装配：配置 → 日志 → PG/Valkey → gin 引擎（五件套中间件 + JWT 占位）→ 健康检查端点 → 优雅停机。
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"panoalbum/internal/albums"
	"panoalbum/internal/auth"
	"panoalbum/internal/config"
	"panoalbum/internal/embed"
	"panoalbum/internal/faces"
	"panoalbum/internal/folders"
	"panoalbum/internal/geo"
	"panoalbum/internal/health"
	"panoalbum/internal/media"
	"panoalbum/internal/middleware"
	"panoalbum/internal/queue"
	"panoalbum/internal/search"
	"panoalbum/internal/shares"
	"panoalbum/internal/spaces"
	"panoalbum/internal/tags"
	"panoalbum/internal/transcode"
)

func main() {
	cfg := config.Load()

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

	q := queue.New("sys", queue.Config{Addr: cfg.ValkeyAddr, Password: cfg.ValkeyPass})
	defer q.Close()

	r := gin.New()
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
	authH := &auth.Handler{Store: authStore, Secret: secret}

	// 种子管理员（开发默认 admin@pano.local / ADMIN_PASSWORD，生产必须经环境变量覆盖）
	adminPwd := os.Getenv("ADMIN_PASSWORD")
	if adminPwd == "" {
		adminPwd = "pano-admin-dev-only"
	}
	if err := authStore.EnsureSeedAdmin(ctx, "admin@pano.local", adminPwd); err != nil {
		log.Warn("管理员种子写入失败", zap.Error(err))
	}

	// 公开端点
	r.POST("/auth/login", authH.Login)
	r.POST("/auth/refresh", authH.Refresh)
	r.POST("/auth/logout", authH.Logout)

	// 鉴权端点
	authed := r.Group("", auth.AuthRequired(secret))
	authed.GET("/auth/me", authH.Me)

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
	}
	permRead := auth.RequirePerm(authStore, "media:read")
	permWrite := auth.RequirePerm(authStore, "media:write")

	authed.GET("/media", permRead, mediaH.List)
	// Phase 3 媒体端点（静态段 trash 优先于 :id，gin 自动处理优先级）
	authed.GET("/media/trash", permRead, mediaH.Trash)
	authed.POST("/media/trash/:id/restore", permWrite, mediaH.Restore)
	authed.DELETE("/media/trash/:id", permWrite, mediaH.Purge)
	authed.GET("/media/date-histogram", permRead, mediaH.DateHistogram) // Job000005 日期密度直方图
	authed.GET("/media/:id", permRead, mediaH.Detail)
	authed.PATCH("/media/:id", permWrite, mediaH.Patch) // Job000005 备注（仅 notes 字段）
	authed.POST("/media/upload", permWrite, mediaH.Upload)
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
	transH := &transcode.Handler{Pool: pool, Q: transQ, HLSDir: cfg.HLSDir}
	authed.POST("/transcode/job", permWrite, transH.CreateJob)
	authed.GET("/transcode/job/:id", permRead, transH.JobStatus)
	authed.GET("/transcode/hls/:id/*file", permRead, transH.ServeHLS)

	// Stage 1 相册 + 评论（读 media:read；写 album:write，归属校验在 handler 内）
	albumsH := &albums.Handler{Store: &albums.Store{Pool: pool}}
	permAlbumWrite := auth.RequirePerm(authStore, "album:write")
	authed.POST("/albums", permAlbumWrite, albumsH.Create)
	authed.GET("/albums", permRead, albumsH.List)
	authed.GET("/albums/:id", permRead, albumsH.Get)
	authed.PATCH("/albums/:id", permAlbumWrite, albumsH.Patch)
	authed.DELETE("/albums/:id", permAlbumWrite, albumsH.Delete)
	authed.POST("/albums/:id/items", permAlbumWrite, albumsH.AddItems)
	authed.DELETE("/albums/:id/items/:media_id", permAlbumWrite, albumsH.RemoveItem)
	authed.GET("/albums/:id/comments", permRead, albumsH.ListComments)
	authed.POST("/albums/:id/comments", permAlbumWrite, albumsH.AddComment)
	authed.DELETE("/albums/:id/comments/:cid", permAlbumWrite, albumsH.DeleteComment)

	// Stage 2 分享（种子无 share:write，member 角色持 share:create，owner/admin 由 share:* 通配覆盖）
	sharesH := &shares.Handler{Store: &shares.Store{Pool: pool, Albums: &albums.Store{Pool: pool}}, HLSDir: cfg.HLSDir}
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
	}
	authed.GET("/geo/clusters", permRead, geoH.Clusters)
	authed.GET("/geo/items", permRead, geoH.Items)
	authed.GET("/geo/histogram", permRead, geoH.Histogram)
	authed.GET("/geo/places", permRead, geoH.Places)               // Job000009 优化：地理位置罗列
	authed.GET("/tiles/amap/:z/:x/:y", permRead, geoH.Tiles.Serve) // Key 服务端注入，前端不持 Key
	authed.GET("/preferences/map", permRead, geoH.GetMapIconPref)  // Job000009 图标配置（账户级）
	authed.PUT("/preferences/map", permRead, geoH.PutMapIconPref)

	// 契约 §7：模糊地理搜索 + 可用底图（读 media:read）。
	// ⚠️ 该端点会打上游（中国走高德 / 国际走 Nominatim）并按 q 落 geo_cache ——
	// 注意上游配额（高德按 Key 计 QPS、Nominatim 条款要求 ≥1 req/s），详见 internal/geo/mapsearch.go 文件头。
	// 无高德 Key 时中国地名优雅降级到 Nominatim，绝不 500（降级归因见响应头 X-Map-Search-Degraded）。
	mapSearchSvc := geo.NewMapSearchService(pool, cfg.AmapKey, os.Getenv("AMAP_SECRET"), log)
	authed.GET("/map/search", permRead, mapSearchSvc.Search)      // ?q= → {candidates:[{name,lon,lat,provider}]}
	authed.GET("/map/providers", permRead, mapSearchSvc.Providers) // → {china,intl,default}（不含密钥）

	// Stage 2 公开端点（无鉴权，token 即凭证；不提供原文件下载）
	r.GET("/public/shares/:token", sharesH.PublicGet)
	r.GET("/public/shares/:token/media/:id/thumb", sharesH.PublicThumb)
	r.GET("/public/shares/:token/media/:id/hls/*file", sharesH.PublicHLS)
	r.GET("/public/shares/:token/media/:id/download", sharesH.PublicDownload) // 占位：统一 403，P1 实现

	// Phase 4 P1：分享页 OG 封面（服务端渲染最简 HTML；社交抓取器不执行 JS，只看初始 HTML）。
	// 绝对 URL 由请求 Host 动态拼出，不写死域名；受密码保护的分享只返回中性卡片（不泄露标题与缩略图）。
	r.GET("/public/shares/:token/og", sharesH.PublicOG)
	// Phase 4 P1：分享带宽自测（契约 §11，token 鉴权、无 JWT）。
	// 三段式：下行探针(GET) → 上行探针(POST) → 无 body 的汇总(POST，落 bandwidth_profiles/tests)。
	r.GET("/public/shares/:token/bandwidth-probe", sharesH.PublicProbeDown)
	r.POST("/public/shares/:token/bandwidth-probe", sharesH.PublicProbeUp)
	r.POST("/public/shares/:token/bandwidth-test", sharesH.PublicBandwidthTest)

	// 管理端点（需 admin:users 权限）
	admin := authed.Group("/admin", auth.RequirePerm(authStore, "admin:users"))
	admin.POST("/users", authH.CreateUser)
	admin.GET("/users", authH.ListUsers)

	h := &health.Handler{Pool: pool, Queue: q, DiskCheckDir: "./data"}
	r.GET("/health", h.Live)
	r.GET("/ready", h.Ready)
	r.GET("/metrics", h.Metrics())

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		log.Info("API 启动", zap.String("addr", srv.Addr), zap.String("env", cfg.Env))
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
