// indexctl 媒体索引命令行：scan 扫描入库并入队缩略图任务；worker 消费缩略图任务。
// 用法：
//
//	indexctl scan -dir <媒体目录> [-thumbdir <缩略图目录>]
//	indexctl worker [-thumbdir <缩略图目录>] [-count N]
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
	"panoalbum/internal/geo"
	"panoalbum/internal/index"
	"panoalbum/internal/queue"
)

// newIndexer 构造索引器：配置了 AMAP_KEY 时启用高德逆地理编码（入库自动填 place），
// 否则降级为不填。Geocoder 内部失败不会阻塞入库，仅记日志。
func newIndexer(cfg config.Config, db *pgxpool.Pool, q *queue.Queue) *index.Indexer {
	if cfg.AmapKey == "" {
		log.Print("AMAP_KEY 未设置：入库不自动填充 place（逆地理编码已禁用）")
		return index.New(db, q)
	}
	log.Print("AMAP_KEY 已加载：入库将自动反查地名填充 place")
	return index.NewWithGeocoder(db, q, &geo.AmapGeocoder{Key: cfg.AmapKey, Secret: cfg.AmapSecret, Pool: db})
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[indexctl] ")

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: indexctl <scan|worker> [flags]")
		os.Exit(2)
	}

	cfg := config.Load()

	// ffprobe 与 ffmpeg 通常同目录：FFPROBE_PATH 未设时回退用 FFMPEG_PATH
	if os.Getenv("FFPROBE_PATH") == "" {
		if p := os.Getenv("FFMPEG_PATH"); p != "" {
			os.Setenv("FFPROBE_PATH", p)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := pgxpool.New(ctx, cfg.PGDSN)
	if err != nil {
		log.Fatalf("连接 PG 失败: %v", err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		log.Fatalf("PG 不可达: %v", err)
	}

	q := queue.New("media", queue.Config{Addr: cfg.ValkeyAddr, Password: cfg.ValkeyPass})
	defer q.Close()
	if err := q.Ping(ctx); err != nil {
		log.Fatalf("Valkey 不可达: %v", err)
	}

	switch os.Args[1] {
	case "scan":
		fs := flag.NewFlagSet("scan", flag.ExitOnError)
		dir := fs.String("dir", "", "待扫描媒体目录（必填）")
		_ = fs.Parse(os.Args[2:])
		if *dir == "" {
			log.Fatal("scan 需要 -dir")
		}
		st, err := newIndexer(cfg, db, q).Scan(ctx, *dir)
		if err != nil {
			log.Fatalf("扫描失败: %v", err)
		}
		fmt.Printf("扫描完成 job=%s total=%d inserted=%d duplicate=%d failed=%d\n",
			st.JobID, st.Total, st.Inserted, st.Duplicate, st.Failed)

	case "worker":
		fs := flag.NewFlagSet("worker", flag.ExitOnError)
		thumbDir := fs.String("thumbdir", "./testdata/thumbnails", "缩略图输出目录")
		count := fs.Int("count", 0, "处理 N 个任务后退出（0=持续消费）")
		_ = fs.Parse(os.Args[2:])
		w := index.NewThumbWorker(db, q, *thumbDir)
		if *count > 0 {
			// 定额模式：逐个处理，便于集成验证
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
		log.Printf("worker 启动，缩略图目录 %s", *thumbDir)
		if _, err := w.Run(ctx, false); err != nil {
			log.Fatalf("worker 错误: %v", err)
		}

	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q（支持 scan|worker）\n", os.Args[1])
		os.Exit(2)
	}
}
