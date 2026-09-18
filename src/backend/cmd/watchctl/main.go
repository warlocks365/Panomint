// watchctl P1 数据迁移工具（TDD v1.1 §8.8 / 计划 T6.1）：
// 全量扫描（断点续扫）+ fsnotify 持续监听增量导入 + 可选 rsync 先同步到 staging。
// 用法：
//
//	watchctl scan  -dir <媒体根> [-state <状态文件>] [-rsync <src>] [-staging <目录>]
//	watchctl watch -dir <媒体根> [-state <状态文件>] [-debounce 3s] [-rsync <src>] [-staging <目录>]
//
// 环境变量：WATCH_STATE（状态文件路径）、WATCH_DEBOUNCE（去抖时长）、RSYNC_BIN 预留；
// PG/Valkey 连接同 indexctl（config.Load / .env）。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/config"
	"panoalbum/internal/geo"
	"panoalbum/internal/index"
	"panoalbum/internal/queue"
	"panoalbum/internal/watch"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[watchctl] ")

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: watchctl <scan|watch> [flags]")
		os.Exit(2)
	}

	if os.Getenv("FFPROBE_PATH") == "" {
		if p := os.Getenv("FFMPEG_PATH"); p != "" {
			os.Setenv("FFPROBE_PATH", p)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		log.Fatalf("配置错误: %v", cfgErr)
	}
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
	// 配置了 AMAP_KEY 时启用逆地理编码（入库自动填 place），否则降级为不填。
	// Geocoder 内部失败不会阻塞入库，仅记日志。
	var indexer *index.Indexer
	if cfg.AmapKey == "" {
		log.Print("AMAP_KEY 未设置：入库不自动填充 place（逆地理编码已禁用）")
		indexer = index.New(db, q)
	} else {
		log.Print("AMAP_KEY 已加载：入库将自动反查地名填充 place")
		indexer = index.NewWithGeocoder(db, q, &geo.AmapGeocoder{Key: cfg.AmapKey, Secret: cfg.AmapSecret, Pool: db})
	}

	switch os.Args[1] {
	case "scan":
		fs := flag.NewFlagSet("scan", flag.ExitOnError)
		o := bindCommon(fs)
		_ = fs.Parse(os.Args[2:])
		root := prepareSource(ctx, o)
		st := mustState(o, root)
		runScan(ctx, indexer, root, st)

	case "watch":
		fs := flag.NewFlagSet("watch", flag.ExitOnError)
		o := bindCommon(fs)
		debounce := fs.Duration("debounce", envDuration("WATCH_DEBOUNCE", 3*time.Second), "事件去抖静默期")
		_ = fs.Parse(os.Args[2:])
		root := prepareSource(ctx, o)
		st := mustState(o, root)
		runWatch(ctx, indexer, root, st, *debounce)

	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q（支持 scan|watch）\n", os.Args[1])
		os.Exit(2)
	}
}

// options 公共 flag。
type options struct {
	dir     *string
	state   *string
	rsync   *string
	staging *string
}

func bindCommon(fs *flag.FlagSet) options {
	return options{
		dir:     fs.String("dir", "", "待导入媒体根目录（必填；rsync 模式下为 staging 落点）"),
		state:   fs.String("state", os.Getenv("WATCH_STATE"), "断点续扫状态文件（默认 <dir>/.watchctl-state.json）"),
		rsync:   fs.String("rsync", "", "可选：rsync 源（如 user@nas:/volume1/photo/），先同步再导入"),
		staging: fs.String("staging", "", "rsync 本地暂存目录（缺省用 -dir）"),
	}
}

// prepareSource 处理 rsync 预同步，返回实际扫描根目录。
func prepareSource(ctx context.Context, o options) string {
	if *o.dir == "" {
		log.Fatal("需要 -dir 指定媒体根目录")
	}
	root := *o.dir
	if *o.rsync != "" {
		staging := root
		if *o.staging != "" {
			staging = *o.staging
		}
		if err := os.MkdirAll(staging, 0o755); err != nil {
			log.Fatalf("建 staging 目录: %v", err)
		}
		log.Printf("rsync 同步 %s -> %s …", *o.rsync, staging)
		if err := watch.Rsync(ctx, *o.rsync, staging, nil, os.Stderr); err != nil {
			log.Fatalf("%v", err) // 含 rsync 不可用时的降级提示
		}
		root = staging
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		log.Fatal(err)
	}
	return abs
}

