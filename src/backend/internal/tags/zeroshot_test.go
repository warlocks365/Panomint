package tags

// zeroshot 的纯逻辑单测：不依赖 ORT / DB / 网络（本机 Windows 无法运行 ONNX）。
//
// 覆盖：
//   - 提示词模板的族隔离与构造（中文族不出现英文模板，反之亦然）
//   - 多模板平均 + 逐条 L2 归一化的数值正确性（用构造向量验证）
//   - 入选门槛 MinTop1 的判决、解析边界与「派生时保留」
//   - 其余阈值解析函数的边界行为，并守住「生产默认值不被误改」

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"panoalbum/internal/embed"
)

// fakeEncoder 按提示词文本查表返回向量的假文本塔，并记录被调用的提示词。
type fakeEncoder struct {
	fam   embed.ModelFamily
	vecs  map[string][]float32
	calls []string
}

func (f *fakeEncoder) Family() embed.ModelFamily { return f.fam }

func (f *fakeEncoder) EncodeText(_ context.Context, text string) ([]float32, error) {
	f.calls = append(f.calls, text)
	v, ok := f.vecs[text]
	if !ok {
		return nil, fmt.Errorf("假编码器未预置提示词 %q 的向量", text)
	}
	return v, nil
}

// hasCJK 是否含 CJK 统一表意文字（用于断言「英文族模板里没有中文」）。
func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func assertUnitNorm(t *testing.T, v []float32) {
	t.Helper()
	var ss float64
	for _, x := range v {
		ss += float64(x) * float64(x)
	}
	if n := math.Sqrt(ss); math.Abs(n-1) > 1e-5 {
		t.Errorf("结果应为单位向量，实得模长 %.6f", n)
	}
}

// 1. 提示词模板：中文族只用中文模板
func TestLabelPromptsChineseFamily(t *testing.T) {
	td := TagDef{Label: "彩虹", EN: "rainbow"}
	got := labelPrompts(td, embed.FamilyChineseCLIP)
	if len(got) != len(chineseCLIPTemplates) {
		t.Fatalf("模板数应为 %d，实得 %d：%+v", len(chineseCLIPTemplates), len(got), got)
	}
	if len(got) < 3 {
		t.Fatalf("多模板集成至少应有 3 条模板，实得 %d：%+v", len(got), got)
	}
	for i, p := range got {
		if !strings.Contains(p, "彩虹") {
			t.Errorf("第 %d 条模板 %q 未包含标签名", i, p)
		}
		if strings.Contains(p, "rainbow") {
			t.Errorf("第 %d 条中文模板混入了英文名：%q", i, p)
		}
		if strings.Contains(p, "photo") || strings.Contains(p, "picture") {
			t.Errorf("第 %d 条中文模板混入了英文模板：%q", i, p)
		}
		if strings.Contains(p, "%") {
			t.Errorf("第 %d 条模板仍有未替换的占位符：%q", i, p)
		}
	}
}

// 2. 提示词模板：英文族只用英文模板（不得出现 CJK）
func TestLabelPromptsEnglishFamily(t *testing.T) {
	td := TagDef{Label: "彩虹", EN: "rainbow"}
	got := labelPrompts(td, embed.FamilyCLIP)
	if len(got) != len(clipCLIPTemplates) {
		t.Fatalf("模板数应为 %d，实得 %d：%+v", len(clipCLIPTemplates), len(got), got)
	}
	for i, p := range got {
		if !strings.Contains(p, "rainbow") {
			t.Errorf("第 %d 条模板 %q 未包含英文名", i, p)
		}
		if hasCJK(p) {
			t.Errorf("第 %d 条英文模板含中文（英文塔不认中文提示词）：%q", i, p)
		}
		if strings.Contains(p, "%") {
			t.Errorf("第 %d 条模板仍有未替换的占位符：%q", i, p)
		}
	}
}

