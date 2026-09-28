package index

import (
	"regexp"
	"unicode/utf8"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/jpeg" // 解码 JPEG 尺寸
	_ "image/png"  // 解码 PNG 尺寸
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"

	"panoalbum/internal/ffmpeg"
)

// 全景投影取值（Meta.Projection）。
const (
	ProjectionEquirect = "equirectangular" // 等距柱状投影（Google Photo Sphere / YouTube 360 默认）
	ProjectionCubemap  = "cubemap"         // 立方体贴图
)

// xmpMagic JPEG APP1 段内 XMP 包的标识前缀，其后紧接 XML 文本。
const xmpMagic = "http://ns.adobe.com/xap/1.0/\x00"

// gpanoNS GPano（Google Photo Sphere / Android 全景）命名空间。
const gpanoNS = "http://ns.google.com/photos/1.0/panorama/"

// maxJPEGSegment 单个 JPEG 段的最大读取长度（限制分配；APP1 段实际不超过 65533）。
const maxJPEGSegment = 1 << 20

// 启发式判定阈值（仅 DetectByAspect 使用，主流程不启用）。
const (
	aspectMin    = 1.9  // 长边/短边 下限
	aspectMax    = 2.1  // 长边/短边 上限
	minShortSide = 1024 // 短边最小像素，低于此值不判定
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
	Is360       bool    // 是否 360/全景媒体
	Projection  string  // ProjectionEquirect | ProjectionCubemap，空串表示非全景或未能判定
	Place       string  // 可读地名（由 GPS 逆地理编码派生，非文件内元数据）；空串表示未取到
}

// ExtractPhotoMeta 读 JPEG EXIF（拍摄时间/GPS/相机型号）、图片尺寸与 XMP 全景信息。
// 缺 EXIF 时 TakenAt 由调用方回退 mtime；尺寸解码失败不视为致命错误。
func ExtractPhotoMeta(path string) (*Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &Meta{}
	// 全景：扫 XMP 会移动文件指针，后续须 Seek 回开头
	m.Is360, m.Projection = detectPhoto360(f)
	if _, err := f.Seek(0, 0); err != nil {
		return m, nil // 忽略 seek 失败，尽力返回已取到的字段
	}
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

// detectPhoto360 从 JPEG 的 XMP（GPano 命名空间）判定是否为全景图。
// r 应位于文件开头；无 XMP 或非全景返回 (false, "")。
func detectPhoto360(r io.Reader) (bool, string) {
	return detect360FromXMP(scanJPEGXMP(r))
}

// detect360FromXMP 按 GPano 字段判定：ProjectionType 优先，缺省时
// UsePanoramaViewer=true 视为等距柱状（GPano 默认投影）。
func detect360FromXMP(xmp string) (bool, string) {
	info := parseGPanoXMP(xmp)
	proj := normalizeProjection(info.ProjectionType)
	if proj == "" && isTrueish(info.UsePanoramaViewer) {
		proj = ProjectionEquirect
	}
	if proj == "" {
		return false, ""
	}
	return true, proj
}

// scanJPEGXMP 扫描 JPEG 的 APP1 段，返回其中的 XMP 包文本；r 须位于文件开头。
// 非 JPEG、无 XMP 或流失步均返回空串——全景元数据缺失不属于错误。
func scanJPEGXMP(r io.Reader) string {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil || soi[0] != 0xFF || soi[1] != 0xD8 {
		return "" // 无 SOI：不是 JPEG
	}
	for {
		b, err := br.ReadByte()
		if err != nil || b != 0xFF {
			return "" // 流结束或已失步
		}
		for { // 0xFF 填充字节：真正的 marker 在后
			if b, err = br.ReadByte(); err != nil {
				return ""
			}
			if b != 0xFF {
				break
			}
		}
		marker := b
		switch {
		case marker == 0x01 || (marker >= 0xD0 && marker <= 0xD9):
			continue // 无长度字段的独立标记（RSTn/EOI/TEM）
		case marker == 0xDA:
			return "" // SOS：其后为压缩数据，XMP 必在之前
		}
		var ln [2]byte
		if _, err := io.ReadFull(br, ln[:]); err != nil {
			return ""
		}
		payload := int(ln[0])<<8 | int(ln[1])
		if payload < 2 {
			return ""
		}
		payload -= 2 // 长度字段自身占 2 字节
		if payload > maxJPEGSegment {
			if _, err := br.Discard(payload); err != nil {
				return ""
			}
			continue
		}
		buf := make([]byte, payload)
		if _, err := io.ReadFull(br, buf); err != nil {
			return ""
		}
		if marker == 0xE1 && bytes.HasPrefix(buf, []byte(xmpMagic)) {
			return string(buf[len(xmpMagic):])
		}
	}
}

// gpanoInfo XMP 中 GPano 命名空间下的全景字段（空串表示未出现）。
type gpanoInfo struct {
	ProjectionType    string
	UsePanoramaViewer string
}

func (g *gpanoInfo) set(field, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	switch field {
	case "projectiontype":
		g.ProjectionType = value
	case "usepanoramaviewer":
		g.UsePanoramaViewer = value
	}
}

// parseGPanoXMP 解析 XMP 文本中的 GPano 字段，属性写法（GPano:ProjectionType="..."）
// 与子元素写法（<GPano:ProjectionType>...</GPano:ProjectionType>）均支持。
// XMP 常含非严格 XML，解析中断时返回已取到的部分而非报错。
func parseGPanoXMP(xmp string) gpanoInfo {
	var info gpanoInfo
	if strings.TrimSpace(xmp) == "" {
		return info
	}
	dec := xml.NewDecoder(strings.NewReader(xmp))
	depth, pendingDepth, pending := 0, 0, ""
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch el := tok.(type) {
		case xml.StartElement:
			depth++
			for _, a := range el.Attr {
				if f, ok := gpanoField(a.Name); ok {
					info.set(f, a.Value)
				}
			}
			if f, ok := gpanoField(el.Name); ok {
				pending, pendingDepth = f, depth
			}
		case xml.CharData:
			if pending != "" && depth == pendingDepth {
				info.set(pending, string(el))
			}
		case xml.EndElement:
			if depth == pendingDepth {
				pending, pendingDepth = "", 0
			}
			depth--
		}
	}
	return info
}

