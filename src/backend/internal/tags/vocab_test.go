package tags

// 词表自洽性单测：不依赖 ORT / DB。
//
// 词表是打标质量的地基 —— 一个缺 EN、重名或单成员互斥组的条目，
// 会在运行时静默产生「向量失去意义」「同义标签刷屏」等难以定位的问题，
// 所以全部在编译期外的单测里守住。

import (
	"strings"
	"testing"
)

// vocabGroups 收集所有非空互斥组及其成员。
func vocabGroups(v *Vocab) map[string][]string {
	m := map[string][]string{}
	for _, c := range v.Classes {
		for _, td := range c.Tags {
			if td.Group != "" {
				m[td.Group] = append(m[td.Group], td.Label)
			}
		}
	}
	return m
}

func containsLabel(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestDefaultVocabSelfConsistency(t *testing.T) {
	v := DefaultVocab()
	seenLabel := map[string]string{}
	seenEN := map[string]string{}
	groupMembers := map[string]int{}

	for _, c := range v.Classes {
		if strings.TrimSpace(c.Name) == "" {
			t.Error("存在空 Class 名")
		}
		if len(c.Tags) == 0 {
			t.Errorf("Class %q 没有任何标签", c.Name)
		}
		for _, td := range c.Tags {
			where := c.Name + "/" + td.Label
			if td.Label == "" {
				t.Errorf("Class %q 存在空 Label", c.Name)
				continue
			}
			if td.Label != strings.TrimSpace(td.Label) {
				t.Errorf("%s 的 Label 有首尾空白", where)
			}
			if strings.ContainsAny(td.Label, "%\n\r") {
				t.Errorf("%s 的 Label 含非法字符（%% 会与提示词模板占位符冲突）", where)
			}
			if prev, dup := seenLabel[td.Label]; dup {
				t.Errorf("Label 重复：%q 同时出现在 %s 与 %s", td.Label, prev, where)
			}
			seenLabel[td.Label] = where

			if td.EN == "" {
				t.Errorf("%s 缺少 EN（英文族文本塔不认中文，缺 EN 会让该标签的向量失去意义）", where)
			} else {
				if td.EN != strings.TrimSpace(td.EN) {
					t.Errorf("%s 的 EN 有首尾空白", where)
				}
				if strings.ContainsAny(td.EN, "%\n\r") {
					t.Errorf("%s 的 EN 含非法字符", where)
				}
				if prev, dup := seenEN[td.EN]; dup {
					t.Errorf("EN 重复：%q 同时出现在 %s 与 %s", td.EN, prev, where)
				}
				seenEN[td.EN] = where
			}

			if td.Group != "" {
				if td.Group != strings.TrimSpace(td.Group) {
					t.Errorf("%s 的 Group 有首尾空白", where)
				}
				groupMembers[td.Group]++
			}
		}
	}

	for g, n := range groupMembers {
		if n < 2 {
			t.Errorf("互斥组 %q 只有 %d 个成员：单成员互斥组没有意义，应去掉 Group 或补齐成员", g, n)
		}
	}
}

func TestDefaultVocabLabelCount(t *testing.T) {
	got := DefaultVocab().LabelCount()
	if got != 108 {
		t.Fatalf("词表应为 108 个（2026-09 二次治理：114 → 108，删除 6 个低精度标签、改名 3 个，理由见 DefaultVocab 注释），实得 %d", got)
	}
}

func TestDefaultVocabClassCounts(t *testing.T) {
	want := map[string]int{"场景": 46, "物体": 44, "事件": 18}
	for _, c := range DefaultVocab().Classes {
		w, ok := want[c.Name]
		if !ok {
			t.Errorf("出现未预期的 Class %q", c.Name)
			continue
		}
		if len(c.Tags) != w {
			t.Errorf("Class %q 应有 %d 个标签，实得 %d", c.Name, w, len(c.Tags))
		}
		delete(want, c.Name)
	}
	for name := range want {
		t.Errorf("缺少 Class %q", name)
	}
}

// 媒体属性（拍摄几何 / 文件类型）由启发式确定性产出，不应出现在 AI 词表里：
// 作为文本标签它们会匹配任何广阔视野或任意单帧，是纯粹的误命中来源。
func TestDefaultVocabExcludesMediaAttributes(t *testing.T) {
	for _, name := range []string{"全景", "视频"} {
		for _, c := range DefaultVocab().Classes {
			for _, td := range c.Tags {
				if td.Label == name {
					t.Errorf("标签 %q 是媒体属性（由启发式 is_360 / type=video 以置信度 1.0 产出），不应出现在 AI 词表里", name)
				}
			}
		}
	}
}

// 季节组必须与 heuristic.go 的 seasonOf() 对齐：启发式会直接产出这 4 个季节名，
// AI 侧若不一致就会出现同一张照片挂着两个互相矛盾的季节标签。
func TestDefaultVocabSeasonGroupMatchesHeuristic(t *testing.T) {
	g := vocabGroups(DefaultVocab())["季节"]
	want := []string{"春天", "夏天", "秋天", "冬天"}
	if len(g) != len(want) {
		t.Fatalf("季节组应恰为 %v，实得 %v", want, g)
	}
	for _, w := range want {
		if !containsLabel(g, w) {
			t.Errorf("季节组缺少 %q，实得 %v（必须与 heuristic.go 的 seasonOf() 对齐）", w, g)
		}
	}
}

// 治理后的互斥关系：夜景 并入「时段」以消除与「夜晚」的同义重复。
func TestDefaultVocabTimeAndWeatherGroups(t *testing.T) {
	groups := vocabGroups(DefaultVocab())

	shiduan := groups["时段"]
	if !containsLabel(shiduan, "夜晚") || !containsLabel(shiduan, "夜景") {
		t.Errorf("「夜景」应并入「时段」组以消除与「夜晚」的同义重复，实得 %v", shiduan)
	}

	tianqi := groups["天气"]
	for _, x := range []string{"晴天", "多云", "阴天", "雨天", "雪天", "雾天"} {
		if !containsLabel(tianqi, x) {
			t.Errorf("「天气」互斥组缺少 %q，实得 %v", x, tianqi)
		}
	}
}

// 低精度标签治理（2026-09 二次）：删 6 个「逐张目视核对准确率 ≤29%」的噪声标签，改 3 个「名实不符」的标签。
// 一个 10 次里有 7 次错的标签对用户是负价值，故整条删除而非调阈值；改名是让措辞与模型实际能识别的东西对齐
// （如模型从不识别冲浪者，只认海浪 → 「冲浪」改「海浪」）。本测试是回归守卫，防止它们被无声地加回。
func TestDefaultVocabExcludesLowPrecisionTags(t *testing.T) {
	removed := []string{"彩虹", "旗帜", "霓虹灯", "展览", "潜水", "天际线", "冰川", "云海", "冲浪"}
	added := []string{"雪山", "云雾", "海浪"}

	labels := map[string]bool{}
	for _, c := range DefaultVocab().Classes {
		for _, td := range c.Tags {
			labels[td.Label] = true
		}
	}
	for _, x := range removed {
		if labels[x] {
			t.Errorf("标签 %q 不应出现在词表里（低精度噪声标签已删 / 已改名）", x)
		}
	}
	for _, x := range added {
		if !labels[x] {
			t.Errorf("标签 %q 应由低精度标签改名而来并保留，实得词表缺少它", x)
		}
	}

	// 改名必须连 EN 一起改，否则英文族文本塔仍编码旧语义。
	wantEN := map[string]string{"雪山": "snow mountain", "云雾": "mist", "海浪": "ocean waves"}
	for _, c := range DefaultVocab().Classes {
		for _, td := range c.Tags {
			if en, ok := wantEN[td.Label]; ok && td.EN != en {
				t.Errorf("标签 %q 的 EN 应为 %q，实得 %q", td.Label, en, td.EN)
			}
		}
	}
}