// 3. 英文名为空时回退中文标签，且仍不得与英文模板交叉
func TestLabelPromptsEnglishFallback(t *testing.T) {
	td := TagDef{Label: "xyz", EN: ""}
	got := labelPrompts(td, embed.FamilyCLIP)
	for i, p := range got {
		if !strings.Contains(p, "xyz") {
			t.Errorf("第 %d 条模板 %q 未回退使用 Label", i, p)
		}
		if hasCJK(p) {
			t.Errorf("英文族模板不得含中文：%q", p)
		}
	}
}

// 4. 空模型族（零值）走中文分支，与改动前的 if 方向一致
func TestLabelPromptsDefaultFamilyIsChinese(t *testing.T) {
	td := TagDef{Label: "猫", EN: "cat"}
	got := labelPrompts(td, embed.ModelFamily(""))
	if len(got) != len(chineseCLIPTemplates) {
		t.Fatalf("零值族应走中文分支，模板数应为 %d，实得 %d", len(chineseCLIPTemplates), len(got))
	}
	for i, p := range got {
		if !strings.Contains(p, "猫") || strings.Contains(p, "cat") {
			t.Errorf("第 %d 条模板不符合中文分支预期：%q", i, p)
		}
	}
}

// 5. 关键数值测试：逐条 L2 归一化后再平均。
//
// 构造：第 0 条模板的模长放大 100 倍，其余为 1。若实现退化为「原始向量求和再归一化」，
// 结果会几乎等于 e0；只有逐条归一化才能得到 5 个单位向量的等权质心（每维 1/√5）。
func TestEncodeLabelNormalizesEachTemplate(t *testing.T) {
	td := TagDef{Label: "彩虹", EN: "rainbow"}
	prompts := labelPrompts(td, embed.FamilyChineseCLIP)
	const width = 8
	if len(prompts) > width {
		t.Fatalf("测试宽度 %d 不足以容纳 %d 条模板", width, len(prompts))
	}
	vecs := make(map[string][]float32, len(prompts))
	for i, p := range prompts {
		v := make([]float32, width)
		v[i] = 1
		if i == 0 {
			v[i] = 100
		}
		vecs[p] = v
	}
	enc := &fakeEncoder{fam: embed.FamilyChineseCLIP, vecs: vecs}

	got, err := encodeLabel(context.Background(), enc, embed.FamilyChineseCLIP, td)
	if err != nil {
		t.Fatalf("encodeLabel 失败: %v", err)
	}
	if len(got) != width {
		t.Fatalf("结果维度应为 %d，实得 %d", width, len(got))
	}
	want := float32(1 / math.Sqrt(float64(len(prompts))))
	for i := 0; i < len(prompts); i++ {
		if d := math.Abs(float64(got[i] - want)); d > 1e-5 {
			t.Errorf("第 %d 维应为 %.6f，实得 %.6f（偏差 %.2e）——说明未逐条归一化再平均",
				i, want, got[i], d)
		}
	}
	for i := len(prompts); i < width; i++ {
		if math.Abs(float64(got[i])) > 1e-6 {
			t.Errorf("第 %d 维应为 0，实得 %v", i, got[i])
		}
	}
	assertUnitNorm(t, got)

	// 不得污染调用方传入的向量（必须先拷贝再归一化）
	if vecs[prompts[0]][0] != 100 {
		t.Errorf("encodeLabel 修改了编码器返回的原始向量：%v", vecs[prompts[0]])
	}
}