// gpanoField 判定 XML 名（元素或属性）是否属于 GPano 全景字段，返回归一化的字段名。
// 命名空间缺失时退化为按本地名匹配，兼容未声明 xmlns:GPano 的 XMP。
func gpanoField(n xml.Name) (string, bool) {
	local := strings.ToLower(n.Local)
	if local != "projectiontype" && local != "usepanoramaviewer" {
		return "", false
	}
	if n.Space != "" && n.Space != gpanoNS {
		return "", false
	}
	return local, true
}

// normalizeProjection 把各种投影写法归一为 equirectangular/cubemap，无法识别返回空串。
func normalizeProjection(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	switch {
	case v == "":
		return ""
	case strings.Contains(v, "equirect"):
		return ProjectionEquirect
	case strings.Contains(v, "cube"):
		return ProjectionCubemap
	default:
		return ""
	}
}

// isTrueish GPano 的布尔写法（"True"/"true"/"1"）判定。
func isTrueish(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// firstNonEmpty 返回首个非空白字符串，全空时返回空串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// probeSideData ffprobe stream.side_data_list 条目；全景条目 side_data_type 为 "Spherical Mapping"。
type probeSideData struct {
	SideDataType string `json:"side_data_type"`
	Projection   string `json:"projection"` // equirectangular / cubemap
}

// probeTags 容器或流标签；JSON 键名匹配大小写不敏感，故 spherical/Spherical 同一字段。
type probeTags struct {
	CreationTime   string `json:"creation_time"` // ISO8601
	Spherical      string `json:"spherical"`     // 部分相机/注入工具写 spherical=true
	ProjectionType string `json:"projectiontype"`
}

// ffprobe JSON 输出结构（仅取需要的字段）。
type probeOutput struct {
	Streams []struct {
		CodecType string          `json:"codec_type"`
		CodecName string          `json:"codec_name"`
		Width     int             `json:"width"`
		Height    int             `json:"height"`
		AvgFrame  string          `json:"avg_frame_rate"` // 形如 "30/1"
		SideData  []probeSideData `json:"side_data_list"`
		Tags      probeTags       `json:"tags"`
	} `json:"streams"`
	Format struct {
		Duration string    `json:"duration"` // 秒，浮点字符串
		Tags     probeTags `json:"tags"`
	} `json:"format"`
}

// detectVideo360 依 spherical side_data 与 spherical/ProjectionType 标签判定全景视频。
// 优先级：流 side_data > 流标签 > 容器标签；side_data 存在但无 projection 字段时按等距柱状处理。
func detectVideo360(p *probeOutput) (bool, string) {
	for _, s := range p.Streams {
		for _, sd := range s.SideData {
			if !strings.Contains(strings.ToLower(sd.SideDataType), "spherical") {
				continue
			}
			if proj := normalizeProjection(sd.Projection); proj != "" {
				return true, proj
			}
			return true, ProjectionEquirect
		}
	}
	for _, s := range p.Streams {
		if proj := normalizeProjection(firstNonEmpty(s.Tags.Spherical, s.Tags.ProjectionType)); proj != "" {
			return true, proj
		}
		if isTrueish(s.Tags.Spherical) {
			return true, ProjectionEquirect
		}
	}
	if proj := normalizeProjection(firstNonEmpty(p.Format.Tags.Spherical, p.Format.Tags.ProjectionType)); proj != "" {
		return true, proj
	}
	if isTrueish(p.Format.Tags.Spherical) {
		return true, ProjectionEquirect
	}
	return false, ""
}

// ExtractVideoMeta 用 ffprobe 读视频时长/分辨率/编码/帧率/creation_time 与全景信息。
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
	m.Is360, m.Projection = detectVideo360(&p)

	// Job000132：视频位置信息——ffprobe format.tags 的 GPS 载体（ISO6709 / DJI comment）。
	// tags 键名含点（com.apple.quicktime.location.ISO6709）无法映射 struct 字段，二次解析为 map。
	var rawTags struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &rawTags); err == nil {
		applyVideoLocationTags(m, rawTags.Format.Tags)
	}
	return m, nil
}

