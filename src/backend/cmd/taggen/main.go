// taggen 标签工具（Phase 4）：AI 零样本自动打标 + 阈值标定。
//
// 用法：
//
//	taggen -mode status                       # 打标进度（总数/已向量化/已 AI 打标/待打标）
//	taggen -mode scan [-limit N] [-dry]        # 单轮清扫：CLIP 建议 + 启发式兜底
//	taggen -mode watch [-interval 30]          # 常驻增量：周期性补打标（清扫式，不接队列）
//	taggen -mode calibrate                     # 只读：打印相似度分布，用于标定阈值
//	taggen -calibrate                          # 等价 -mode calibrate
//
// 阈值（族感知，可覆盖）：
//
//	-min-sim    或 TAG_MIN_SIM        绝对相似度下限（≤0 按族默认：chinese-clip 0.35 / clip 0.24）
//	-top-ratio  或 TAG_TOP_RATIO      相对 top1 比例（默认 0.88）
//	-max-tags   或 TAG_MAX_PER_MEDIA  每图上限（默认 3）
//
// 写入策略：
//
//	CLIP 建议   → origin='ai'，confirmed=false（**需人工确认**，前端「AI 待确认」区）
//	启发式标签 → origin='heuristic'，confirmed=true（GPS 城市 / 是否 360 / 季节，无需逐张确认）
//	-confirm    → 把 CLIP 建议也直接置为已确认（用于可信度已标定的部署，默认关闭）
//
// 设备选择与 embedgen 完全一致（GPU/CPU 双接口，同一二进制由配置切换），
// 推理一律在**本地**完成，不依赖任何远程 GPU 节点。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/config"
	"panoalbum/internal/embed"
	"panoalbum/internal/tags"
)

type options struct {
	mode      string
	limit     int
	interval  int
	calibrate bool
	confirm   bool
	dry       bool
	media     string

	minSim   float64
	topRatio float64
	maxTags  int

	modelDir string
	lib      string
	device   string
	family   string
	deviceID int
	threads  int
	gpuMemMB int
}

func main() {
	var o options
	flag.StringVar(&o.mode, "mode", "status", "status|scan|watch|calibrate")
	flag.IntVar(&o.limit, "limit", 200, "scan/watch 单轮最多处理条数")
	flag.IntVar(&o.interval, "interval", 30, "watch 扫描间隔（秒）")
	flag.BoolVar(&o.calibrate, "calibrate", false, "等同 -mode calibrate（只读标定）")
	flag.BoolVar(&o.confirm, "confirm", false, "AI 建议直接置为已确认（默认需人工确认）")
	flag.BoolVar(&o.dry, "dry", false, "只计算不写库（调试）")
	flag.StringVar(&o.media, "media", "", "只处理指定 media id（调试）")
	flag.Float64Var(&o.minSim, "min-sim", 0, "绝对相似度下限（0=按族默认）")
	flag.Float64Var(&o.topRatio, "top-ratio", 0, "相对 top1 比例（0=0.88）")
	flag.IntVar(&o.maxTags, "max-tags", 0, "每图标签上限（0=3）")
	flag.StringVar(&o.modelDir, "modeldir", "", "CLIP 模型目录")
	flag.StringVar(&o.lib, "lib", "", "onnxruntime 原生库路径")
	flag.StringVar(&o.device, "device", "", "推理设备 cpu|cuda|auto")
	flag.StringVar(&o.family, "family", "", "模型族 clip|chinese-clip")
	flag.IntVar(&o.deviceID, "deviceID", -1, "CUDA 设备序号")
	flag.IntVar(&o.threads, "threads", -1, "CPU 线程数")
	flag.IntVar(&o.gpuMemMB, "gpuMemMB", -1, "CUDA 显存上限 MB")
	flag.Parse()

	if o.calibrate {
		o.mode = "calibrate"
	}

	ctx, cancel := newContext(o.mode)
	defer cancel()

	appCfg := config.Load()
	pool, err := pgxpool.New(ctx, appCfg.PGDSN)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	st := &tags.Store{Pool: pool}

	if o.mode == "status" {
		printStatus(ctx, st)
		return
	}

	cfg := buildConfig(o)
	enc, err := newEncoder(cfg)
	if err != nil {
		log.Fatalf("加载编码器失败（本模式需要可用模型）: %v", err)
	}
	defer enc.Close()

	clf, err := tags.NewClassifier(ctx, enc, tags.DefaultVocab(), tags.ClassifyConfig{
		MinSim: o.minSim, TopRatio: o.topRatio, MaxTags: o.maxTags,
	})
	if err != nil {
		log.Fatalf("构建分类器失败: %v", err)
	}
	minSim, topR, maxTags, fam := clf.Params()
	log.Printf("分类器就绪：family=%s provider=%s 标签数=%d 阈值 min_sim=%.3f top_ratio=%.2f max_tags=%d",
		fam, enc.Provider(), clf.LabelCount(), minSim, topR, maxTags)

	switch o.mode {
	case "calibrate":
		runCalibrate(ctx, st, clf)
	case "scan":
		runScan(ctx, st, clf, o.limit, o.confirm, o.dry, o.media)
	case "watch":
		runWatch(ctx, st, clf, o.limit, o.confirm, o.dry, time.Duration(o.interval)*time.Second)
	default:
		log.Fatalf("未知 mode: %s", o.mode)
	}
}

