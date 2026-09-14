package tags

// applier 的纯逻辑单测：只覆盖 filterAISuggestions（不触碰 DB / ORT）。
//
// ApplyOne 本身需要 pgx 连接，无法在本机验证；把「来源冲突压制」抽成纯函数，
// 正是为了让这条规则可以被单测守住。

import (
	"testing"
	"time"
)

// mustDate 构造固定时刻，避免测试依赖当前时间。
func mustDate(t *testing.T, y int, m time.Month, d int) time.Time {
	t.Helper()
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

func TestFilterAISuggestionsDropsConflictingGroup(t *testing.T) {
	sugg := []Suggestion{
		{Tag: "秋天", Group: "季节", Confidence: 0.4304},
		{Tag: "森林", Group: "", Confidence: 0.4460},
		{Tag: "黄昏", Group: "时段", Confidence: 0.4371},
	}
	hs := []HeuristicTag{{Tag: "夏天", Group: "季节", Confidence: 1.0, Rule: "season"}}

	got := filterAISuggestions(sugg, hs)
	if len(got) != 2 {
		t.Fatalf("应丢弃与启发式同组的「秋天」，保留 2 条，实得 %d：%+v", len(got), got)
	}
	for _, s := range got {
		if s.Tag == "秋天" {
			t.Errorf("「秋天」与启发式的「夏天」同属季节组，应被丢弃")
		}
	}
	if got[0].Tag != "森林" || got[1].Tag != "黄昏" {
		t.Errorf("保留顺序应保持原相对顺序，实得 %+v", got)
	}
}

// 启发式只产出 Group 为空的标签时（城市 / 全景 / 视频），不得压制任何 AI 建议。
func TestFilterAISuggestionsKeepsAllWhenNoGroupOccupied(t *testing.T) {
	sugg := []Suggestion{
		{Tag: "秋天", Group: "季节"},
		{Tag: "城市", Group: ""},
	}
	hs := []HeuristicTag{
		{Tag: "北京·圆明园-遗址", Group: "", Confidence: 0.9},
		{Tag: "全景", Group: "", Confidence: 1.0},
		{Tag: "视频", Group: "", Confidence: 1.0},
	}
	if got := filterAISuggestions(sugg, hs); len(got) != 2 {
		t.Fatalf("启发式未占用任何互斥组时应全部保留，实得 %d：%+v", len(got), got)
	}
}

// 启发式生产出的季节标签与 AI 一致时：丢弃 AI 的那条，不改变最终标签集合
// （启发式置信度 1.0 且直接 confirmed，本来就应当胜出）。
func TestFilterAISuggestionsSameSeasonTag(t *testing.T) {
	sugg := []Suggestion{{Tag: "夏天", Group: "季节", Confidence: 0.4140}}
	hs := []HeuristicTag{{Tag: "夏天", Group: "季节", Confidence: 1.0}}
	if got := filterAISuggestions(sugg, hs); len(got) != 0 {
		t.Fatalf("AI 与启发式同季时应丢弃 AI 条目，实得 %+v", got)
	}
}

func TestFilterAISuggestionsEmptyInputs(t *testing.T) {
	hs := []HeuristicTag{{Tag: "冬天", Group: "季节"}}
	if got := filterAISuggestions(nil, hs); got != nil {
		t.Errorf("无 AI 建议时应返回 nil，实得 %+v", got)
	}
	sugg := []Suggestion{{Tag: "秋天", Group: "季节"}}
	if got := filterAISuggestions(sugg, nil); len(got) != 1 {
		t.Errorf("无启发式时不应过滤，实得 %+v", got)
	}
}

// 回归守卫：启发式的季节标签必须带 Group="季节"，否则来源冲突压制会静默失效。
func TestHeuristicSeasonCarriesGroup(t *testing.T) {
	got := Heuristics(MediaMeta{ID: "m", TakenAt: mustDate(t, 2026, 6, 10)})
	found := false
	for _, h := range got {
		if h.Tag == "夏天" {
			found = true
			if h.Group != "季节" {
				t.Errorf("启发式季节标签的 Group 应为「季节」，实得 %q", h.Group)
			}
		}
	}
	if !found {
		t.Fatalf("2026-06 应产出「夏天」，实得 %+v", got)
	}
}

// 回归守卫：启发式的城市 / 全景 / 视频 必须保持 Group 为空，否则会误压制 AI 建议。
func TestHeuristicNonSeasonTagsHaveNoGroup(t *testing.T) {
	got := Heuristics(MediaMeta{
		ID: "m1", Place: "北京市", Is360: true, Type: "video",
		TakenAt: mustDate(t, 2026, 6, 10),
	})
	for _, h := range got {
		if h.Tag == "全景" || h.Tag == "视频" || h.Tag == "北京市" {
			if h.Group != "" {
				t.Errorf("启发式标签 %q 的 Group 应为空（否则会误压制 AI 建议），实得 %q", h.Tag, h.Group)
			}
		}
	}
}
