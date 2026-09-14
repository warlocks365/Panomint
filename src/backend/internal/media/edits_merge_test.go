package media

import "testing"

// C10 旋转接口「读-改-写」合并语义的纯逻辑单测（不触库）。
// 复现并锁定原缺陷：整体覆盖写入会抹掉另一维度的编辑参数。

func testRect(x, y, w, h float64) *CropRect {
	return &CropRect{X: x, Y: y, W: w, H: h}
}

// 核心回归：op=rotate 不得抹掉库中已有的 crop。
func TestMergeRotateEditPreservesCrop(t *testing.T) {
	cur := &Edits{Rotate: 90, Crop: testRect(0.1, 0.2, 0.5, 0.5)}
	got := MergeRotateEdit(cur, 180)
	if got.Rotate != 180 {
		t.Fatalf("rotate 应为 180，实际 %d", got.Rotate)
	}
	if got.Crop == nil {
		t.Fatal("crop 被抹掉了（原缺陷：POST op=rotate 后库里只剩 {\"rotate\":180}）")
	}
	if *got.Crop != *cur.Crop {
		t.Fatalf("crop 应保留原值，期望 %+v 实际 %+v", *cur.Crop, *got.Crop)
	}
}

// 核心回归：op=crop 不得把 rotate 重置为 0。
func TestMergeCropEditPreservesRotate(t *testing.T) {
	cur := &Edits{Rotate: 270}
	want := testRect(0, 0, 1, 1)
	got := MergeCropEdit(cur, want)
	if got.Rotate != 270 {
		t.Fatalf("rotate 应为 270，实际 %d（原缺陷会重置为 0）", got.Rotate)
	}
	if got.Crop != want {
		t.Fatalf("crop 应指向本次传入的裁剪框，实际 %+v", got.Crop)
	}
}

// 库中尚无编辑参数（edits 为 NULL）时的行为。
func TestMergeWithNilCurrent(t *testing.T) {
	r := MergeRotateEdit(nil, 90)
	if r == nil || r.Rotate != 90 || r.Crop != nil {
		t.Fatalf("nil 基线 + rotate 应为 {rotate:90, crop:nil}，实际 %+v", r)
	}
	c := testRect(0.25, 0.25, 0.5, 0.5)
	g := MergeCropEdit(nil, c)
	if g == nil || g.Rotate != 0 || g.Crop != c {
		t.Fatalf("nil 基线 + crop 应为 {rotate:0, crop:c}，实际 %+v", g)
	}
}

// 合并不得就地修改入参。
func TestMergeDoesNotMutateCurrent(t *testing.T) {
	orig := testRect(0.1, 0.1, 0.5, 0.5)
	cur := &Edits{Rotate: 90, Crop: orig}
	_ = MergeRotateEdit(cur, 180)
	if cur.Rotate != 90 {
		t.Fatalf("入参 cur.Rotate 被就地修改为 %d", cur.Rotate)
	}
	newRect := testRect(0.2, 0.2, 0.4, 0.4)
	_ = MergeCropEdit(cur, newRect)
	if cur.Rotate != 90 || cur.Crop != orig {
		t.Fatalf("入参 cur 被就地修改: %+v", *cur)
	}
}

// 组合编辑序列：PATCH 整体写入 → rotate 合并 → crop 合并，最终两个维度都保留。
func TestMergeSequenceKeepsBothDimensions(t *testing.T) {
	base := &Edits{Rotate: 90, Crop: testRect(0.1, 0.1, 0.8, 0.8)}
	afterRotate := MergeRotateEdit(base, 180)
	afterCrop := MergeCropEdit(afterRotate, testRect(0.0, 0.0, 0.5, 0.5))
	if afterCrop.Rotate != 180 {
		t.Fatalf("最终 rotate 应为 180，实际 %d", afterCrop.Rotate)
	}
	if afterCrop.Crop == nil || *afterCrop.Crop != *testRect(0.0, 0.0, 0.5, 0.5) {
		t.Fatalf("最终 crop 应为最后一次裁剪，实际 %+v", afterCrop.Crop)
	}
}

// NormalizeEdits 边界（rotate 缺省为 0；未知字段/越界裁剪被拒）。
func TestNormalizeEditsBasics(t *testing.T) {
	e, err := NormalizeEdits([]byte(`{}`))
	if err != nil || e.Rotate != 0 || e.Crop != nil {
		t.Fatalf("空对象应为 {0,nil}，实际 %+v err=%v", e, err)
	}
	if _, err := NormalizeEdits([]byte(`{"rotate":45}`)); err == nil {
		t.Fatal("rotate=45 应被拒")
	}
	if _, err := NormalizeEdits([]byte(`{"crop":{"x":0.5,"y":0,"w":0.9,"h":1}}`)); err == nil {
		t.Fatal("越界 crop 应被拒")
	}
	if _, err := NormalizeEdits([]byte(`{"rotate":90,"z":1}`)); err == nil {
		t.Fatal("未知字段应被拒")
	}
}