func newContext(mode string) (context.Context, context.CancelFunc) {
	if mode == "watch" {
		return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	}
	return context.WithTimeout(context.Background(), 60*time.Minute)
}

func buildConfig(o options) embed.Config {
	cfg := embed.ConfigFromEnv()
	if o.modelDir != "" {
		cfg.ModelDir = o.modelDir
	}
	if o.lib != "" {
		cfg.LibPath = o.lib
	}
	if o.device != "" {
		cfg.Device = embed.ParseDevice(o.device)
	}
	if o.family != "" {
		cfg.Family = embed.ParseFamily(o.family)
	}
	if o.deviceID >= 0 {
		cfg.DeviceID = o.deviceID
	}
	if o.threads >= 0 {
		cfg.IntraThreads = o.threads
	}
	if o.gpuMemMB >= 0 {
		cfg.GpuMemLimitMB = o.gpuMemMB
	}
	return cfg
}

func newEncoder(cfg embed.Config) (*embed.Encoder, error) {
	log.Printf("加载模型：family=%s dir=%s lib=%s 请求设备=%s", cfg.Family, cfg.ModelDir, cfg.LibPath, cfg.Device)
	enc, err := embed.NewEncoder(cfg)
	if err != nil {
		return nil, err
	}
	log.Printf("执行提供器：%s（设备=%s，模型族=%s）", enc.Provider(), enc.Device(), enc.Family())
	return enc, nil
}

func printStatus(ctx context.Context, st *tags.Store) {
	total, embedded, aiTagged, pending, err := st.Counts(ctx)
	if err != nil {
		log.Fatalf("统计失败: %v", err)
	}
	fmt.Printf("媒体总数 %d；已向量化 %d；已 AI 打标 %d；待 AI 打标 %d\n", total, embedded, aiTagged, pending)
}

// runScan 单轮清扫。
func runScan(ctx context.Context, st *tags.Store, clf *tags.Classifier, limit int, confirm, dry bool, mediaID string) {
	app := &tags.Applier{Store: st, Clf: clf, ConfirmAI: confirm}
	list, err := st.ListPendingAI(ctx, limit, mediaID)
	if err != nil {
		log.Fatalf("查询待打标媒体失败: %v", err)
	}
	if len(list) == 0 {
		fmt.Println("没有待打标媒体")
		return
	}
	fmt.Printf("待打标 %d 条（dry=%v confirm=%v）\n", len(list), dry, confirm)

	var ok, fail, tagged int
	start := time.Now()
	for i, m := range list {
		vec, err := st.LoadEmbedding(ctx, m.ID)
		if err != nil {
			fail++
			log.Printf("[%d/%d] %s 读向量失败: %v", i+1, len(list), m.Filename, err)
			continue
		}
		if dry {
			for _, s := range clf.Suggest(vec) {
				fmt.Printf("  · %s → %s(%.3f)\n", m.Filename, s.Tag, s.Confidence)
			}
			tagged += len(clf.Suggest(vec))
			continue
		}
		n, err := app.ApplyOne(ctx, m, vec)
		if err != nil {
			fail++
			log.Printf("[%d/%d] %s 写库失败: %v", i+1, len(list), m.Filename, err)
			continue
		}
		ok++
		tagged += n
		if (i+1)%20 == 0 || i+1 == len(list) {
			log.Printf("[%d/%d] 已处理（成功 %d 失败 %d 标签 %d）", i+1, len(list), ok, fail, tagged)
		}
	}
	fmt.Printf("完成：成功 %d，失败 %d，写入标签 %d，用时 %s\n",
		ok, fail, tagged, time.Since(start).Round(time.Millisecond))
}

