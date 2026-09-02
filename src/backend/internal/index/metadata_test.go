package index

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"panoalbum/internal/ffmpeg"
)

// ---------------------------------------------------------------- 测试夹具

// gpanoXMPAttr 属性写法的 GPano XMP（相机/手机注入的常见形态）。
const gpanoXMPAttr = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="Adobe XMP Core 5.6">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:GPano="http://ns.google.com/photos/1.0/panorama/"
    GPano:ProjectionType="equirectangular"
    GPano:UsePanoramaViewer="True"
    GPano:CroppedAreaImageWidthPixels="4000"
    GPano:FullPanoWidthPixels="4000"/>
 </rdf:RDF>
</x:xmpmeta>`

// gpanoXMPElem 子元素写法的 GPano XMP（部分工具写成元素而非属性）。
const gpanoXMPElem = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description xmlns:GPano="http://ns.google.com/photos/1.0/panorama/">
   <GPano:ProjectionType>cubemap</GPano:ProjectionType>
   <GPano:UsePanoramaViewer>true</GPano:UsePanoramaViewer>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>`

// plainXMP 不含 GPano 的普通 XMP（普通照片由 Lightroom 等写入）。
const plainXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">普通照片</rdf:li></rdf:Alt></dc:title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>`

// app1XMP 构造 APP1 段：0xFFE1 + 长度 + XMP magic + XMP 文本。
func app1XMP(xmp string) []byte {
	return app1(append([]byte(xmpMagic), xmp...))
}

// app1EXIF 构造含 DateTimeOriginal(0x9003) 的极简 EXIF APP1 段，
// 用于验证读 XMP 后文件指针复位、EXIF 仍能被 goexif 正常解析。
func app1EXIF(dateTime string) []byte {
	dt := append([]byte(dateTime), 0) // ASCII 类型以 NUL 结尾
	const dataOff = 26                // TIFF 头 8 + IFD 计数 2 + 条目 12 + 下一 IFD 偏移 4
	var tiff bytes.Buffer
	tiff.WriteString("II")
	binary.Write(&tiff, binary.LittleEndian, uint16(42))      // TIFF magic
	binary.Write(&tiff, binary.LittleEndian, uint32(8))       // IFD0 偏移
	binary.Write(&tiff, binary.LittleEndian, uint16(1))       // 条目数
	binary.Write(&tiff, binary.LittleEndian, uint16(0x9003))  // tag: DateTimeOriginal
	binary.Write(&tiff, binary.LittleEndian, uint16(2))       // type: ASCII
	binary.Write(&tiff, binary.LittleEndian, uint32(len(dt))) // count
	binary.Write(&tiff, binary.LittleEndian, uint32(dataOff)) // 值偏移
	binary.Write(&tiff, binary.LittleEndian, uint32(0))       // 无下一 IFD
	tiff.Write(dt)
	return app1(append([]byte("Exif\x00\x00"), tiff.Bytes()...))
}

// app1 把 payload 包装成 APP1 段（2 字节长度含长度字段自身）。
func app1(payload []byte) []byte {
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	return append(seg, payload...)
}

// writeJPEG 生成一张 w×h 的 JPEG，并把 segs 按序插到 SOI（及 JFIF APP0）之后。
func writeJPEG(t *testing.T, w, h int, segs ...[]byte) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ { // 渐变填充，避免纯色被编码器优化
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("生成测试 JPEG: %v", err)
	}
	src := buf.Bytes()

	// 插入点：跳过 SOI 及其后的 APP0(JFIF) 段，保持 JFIF 仍紧邻 SOI
	pos := 2
	if len(src) > 4 && src[2] == 0xFF && src[3] == 0xE0 {
		pos += 2 + int(src[4])<<8 + int(src[5])
	}
	out := make([]byte, 0, len(src)+4096)
	out = append(out, src[:pos]...)
	for _, s := range segs {
		out = append(out, s...)
	}
	out = append(out, src[pos:]...)

	dir := t.TempDir()
	p := filepath.Join(dir, "test.jpg")
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatalf("写测试文件: %v", err)
	}
	return p
}

