// phashgen 感知哈希流水线工具（TDD §381）：MD 缩略图 → DCT pHash → 回填 media.phash。
//
// 用法：
//
//	phashgen -mode status                     # 查看进度（已哈希 / 待扫描 / 失败）
//	phashgen -mode scan [-limit N] [-force]   # 批量计算一轮
//	phashgen -mode watch [-interval 30]       # 常驻增量：周期性扫描未处理媒体（默认 30s）
//	phashgen -mode probe [-limit N] [-force]  # 只读自检：逐条打印路径、指纹、耗时（不写库）
//
// 缩略图目录：-thumbdir 或 THUMB_DIR（默认 ./data/thumbnails）。
//
// 增量为何用「清扫式」而非队列：与 embedgen / facesgen 同理——队列消费方对未知 kind
// 会直接 Ack 吞掉任务，而清扫天然幂等、能自动重试历史失败项、且不触碰既有队列语义。
//
// **无论成功失败都会回填 media.phash_scanned_at**（见 scanOne 注释）：缩略图缺失/解码失败
// 属于确定性失败，重试一万次结果一样，不回填就等于让坏行永久占着清扫队列。
//
// 为什么取 MD（宽 640）而不是 LG（宽 1280）：pHash 最终只把图缩到 32×32，
// 640 已远超所需；MD 由 index worker 更早产出，命中更快、解码更省。
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
	"panoalbum/internal/phash"
)

type options struct {
	mode     string
	limit    int
	force    bool
	thumbDir string
	interval int
}

func main() {
	var o options
	flag.StringVar(&o.mode, "mode", "status", "status|scan|watch|probe")
	flag.IntVar(&o.limit, "limit", 200, "scan/probe 模式最多处理条数")
	flag.BoolVar(&o.force, "force", false, "忽略扫描标记，重算全部媒体")
	flag.StringVar(&o.thumbDir, "thumbdir", "", "缩略图目录")
	flag.IntVar(&o.interval, "interval", 30, "watch 模式扫描间隔（秒）")
	flag.Parse()

	thumbDir := resolveThumbDir(o.thumbDir)

	appCfg := config.Load()
	// watch 为常驻服务：用信号驱动退出；其余模式限时（与 facesgen 一致）
	var ctx context.Context
	var cancel context.CancelFunc
	if o.mode == "watch" {
		ctx, cancel = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Minute)
	}
	defer cancel()

	pool, err := pgxpool.New(ctx, appCfg.PGDSN)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	st := &phash.Store{Pool: pool}

	switch o.mode {
	case "status":
		total, hashed, pending, failed, err := st.Stats(ctx)
		if err != nil {
			log.Fatalf("统计失败: %v", err)
		}
		fmt.Printf("媒体 %d：已哈希 %d，待扫描 %d，失败 %d\n", total, hashed, pending, failed)

	case "scan":
		runScan(ctx, st, thumbDir, o.limit, o.force)

	case "watch":
		runWatch(ctx, st, thumbDir, time.Duration(o.interval)*time.Second)

	case "probe":
		if err := probe(ctx, st, thumbDir, o.limit, o.force); err != nil {
			log.Fatalf("自检失败: %v", err)
		}

	default:
		log.Fatalf("未知 mode: %s", o.mode)
	}
}

// resolveThumbDir 合并 flag / 环境变量 / 默认值。
func resolveThumbDir(flagVal string) string {
	td := flagVal
	if td == "" {
		td = os.Getenv("THUMB_DIR")
	}
	if td == "" {
		td = filepath.Join(".", "data", "thumbnails")
	}
	return td
}

// runScan 批量扫描一轮（顺序执行：解码 + DCT 是纯 CPU 轻活，无需并发或批处理）。
func runScan(ctx context.Context, st *phash.Store, thumbDir string, limit int, force bool) {
	list, err := st.ListPending(ctx, force, limit)
	if err != nil {
		log.Fatalf("查询待扫描媒体失败: %v", err)
	}
	if len(list) == 0 {
		fmt.Println("没有待扫描媒体")
		return
	}
	fmt.Printf("待扫描 %d 条，缩略图目录 %s\n", len(list), thumbDir)

	ok, fail := 0, 0
	start := time.Now()
	for i, c := range list {
		if err := scanOne(ctx, st, thumbDir, c); err != nil {
			fail++
			// 单条失败只记日志并继续：一张坏缩略图绝不能中断整轮清扫
			log.Printf("[%d/%d] %s 计算失败: %v", i+1, len(list), c.Filename, err)
			continue
		}
		ok++
		if (i+1)%20 == 0 || i+1 == len(list) {
			log.Printf("[%d/%d] 已处理（成功 %d 失败 %d）", i+1, len(list), ok, fail)
		}
	}
	fmt.Printf("完成：成功 %d，失败 %d，用时 %s（%.2f 张/秒）\n",
		ok, fail, time.Since(start).Round(time.Millisecond), float64(ok)/time.Since(start).Seconds())
}

