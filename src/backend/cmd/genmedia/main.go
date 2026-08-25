// genmedia 合成测试媒体生成器（T1.4 集成验证用）。
// 生成 100 张 JPEG（ffmpeg lavfi testsrc2 单帧，不同尺寸，文件名含日期前缀便于验证时间轴）
// 与 5 段不同时长的 mp4，输出到 -out 指定目录（默认 ./testdata/media）。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"panoalbum/internal/ffmpeg"
)

// 照片尺寸档位（循环取用，保证宽高各异）。
var photoSizes = [][2]int{
	{640, 480}, {1280, 720}, {1920, 1080}, {800, 600}, {1024, 768},
}

// 视频时长档位（秒）。
var videoDurations = []int{2, 3, 5, 8, 13}

func run(ctx context.Context, args []string) error {
	_, err := ffmpeg.New(args).Run(ctx)
	return err
}

func main() {
	log.SetFlags(0)
	out := flag.String("out", "./testdata/media", "输出目录")
	photos := flag.Int("photos", 100, "JPEG 数量")
	videos := flag.Int("videos", 5, "MP4 数量")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("建目录失败: %v", err)
	}
	ctx := context.Background()

	// JPEG：testsrc2 单帧；逐张微调尺寸保证内容唯一（同尺寸同参数产物字节相同会被 hash 去重）。
	// 文件名带日期前缀（2024-01-01 起逐日递增）验证时间轴排序。
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < *photos; i++ {
		size := photoSizes[i%len(photoSizes)]
		w := size[0] + 2*(i/len(photoSizes)) // 每轮 +2px（保持偶数，yuv420 友好）
		h := size[1] + 2*(i/len(photoSizes))
		name := fmt.Sprintf("%s_photo_%03d.jpg", base.AddDate(0, 0, i).Format("2006-01-02"), i)
		dst := filepath.Join(*out, name)
		args := []string{
			"-f", "lavfi",
			"-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=1:duration=1", w, h),
			"-frames:v", "1",
			"-q:v", "2",
			"-y", dst,
		}
		if err := run(ctx, args); err != nil {
			log.Fatalf("生成 %s 失败: %v", name, err)
		}
	}
	log.Printf("已生成 %d 张 JPEG", *photos)

	// MP4：testsrc2 + 正弦音轨，不同时长
	for i := 0; i < *videos && i < len(videoDurations); i++ {
		d := videoDurations[i]
		name := fmt.Sprintf("%s_video_%02d.mp4", base.AddDate(0, 1, i).Format("2006-01-02"), i)
		dst := filepath.Join(*out, name)
		args := []string{
			"-f", "lavfi",
			"-i", fmt.Sprintf("testsrc2=size=1280x720:rate=30:duration=%d", d),
			"-f", "lavfi",
			"-i", fmt.Sprintf("sine=frequency=440:duration=%d", d),
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "veryfast",
			"-c:a", "aac",
			"-shortest",
			"-y", dst,
		}
		if err := run(ctx, args); err != nil {
			log.Fatalf("生成 %s 失败: %v", name, err)
		}
	}
	log.Printf("已生成 %d 段 MP4", *videos)
}