// ---------------------------------------------------------------- XMP 解析

func TestDetect360FromXMP(t *testing.T) {
	wrap := func(attrs string) string {
		return `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF ` +
			`xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
			`<rdf:Description xmlns:GPano="http://ns.google.com/photos/1.0/panorama/" ` +
			attrs + `/></rdf:RDF></x:xmpmeta>`
	}
	cases := []struct {
		name string
		xmp  string
		want bool
		proj string
	}{
		{"属性写法 equirectangular", gpanoXMPAttr, true, ProjectionEquirect},
		{"子元素写法 cubemap", gpanoXMPElem, true, ProjectionCubemap},
		{"仅 UsePanoramaViewer 兜底等距柱状", wrap(`GPano:UsePanoramaViewer="True"`),
			true, ProjectionEquirect},
		{"UsePanoramaViewer=False 不算全景", wrap(`GPano:UsePanoramaViewer="False"`),
			false, ""},
		{"大小写混合 EquiRectangular", wrap(`GPano:ProjectionType="EquiRectangular"`),
			true, ProjectionEquirect},
		{"未知投影值不判定", wrap(`GPano:ProjectionType="cylindrical"`),
			false, ""},
		{"非 GPano 命名空间的 ProjectionType 忽略",
			`<x:xmpmeta><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
				`<rdf:Description xmlns:Other="http://example.com/other/" ` +
				`Other:ProjectionType="equirectangular"/></rdf:RDF></x:xmpmeta>`,
			false, ""},
		{"不含 GPano 的普通 XMP", plainXMP, false, ""},
		{"空 XMP", "", false, ""},
		{"残缺 XML 不 panic", `<x:xmpmeta><rdf:RDF><rdf:Description ` +
			`xmlns:GPano="http://ns.google.com/photos/1.0/panorama/" GPano:ProjectionType=`,
			false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got360, gotProj := detect360FromXMP(c.xmp)
			if got360 != c.want || gotProj != c.proj {
				t.Errorf("detect360FromXMP() = (%v,%q)，期望 (%v,%q)", got360, gotProj, c.want, c.proj)
			}
		})
	}
}

func TestParseGPanoXMPFields(t *testing.T) {
	info := parseGPanoXMP(gpanoXMPAttr)
	if info.ProjectionType != "equirectangular" {
		t.Errorf("ProjectionType = %q，期望 equirectangular", info.ProjectionType)
	}
	if info.UsePanoramaViewer != "True" {
		t.Errorf("UsePanoramaViewer = %q，期望 True", info.UsePanoramaViewer)
	}
	plain := parseGPanoXMP(plainXMP)
	if plain.ProjectionType != "" || plain.UsePanoramaViewer != "" {
		t.Errorf("普通 XMP 不应解析出 GPano 字段: %+v", plain)
	}
}

// ---------------------------------------------------------------- JPEG 通道端到端