// mustState 加载断点续扫状态（默认落在扫描根下，工具可独立运行、不依赖额外服务）。
func mustState(o options, root string) *watch.State {
	path := *o.state
	if path == "" {
		path = filepath.Join(root, ".watchctl-state.json")
	}
	st, err := watch.LoadState(path, root)
	if err != nil {
		log.Fatalf("加载状态: %v", err)
	}
	return st
}

// importBatch 批量入库并按条更新断点状态（每 20 条落盘一次，中断不丢进度）。
func importBatch(ctx context.Context, indexer *index.Indexer, kind string,
	entries []index.FileEntry, st *watch.State) (*index.ScanStats, error) {
	n := 0
	stats, err := indexer.IndexEntries(ctx, kind, entries, func(e index.FileEntry, oc index.EntryOutcome) {
		if oc != index.OutcomeFailed {
			st.Mark(e) // failed 不记录，下次续扫重试
		}
		n++
		if n%20 == 0 {
			if err := st.Save(); err != nil {
				log.Printf("状态落盘失败: %v", err)
			}
		}
	})
	if serr := st.Save(); serr != nil {
		log.Printf("状态落盘失败: %v", serr)
	}
	return stats, err
}

// runScan 全量扫描：ScanDir → 状态过滤（断点续扫）→ 入库。
func runScan(ctx context.Context, indexer *index.Indexer, root string, st *watch.State) {
	entries, err := index.ScanDir(ctx, root)
	if err != nil {
		log.Fatalf("扫描目录: %v", err)
	}
	pending := st.Pending(entries)
	log.Printf("发现 %d 个媒体文件，待导入 %d（断点跳过 %d）", len(entries), len(pending), len(entries)-len(pending))
	if len(pending) == 0 {
		fmt.Println("无待导入文件")
		return
	}
	stats, err := importBatch(ctx, indexer, "full", pending, st)
	if err != nil {
		log.Printf("扫描中断/失败: %v（进度已保存，重跑续扫）", err)
	}
	fmt.Printf("扫描完成 job=%s total=%d inserted=%d duplicate=%d failed=%d state=%s\n",
		stats.JobID, stats.Total, stats.Inserted, stats.Duplicate, stats.Failed, st.Path())
}

// runWatch 监听模式：先补一轮增量扫（覆盖停机期间变更），再 fsnotify 持续监听。
func runWatch(ctx context.Context, indexer *index.Indexer, root string, st *watch.State, debounce time.Duration) {
	// 启动补扫：捕获监听空窗期（进程停止期间）的新增/变更文件
	if entries, err := index.ScanDir(ctx, root); err == nil {
		if pending := st.Pending(entries); len(pending) > 0 {
			log.Printf("启动补扫 %d 个文件…", len(pending))
			if stats, err := importBatch(ctx, indexer, "incremental", pending, st); err != nil {
				log.Printf("补扫中断: %v", err)
			} else {
				log.Printf("补扫完成 inserted=%d duplicate=%d failed=%d", stats.Inserted, stats.Duplicate, stats.Failed)
			}
		}
	} else {
		log.Printf("启动补扫失败（继续监听）: %v", err)
	}

	onBatch := func(paths []string) {
		entries := watch.EntriesFromPaths(root, paths)
		if len(entries) == 0 {
			return
		}
		pending := st.Pending(entries)
		if len(pending) == 0 {
			return // 去重：指纹未变（如同内容覆写触发的事件）
		}
		stats, err := importBatch(ctx, indexer, "incremental", pending, st)
		if err != nil {
			log.Printf("增量导入失败: %v", err)
			return
		}
		log.Printf("增量导入 job=%s inserted=%d duplicate=%d failed=%d",
			stats.JobID, stats.Inserted, stats.Duplicate, stats.Failed)
	}

	w, err := watch.NewWatcher(root, debounce, onBatch)
	if err != nil {
		log.Fatalf("启动监听: %v", err)
	}
	defer w.Close()
	log.Printf("监听 %s（去抖 %s，状态 %s，已完成 %d）…", root, debounce, st.Path(), st.Done())
	if err := w.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("监听错误: %v", err)
	}
	fmt.Println("监听已停止")
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
