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
	"panoalbum/internal/folders"
	"panoalbum/internal/geo"
	"panoalbum/internal/health"
	"panoalbum/internal/media"
	"panoalbum/internal/middleware"
	"panoalbum/internal/queue"
	"panoalbum/internal/search"
	"panoalbum/internal/shares"
	"panoalbum/internal/spaces"
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

	// Job000005 手工标签（读 media:read；写 media:write + 归属校验）
	authed.GET("/tags", permRead, mediaH.ListTags)
	authed.POST("/media/:id/tags", permWrite, mediaH.AddTag)
	authed.DELETE("/media/:id/tags/:tag_id", permWrite, mediaH.RemoveTag)

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

	// Stage 3 结构化搜索（Job000001，API §10；SemanticRecaller 为 Stage 4 语义召回占位插槽，
	// place 降级经 geo_cache 缓存的 Nominatim resolver）
	searchH := &search.Handler{Store: &search.Store{
		Pool:     pool,
		Recaller: search.SemanticRecaller{},
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
	authed.GET("/tiles/amap/:z/:x/:y", permRead, geoH.Tiles.Serve) // Key 服务端注入，前端不持 Key
	authed.GET("/preferences/map", permRead, geoH.GetMapIconPref)    // Job000009 图标配置（账户级）
	authed.PUT("/preferences/map", permRead, geoH.PutMapIconPref)

	// Stage 2 公开端点（无鉴权，token 即凭证；不提供原文件下载）
	r.GET("/public/shares/:token", sharesH.PublicGet)
	r.GET("/public/shares/:token/media/:id/thumb", sharesH.PublicThumb)
	r.GET("/public/shares/:token/media/:id/hls/*file", sharesH.PublicHLS)
	r.GET("/public/shares/:token/media/:id/download", sharesH.PublicDownload) // 占位：统一 403，P1 实现

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