// scanOne 计算单条媒体的感知哈希并入库。
//
// **失败也回填扫描标记**——这是与 facesgen 相反的取舍，理由要写清楚：
// facesgen 的失败来自 ONNX 会话级抖动（随机、下次可能成功），故不回填、留待重试；
// 而 pHash 的失败只有一种——缩略图打不开（文件被截断、格式不支持、磁盘掉了）。
// 这是**确定性**失败，重试一万次结果相同，不回填就等于让这条坏行永久占据清扫队列。
// 代价是修好坏缩略图后需 -force 才会重算，而这正是 -force 存在的意义。
func scanOne(ctx context.Context, st *phash.Store, thumbDir string, c phash.Candidate) error {
	path := filepath.Join(thumbDir, filepath.Base(c.ThumbMD))
	h, err := phash.HashFile(path)
	if err != nil {
		// 回填失败不覆盖原始错误：解码失败才是诊断价值更高的那条。
		if merr := st.MarkScanned(ctx, c.ID); merr != nil {
			log.Printf("  回填扫描标记失败 id=%s: %v", c.ID, merr)
		}
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := st.Save(ctx, c.ID, h); err != nil {
		return fmt.Errorf("写入指纹失败: %w", err)
	}
	return nil
}

// runWatch 周期清扫未处理媒体（常驻增量）。
func runWatch(ctx context.Context, st *phash.Store, thumbDir string, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	log.Printf("感知哈希增量扫描已启动：每 %s 扫描一次", interval)
	sweepOnce(ctx, st, thumbDir)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("收到退出信号，感知哈希增量扫描停止")
			return
		case <-t.C:
			sweepOnce(ctx, st, thumbDir)
		}
	}
}

// sweepOnce 扫描一轮；无待办时静默（避免刷日志）。
func sweepOnce(ctx context.Context, st *phash.Store, thumbDir string) {
	list, err := st.ListPending(ctx, false, 200)
	if err != nil {
		log.Printf("扫描待处理媒体失败: %v", err)
		return
	}
	if len(list) == 0 {
		return
	}
	log.Printf("发现 %d 条待扫描媒体", len(list))
	ok, fail := 0, 0
	for i, c := range list {
		if err := scanOne(ctx, st, thumbDir, c); err != nil {
			fail++
			log.Printf("[%d/%d] %s 计算失败: %v", i+1, len(list), c.Filename, err)
			continue
		}
		ok++
	}
	log.Printf("本轮完成：成功 %d 失败 %d", ok, fail)
}

// probe 只读自检：逐条打印路径、指纹（十六进制）与耗时，**绝不写数据库**。
//
// 与 scan 分开的意义：拿真实缩略图先验算法本身（换缩放器 / 换阈值 / 与上一版指纹对比时），
// 不该顺手污染 media.phash。要落库就显式跑 -mode scan。
func probe(ctx context.Context, st *phash.Store, thumbDir string, limit int, force bool) error {
	list, err := st.ListPending(ctx, force, limit)
	if err != nil {
		return fmt.Errorf("查询候选媒体失败: %w", err)
	}
	fmt.Printf("PROBE 候选 %d 条，缩略图目录 %s\n", len(list), thumbDir)
	for i, c := range list {
		path := filepath.Join(thumbDir, filepath.Base(c.ThumbMD))
		start := time.Now()
		h, err := phash.HashFile(path)
		elapsed := time.Since(start).Round(time.Microsecond)
		if err != nil {
			fmt.Printf("[%d/%d] %s elapsed=%s 失败: %v\n", i+1, len(list), path, elapsed, err)
			continue
		}
		fmt.Printf("[%d/%d] %s hash=%016x elapsed=%s\n", i+1, len(list), path, h, elapsed)
	}
	return nil
}