// 6. encodeLabel 只调用本族的模板，不交叉
func TestEncodeLabelUsesOnlyFamilyTemplates(t *testing.T) {
	td := TagDef{Label: "猫", EN: "cat"}

	zh := map[string][]float32{}
	for _, p := range labelPrompts(td, embed.FamilyChineseCLIP) {
		v := make([]float32, 4)
		v[0] = 1
		zh[p] = v
	}
	encZH := &fakeEncoder{fam: embed.FamilyChineseCLIP, vecs: zh}
	got, err := encodeLabel(context.Background(), encZH, embed.FamilyChineseCLIP, td)
	if err != nil {
		t.Fatalf("中文族编码失败: %v", err)
	}
	assertUnitNorm(t, got)
	if len(encZH.calls) != len(chineseCLIPTemplates) {
		t.Errorf("中文族应调用 %d 次文本编码，实得 %d", len(chineseCLIPTemplates), len(encZH.calls))
	}
	for _, p := range encZH.calls {
		if !strings.Contains(p, "猫") || strings.Contains(p, "cat") {
			t.Errorf("中文族调用了不符合预期的提示词：%q", p)
		}
	}

	en := map[string][]float32{}
	for _, p := range labelPrompts(td, embed.FamilyCLIP) {
		v := make([]float32, 4)
		v[0] = 1
		en[p] = v
	}
	encEN := &fakeEncoder{fam: embed.FamilyCLIP, vecs: en}
	got, err = encodeLabel(context.Background(), encEN, embed.FamilyCLIP, td)
	if err != nil {
		t.Fatalf("英文族编码失败: %v", err)
	}
	assertUnitNorm(t, got)
	if len(encEN.calls) != len(clipCLIPTemplates) {
		t.Errorf("英文族应调用 %d 次文本编码，实得 %d", len(clipCLIPTemplates), len(encEN.calls))
	}
	for _, p := range encEN.calls {
		if !strings.Contains(p, "cat") || hasCJK(p) {
			t.Errorf("英文族调用了不符合预期的提示词：%q", p)
		}
	}
}