func TestExtractPhotoMetaPanorama(t *testing.T) {
	const w, h = 4000, 2000
	const (
		exifTime = "2023:05:01 12:34:56" // EXIF ASCII 规定用冒号分隔日期
		wantTime = "2023-05-01 12:34:56" // time.Time 格式化后的期望值
	)
	exifSeg := func() []byte { return app1EXIF(exifTime) }

	cases := []struct {
		name     string
		segs     [][]byte
		want360  bool
		wantProj string
		wantTime string // 非空表示要求 EXIF 拍摄时间仍可读（验证指针复位）
	}{
		{"含 GPano 的 XMP", [][]byte{app1XMP(gpanoXMPAttr)}, true, ProjectionEquirect, ""},
		{"子元素 GPano cubemap", [][]byte{app1XMP(gpanoXMPElem)}, true, ProjectionCubemap, ""},
		{"无任何 XMP 段", nil, false, "", ""},
		{"XMP 无 GPano", [][]byte{app1XMP(plainXMP)}, false, "", ""},
		{"EXIF 在 XMP 之前", [][]byte{exifSeg(), app1XMP(gpanoXMPAttr)}, true, ProjectionEquirect, wantTime},
		{"仅 EXIF 无 XMP", [][]byte{exifSeg()}, false, "", wantTime},
		// goexif 只读取文件中首个 APP1 段：XMP 排在 EXIF 之前时 EXIF 会取不到
		// （既有库限制，与本次 XMP 扫描无关）。此时应优雅降级：不报错且全景判定照常生效。
		{"XMP 在 EXIF 之前", [][]byte{app1XMP(gpanoXMPAttr), exifSeg()}, true, ProjectionEquirect, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := writeJPEG(t, w, h, c.segs...)
			m, err := ExtractPhotoMeta(p)
			if err != nil {
				t.Fatalf("ExtractPhotoMeta: %v", err)
			}
			if m.Is360 != c.want360 || m.Projection != c.wantProj {
				t.Errorf("全景判定 = (%v,%q)，期望 (%v,%q)", m.Is360, m.Projection, c.want360, c.wantProj)
			}
			// 扫 XMP 不得破坏后续的尺寸与 EXIF 读取
			if m.Width != w || m.Height != h {
				t.Errorf("尺寸 = %dx%d，期望 %dx%d", m.Width, m.Height, w, h)
			}
			if c.wantTime == "" {
				return
			}
			if m.TakenAt == nil {
				t.Fatal("EXIF 拍摄时间丢失，说明读 XMP 后文件指针未复位")
			}
			if got := m.TakenAt.Format("2006-01-02 15:04:05"); got != c.wantTime {
				t.Errorf("TakenAt = %s，期望 %s", got, c.wantTime)
			}
		})
	}
}

func TestExtractPhotoMetaPNG(t *testing.T) {
	// PNG 无 APP1 段，扫描应安全跳过且不报错
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 8))); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ExtractPhotoMeta(p)
	if err != nil {
		t.Fatalf("ExtractPhotoMeta(PNG): %v", err)
	}
	if m.Is360 || m.Projection != "" {
		t.Errorf("PNG 不应判定为全景: Is360=%v Projection=%q", m.Is360, m.Projection)
	}
	if m.Width != 16 || m.Height != 8 {
		t.Errorf("PNG 尺寸 = %dx%d，期望 16x8", m.Width, m.Height)
	}
}

func TestScanJPEGXMPEdge(t *testing.T) {
	// 截断/非 JPEG 输入：不得 panic，返回空串
	for _, data := range [][]byte{nil, {0xFF}, {0xFF, 0xD8}, {0x89, 'P', 'N', 'G'}} {
		if got := scanJPEGXMP(bytes.NewReader(data)); got != "" {
			t.Errorf("scanJPEGXMP(%v) = %q，期望空串", data, got)
		}
	}
	// SOS 之后的 APP1 不应被读到（XMP 必在压缩数据之前）
	trunc := append([]byte{0xFF, 0xD8, 0xFF, 0xDA}, make([]byte, 8)...)
	if got := scanJPEGXMP(bytes.NewReader(trunc)); got != "" {
		t.Errorf("SOS 后不应扫到 XMP，实际 %q", got)
	}
}

// ---------------------------------------------------------------- 视频通道

// mkProbe 构造含一条视频流的 probeOutput。
func mkProbe(sideData []probeSideData, streamTags, formatTags probeTags) *probeOutput {
	var p probeOutput
	p.Streams = append(p.Streams, struct {
		CodecType string          `json:"codec_type"`
		CodecName string          `json:"codec_name"`
		Width     int             `json:"width"`
		Height    int             `json:"height"`
		AvgFrame  string          `json:"avg_frame_rate"`
		SideData  []probeSideData `json:"side_data_list"`
		Tags      probeTags       `json:"tags"`
	}{
		CodecType: "video", CodecName: "h264", Width: 3840, Height: 1920,
		AvgFrame: "30/1", SideData: sideData, Tags: streamTags,
	})
	p.Format.Tags = formatTags
	return &p
}

