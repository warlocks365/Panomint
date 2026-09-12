// facesgen 人脸流水线工具（Job000011）：检测 → 对齐 → 特征 → 增量聚类入库。
//
// 用法：
//
//	facesgen -mode probe              # 自检：加载 YuNet + SFace，报告执行提供器与输入尺寸
//	facesgen -mode status             # 查看扫描进度（已扫描媒体 / 人脸数 / 聚类数）
//	facesgen -mode scan [-limit N] [-force]  # 批量扫描（LG 缩略图 → 人脸 → 聚类 → faces 表）
//	facesgen -mode watch [-interval 30]      # 常驻增量：周期性扫描未处理媒体（默认 30s）
//
// 增量为何用「清扫式」而非队列：与 embedgen 同理——队列消费方对未知 kind 会直接 Ack 吞掉任务，
// 而清扫天然幂等、能自动重试历史失败项、且不触碰既有队列语义。
// 扫描完成后**无论是否检出人脸都会回填 media.faces_scanned_at**，避免 0 人脸媒体被反复重扫。
//
// 设备选择（GPU / CPU 双接口，同一二进制由配置切换）：
//
//	-device cpu|cuda|auto   或 FACE_DEVICE（默认 auto：优先 CUDA，不可用回落 CPU）
//	-deviceID N / -threads N / -gpuMemMB N   或 FACE_DEVICE_ID / FACE_THREADS / FACE_GPU_MEM_MB
//
// 路径与阈值：
//
//	模型目录  -modeldir  或 FACE_MODEL_DIR（默认 assets/models/faces）
//	原生库    -lib       或 FACE_LIB / EMBED_LIB（默认按平台名走系统搜索；CUDA 需指向 GPU 版库）
//	缩略图    -thumbdir  或 THUMB_DIR（默认 ./data/thumbnails）
//	-faceconf/-facenms/-minpx/-merge  或 FACE_CONF / FACE_NMS / FACE_MIN_PX / FACE_MERGE_SIM
//
// 说明：推理在**本地** CPU / GPU 完成，不依赖任何远程 GPU 节点。
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
	"panoalbum/internal/faces"
	"panoalbum/internal/ortx"
)

type options struct {
	mode      string
	limit     int
	force     bool
	modelDir  string
	lib       string
	thumbDir  string
	device    string
	deviceID  int
	threads   int
	gpuMemMB  int
	inputSize int
	conf      float64
	nms       float64
	minPx     int
	merge     float64
	interval  int
}

func main() {
	var o options
	flag.StringVar(&o.mode, "mode", "status", "probe|status|scan|watch")
	flag.IntVar(&o.limit, "limit", 200, "scan 模式最多处理条数")
	flag.BoolVar(&o.force, "force", false, "scan 模式重算已扫描过的媒体")
	flag.StringVar(&o.modelDir, "modeldir", "", "人脸模型目录（YuNet + SFace）")
	flag.StringVar(&o.lib, "lib", "", "onnxruntime 原生库路径")
	flag.StringVar(&o.thumbDir, "thumbdir", "", "缩略图目录")
	flag.StringVar(&o.device, "device", "", "推理设备 cpu|cuda|auto")
	flag.IntVar(&o.deviceID, "deviceID", -1, "CUDA 设备序号")
	flag.IntVar(&o.threads, "threads", -1, "CPU 线程数")
	flag.IntVar(&o.gpuMemMB, "gpuMemMB", -1, "CUDA 显存上限 MB")
	flag.IntVar(&o.inputSize, "inputsize", 0, "检测输入边长（0=用模型声明值）")
	flag.Float64Var(&o.conf, "faceconf", 0, "检测置信度阈值（默认 0.9）")
	flag.Float64Var(&o.nms, "facenms", 0, "检测 NMS IoU 阈值（默认 0.3）")
	flag.IntVar(&o.minPx, "minpx", 0, "最小人脸边长像素（默认 24）")
	flag.Float64Var(&o.merge, "merge", 0, "聚类合并余弦阈值（默认 0.33）")
	flag.IntVar(&o.interval, "interval", 30, "watch 模式扫描间隔（秒）")
	flag.Parse()

	opts, thumbDir := buildOptions(o)

	if o.mode == "probe" {
		if err := probe(opts); err != nil {
			log.Fatalf("自检失败: %v", err)
		}
		return
	}

	appCfg := config.Load()
	// watch 为常驻服务：用信号驱动退出；其余模式限时
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

	st := &faces.Store{Pool: pool}
	opts.Store = st

	switch o.mode {
	case "status":
		scanned, total, faceCount, clusters, err := st.CountStats(ctx)
		if err != nil {
			log.Fatalf("统计失败: %v", err)
		}
		fmt.Printf("已扫描媒体 %d / %d，人脸 %d 张，聚类 %d 个\n", scanned, total, faceCount, clusters)

	case "scan":
		runScan(ctx, opts, thumbDir, o.limit, o.force)

	case "watch":
		runWatch(ctx, opts, thumbDir, time.Duration(o.interval)*time.Second)

	default:
		log.Fatalf("未知 mode: %s", o.mode)
	}
}

