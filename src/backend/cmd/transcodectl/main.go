// transcodectl 转码命令行：worker 子命令消费 HLS 转码任务；enqueue-videos 批量补齐缺失的 HLS 任务。
// 用法：
//
//	transcodectl worker [-hlsdir ./data/hls] [-mediaroot ./testdata/media] [-uploaddir ./data/media] [-count N]
//	transcodectl enqueue-videos [-dsn <PG 连接串>] [-profile 1080p|2k|4k] [-dry-run]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/config"
	"panoalbum/internal/queue"
	"panoalbum/internal/transcode"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[transcodectl] ")

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: transcodectl <worker|enqueue-videos> [flags]")
		os.Exit(2)
	}

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		log.Fatalf("配置错误: %v", cfgErr)
	}

	// ffprobe 与 ffmpeg 通常同目录：FFPROBE_PATH 未设时回退用 FFMPEG_PATH
	if os.Getenv("FFPROBE_PATH") == "" {
		if p := os.Getenv("FFMPEG_PATH"); p != "" {
			os.Setenv("FFPROBE_PATH", p)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "worker":
		fs := flag.NewFlagSet("worker", flag.ExitOnError)
		hlsDir := fs.String("hlsdir", "./data/hls", "HLS 输出目录")
		mediaRoot := fs.String("mediaroot", envOr("MEDIA_ROOT", ""), "既有索引媒体根目录（必填；或设 MEDIA_ROOT）")
		uploadDir := fs.String("uploaddir", envOr("UPLOAD_DIR", "./data/media"), "上传媒体根目录")
		count := fs.Int("count", 0, "处理 N 个任务后退出（0=持续消费）")
		_ = fs.Parse(os.Args[2:])
		if *mediaRoot == "" {
			// 不给默认路径（P2-07）：忘传参时静默读写错误目录比直接报错更糟。
			log.Fatal("worker 需要 -mediaroot（或设 MEDIA_ROOT）")
		}

		db := openPG(ctx, cfg.PGDSN)
		defer db.Close()
		q := openQueue(ctx, cfg)
		defer q.Close()

		w := transcode.NewHLSWorker(db, q, *hlsDir, *uploadDir, *mediaRoot)
		if *count > 0 {
			for i := 0; i < *count; i++ {
				n, err := w.Run(ctx, true)
				if err != nil {
					log.Fatalf("worker 错误: %v", err)
				}
				if n == 0 {
					break // 队列已空
				}
			}
			fmt.Println("worker 定额模式结束")
			return
		}
		log.Printf("worker 启动，HLS 目录 %s", *hlsDir)
		if _, err := w.Run(ctx, false); err != nil {
			log.Fatalf("worker 错误: %v", err)
		}

	case "enqueue-videos":
		fs := flag.NewFlagSet("enqueue-videos", flag.ExitOnError)
		dsn := fs.String("dsn", cfg.PGDSN, "PostgreSQL 连接串（默认取 PG_DSN / .env）")
		profile := fs.String("profile", "", "强制档位 1080p|2k|4k（默认按源分辨率自动选择）")
		dryRun := fs.Bool("dry-run", false, "只打印将要入队的清单，不写库不入队")
		_ = fs.Parse(os.Args[2:])

		db := openPG(ctx, *dsn)
		defer db.Close()
		var q *queue.Queue // dry-run 不需要队列
		if !*dryRun {
			q = openQueue(ctx, cfg)
			defer q.Close()
		}
		res, err := transcode.EnqueueMissingHLS(ctx, db, q, *dryRun, *profile)
		if err != nil {
			log.Fatalf("批量入队失败: %v", err)
		}
		fmt.Printf("入队完成 pending=%d enqueued=%d failed=%d\n", res.Total, res.Enqueued, res.Failed)

	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q（支持 worker|enqueue-videos）\n", os.Args[1])
		os.Exit(2)
	}
}

// openPG 建立 PG 连接池并探活。
func openPG(ctx context.Context, dsn string) *pgxpool.Pool {
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("连接 PG 失败: %v", err)
	}
	if err := db.Ping(ctx); err != nil {
		log.Fatalf("PG 不可达: %v", err)
	}
	return db
}

// openQueue 建立转码队列客户端并探活。
func openQueue(ctx context.Context, cfg config.Config) *queue.Queue {
	q := queue.New("transcode", queue.Config{Addr: cfg.ValkeyAddr, Password: cfg.ValkeyPass})
	if err := q.Ping(ctx); err != nil {
		log.Fatalf("Valkey 不可达: %v", err)
	}
	return q
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
