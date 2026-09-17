package albums

import (
	"strings"
	"testing"
	"time"
)

// 智能条件生成：type 过滤。
func TestBuildCriteriaWhere_Type(t *testing.T) {
	const owner = "11111111-1111-1111-1111-111111111111"
	where, args := buildCriteriaWhere(&Criteria{Type: "photo"}, owner)
	if !strings.Contains(where, "m.deleted_at IS NULL") {
		t.Error("必须排除已删除媒体")
	}
	if !strings.Contains(where, "m.type = $1 AND m.is_360 = false") {
		t.Errorf("photo 条件错误: %s", where)
	}
	if len(args) != 2 || args[0] != "photo" || args[1] != owner {
		t.Errorf("参数错误: %v", args)
	}

	where, args = buildCriteriaWhere(&Criteria{Type: "360"}, owner)
	if !strings.Contains(where, "m.is_360 = true") || len(args) != 1 || args[0] != owner {
		t.Errorf("360 条件错误: %s args=%v", where, args)
	}
}

// 智能条件生成：日期区间（date_to 纯日期按闭区间转次日开区间）。
func TestBuildCriteriaWhere_DateRange(t *testing.T) {
	where, args := buildCriteriaWhere(&Criteria{DateFrom: "2026-01-01", DateTo: "2026-03-31"},
		"11111111-1111-1111-1111-111111111111")
	if !strings.Contains(where, "m.taken_at >= $1") || !strings.Contains(where, "m.taken_at < $2") {
		t.Errorf("日期条件错误: %s", where)
	}
	if len(args) != 3 {
		t.Fatalf("args 应为 日期上界/下界 + 相册属主 共 3 项，实际 %d: %v", len(args), args)
	}
	from := args[0].(time.Time)
	to := args[1].(time.Time)
	if from.Format("2006-01-02") != "2026-01-01" {
		t.Errorf("date_from 解析错误: %v", from)
	}
	// 2026-03-31 闭区间 → < 2026-04-01
	if to.Format("2006-01-02") != "2026-04-01" {
		t.Errorf("date_to 应转为次日开区间: %v", to)
	}
}

// 智能条件生成：place 模糊 + favorites 成员 EXISTS。
func TestBuildCriteriaWhere_PlaceFavorites(t *testing.T) {
	const owner = "11111111-1111-1111-1111-111111111111"
	where, args := buildCriteriaWhere(&Criteria{Place: "杭州", Favorites: true}, owner)
	if !strings.Contains(where, "m.place ILIKE '%' || $1 || '%'") {
		t.Errorf("place 条件错误: %s", where)
	}
	if !strings.Contains(where, "a.type = 'favorites'") {
		t.Errorf("favorites 条件错误: %s", where)
	}
	if len(args) != 2 || args[0] != "杭州" || args[1] != owner {
		t.Errorf("参数错误: %v", args)
	}
}

// 空条件：仅排除已删除 + 收敛到相册属主。
//
// 旧断言是 `where == "m.deleted_at IS NULL" && len(args) == 0` —— 那个预期**本身编码了缺陷**：
// 无条件的智能相册等价于"全站所有媒体"，任何能看到该相册的人都能枚举别人的照片。
// 现在属主约束恒存在，故断言随之收严。
func TestBuildCriteriaWhere_Empty(t *testing.T) {
	const owner = "11111111-1111-1111-1111-111111111111"
	where, args := buildCriteriaWhere(nil, owner)
	const want = "m.deleted_at IS NULL AND m.owner_id = $1"
	if where != want || len(args) != 1 || args[0] != owner {
		t.Errorf("空条件应仍收敛到相册属主：want %q args=[%s]，实际 %q args=%v", want, owner, where, args)
	}
}

// 两级评论约束：父评论自身有 parent 则拒绝。
func TestCheckReplyAllowed(t *testing.T) {
	if !checkReplyAllowed(false) {
		t.Error("回复顶级评论应允许")
	}
	if checkReplyAllowed(true) {
		t.Error("回复回复（第三级）应拒绝")
	}
}

// 首图回填：cover 为空时取首项媒体，非空时保留。
func TestEffectiveCover(t *testing.T) {
	str := func(s string) *string { return &s }
	if got := effectiveCover(str("a"), str("b")); *got != "a" {
		t.Errorf("已有封面不应被回填: %v", *got)
	}
	if got := effectiveCover(nil, str("b")); *got != "b" {
		t.Errorf("空封面应回填首项: %v", *got)
	}
	if got := effectiveCover(nil, nil); got != nil {
		t.Errorf("空相册封面应为 nil: %v", got)
	}
	empty := ""
	if got := effectiveCover(&empty, str("b")); *got != "b" {
		t.Errorf("空字符串封面应回填首项: %v", *got)
	}
}

// kind 映射。
func TestKindMapping(t *testing.T) {
	if kindToType("smart") != "smart" || kindToType("normal") != "manual" || kindToType("") != "manual" {
		t.Error("kindToType 映射错误")
	}
	if typeToKind("manual") != "normal" || typeToKind("smart") != "smart" || typeToKind("favorites") != "favorites" {
		t.Error("typeToKind 映射错误")
	}
}
