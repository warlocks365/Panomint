// embedgen CLIP 向量工具（Job000010）：为媒体生成 embedding / 语义检索自检 / 设备探针。
//
// 用法：
//
//	embedgen -mode probe                         # 设备探针（报告实际生效的 CPU/CUDA 执行提供器）
//	embedgen -mode status                        # 查看已向量化进度
//	embedgen -mode encode [-limit N] [-force]    # 批量生成（缩略图 → CLIP 图像塔 → media.embedding）
//	embedgen -mode query -text "sunset" [-k 10]  # 语义检索自检（文本塔 → pgvector 余弦检索）
//	embedgen -mode selftest                      # 编码器自检（无需 DB：文本/图像各编码一次）
//
// 设备选择（GPU / CPU 双接口，同一二进制由配置切换）：
//
//	-device cpu|cuda|auto   或 EMBED_DEVICE（默认 auto：优先 CUDA，不可用回落 CPU）
//	-deviceID N             或 EMBED_DEVICE_ID（CUDA 设备序号，默认 0）
//	-threads N              或 EMBED_THREADS（CPU 线程数，0=ORT 默认）
//	-gpuMemMB N             或 EMBED_GPU_MEM_MB（CUDA 显存上限，0=不限）
//
// 路径：
//
//	模型目录  -modeldir  或 EMBED_MODEL_DIR（默认 assets/models/clip）
//	原生库    -lib       或 EMBED_LIB（默认按平台名走系统搜索；CUDA 需指向 GPU 版库）
//	缩略图    -thumbdir  或 THUMB_DIR（默认 ./data/thumbnails）
//
// 说明：推理在**本地** CPU / GPU 完成，不依赖任何远程 GPU 节点。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/config"
	"panoalbum/internal/embed"
)

type options struct {
	mode     string
	limit    int
	force    bool
	text     string
	k        int
	modelDir string
	lib      string
	thumbDir string
	device   string
	family   string
	deviceID int
	threads  int
	gpuMemMB int
}

func main() {
	var o options
	flag.StringVar(&o.mode, "mode", "status", "probe|status|encode|query|selftest")
	flag.IntVar(&o.limit, "limit", 500, "encode 模式最多处理条数")
	flag.BoolVar(&o.force, "force", false, "encode 模式重算已有向量")
	flag.StringVar(&o.text, "text", "", "query 模式的查询文本")
	flag.IntVar(&o.k, "k", 10, "query 模式返回条数")
	flag.StringVar(&o.modelDir, "modeldir", "", "CLIP 模型目录")
	flag.StringVar(&o.lib, "lib", "", "onnxruntime 原生库路径")
	flag.StringVar(&o.thumbDir, "thumbdir", "", "缩略图目录")
	flag.StringVar(&o.device, "device", "", "推理设备 cpu|cuda|auto")
	flag.StringVar(&o.family, "family", "", "模型族 clip|chinese-clip")
	flag.IntVar(&o.deviceID, "deviceID", -1, "CUDA 设备序号")
	flag.IntVar(&o.threads, "threads", -1, "CPU 线程数")
	flag.IntVar(&o.gpuMemMB, "gpuMemMB", -1, "CUDA 显存上限 MB")
	flag.Parse()

	cfg, thumbDir := buildConfig(o)

	if o.mode == "selftest" || o.mode == "probe" {
		if err := selfTest(cfg, o.mode == "probe"); err != nil {
			log.Fatalf("失败: %v", err)
		}
		return
	}

	appCfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, appCfg.PGDSN)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	st := &embed.Store{Pool: pool}

	switch o.mode {
	case "status":
		done, total, err := st.CountEmbedded(ctx)
		if err != nil {
			log.Fatalf("统计失败: %v", err)
		}
		fmt.Printf("已向量化 %d / %d\n", done, total)

	case "encode":
		runEncode(ctx, st, cfg, thumbDir, o.limit, o.force)

	case "query":
		if o.text == "" {
			log.Fatal("query 模式需要 -text")
		}
		runQuery(ctx, st, cfg, o.text, o.k)

	default:
		log.Fatalf("未知 mode: %s", o.mode)
	}
}

// buildConfig 合并 flag / 环境变量 / 默认值，返回编码器配置与缩略图目录。
func buildConfig(o options) (embed.Config, string) {
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
	td := o.thumbDir
	if td == "" {
		td = os.Getenv("THUMB_DIR")
	}
	if td == "" {
		td = filepath.Join(".", "data", "thumbnails")
	}
	return cfg, td
}