// 7. 维度不一致的模板被跳过，不污染结果
func TestEncodeLabelSkipsMismatchedDim(t *testing.T) {
	td := TagDef{Label: "A", EN: "A"}
	prompts := labelPrompts(td, embed.FamilyChineseCLIP)
	vecs := make(map[string][]float32, len(prompts))
	for i, p := range prompts {
		if i == 1 {
			vecs[p] = []float32{1, 2, 3} // 维度不同，应被跳过
			continue
		}
		v := make([]float32, 4)
		v[0] = 1
		vecs[p] = v
	}
	enc := &fakeEncoder{fam: embed.FamilyChineseCLIP, vecs: vecs}
	got, err := encodeLabel(context.Background(), enc, embed.FamilyChineseCLIP, td)
	if err != nil {
		t.Fatalf("encodeLabel 失败: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("维度应由首个可用模板决定（4），实得 %d", len(got))
	}
	assertUnitNorm(t, got)
}

// 8. 入选门槛判决：top1 不达标 → 「不属于任何标签」
func TestSuggestMinTop1GatesNoTag(t *testing.T) {
	labels := []labelVec{
		{label: "A", vec: unit(0)},
		{label: "B", vec: unit(1)},
	}
	base := &Classifier{family: embed.FamilyChineseCLIP, minSim: 0.1, topR: 0.88, maxTags: 3, labels: labels}
	vec := mix(2) // 与 A、B 的相似度均为 1/√2 ≈ 0.7071
	if got := base.Suggest(vec); len(got) != 2 {
		t.Fatalf("未设门槛时应命中 2 个，实得 %d：%+v", len(got), got)
	}

	gated := *base
	gated.minTop1 = 0.8 // 高于 0.7071
	if got := gated.Suggest(vec); len(got) != 0 {
		t.Fatalf("top1 低于入选门槛时应返回空（无标签），实得 %d：%+v", len(got), got)
	}

	gated.minTop1 = 0.7 // 低于 0.7071
	if got := gated.Suggest(vec); len(got) != 2 {
		t.Fatalf("top1 达标时应正常产出，实得 %d：%+v", len(got), got)
	}

	// 门槛默认关闭（0）时行为与改动前一致
	if got := base.Suggest(vec); len(got) != 2 {
		t.Fatalf("门槛关闭时不应影响产出，实得 %d", len(got))
	}
}

// 9. MinTop1 解析边界：显式优先 → env → 默认 0（关闭）
func TestResolveMinTop1Boundaries(t *testing.T) {
	t.Setenv("TAG_MIN_TOP1", "")
	if got := resolveMinTop1(0.55); got != 0.55 {
		t.Errorf("显式值应优先，期望 0.55，实得 %v", got)
	}
	if defaultMinTop1 != 0 {
		t.Fatalf("本轮入选门槛必须默认关闭（0），实得 %v", defaultMinTop1)
	}
	if got := resolveMinTop1(0); got != 0 {
		t.Errorf("env 为空时应返回默认关闭（0），实得 %v", got)
	}
	for _, bad := range []string{"abc", "0.01", "1.5", "0", "-1"} {
		t.Setenv("TAG_MIN_TOP1", bad)
		if got := resolveMinTop1(0); got != defaultMinTop1 {
			t.Errorf("非法 env %q 应被忽略并回退默认 %v，实得 %v", bad, defaultMinTop1, got)
		}
	}
	t.Setenv("TAG_MIN_TOP1", "0.42")
	if got := resolveMinTop1(0); got != 0.42 {
		t.Errorf("合法 env 应采用 0.42，实得 %v", got)
	}
}

// 10. 其余阈值解析边界 + 生产默认值守卫
func TestResolveThresholdBoundsAndProductionDefaults(t *testing.T) {
	if defaultMinSimChineseCLIP != 0.40 {
		t.Errorf("chinese-clip 绝对下限本轮不得改动，期望 0.40，实得 %v", defaultMinSimChineseCLIP)
	}
	if defaultMinSimCLIP != 0.24 {
		t.Errorf("clip 绝对下限本轮不得改动，期望 0.24，实得 %v", defaultMinSimCLIP)
	}
	if defaultTopRatio != 0.88 {
		t.Errorf("相对阈值本轮不得改动，期望 0.88，实得 %v", defaultTopRatio)
	}
	if defaultMaxTags != 3 {
		t.Errorf("每图上限本轮不得改动，期望 3，实得 %d", defaultMaxTags)
	}

	t.Setenv("TAG_MIN_SIM", "")
	if got := resolveMinSim(0, embed.FamilyChineseCLIP); got != defaultMinSimChineseCLIP {
		t.Errorf("chinese-clip 默认下限期望 %v，实得 %v", defaultMinSimChineseCLIP, got)
	}
	if got := resolveMinSim(0, embed.FamilyCLIP); got != defaultMinSimCLIP {
		t.Errorf("clip 默认下限期望 %v，实得 %v", defaultMinSimCLIP, got)
	}
	if got := resolveMinSim(0, embed.ModelFamily("")); got != defaultMinSimChineseCLIP {
		t.Errorf("空族应按 chinese-clip 处理，实得 %v", got)
	}
	if got := resolveMinSim(0.5, embed.FamilyCLIP); got != 0.5 {
		t.Errorf("显式下限应优先，期望 0.5，实得 %v", got)
	}
	t.Setenv("TAG_MIN_SIM", "0.01")
	if got := resolveMinSim(0, embed.FamilyChineseCLIP); got != defaultMinSimChineseCLIP {
		t.Errorf("非法 TAG_MIN_SIM 应被忽略，实得 %v", got)
	}
	t.Setenv("TAG_MIN_SIM", "0.42")
	if got := resolveMinSim(0, embed.FamilyChineseCLIP); got != 0.42 {
		t.Errorf("合法 TAG_MIN_SIM 应采用 0.42，实得 %v", got)
	}

	t.Setenv("TAG_TOP_RATIO", "")
	if got := resolveTopRatio(0); got != defaultTopRatio {
		t.Errorf("相对阈值默认期望 %v，实得 %v", defaultTopRatio, got)
	}
	if got := resolveTopRatio(0.93); got != 0.93 {
		t.Errorf("显式相对阈值应优先，期望 0.93，实得 %v", got)
	}
	for _, bad := range []string{"0.05", "1.5", "abc", "0"} {
		t.Setenv("TAG_TOP_RATIO", bad)
		if got := resolveTopRatio(0); got != defaultTopRatio {
			t.Errorf("非法 TAG_TOP_RATIO=%q 应回退默认，实得 %v", bad, got)
		}
	}
	t.Setenv("TAG_TOP_RATIO", "0.95")
	if got := resolveTopRatio(0); got != 0.95 {
		t.Errorf("合法 TAG_TOP_RATIO 应采用 0.95，实得 %v", got)
	}

	t.Setenv("TAG_MAX_PER_MEDIA", "")
	if got := resolveMaxTags(0); got != defaultMaxTags {
		t.Errorf("每图上限默认期望 %d，实得 %d", defaultMaxTags, got)
	}
	if got := resolveMaxTags(2); got != 2 {
		t.Errorf("显式上限应优先，期望 2，实得 %d", got)
	}
	for _, bad := range []string{"0", "21", "abc", "-3"} {
		t.Setenv("TAG_MAX_PER_MEDIA", bad)
		if got := resolveMaxTags(0); got != defaultMaxTags {
			t.Errorf("非法 TAG_MAX_PER_MEDIA=%q 应回退默认，实得 %d", bad, got)
		}
	}
	t.Setenv("TAG_MAX_PER_MEDIA", "2")
	if got := resolveMaxTags(0); got != 2 {
		t.Errorf("合法 TAG_MAX_PER_MEDIA 应采用 2，实得 %d", got)
	}
}

// 11. 派生分类器时入选门槛必须保留（标定扫描不得静默改变判决行为）
func TestWithThresholdsPreservesMinTop1(t *testing.T) {
	t.Setenv("TAG_MIN_SIM", "")
	t.Setenv("TAG_TOP_RATIO", "")
	t.Setenv("TAG_MAX_PER_MEDIA", "")

	c := &Classifier{
		family: embed.FamilyChineseCLIP, minSim: 0.40, topR: 0.88, maxTags: 3, minTop1: 0.45,
		labels: []labelVec{{label: "A", vec: unit(0)}},
	}
	cp := c.WithThresholds(0.42, 0.95, 2)
	if cp.MinTop1() != 0.45 {
		t.Errorf("派生后入选门槛应保持 0.45，实得 %v", cp.MinTop1())
	}
	minSim, topR, maxTags, fam := cp.Params()
	if minSim != 0.42 || topR != 0.95 || maxTags != 2 || fam != string(embed.FamilyChineseCLIP) {
		t.Errorf("派生参数错误：minSim=%v topRatio=%v maxTags=%d family=%s", minSim, topR, maxTags, fam)
	}
	// 原分类器不受影响
	if c.minSim != 0.40 || c.topR != 0.88 || c.maxTags != 3 || c.minTop1 != 0.45 {
		t.Errorf("原分类器被派生操作修改：%+v", c)
	}
}

// 12. nil 安全
func TestClassifierNilSafety(t *testing.T) {
	var c *Classifier
	if c.MinTop1() != 0 {
		t.Errorf("nil 分类器的门槛应为 0")
	}
	if got := c.Suggest(unit(0)); got != nil {
		t.Errorf("nil 分类器应返回 nil，实得 %+v", got)
	}
	if _, _, _, fam := c.Params(); fam != "" {
		t.Errorf("nil 分类器族应为空串，实得 %q", fam)
	}
	if cp := c.WithThresholds(0.4, 0.9, 2); cp != nil {
		t.Errorf("nil 分类器派生应返回 nil")
	}
	if c.LabelCount() != 0 {
		t.Errorf("nil 分类器标签数应为 0")
	}
}