// applyVideoLocationTags 从 ffprobe format.tags 提取视频 GPS 与地址信息。
// 优先级：location（ISO6709，iPhone/DJI 主流）> com.apple.quicktime.location.ISO6709 >
// comment 内嵌 drone-dji 坐标 XML。comment 为纯文本（非坐标）且 place 未取到时，
// 截断写入 place 作为「地址信息」展示。
func applyVideoLocationTags(m *Meta, tags map[string]string) {
	if m.Lat != nil && m.Lng != nil {
		return
	}
	get := func(k string) string {
		if v, ok := tags[k]; ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	if lat, lng, ok := videoGPSFromTags(tags); ok {
		m.Lat, m.Lng = &lat, &lng
		return
	}
	// comment 纯文本地址：仅当尚未有地名时作为 place 兜底（截断防异常长串）。
	if m.Place == "" {
		if c := get("comment"); c != "" && !strings.ContainsAny(c, "<>") && utf8.RuneCountInString(c) <= 200 {
			m.Place = c
		}
	}
}

// videoGPSFromTags 按优先级从 tags 提取坐标。ok=false 表示所有载体都未命中或非法。
func videoGPSFromTags(tags map[string]string) (lat, lng float64, ok bool) {
	get := func(k string) string {
		if v, ok2 := tags[k]; ok2 {
			return strings.TrimSpace(v)
		}
		return ""
	}
	if la, ln, ok2 := parseISO6709(get("location")); ok2 {
		return la, ln, true
	}
	if la, ln, ok2 := parseISO6709(get("com.apple.quicktime.location.ISO6709")); ok2 {
		return la, ln, true
	}
	if la, ln, ok2 := parseDJIXMLComment(get("comment")); ok2 {
		return la, ln, true
	}
	return 0, 0, false
}

// parseISO6709 解析 ISO 6709 坐标串（±DD.DDDD±DDD.DDDD[/±HH.H/]），
// 覆盖 iPhone（com.apple.quicktime.location.ISO6709）与 DJI/安卓的 location tag。
func parseISO6709(s string) (lat, lng float64, ok bool) {
	if s == "" {
		return 0, 0, false
	}
	re := regexp.MustCompile(`^([+-]\d{1,3}(?:\.\d+)?)([+-]\d{1,3}(?:\.\d+)?)(?:/?[+-]\d+(?:\.\d+)?)?/?`)
	mm := re.FindStringSubmatch(strings.TrimSpace(s))
	if mm == nil {
		return 0, 0, false
	}
	la, err1 := strconv.ParseFloat(mm[1], 64)
	lng, err2 := strconv.ParseFloat(mm[2], 64)
	if err1 != nil || err2 != nil || la < -90 || la > 90 || lng < -180 || lng > 180 {
		return 0, 0, false
	}
	return la, lng, true
}

// parseDJIXMLComment 解析 DJI 系（Mavic/Mini/Air）写入 comment 的遥测 XML：
// <drone-dji:latitude>27.1700</drone-dji:latitude><drone-dji:longitude>-80.0389</drone-dji:longitude>。
func parseDJIXMLComment(s string) (lat, lng float64, ok bool) {
	if s == "" || !strings.Contains(s, "drone-dji") {
		return 0, 0, false
	}
	laRe := regexp.MustCompile(`drone-dji:latitude[>"]+\s*(-?\d+(?:\.\d+)?)`)
	lngRe := regexp.MustCompile(`drone-dji:longitude[>"]+\s*(-?\d+(?:\.\d+)?)`)
	laM, lngM := laRe.FindStringSubmatch(s), lngRe.FindStringSubmatch(s)
	if laM == nil || lngM == nil {
		return 0, 0, false
	}
	la, err1 := strconv.ParseFloat(laM[1], 64)
	lng, err2 := strconv.ParseFloat(lngM[1], 64)
	if err1 != nil || err2 != nil || la < -90 || la > 90 || lng < -180 || lng > 180 {
		return 0, 0, false
	}
	return la, lng, true
}

// DetectByAspect 启发式回退：长宽比接近 2:1 且短边足够大时判定为等距柱状全景。
// 主流程不启用（元数据缺失时误判率高，如裁切过的横幅图），由调用方在需要时自行调用：
//
//	if !m.Is360 {
//	    m.Is360, m.Projection = index.DetectByAspect(m.Width, m.Height)
//	}
func DetectByAspect(width, height int) (is360 bool, projection string) {
	if width <= 0 || height <= 0 {
		return false, ""
	}
	short, long := width, height
	if width > height {
		short, long = height, width
	}
	if short < minShortSide {
		return false, ""
	}
	if ratio := float64(long) / float64(short); ratio < aspectMin || ratio > aspectMax {
		return false, ""
	}
	return true, ProjectionEquirect
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