func newEncoder(cfg embed.Config) (*embed.Encoder, error) {
	log.Printf("加载模型：family=%s dir=%s lib=%s 请求设备=%s", cfg.Family, cfg.ModelDir, cfg.LibPath, cfg.Device)
	enc, err := embed.NewEncoder(cfg)
	if err != nil {
		return nil, err
	}
	log.Printf("执行提供器：%s（设备=%s，模型族=%s，文本长度=%d）", enc.Provider(), enc.Device(), enc.Family(), enc.ContextLen())
	return enc, nil
}

func runEncode(ctx context.Context, st *embed.Store, cfg embed.Config, thumbDir string, limit int, force bool) {
	list, err := st.ListPending(ctx, force, limit)
	if err != nil {
		log.Fatalf("查询待编码媒体失败: %v", err)
	}
	if len(list) == 0 {
		fmt.Println("没有待编码媒体")
		return
	}
	fmt.Printf("待编码 %d 条，缩略图目录 %s\n", len(list), thumbDir)

	enc, err := newEncoder(cfg)
	if err != nil {
		log.Fatalf("加载编码器失败: %v", err)
	}
	defer enc.Close()

	var ok, skip, fail int
	start := time.Now()
	for i, m := range list {
		if m.ThumbMD == "" {
			skip++
			continue
		}
		path := filepath.Join(thumbDir, filepath.Base(m.ThumbMD))
		vec, err := enc.EncodeImage(ctx, path)
		if err != nil {
			fail++
			log.Printf("[%d/%d] %s 编码失败: %v", i+1, len(list), m.Filename, err)
			continue
		}
		if err := st.SaveEmbedding(ctx, m.ID, vec); err != nil {
			fail++
			log.Printf("[%d/%d] %s 写库失败: %v", i+1, len(list), m.Filename, err)
			continue
		}
		ok++
		if (i+1)%20 == 0 || i+1 == len(list) {
			log.Printf("[%d/%d] 已处理（成功 %d 跳过 %d 失败 %d）", i+1, len(list), ok, skip, fail)
		}
	}
	rate := float64(ok) / time.Since(start).Seconds()
	fmt.Printf("完成：成功 %d，跳过 %d（无缩略图），失败 %d，用时 %s（%.1f 张/秒）· 设备 %s\n",
		ok, skip, fail, time.Since(start).Round(time.Millisecond), rate, enc.Provider())
}

func runQuery(ctx context.Context, st *embed.Store, cfg embed.Config, text string, k int) {
	enc, err := newEncoder(cfg)
	if err != nil {
		log.Fatalf("加载编码器失败: %v", err)
	}
	defer enc.Close()

	start := time.Now()
	vec, err := enc.EncodeText(ctx, text)
	if err != nil {
		log.Fatalf("文本编码失败: %v", err)
	}
	encMS := time.Since(start).Milliseconds()

	hits, err := st.SearchByVector(ctx, vec, k)
	if err != nil {
		log.Fatalf("检索失败: %v", err)
	}
	fmt.Printf("查询 %q（编码 %dms，%s，命中 %d 条）：\n", text, encMS, enc.Provider(), len(hits))
	for i, h := range hits {
		tag := h.Type
		if h.Is360 {
			tag = "360"
		}
		fmt.Printf("  %2d. dist=%.4f  [%s] %s  %s\n", i+1, h.Distance, tag, h.Filename, h.Place)
	}
}

// selfTest 编码器自检；probe 模式额外报告设备装配情况（用于 GPU 功能验证）。
func selfTest(cfg embed.Config, probe bool) error {
	ctx := context.Background()
	log.Printf("请求配置：dir=%s lib=%s device=%s deviceID=%d",
		cfg.ModelDir, cfg.LibPath, cfg.Device, cfg.DeviceID)

	enc, err := embed.NewEncoder(cfg)
	if err != nil {
		if probe {
			fmt.Printf("PROBE=FAIL err=%v\n", err)
		}
		return err
	}
	defer enc.Close()

	fmt.Printf("PROBE=OK provider=%s device=%s family=%s ctx=%d lib=%s\n", enc.Provider(), enc.Device(), enc.Family(), enc.ContextLen(), enc.LibPath())
	if !probe {
		for _, s := range []string{"a photo of a sunset over the sea", "a photo of a red car"} {
			v, err := enc.EncodeText(ctx, s)
			if err != nil {
				return fmt.Errorf("编码 %q 失败: %w", s, err)
			}
			fmt.Printf("text %-40q dim=%d 前3维=%.4f,%.4f,%.4f\n", s, len(v), v[0], v[1], v[2])
		}
	}
	return nil
}
