package ffmpeg

import (
	"reflect"
	"strings"
	"testing"
)

// TestEditFilterOrderInvariant 守住**顺序契约**：
// 必须「先裁剪（按未旋转方向）后旋转」，与前端保持一致
// （前端 = clip-path 裁剪 + CSS rotate，见 MediaViewer.vue）。
// 顺序反过来会导致旋转后裁剪框错位。
func TestEditFilterOrderInvariant(t *testing.T) {
	got := EditFilter(90, true, 0.1, 0.2, 0.8, 0.6)
	want := "crop=iw*0.8:ih*0.6:iw*0.1:ih*0.2,transpose=1"
	if got != want {
		t.Fatalf("裁剪+旋转滤镜链错误\n got %q\nwant %q", got, want)
	}
	if strings.Index(got, "crop=") > strings.Index(got, "transpose=") {
		t.Error("裁剪必须排在旋转之前")
	}
}

func TestEditFilterRotations(t *testing.T) {
	cases := map[int]string{
		0:   "",
		90:  "transpose=1",
		180: "transpose=2,transpose=2",
		270: "transpose=2",
		360: "",
		-90: "transpose=2", // 负数归一化
	}
	for deg, want := range cases {
		if got := EditFilter(deg, false, 0, 0, 0, 0); got != want {
			t.Errorf("EditFilter(%d) = %q，期望 %q", deg, got, want)
		}
	}
}

func TestEditFilterCropOnlyAndGuards(t *testing.T) {
	if got := EditFilter(0, true, 0.25, 0.25, 0.5, 0.5); got != "crop=iw*0.5:ih*0.5:iw*0.25:ih*0.25" {
		t.Errorf("仅裁剪滤镜错误：%q", got)
	}
	// 非法裁剪（宽或高为 0）应被忽略，避免生成非法滤镜导致 ffmpeg 失败
	if got := EditFilter(0, true, 0, 0, 0, 0.5); got != "" {
		t.Errorf("零宽裁剪应被忽略，实得 %q", got)
	}
}

// TestThumbnailArgsEdited 确保 editFilter 为空时与原行为完全一致，
// 非空时插入在 scale 之前。
func TestThumbnailArgsEdited(t *testing.T) {
	base := ThumbnailArgs("in.mp4", "out.webp", ThumbMD, 0)
	edited := ThumbnailArgsEdited("in.mp4", "out.webp", ThumbMD, 0, "")
	if !reflect.DeepEqual(base, edited) {
		t.Errorf("空 editFilter 应与 ThumbnailArgs 完全等价\n base %v\n edit %v", base, edited)
	}
	withEdit := ThumbnailArgsEdited("in.mp4", "out.webp", ThumbMD, 0, "transpose=1")
	vf := ""
	for i, a := range withEdit {
		if a == "-vf" && i+1 < len(withEdit) {
			vf = withEdit[i+1]
		}
	}
	if !strings.HasPrefix(vf, "transpose=1,scale=") {
		t.Errorf("编辑滤镜必须插在 scale 之前，实得 %q", vf)
	}
}
