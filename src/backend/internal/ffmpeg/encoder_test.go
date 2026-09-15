package ffmpeg

import (
	"reflect"
	"testing"
)

// 本文件覆盖「编码器选择」这一层。它的价值在于把**双接口原则**变成可断言的代码事实：
// CPU 与 GPU 两条路径同时在册，且默认路径（软编）的行为**逐字节不变**。

// TestHLSArgsDefaultIsSoftwareAndUnchanged 默认路径必须仍是软编，且与显式软编逐字节一致。
//
// 这是重构的回归闸门：HLSArgs 是全项目唯一的 HLS 参数生成入口，
// 它的输出变了就等于所有转码的参数都变了（而"参数变了但看起来还能转"最难发现）。
func TestHLSArgsDefaultIsSoftwareAndUnchanged(t *testing.T) {
	ladder := DefaultHLSLadder()[:1]
	dir := "/tmp/out"

	got := HLSArgs("in.mp4", dir, ladder, 4, false)
	want := HLSArgsEnc("in.mp4", dir, ladder, 4, false, EncoderX264)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HLSArgs 必须等价于 HLSArgsEnc(..., EncoderX264)\n got=%v\nwant=%v", got, want)
	}
	if v := pairValue(t, got, "-c:v:0"); v != "libx264" {
		t.Fatalf("默认编码器应为 libx264，实际 %q", v)
	}
}

// TestHLSArgsEncSwitchesCodecAndPreset 指定 nvenc 时必须同时换掉编码器与 preset。
//
// ⚠️ preset 名两个编码器**不通用**：把 x264 的 veryfast 传给 nvenc 会让 ffmpeg 直接报
// `Undefined constant or missing '(' in 'veryfast'` 退出。这条断言把"不许混用"钉死，
// 因为该错误只在有卡的机器上才暴露 —— 正是最难在开发机发现的一类问题。
func TestHLSArgsEncSwitchesCodecAndPreset(t *testing.T) {
	ladder := DefaultHLSLadder()[:1]
	// withAudio=true 让参数集包含音频相关实参，从而把"换编码器是否误伤音频映射"也一并断言到。
	soft := HLSArgsEnc("in.mp4", "/tmp/out", ladder, 4, true, EncoderX264)
	nv := HLSArgsEnc("in.mp4", "/tmp/out", ladder, 4, true, EncoderNVENC)

	if v := pairValue(t, nv, "-c:v:0"); v != "h264_nvenc" {
		t.Fatalf("编码器应为 h264_nvenc，实际 %q", v)
	}
	if v := pairValue(t, nv, "-preset:v:0"); v != "p4" {
		t.Fatalf("nvenc 的 preset 应为 p4（不得沿用 x264 的 veryfast），实际 %q", v)
	}
	// 码率/分片/变体映射等**除编码器与 preset 外**的实参必须逐项相同：
	// 换编码器不该顺带改码率结构。这里与软编那套直接对比，避免把档位表的取值抄进断言里
	// （抄进去就会随档位表改动而漂移，变成"改档位要改测试"）。
	for _, flag := range []string{"-b:v:0", "-maxrate:v:0", "-bufsize:v:0", "-c:a", "-b:a",
		"-var_stream_map", "-master_pl_name", "-hls_time"} {
		if a, b := pairValue(t, soft, flag), pairValue(t, nv, flag); a != b {
			t.Fatalf("实参 %s 不应随编码器变化：软编=%q 硬编=%q", flag, a, b)
		}
	}
}

// TestHLSArgsEncUnknownEncoderFallsBackToSoftware 未知编码器取值必须回退软编，而不是生成非法参数。
//
// 场景：节点配置文件里写了拼错的编码器名，或未来新增了一个本版本还不认识的取值。
// 这时"回退软编"是对的（任务照常完成），"原样拼进参数"是错的（每个任务都失败）。
func TestHLSArgsEncUnknownEncoderFallsBackToSoftware(t *testing.T) {
	ladder := DefaultHLSLadder()[:1]
	dir := "/tmp/out"
	unk := HLSArgsEnc("in.mp4", dir, ladder, 4, false, VideoEncoder("h264_qsv_typo"))
	soft := HLSArgsEnc("in.mp4", dir, ladder, 4, false, EncoderX264)
	if !reflect.DeepEqual(unk, soft) {
		t.Fatalf("未知编码器应完全回退软编参数\n got=%v\nwant=%v", unk, soft)
	}
}

// TestValidVideoEncoder 两个在册编码器必须都有效（双接口：不许把任一条删掉）。
func TestValidVideoEncoder(t *testing.T) {
	for _, e := range []VideoEncoder{EncoderX264, EncoderNVENC} {
		if !ValidVideoEncoder(e) {
			t.Fatalf("%q 应当在册（双接口原则：CPU/GPU 两条路径必须同时存在）", e)
		}
	}
	if ValidVideoEncoder(VideoEncoder("")) || ValidVideoEncoder(VideoEncoder("nope")) {
		t.Fatal("空串与未知取值不应被判为有效")
	}
}

// pairValue 取 "-flag value" 形式的实参值（只取第一个匹配）。
func pairValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	t.Fatalf("参数里没有 %q：%v", flag, args)
	return ""
}