func TestDetectVideo360(t *testing.T) {
	cases := []struct {
		name string
		p    *probeOutput
		want bool
		proj string
	}{
		{"side_data equirectangular",
			mkProbe([]probeSideData{{"Spherical Mapping", "equirectangular"}}, probeTags{}, probeTags{}),
			true, ProjectionEquirect},
		{"side_data cubemap",
			mkProbe([]probeSideData{{"Spherical Mapping", "cubemap"}}, probeTags{}, probeTags{}),
			true, ProjectionCubemap},
		{"side_data 无 projection 字段",
			mkProbe([]probeSideData{{"Spherical Mapping", ""}}, probeTags{}, probeTags{}),
			true, ProjectionEquirect},
		{"非 spherical side_data 忽略",
			mkProbe([]probeSideData{{"Stereo 3D", ""}}, probeTags{}, probeTags{}),
			false, ""},
		{"流标签 ProjectionType",
			mkProbe(nil, probeTags{ProjectionType: "equirectangular"}, probeTags{}),
			true, ProjectionEquirect},
		{"流标签 spherical=true",
			mkProbe(nil, probeTags{Spherical: "true"}, probeTags{}),
			true, ProjectionEquirect},
		{"容器标签 ProjectionType",
			mkProbe(nil, probeTags{}, probeTags{ProjectionType: "cubemap"}),
			true, ProjectionCubemap},
		{"容器标签 Spherical 布尔值",
			mkProbe(nil, probeTags{}, probeTags{Spherical: "True"}),
			true, ProjectionEquirect},
		{"无全景信息",
			mkProbe(nil, probeTags{}, probeTags{CreationTime: "2024-01-02T03:04:05Z"}),
			false, ""},
		{"无流", &probeOutput{}, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got360, gotProj := detectVideo360(c.p)
			if got360 != c.want || gotProj != c.proj {
				t.Errorf("detectVideo360() = (%v,%q)，期望 (%v,%q)", got360, gotProj, c.want, c.proj)
			}
		})
	}
}

func TestProbeOutputUnmarshalSpherical(t *testing.T) {
	// 真实 ffprobe 输出片段：验证 side_data_list 与 tags 能被正确反序列化
	raw := `{"streams":[{"codec_type":"video","codec_name":"h264","width":4096,"height":2048,
		"avg_frame_rate":"30/1","side_data_list":[{"side_data_type":"Spherical Mapping",
		"projection":"equirectangular","yaw":0,"pitch":0,"roll":0}],
		"tags":{"spherical":"equirectangular"}}],
		"format":{"duration":"12.345","tags":{"creation_time":"2024-03-04T05:06:07Z"}}}`
	var p probeOutput
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("反序列化: %v", err)
	}
	if len(p.Streams) != 1 || len(p.Streams[0].SideData) != 1 {
		t.Fatalf("side_data_list 未解析: %+v", p.Streams)
	}
	if p.Streams[0].SideData[0].SideDataType != "Spherical Mapping" {
		t.Errorf("side_data_type = %q", p.Streams[0].SideData[0].SideDataType)
	}
	if p.Streams[0].Tags.Spherical != "equirectangular" {
		t.Errorf("流 tags.spherical = %q", p.Streams[0].Tags.Spherical)
	}
	// 既有字段不受影响
	if p.Format.Tags.CreationTime != "2024-03-04T05:06:07Z" || p.Format.Duration != "12.345" {
		t.Errorf("既有字段被破坏: %+v", p.Format)
	}
	if ok, proj := detectVideo360(&p); !ok || proj != ProjectionEquirect {
		t.Errorf("detectVideo360 = (%v,%q)，期望 (true,equirectangular)", ok, proj)
	}
}

