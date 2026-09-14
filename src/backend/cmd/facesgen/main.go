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
// 检测输入边长（默认**按源图长边自适应**，不再固定 640）：
//
//	-inputsize N 或 FACE_INPUT_SIZE   显式指定边长（**优先于一切推导**；静态图只能用模型声明值）
//	FACE_INPUT_MAX=N                  自适应上限（默认 1280；设 640 完全退化回旧的固定 640 行为）
//
// 自适应规则：size = clamp(roundUp32(源图长边), 640, FACE_INPUT_MAX)——LG 缩略图宽
// 1280，于是合影直接以 1280 画布 1:1 送网（640 时代等于白丢一半线性分辨率）。
// ⚠️ 仅当模型输入为**动态**时生效；现役 face_detection_yunet_2023mar.onnx 是静态
// [1,3,640,640]，只能取 640。详见 internal/faces/face.go 的 DefaultInputMax。
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
	flag.BoolVar(&o.force, "force", false, "scan 模式忽略扫描标记、重算全部媒体（单张媒体是否重扫不影响幂等：无 force 也是整体替换）")
	flag.StringVar(&o.modelDir, "modeldir", "", "人脸模型目录（YuNet + SFace）")
	flag.StringVar(&o.lib, "lib", "", "onnxruntime 原生库路径")
	flag.StringVar(&o.thumbDir, "thumbdir", "", "缩略图目录")
	flag.StringVar(&o.device, "device", "", "推理设备 cpu|cuda|auto")
	flag.IntVar(&o.deviceID, "deviceID", -1, "CUDA 设备序号")
	flag.IntVar(&o.threads, "threads", -1, "CPU 线程数")
	flag.IntVar(&o.gpuMemMB, "gpuMemMB", -1, "CUDA 显存上限 MB")
	flag.IntVar(&o.inputSize, "inputsize", 0, "检测输入边长；缺省=按源图长边自适应（clamp(roundUp32(源长边),640,FACE_INPUT_MAX=1280)，仅动态输入模型生效）。显式值优先，且静态输入模型只接受其声明值")
	flag.Float64Var(&o.conf, "faceconf", 0, "检测置信度阈值（默认 0.9）")
	flag.Float64Var(&o.nms, "facenms", 0, "检测 NMS IoU 阈值（默认 0.3）")
	flag.IntVar(&o.minPx, "minpx", 0, "最小人脸边长像素（默认 24）")
	flag.Float64Var(&o.merge, "merge", 0, "聚类合并余弦阈值（默认 0.40）")
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
		n, nc, err := scanOne(ctx, opts, det, rec, thumbDir, m)
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
// **幂等（本函数的核心契约）**：写入前**无条件**删除该媒体已入库的 faces 行，
// 即「某媒体的人脸 = 该媒体最新一次检出的完整替换」，而不是追加。
//
// 为什么必须无条件删：POST /ai/faces 只是把 media.faces_scanned_at 复位为 NULL
// （见 faces.Store.ResetScanned），让该媒体重新进入待扫队列；若此时不删旧行，
// 同一张脸会被再插一次 —— 实测一次 scope=all 就让人脸数从 56 涨到 67。
// 也就是说「复位标记」与「重扫幂等」必须成对存在，缺一就会重复累积。
// （曾有的写法是只在 -force 下删，导致 -scan/-watch 走的非 force 分支会堆积。）
//
// **用户命名不丢**：删除前先读出旧脸，用框重叠（IoU ≥ faces.MatchMinIoU）把
// faces.person_id / is_pet 迁移到新检出上——重扫只刷新几何与特征，不牺牲命名劳动。
// 已命名的人脸**连同其簇 ID 一起保留**：用户既然认可了这条聚类，重扫就不该把它拆散
// （增量聚类对扫描上下文敏感，实测同一批 embedding 在不同上下文下簇划分可差 ±2~3；
// 若连簇一起重算，已命名的人会被拆到不同簇，甚至混进未命名簇被后续命名覆盖）。
// 未命名人脸一律重算簇——这正是「重扫=按最新模型/参数重新聚类」的意义所在。
//
// **不拿失败换数据**：特征提取在删除旧行**之前**全部算完；若检出到人脸却一张特征都
// 没算出来（ORT 会话级失败），直接报错返回、保留旧数据且不回填标记，留待下轮重试。
//
// 返回 (人脸数, 新建聚类数, error)。**只有成功走完全流程才回填 faces_scanned_at**——
// 失败项留待下一轮清扫自动重试（重试时会再次整体替换，故中途失败也不会产生半份残留）。
func scanOne(ctx context.Context, opts faces.Options, det *faces.Detector, rec *faces.Recognizer,
	thumbDir string, m faces.MediaItem) (int, int, error) {

	path := filepath.Join(thumbDir, filepath.Base(m.ThumbLG))
	img, err := faces.DecodeImage(path)
	if err != nil {
		return 0, 0, err
	}
	dets, err := det.Detect(img)
	if err != nil {
		return 0, 0, err
	}

	// 旧脸必须在删除前读出：删掉之后就再也找不回用户的命名关联了。
	olds, err := opts.Store.FacesByMedia(ctx, m.ID)
	if err != nil {
		return 0, 0, fmt.Errorf("读取旧人脸失败: %w", err)
	}

	// 先把全部特征算完再动数据库。EmbedFace 的失败是**会话级**的（ORT 推理出错时
	// 本媒体每张脸都会失败），若先删后算，一次整体失败就会把该媒体已入库的人脸
	// 连同用户命名一起清空，还会因回填标记而永不重试。
	embs := make([][]float32, len(dets))
	embedded := 0
	for i, d := range dets {
		emb, err := rec.EmbedFace(img, d.Landmarks)
		if err != nil {
			log.Printf("  %s 人脸特征失败（跳过该脸）: %v", m.Filename, err)
			continue
		}
		embs[i] = emb
		embedded++
	}
	// 检出有脸却一张特征都没算出来 = 本轮整体失败：保留旧数据、不回填扫描标记，
	// 让下一轮清扫重试。（注意区分「本就不该有脸」：dets 为空时正常清空并回填。）
	if len(dets) > 0 && embedded == 0 {
		return 0, 0, fmt.Errorf("%d 张检出人脸全部特征提取失败，保留旧数据待下轮重试", len(dets))
	}

	if err := opts.Store.DeleteFacesByMedia(ctx, m.ID); err != nil {
		return 0, 0, fmt.Errorf("清理旧人脸失败: %w", err)
	}

	newClusters := 0
	saved := 0
	for i, d := range dets {
		emb := embs[i]
		if emb == nil {
			continue // 本张脸特征提取失败：连同行一起丢弃（旧行已在上一步删除）
		}
		// 命名迁移：同一张脸重扫前后框几乎重合，取重叠度最高的旧脸即可；
		// 旧脸未命中或旧脸本身未命名时 personID 为空串，等价于不迁移。
		old, matched := faces.BestFaceMatch(olds, d)
		personID, isPet := "", false
		if matched {
			personID, isPet = old.PersonID, old.IsPet
		}

		clusterID := ""
		if personID != "" && old.ClusterID != "" {
			clusterID = old.ClusterID // 已命名：簇与命名一起保留，见函数注释
		} else {
			// 与已有簇质心比较：命中则归入，否则新建簇（增量聚类）。
			// NearestClusters 会排除「本媒体已有脸所属的簇」，保证同媒体内最多一张脸进同一簇。
			refs, err := opts.Store.NearestClusters(ctx, m.ID, emb, 5)
			if err != nil {
				return saved, newClusters, fmt.Errorf("查询相似簇失败: %w", err)
			}
			var sim float64
			clusterID, sim = faces.PickCluster(emb, refs, opts.MergeSim)
			if clusterID == "" {
				clusterID = faces.NewClusterID()
				newClusters++
				// 打印最近相似度，便于按自有语料标定 FACE_MERGE_SIM
				log.Printf("  %s 新建聚类 %s（最近簇相似度 %.3f）", m.Filename, clusterID, sim)
			}
		}
		if err := opts.Store.SaveFace(ctx, m.ID, d, emb, clusterID, personID, isPet); err != nil {
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
		n, _, err := scanOne(ctx, opts, det, rec, thumbDir, m)
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