// buildOptions 合并 flag / 环境变量 / 默认值，返回人脸配置与缩略图目录。
func buildOptions(o options) (faces.Options, string) {
	opts := faces.OptionsFromEnv()
	if o.modelDir != "" {
		opts.ModelDir = o.modelDir
	}
	if o.lib != "" {
		opts.LibPath = o.lib
	}
	if o.device != "" {
		opts.Device = ortx.ParseDevice(o.device)
	}
	if o.deviceID >= 0 {
		opts.DeviceID = o.deviceID
	}
	if o.threads >= 0 {
		opts.IntraThreads = o.threads
	}
	if o.gpuMemMB >= 0 {
		opts.GpuMemLimitMB = o.gpuMemMB
	}
	if o.inputSize > 0 {
		opts.InputSize = o.inputSize
	}
	if o.conf > 0 {
		opts.ConfThreshold = o.conf
	}
	if o.nms > 0 {
		opts.NMSThreshold = o.nms
	}
	if o.minPx > 0 {
		opts.MinFacePx = o.minPx
	}
	if o.merge > 0 {
		opts.MergeSim = o.merge
	}
	opts = opts.WithDefaults()

	td := o.thumbDir
	if td == "" {
		td = os.Getenv("THUMB_DIR")
	}
	if td == "" {
		td = filepath.Join(".", "data", "thumbnails")
	}
	return opts, td
}

func newEngines(opts faces.Options) (*faces.Detector, *faces.Recognizer, error) {
	log.Printf("加载人脸模型：dir=%s lib=%s 请求设备=%s 阈值 conf=%.2f nms=%.2f minpx=%d merge=%.3f",
		opts.ModelDir, opts.LibPath, opts.Device, opts.ConfThreshold, opts.NMSThreshold, opts.MinFacePx, opts.MergeSim)
	det, err := faces.NewDetector(opts)
	if err != nil {
		return nil, nil, err
	}
	rec, err := faces.NewRecognizer(opts)
	if err != nil {
		det.Close()
		return nil, nil, err
	}
	log.Printf("执行提供器：检测=%s 识别=%s（设备=%s，输入=%d，维度=%d）",
		det.Provider(), rec.Provider(), det.Device(), det.Size(), faces.EmbeddingDim)
	return det, rec, nil
}

// runScan 批量扫描一轮。
func runScan(ctx context.Context, opts faces.Options, thumbDir string, limit int, force bool) {
	list, err := opts.Store.ListPending(ctx, force, limit)
	if err != nil {
		log.Fatalf("查询待扫描媒体失败: %v", err)
	}
	if len(list) == 0 {
		fmt.Println("没有待扫描媒体")
		return
	}
	fmt.Printf("待扫描 %d 条，缩略图目录 %s\n", len(list), thumbDir)

	det, rec, err := newEngines(opts)
	if err != nil {
		log.Fatalf("加载人脸模型失败: %v", err)
	}
	defer det.Close()
	defer rec.Close()

	ok, fail, faceTotal, newClusters := 0, 0, 0, 0
	start := time.Now()
	for i, m := range list {
		n, nc, err := scanOne(ctx, opts, det, rec, thumbDir, m, force)
		if err != nil {
			fail++
			log.Printf("[%d/%d] %s 扫描失败: %v", i+1, len(list), m.Filename, err)
			continue
		}
		ok++
		faceTotal += n
		newClusters += nc
		if (i+1)%20 == 0 || i+1 == len(list) {
			log.Printf("[%d/%d] 已处理（成功 %d 失败 %d 人脸 %d）", i+1, len(list), ok, fail, faceTotal)
		}
	}
	rate := float64(ok) / time.Since(start).Seconds()
	fmt.Printf("完成：媒体成功 %d，失败 %d，人脸 %d 张（新建聚类 %d），用时 %s（%.2f 张/秒）· 提供器 %s\n",
		ok, fail, faceTotal, newClusters, time.Since(start).Round(time.Millisecond), rate, det.Provider())
}