// runWatch 常驻增量（清扫式；不触碰队列，避免多 kind 消费方互相 Ack）。
func runWatch(ctx context.Context, st *tags.Store, clf *tags.Classifier, limit int, confirm, dry bool, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	app := &tags.Applier{Store: st, Clf: clf, ConfirmAI: confirm, Logger: log.Default()}
	log.Printf("增量 AI 打标已启动：每 %s 扫描一次", interval)
	sweepOnce(ctx, app, st, limit, dry)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("收到退出信号，增量打标停止")
			return
		case <-t.C:
			sweepOnce(ctx, app, st, limit, dry)
		}
	}
}

// sweepOnce 扫描并补打标一轮；无待办时静默。
func sweepOnce(ctx context.Context, app *tags.Applier, st *tags.Store, limit int, dry bool) {
	list, err := st.ListPendingAI(ctx, limit, "")
	if err != nil {
		log.Printf("扫描待打标媒体失败: %v", err)
		return
	}
	if len(list) == 0 {
		return
	}
	log.Printf("发现 %d 条待打标", len(list))
	ok, fail, tagged := 0, 0, 0
	for i, m := range list {
		vec, err := st.LoadEmbedding(ctx, m.ID)
		if err == nil && !dry {
			var n int
			n, err = app.ApplyOne(ctx, m, vec)
			tagged += n
		}
		if err != nil {
			fail++
			log.Printf("[%d/%d] %s 打标失败: %v", i+1, len(list), m.Filename, err)
			continue
		}
		ok++
	}
	log.Printf("本轮完成：成功 %d 失败 %d 标签 %d", ok, fail, tagged)
}

// runCalibrate 只读打印相似度分布，供标定 -min-sim / -top-ratio / -max-tags。
func runCalibrate(ctx context.Context, st *tags.Store, clf *tags.Classifier) {
	list, err := st.ListEmbedded(ctx, 5000)
	if err != nil {
		log.Fatalf("读取向量失败: %v", err)
	}
	if len(list) == 0 {
		log.Fatal("库内暂无向量，无法标定（先跑 embedgen -mode encode）")
	}
	var top1, all []float64
	for _, m := range list {
		scores := clf.AllScores(m.Vec)
		if len(scores) == 0 {
			continue
		}
		top1 = append(top1, scores[0].Confidence)
		for _, s := range scores {
			all = append(all, s.Confidence)
		}
	}
	sort.Float64s(top1)
	sort.Float64s(all)

	fmt.Printf("样本：%d 条媒体，标签 %d 个（%s 族）\n", len(top1), clf.LabelCount(), clf.Family())
	printQuantiles("top1（每图最高分）", top1)
	printQuantiles("全部标签相似度    ", all)

	// 不同绝对下限下「至少命中 1 个标签」的媒体占比（相对阈值同用默认定值）。
	_, topR, maxTags, _ := clf.Params()
	fmt.Printf("\n阈值扫描（top_ratio=%.2f max_tags=%d）：\n", topR, maxTags)
	for _, floor := range []float64{0.24, 0.28, 0.30, 0.32, 0.34, 0.36, 0.38, 0.40, 0.44} {
		c := clf.WithThresholds(floor, topR, maxTags)
		hitMedia, totalTags := 0, 0
		for _, m := range list {
			n := len(c.Suggest(m.Vec))
			if n > 0 {
				hitMedia++
			}
			totalTags += n
		}
		fmt.Printf("  min_sim=%.2f → 命中媒体 %d/%d (%.0f%%)，平均标签 %.2f 个/图\n",
			floor, hitMedia, len(list), 100*float64(hitMedia)/float64(len(list)),
			float64(totalTags)/float64(len(list)))
	}

	fmt.Println("\n标定建议：取「命中媒体占比」骤降前的拐点作为 min_sim；")
	fmt.Println("chinese-clip 常见 0.34~0.38，clip 常见 0.22~0.26。")
}

func printQuantiles(name string, xs []float64) {
	if len(xs) == 0 {
		return
	}
	q := func(p float64) float64 {
		i := int(p * float64(len(xs)-1))
		return xs[i]
	}
	fmt.Printf("%s：min=%.3f p25=%.3f p50=%.3f p75=%.3f p90=%.3f p95=%.3f max=%.3f\n",
		name, xs[0], q(0.25), q(0.50), q(0.75), q(0.90), q(0.95), xs[len(xs)-1])
}
