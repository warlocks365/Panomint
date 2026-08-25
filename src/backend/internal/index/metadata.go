package index

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // 解码 JPEG 尺寸
	_ "image/png"  // 解码 PNG 尺寸
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"

	"panoalbum/internal/ffmpeg"
)

// Meta 提取到的媒体元数据（字段与 media 表对应，零值表示未取到）。
type Meta struct {
	TakenAt     *time.Time // 拍摄时间（EXIF / 视频 creation_time）
	Width       int
	Height      int
	CameraMake  string
	CameraModel string
	Lat         *float64 // WGS-84
	Lng         *float64
	DurationSec *int    // 视频时长（秒）
	Codec       string  // 视频编码
	FPS         float64 // 视频帧率
}

// ExtractPhotoMeta 读 JPEG EXIF（拍摄时间/GPS/相机型号）与图片尺寸。
// 缺 EXIF 时 TakenAt 由调用方回退 mtime；尺寸解码失败不视为致命错误。
func ExtractPhotoMeta(path string) (*Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &Meta{}
	// 尺寸（须在读 EXIF 前 Seek 回开头，goexif 会消耗 reader）
	if cfg, _, err := image.DecodeConfig(f); err == nil {
		m.Width, m.Height = cfg.Width, cfg.Height
	}
	if _, err := f.Seek(0, 0); err != nil {
		return m, nil // 忽略 seek 失败，尽力返回尺寸
	}

	x, err := exif.Decode(f)
	if err != nil {
		return m, nil // 无 EXIF（如 PNG/合成图），不视为错误
	}
	if tm, err := x.DateTime(); err == nil {
		m.TakenAt = &tm
	}
	if lat, lng, err := x.LatLong(); err == nil {
		m.Lat, m.Lng = &lat, &lng
	}
	if v, err := x.Get(exif.Make); err == nil {
		m.CameraMake = cleanExifString(v)
	}
	if v, err := x.Get(exif.Model); err == nil {
		m.CameraModel = cleanExifString(v)
	}
	return m, nil
}

// cleanExifString 去掉 EXIF 字符串的引号与尾零。
func cleanExifString(t *tiff.Tag) string {
	s, err := t.StringVal()
	if err != nil {
		return strings.Trim(t.String(), `"`)
	}
	return strings.TrimRight(strings.TrimSpace(s), "\x00")
}

// ffprobe JSON 输出结构（仅取需要的字段）。
type probeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		AvgFrame  string `json:"avg_frame_rate"` // 形如 "30/1"
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"` // 秒，浮点字符串
		Tags     struct {
			CreationTime string `json:"creation_time"` // ISO8601
		} `json:"tags"`
	} `json:"format"`
}

// ExtractVideoMeta 用 ffprobe 读视频时长/分辨率/编码/帧率/creation_time。
func ExtractVideoMeta(ctx context.Context, path string) (*Meta, error) {
	bin, err := ffmpeg.LookProbePath()
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, bin,
		"-v", "error", "-show_streams", "-show_format", "-of", "json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("ffprobe 输出解析: %w", err)
	}

	m := &Meta{}
	for _, s := range p.Streams {
		if s.CodecType != "video" {
			continue
		}
		m.Width, m.Height = s.Width, s.Height
		m.Codec = s.CodecName
		m.FPS = parseFPS(s.AvgFrame)
		break
	}
	if sec, err := strconv.ParseFloat(strings.TrimSpace(p.Format.Duration), 64); err == nil && sec > 0 {
		d := int(sec + 0.5)
		m.DurationSec = &d
	}
	if p.Format.Tags.CreationTime != "" {
		if tm, err := time.Parse(time.RFC3339, p.Format.Tags.CreationTime); err == nil {
			m.TakenAt = &tm
		}
	}
	return m, nil
}

// parseFPS 解析 "30000/1001" 形式的帧率。
func parseFPS(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || d == 0 {
		return 0
	}
	return n / d
}
