package geo

import (
	"strings"
	"testing"
)

// 地图筛选栏的分类过滤：三种取值必须**构成一个划分**（两两不相交、并集为全部），
// 否则"全部"的计数会不等于三类之和，用户会以为有数据丢了。

func TestValidKind(t *testing.T) {
	for _, k := range []string{KindAll, KindPhoto, KindVideo, KindPano} {
		if !ValidKind(k) {
			t.Fatalf("%q 应合法", k)
		}
	}
	for _, k := range []string{"", "PHOTO", "video360", "image", "all "} {
		if ValidKind(k) {
			t.Fatalf("%q 不应合法（调用方需先归一化大小写/空白）", k)
		}
	}
}

func TestAppendKindConditions(t *testing.T) {
	t.Run("all 不加任何条件", func(t *testing.T) {
		for _, k := range []string{KindAll, ""} {
			conds, args := appendKind([]string{"1=1"}, []any{"x"}, k)
			if len(conds) != 1 || len(args) != 1 {
				t.Fatalf("kind=%q 不应追加条件，实际 conds=%v args=%v", k, conds, args)
			}
		}
	})

	t.Run("photo 排除 360", func(t *testing.T) {
		conds, _ := appendKind(nil, nil, KindPhoto)
		if len(conds) != 1 {
			t.Fatalf("应恰好 1 条条件，实际 %v", conds)
		}
		got := strings.Join(strings.Fields(conds[0]), "")
		if got != "type='photo'ANDNOTCOALESCE(is_360,false)" {
			t.Fatalf("photo 条件应为 type='photo' 且非 360，实际 %q", conds[0])
		}
	})

	t.Run("video 排除 360", func(t *testing.T) {
		conds, _ := appendKind(nil, nil, KindVideo)
		got := strings.Join(strings.Fields(conds[0]), "")
		if got != "type='video'ANDNOTCOALESCE(is_360,false)" {
			t.Fatalf("video 条件应为 type='video' 且非 360，实际 %q", conds[0])
		}
	})

	t.Run("pano 只按 360 判定（照片与视频都算）", func(t *testing.T) {
		conds, _ := appendKind(nil, nil, KindPano)
		got := strings.Join(strings.Fields(conds[0]), "")
		if got != "COALESCE(is_360,false)" {
			t.Fatalf("pano 条件应为仅 is_360，实际 %q", conds[0])
		}
		// 关键：pano 不得再按 type 过滤，否则会漏掉全景视频或全景照片
		if strings.Contains(got, "type") {
			t.Fatal("pano 不应按 type 过滤（全景照片与全景视频都要算）")
		}
	})

	t.Run("不改动入参与参数（占位符编号不被污染）", func(t *testing.T) {
		// appendKind 不追加任何参数，故既有 args 与后续占位符编号都不受影响 ——
		// 若它偷偷 append 了参数，调用方拼的 $n 会整体错位。
		args := []any{"a", "b"}
		_, got := appendKind(nil, args, KindPhoto)
		if len(got) != 2 {
			t.Fatalf("appendKind 不应追加参数，实际 %v", got)
		}
	})
}
