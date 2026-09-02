// inject360 给测试样本补注入 360 全景元数据，供 internal/index 的检测逻辑做端到端验证。
// 用法：
//
//	inject360 -photo input.jpg [-o output.jpg]
//	inject360 -video input.mp4 [-o output.mp4]
//	inject360 -verify <文件>
//
// 缺省 -o 时输出到 <原名>.360.<扩展名>；绝不覆盖已存在的文件。
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image/jpeg"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"panoalbum/internal/ffmpeg"
	"panoalbum/internal/index"
)

// xmpMagic JPEG APP1 段内 XMP 包的标识前缀（与 internal/index 检测约定一致）。
const xmpMagic = "http://ns.adobe.com/xap/1.0/\x00"

// maxAPP1Payload JPEG 段长度字段为 2 字节且含长度字段自身，payload 上限 65533。
const maxAPP1Payload = 65533

// gpanoXMP 构造全套 GPano 全景 XMP（宽高取 JPEG SOF 实际尺寸，等距柱状满幅未裁切）。
func gpanoXMP(width, height int) string {
	return fmt.Sprintf(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="inject360">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:GPano="http://ns.google.com/photos/1.0/panorama/"
    GPano:ProjectionType="equirectangular"
    GPano:UsePanoramaViewer="True"
    GPano:FullPanoWidthPixels="%d"
    GPano:FullPanoHeightPixels="%d"
    GPano:CroppedAreaImageWidthPixels="%d"
    GPano:CroppedAreaImageHeightPixels="%d"
    GPano:CroppedAreaLeftPixels="0"
    GPano:CroppedAreaTopPixels="0"
    GPano:InitialViewHeadingDegrees="0"
    GPano:InitialViewPitchDegrees="0"
    GPano:InitialViewRollDegrees="0"
    GPano:PoseHeadingDegrees="0"
    GPano:PosePitchDegrees="0"
    GPano:PoseRollDegrees="0"/>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`, width, height, width, height)
}

// defaultOutput 缺省输出名：<原名>.360.<扩展名>。
func defaultOutput(src string) string {
	ext := filepath.Ext(src)
	return strings.TrimSuffix(src, ext) + ".360" + ext
}

// checkDst 校验输出路径：不得与输入相同，且不得覆盖已存在文件。
func checkDst(src, dst string) error {
	if dst == src {
		return errors.New("输出路径与输入相同（绝不覆盖原文件）")
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("输出文件已存在，拒绝覆盖: %s", dst)
	}
	return nil
}

// injectPhoto 在 JPEG 的 SOI 之后插入含 GPano XMP 的 APP1 段，写入 dst。
// scanJPEGXMP 自文件头逐段扫描至 SOS，SOI 后即插入必能被扫到。
func injectPhoto(src, dst string) error {
	if err := checkDst(src, dst); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return fmt.Errorf("%s 不是 JPEG（无 SOI）", src)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("解码 JPEG 尺寸失败: %w", err)
	}
	payload := append([]byte(xmpMagic), gpanoXMP(cfg.Width, cfg.Height)...)
	if len(payload) > maxAPP1Payload {
		return fmt.Errorf("XMP 包过大: %d 字节（上限 %d）", len(payload), maxAPP1Payload)
	}
	seg := make([]byte, 4, 4+len(payload))
	seg[0], seg[1] = 0xFF, 0xE1
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)

	out := make([]byte, 0, len(data)+len(seg))
	out = append(out, data[:2]...) // SOI
	out = append(out, seg...)
	out = append(out, data[2:]...)
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		return err
	}
	log.Printf("照片注入完成 %s -> %s（%dx%d, APP1 %d 字节）", src, dst, cfg.Width, cfg.Height, len(seg))
	return nil
}

// injectVideo 用 ffmpeg tag 通道写入 spherical 元数据（-c copy 不重编码），写入 dst。
// detectVideo360 的末级判定认 format tags 的 spherical/ProjectionType，回读验证不通过时报错。
func injectVideo(ctx context.Context, src, dst string) error {
	if err := checkDst(src, dst); err != nil {
		return err
	}
	bin, err := ffmpeg.LookPath()
	if err != nil {
		return fmt.Errorf("定位 ffmpeg 失败: %w", err)
	}
	cmd := exec.CommandContext(ctx, bin,
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", src,
		"-c", "copy",
		"-movflags", "use_metadata_tags",
		"-metadata", "spherical=equirectangular",
		"-metadata", "ProjectionType=equirectangular",
		dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg 注入失败: %w\n%s", err, out)
	}
	// 注入即用项目检测代码回读，闭环验证；不认则视为注入失败
	m, err := index.ExtractVideoMeta(ctx, dst)
	if err != nil {
		return fmt.Errorf("回读验证失败: %w", err)
	}
	if !m.Is360 {
		return errors.New("注入后检测代码仍判定为非 360（tag 通道未生效，需改走 spatialmedia 方案）")
	}
	log.Printf("视频注入完成 %s -> %s（tag 通道, Is360=%v Projection=%s）", src, dst, m.Is360, m.Projection)
	return nil
}

// verify 只检测不注入：按扩展名分派到照片/视频检测入口，打印判定结果。
func verify(ctx context.Context, path string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		m, err := index.ExtractPhotoMeta(path)
		if err != nil {
			return err
		}
		fmt.Printf("%s: Is360=%v Projection=%q (%dx%d)\n", path, m.Is360, m.Projection, m.Width, m.Height)
	case ".mp4", ".mov", ".m4v", ".mkv", ".webm":
		m, err := index.ExtractVideoMeta(ctx, path)
		if err != nil {
			return err
		}
		fmt.Printf("%s: Is360=%v Projection=%q (%dx%d)\n", path, m.Is360, m.Projection, m.Width, m.Height)
	default:
		return fmt.Errorf("无法识别的扩展名: %s", path)
	}
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[inject360] ")

	photo := flag.String("photo", "", "注入 JPEG 照片（GPano XMP）")
	video := flag.String("video", "", "注入 MP4 视频（spherical 元数据）")
	out := flag.String("o", "", "输出路径（缺省 <原名>.360.<扩展名>）")
	verifyPath := flag.String("verify", "", "只检测不注入，打印 Is360/Projection")
	flag.Parse()

	// ffprobe 与 ffmpeg 通常同目录：FFPROBE_PATH 未设时回退用 FFMPEG_PATH
	if os.Getenv("FFPROBE_PATH") == "" {
		if p := os.Getenv("FFMPEG_PATH"); p != "" {
			os.Setenv("FFPROBE_PATH", p)
		}
	}
	ctx := context.Background()

	modes := 0
	for _, s := range []string{*photo, *video, *verifyPath} {
		if s != "" {
			modes++
		}
	}
	if modes != 1 {
		fmt.Fprintln(os.Stderr, "用法: inject360 -photo <in.jpg> [-o out.jpg] | -video <in.mp4> [-o out.mp4] | -verify <文件>")
		os.Exit(2)
	}

	var err error
	switch {
	case *verifyPath != "":
		err = verify(ctx, *verifyPath)
	case *photo != "":
		dst := *out
		if dst == "" {
			dst = defaultOutput(*photo)
		}
		err = injectPhoto(*photo, dst)
	case *video != "":
		dst := *out
		if dst == "" {
			dst = defaultOutput(*video)
		}
		err = injectVideo(ctx, *video, dst)
	}
	if err != nil {
		log.Fatalf("失败: %v", err)
	}
}