// scanOne 处理单个媒体：检测 → 逐脸对齐/特征/聚类/入库 → 回填扫描标记。
//
// 返回 (人脸数, 新建聚类数, error)。**只有成功走完全流程才回填 faces_scanned_at**——
// 失败项留待下一轮清扫自动重试。
func scanOne(ctx context.Context, opts faces.Options, det *faces.Detector, rec *faces.Recognizer,
	thumbDir string, m faces.MediaItem, force bool) (int, int, error) {

	path := filepath.Join(thumbDir, filepath.Base(m.ThumbLG))
	img, err := faces.DecodeImage(path)
	if err != nil {
		return 0, 0, err
	}
	dets, err := det.Detect(img)
	if err != nil {
		return 0, 0, err
	}

	// 重算：先清掉该媒体旧人脸，避免残留过期框（非 force 时 faces_scanned_at IS NULL 已保证无旧数据）
	if force {
		if err := opts.Store.DeleteFacesByMedia(ctx, m.ID); err != nil {
			return 0, 0, fmt.Errorf("清理旧人脸失败: %w", err)
		}
	}

	newClusters := 0
	saved := 0
	for _, d := range dets {
		emb, err := rec.EmbedFace(img, d.Landmarks)
		if err != nil {
			log.Printf("  %s 人脸特征失败（跳过该脸）: %v", m.Filename, err)
			continue
		}
		// 与已有簇质心比较：命中则归入，否则新建簇（增量聚类）
		refs, err := opts.Store.NearestClusters(ctx, emb, 5)
		if err != nil {
			return saved, newClusters, fmt.Errorf("查询相似簇失败: %w", err)
		}
		clusterID, sim := faces.PickCluster(emb, refs, opts.MergeSim)
		if clusterID == "" {
			clusterID = faces.NewClusterID()
			newClusters++
			// 打印最近相似度，便于按自有语料标定 FACE_MERGE_SIM
			log.Printf("  %s 新建聚类 %s（最近簇相似度 %.3f）", m.Filename, clusterID, sim)
		}
		if err := opts.Store.SaveFace(ctx, m.ID, d, emb, clusterID); err != nil {
			return saved, newClusters, fmt.Errorf("写入人脸失败: %w", err)
		}
		saved++
	}

	if err := opts.Store.MarkScanned(ctx, m.ID); err != nil {
		return saved, newClusters, fmt.Errorf("回填扫描标记失败: %w", err)
	}
	return saved, newClusters, nil
}

// runWatch 周期清扫未扫描媒体（常驻增量）。
func runWatch(ctx context.Context, opts faces.Options, thumbDir string, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	det, rec, err := newEngines(opts)
	if err != nil {
		// 常驻服务无模型即无意义：直接失败并让编排层重启
		log.Fatalf("加载人脸模型失败（watch 模式需要可用模型）: %v", err)
	}
	defer det.Close()
	defer rec.Close()

	log.Printf("人脸增量扫描已启动：每 %s 扫描一次（提供器=%s，合并阈值=%.3f）", interval, det.Provider(), opts.MergeSim)
	sweepOnce(ctx, opts, det, rec, thumbDir)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("收到退出信号，人脸增量扫描停止")
			return
		case <-t.C:
			sweepOnce(ctx, opts, det, rec, thumbDir)
		}
	}
}

// sweepOnce 扫描一轮；无待办时静默（避免刷日志）。
func sweepOnce(ctx context.Context, opts faces.Options, det *faces.Detector, rec *faces.Recognizer, thumbDir string) {
	list, err := opts.Store.ListPending(ctx, false, 200)
	if err != nil {
		log.Printf("扫描待处理媒体失败: %v", err)
		return
	}
	if len(list) == 0 {
		return
	}
	log.Printf("发现 %d 条待扫描媒体", len(list))
	ok, fail, faceTotal := 0, 0, 0
	for i, m := range list {
		n, _, err := scanOne(ctx, opts, det, rec, thumbDir, m, false)
		if err != nil {
			fail++
			log.Printf("[%d/%d] %s 扫描失败: %v", i+1, len(list), m.Filename, err)
			continue
		}
		ok++
		faceTotal += n
	}
	log.Printf("本轮完成：媒体成功 %d 失败 %d 人脸 %d", ok, fail, faceTotal)
}

// probe 自检：加载两模型并报告执行提供器（用于部署机 / CUDA EP 的功能验证）。
func probe(opts faces.Options) error {
	det, rec, err := newEngines(opts)
	if err != nil {
		fmt.Printf("PROBE=FAIL err=%v\n", err)
		return err
	}
	defer det.Close()
	defer rec.Close()

	fmt.Printf("PROBE=OK detector=%s recognizer=%s device=%s input=%d dim=%d\n",
		det.Provider(), rec.Provider(), det.Device(), det.Size(), faces.EmbeddingDim)
	return nil
}