// TestExtractVideoMetaSmoke 端到端跑一次真实 ffprobe/ffmpeg（不可用时跳过），
// 确保新增的 side_data/tags 字段不影响既有解析路径，且普通视频不被误判。
func TestExtractVideoMetaSmoke(t *testing.T) {
	bin, err := ffmpeg.LookPath()
	if err != nil {
		t.Skipf("未找到 ffmpeg，跳过: %v", err)
	}
	if _, err := ffmpeg.LookProbePath(); err != nil {
		t.Skipf("未找到 ffprobe，跳过: %v", err)
	}
	src := filepath.Join(t.TempDir(), "plain.mp4")
	out, err := exec.Command(bin, "-v", "error", "-y", "-f", "lavfi", "-i",
		"testsrc=size=320x160:rate=10:duration=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", src).CombinedOutput()
	if err != nil {
		t.Skipf("生成测试视频失败，跳过: %v %s", err, out)
	}
	m, err := ExtractVideoMeta(context.Background(), src)
	if err != nil {
		t.Fatalf("ExtractVideoMeta: %v", err)
	}
	if m.Width != 320 || m.Height != 160 {
		t.Errorf("尺寸 = %dx%d，期望 320x160", m.Width, m.Height)
	}
	if m.FPS < 9 || m.FPS > 11 {
		t.Errorf("FPS = %v，期望约 10", m.FPS)
	}
	if m.DurationSec == nil {
		t.Error("未取到时长")
	}
	if m.Is360 || m.Projection != "" {
		t.Errorf("普通视频被误判为全景: Is360=%v Projection=%q", m.Is360, m.Projection)
	}
	// 启发式未接线：2:1 的普通视频也不应被主流程判为全景
	if ok, _ := DetectByAspect(m.Width, m.Height); ok {
		t.Error("启发式对 320x160 生效，阈值或接线有误")
	}
}

// ---------------------------------------------------------------- 启发式回退

func TestDetectByAspect(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want bool
		proj string
	}{
		{"标准 4K 全景", 3840, 1920, true, ProjectionEquirect},
		{"竖置 2:1", 1920, 3840, true, ProjectionEquirect},
		{"短边恰为 1024", 2048, 1024, true, ProjectionEquirect},
		{"短边 1023 不判定", 2046, 1023, false, ""},
		{"比例恰为 1.9", 1946, 1024, true, ProjectionEquirect},
		{"比例略低于 1.9", 1945, 1024, false, ""},
		{"比例恰为 2.1", 2150, 1024, true, ProjectionEquirect},
		{"比例略高于 2.1", 2151, 1024, false, ""},
		{"16:9 普通视频", 1920, 1080, false, ""},
		{"正方形", 2000, 2000, false, ""},
		{"2:1 但分辨率太小", 1000, 500, false, ""},
		{"零宽", 0, 1920, false, ""},
		{"零高", 3840, 0, false, ""},
		{"负数", -3840, 1920, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got360, gotProj := DetectByAspect(c.w, c.h)
			if got360 != c.want || gotProj != c.proj {
				t.Errorf("DetectByAspect(%d,%d) = (%v,%q)，期望 (%v,%q)",
					c.w, c.h, got360, gotProj, c.want, c.proj)
			}
		})
	}
}

// 启发式不得被硬编码进主流程：无 XMP 的 2:1 图片仍应判定为非全景。
func TestDetectByAspectNotWiredIntoMainFlow(t *testing.T) {
	p := writeJPEG(t, 2048, 1024, app1XMP(plainXMP)) // 2:1 且短边 1024，但无 GPano
	m, err := ExtractPhotoMeta(p)
	if err != nil {
		t.Fatalf("ExtractPhotoMeta: %v", err)
	}
	if m.Is360 || m.Projection != "" {
		t.Errorf("主流程不应启用启发式: Is360=%v Projection=%q", m.Is360, m.Projection)
	}
	if ok, proj := DetectByAspect(m.Width, m.Height); !ok || proj != ProjectionEquirect {
		t.Errorf("DetectByAspect(2048,1024) = (%v,%q)，期望 (true,equirectangular)", ok, proj)
	}
}

func TestNormalizeProjection(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"equirectangular": ProjectionEquirect,
		"EquiRectangular": ProjectionEquirect,
		" equirect ":      ProjectionEquirect,
		"cubemap":         ProjectionCubemap,
		"CUBEMAP":         ProjectionCubemap,
		"cylindrical":     "",
		"garbage":         "",
	}
	for in, want := range cases {
		if got := normalizeProjection(in); got != want {
			t.Errorf("normalizeProjection(%q) = %q，期望 %q", in, got, want)
		}
	}
}
