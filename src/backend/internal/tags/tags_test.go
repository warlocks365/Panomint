package tags

import (
	"math"
	"testing"
	"time"

	"panoalbum/internal/embed"
	"panoalbum/internal/vecutil"
)

const dim = embed.EmbeddingDim

// unit 第 i 维为 1 的单位向量。
func unit(i int) []float32 {
	v := make([]float32, dim)
	v[i] = 1
	return v
}

// mix 单位向量 0..n-1 之和的 L2 归一化（与各标签等相似）。
func mix(n int) []float32 {
	v := make([]float32, dim)
	for i := 0; i < n; i++ {
		v[i] = 1
	}
	return vecutil.L2Normalize(v)
}

func newTestClassifier(minSim, topR float64, maxTags int, labels []labelVec) *Classifier {
	return &Classifier{family: embed.FamilyChineseCLIP, minSim: minSim, topR: topR, maxTags: maxTags, labels: labels}
}

func TestSuggestThresholdAndGroupMutex(t *testing.T) {
	// 同组两个标签，图像与第 0 个完全一致 → 只保留最高且同组互斥。
	c := newTestClassifier(0.35, 0.88, 3, []labelVec{
		{label: "海滩", group: "地貌", vec: unit(0)},
		{label: "山地", group: "地貌", vec: unit(1)},
		{label: "猫", group: "动物", vec: unit(2)},
	})
	got := c.Suggest(unit(0))
	if len(got) != 1 || got[0].Tag != "海滩" {
		t.Fatalf("期望仅命中「海滩」，实得 %+v", got)
	}
	if math.Abs(got[0].Confidence-1.0) > 1e-6 {
		t.Fatalf("置信度应为 1.0，实得 %v", got[0].Confidence)
	}
}

func TestSuggestEqualScoresGroupMutex(t *testing.T) {
	c := newTestClassifier(0.35, 0.88, 3, []labelVec{
		{label: "A", group: "g", vec: unit(0)},
		{label: "B", group: "g", vec: unit(1)},
	})
	got := c.Suggest(mix(2)) // 与 A、B 等相似
	if len(got) != 1 {
		t.Fatalf("同组应只留 1 个，实得 %d：%+v", len(got), got)
	}
}

func TestSuggestMaxTags(t *testing.T) {
	c := newTestClassifier(0.10, 0.88, 3, []labelVec{
		{label: "A", vec: unit(0)},
		{label: "B", vec: unit(1)},
		{label: "C", vec: unit(2)},
		{label: "D", vec: unit(3)},
		{label: "E", vec: unit(4)},
	})
	got := c.Suggest(mix(5))
	if len(got) != 3 {
		t.Fatalf("每图上限应为 3，实得 %d", len(got))
	}
}

func TestSuggestBelowFloor(t *testing.T) {
	// 绝对下限 0.9：即便 top1 也是 0.5，仍应全灭。
	c := newTestClassifier(0.9, 0.88, 3, []labelVec{{label: "A", vec: unit(0)}, {label: "B", vec: unit(1)}})
	if got := c.Suggest(mix(2)); len(got) != 0 {
		t.Fatalf("低于下限不应产出，实得 %+v", got)
	}
}

func TestSuggestDimensionMismatch(t *testing.T) {
	c := newTestClassifier(0.1, 0.88, 3, []labelVec{{label: "A", vec: []float32{1, 0, 0}}})
	if got := c.Suggest(unit(0)); len(got) != 0 {
		t.Fatalf("维度不符应跳过，实得 %+v", got)
	}
}

func TestCityOf(t *testing.T) {
	cases := map[string]string{
		"北京市朝阳区":       "北京市",
		"杭州市":          "杭州市",
		"Kyoto, Japan": "Kyoto",
		"":             "",
		"   ":          "",
	}
	for in, want := range cases {
		if got := cityOf(in); got != want {
			t.Errorf("cityOf(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestHeuristics(t *testing.T) {
	m := MediaMeta{ID: "m1", Place: "北京市", Is360: true, Type: "video", TakenAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)}
	got := Heuristics(m)
	want := map[string]bool{"全景": true, "视频": true, "北京市": true, "冬天": true}
	if len(got) != len(want) {
		t.Fatalf("期望 %d 条，实得 %d：%+v", len(want), len(got), got)
	}
	for _, h := range got {
		if !want[h.Tag] {
			t.Errorf("意外标签 %q", h.Tag)
		}
	}
}

func TestSeasonOf(t *testing.T) {
	cases := map[time.Month]string{
		time.January: "冬天", time.April: "春天", time.July: "夏天", time.October: "秋天", time.December: "冬天",
	}
	for m, want := range cases {
		if got := seasonOf(time.Date(2026, m, 10, 0, 0, 0, 0, time.UTC)); got != want {
			t.Errorf("%v 期望 %q，实得 %q", m, want, got)
		}
	}
}

func TestParseVectorLiteral(t *testing.T) {
	v, err := ParseVectorLiteral("[1.5,-2,3]")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(v) != 3 || v[0] != 1.5 || v[1] != -2 || v[2] != 3 {
		t.Fatalf("解析结果错误: %+v", v)
	}
	if _, err := ParseVectorLiteral("[]"); err == nil {
		t.Fatal("空向量应报错")
	}
}

func TestDefaultVocabSize(t *testing.T) {
	n := DefaultVocab().LabelCount()
	if n < 80 || n > 150 {
		t.Fatalf("词表规模应在 80~150，实得 %d", n)
	}
}
