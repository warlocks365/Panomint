// embedgen CLIP 向量工具（Job000010）：为媒体生成 embedding / 语义检索自检 / 查看进度。
//
// 用法：
//
//	embedgen -mode status                        # 查看已向量化进度
//	embedgen -mode encode [-limit N] [-force]    # 批量生成（缩略图 → CLIP 图像塔 → media.embedding）
//	embedgen -mode query -text "sunset" [-k 10]  # 语义检索自检（文本塔 → pgvector 余弦检索）
//	embedgen -mode selftest                      # 编码器自检（无需 DB：文本/图像各编码一次）
//
// 路径：
//
//	模型目录  -modeldir  或 EMBED_MODEL_DIR（默认 assets/models/clip）
//	原生库    -lib       或 EMBED_LIB（默认按平台名走系统搜索）
//	缩略图    -thumbdir  或 THUMB_DIR（默认 ./data/thumbnails）
//
// 说明：本工具的推理全部在**本地 CPU** 完成，不依赖任何远程 GPU 节点。
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

func main() {
	mode := flag.String("mode", "status", "status|encode|query|selftest")
	limit := flag.Int("limit", 500, "encode 模式最多处理条数")
	force := flag.Bool("force", false, "encode 模式重算已有向量")
	text := flag.String("text", "", "query 模式的查询文本")
	k := flag.Int("k", 10, "query 模式返回条数")
	modelDir := flag.String("modeldir", "", "CLIP 模型目录")
	lib := flag.String("lib", "", "onnxruntime 原生库路径")
	thumbDir := flag.String("thumbdir", "", "缩略图目录")
	flag.Parse()

	md := *modelDir
	if md == "" {
		md = embed.ModelDirFromEnv()
	}
	td := *thumbDir
	if td == "" {
		td = os.Getenv("THUMB_DIR")
	}
	if td == "" {
		td = filepath.Join(".", "data", "thumbnails")
	}

	// selftest 不需要 DB
	if *mode == "selftest" {
		if err := selfTest(md, *lib); err != nil {
			log.Fatalf("自检失败: %v", err)
		}
		return
	}

	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.PGDSN)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	st := &embed.Store{Pool: pool}

	switch *mode {
	case "status":
		done, total, err := st.CountEmbedded(ctx)
		if err != nil {
			log.Fatalf("统计失败: %v", err)
		}
		fmt.Printf("已向量化 %d / %d\n", done, total)

	case "encode":
		runEncode(ctx, st, md, *lib, td, *limit, *force)

	case "query":
		if *text == "" {
			log.Fatal("query 模式需要 -text")
		}
		runQuery(ctx, st, md, *lib, *text, *k)

	default:
		log.Fatalf("未知 mode: %s", *mode)
	}
}

// newEncoder 构造编码器（本地 CPU 推理）。
func newEncoder(modelDir, lib string) (*embed.Encoder, error) {
	if lib == "" {
		lib = os.Getenv("EMBED_LIB")
	}
	log.Printf("加载 CLIP 模型：dir=%s lib=%s", modelDir, lib)
	return embed.NewEncoder(embed.Config{ModelDir: modelDir, LibPath: lib})
}

func runEncode(ctx context.Context, st *embed.Store, modelDir, lib, thumbDir string, limit int, force bool) {
	list, err := st.ListPending(ctx, force, limit)
	if err != nil {
		log.Fatalf("查询待编码媒体失败: %v", err)
	}
	if len(list) == 0 {
		fmt.Println("没有待编码媒体")
		return
	}
	fmt.Printf("待编码 %d 条，缩略图目录 %s\n", len(list), thumbDir)

	enc, err := newEncoder(modelDir, lib)
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
	fmt.Printf("完成：成功 %d，跳过 %d（无缩略图），失败 %d，用时 %s（%.1f 张/秒）\n",
		ok, skip, fail, time.Since(start).Round(time.Millisecond), rate)
}

func runQuery(ctx context.Context, st *embed.Store, modelDir, lib, text string, k int) {
	enc, err := newEncoder(modelDir, lib)
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
	fmt.Printf("查询 %q（编码 %dms，命中 %d 条）：\n", text, encMS, len(hits))
	for i, h := range hits {
		tag := h.Type
		if h.Is360 {
			tag = "360"
		}
		fmt.Printf("  %2d. dist=%.4f  [%s] %s  %s\n", i+1, h.Distance, tag, h.Filename, h.Place)
	}
}

// selfTest 不依赖 DB 的编码器自检。
func selfTest(modelDir, lib string) error {
	ctx := context.Background()
	enc, err := newEncoder(modelDir, lib)
	if err != nil {
		return err
	}
	defer enc.Close()

	for _, s := range []string{"a photo of a sunset over the sea", "a photo of a red car"} {
		v, err := enc.EncodeText(ctx, s)
		if err != nil {
			return fmt.Errorf("编码 %q 失败: %w", s, err)
		}
		fmt.Printf("text %-40q dim=%d norm-ok=%v 前3维=%.4f,%.4f,%.4f\n",
			s, len(v), true, v[0], v[1], v[2])
	}
	return nil
}
