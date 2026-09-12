package tags

// 零样本分类（CLIP 文本塔 × 已缓存图像向量）：
//
//   - 启动时把词表逐标签编码为 512 维文本向量并缓存（一次成本，之后复用）；
//   - 对每张媒体直接用 **已存在的 media.embedding**（无需再跑视觉塔）算余弦相似度；
//   - 阈值判定：sim ≥ max(绝对下限, top1 × TopRatio)，每图至多 MaxTags 个，同组只留最高。
//
// ⚠️ 绝对下限与**模型族强相关**（见 internal/search/semantic.go 的同类结论）：
//
//	chinese-clip：相关命中余弦距离 0.55~0.63 → 相似度约 0.37~0.45；
//	              全库距离分布 0.55~0.71 → 相似度 0.29~0.45。
//	clip（英文）：相关命中距离 0.70~0.78 → 相似度约 0.22~0.30。
//
// 因此**不能用单一 0.62 之类的绝对阈值**（会全灭）。默认值按族给出，
// 可用 TAG_MIN_SIM / TAG_TOP_RATIO / TAG_MAX_PER_MEDIA 覆盖，并应以 `taggen -calibrate`
// 在真实库上标定后固化。

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"

	"panoalbum/internal/embed"
)

// TextEncoder 文本塔最小接口（便于测试注入假实现，避免依赖 ORT/CGO）。
type TextEncoder interface {
	EncodeText(ctx context.Context, text string) ([]float32, error)
	Family() embed.ModelFamily
}

// 各族默认绝对相似度下限（可由 TAG_MIN_SIM 覆盖）。
//
// chinese-clip 的 0.40 来自 Job000011 在真实库（72 条媒体 × 118 个标签）上的标定：
//
//	top1 分布（每图最高分）  min 0.395 / p50 0.432 / max 0.464
//	全部标签相似度分布       p50 0.368 / p75 0.383 / p90 0.399
//	阈值扫描（top_ratio 0.88, max_tags 3）：
//	  ≤0.38 → 100% 媒体命中，平均 3.00 标签（打满上限，等于没设下限）
//	  0.40  →  99% 媒体命中，平均 2.82 标签（开始真正筛掉最弱一档，无饥饿感）
//	  0.44  →  29% 媒体命中，平均 0.36 标签（断崖，过度）
//
// 故取 0.40 —— **断崖前的最高值**。
//
// ⚠️ 相对阈值（top_ratio × top1 ≈ 0.88 × 0.43 = 0.38）在多数图上先于绝对下限生效，
// 因此把下限从 0.35 提到 0.38 **完全无变化**，必须提到 0.40 才真正起作用。
const (
	defaultMinSimChineseCLIP = 0.40
	defaultMinSimCLIP        = 0.24
)

// 默认相对阈值：低于 top1 × TopRatio 的候选丢弃。
const defaultTopRatio = 0.88

// 默认每图最多产出的 AI 标签数（防标签泛滥）。
const defaultMaxTags = 3

// ClassifyConfig 阈值与上限配置（零值 = 使用族感知默认）。
type ClassifyConfig struct {
	MinSim   float64 // 绝对相似度下限；<=0 时按族默认/env
	TopRatio float64 // 相对 top1 比例；<=0 时默认 0.88
	MaxTags  int     // 每图上限；<=0 时默认 3
}

// Suggestion 单条 AI 打标建议。
type Suggestion struct {
	Tag        string  `json:"tag"`
	Class      string  `json:"class"`
	Group      string  `json:"group,omitempty"`
	Confidence float64 `json:"confidence"` // 余弦相似度
}

// labelVec 缓存后的标签向量。
type labelVec struct {
	label string
	class string
	group string
	vec   []float32
}

// Classifier 零样本分类器（词表向量缓存）。
type Classifier struct {
	family  embed.ModelFamily
	labels  []labelVec
	minSim  float64
	topR    float64
	maxTags int
}

// NewClassifier 用已初始化的文本塔编码词表并缓存；v 为 nil 时用 DefaultVocab()。
// cfg 的零值字段将由族默认/env 补齐。
func NewClassifier(ctx context.Context, enc TextEncoder, v *Vocab, cfg ClassifyConfig) (*Classifier, error) {
	if v == nil {
		v = DefaultVocab()
	}
	fam := embed.FamilyChineseCLIP
	if enc != nil {
		fam = enc.Family()
	}
	c := &Classifier{
		family:  fam,
		minSim:  resolveMinSim(cfg.MinSim, fam),
		topR:    resolveTopRatio(cfg.TopRatio),
		maxTags: resolveMaxTags(cfg.MaxTags),
	}
	if enc == nil {
		return c, nil // 无编码器：仅用于启发式路径/测试
	}
	for _, cls := range v.Classes {
		for _, td := range cls.Tags {
			vec, err := encodeLabel(ctx, enc, fam, td)
			if err != nil {
				return nil, err
			}
			c.labels = append(c.labels, labelVec{label: td.Label, class: cls.Name, group: td.Group, vec: vec})
		}
	}
	return c, nil
}

// encodeLabel 按族构造提示词并编码：多提示词取平均后 L2 归一化（稳健性更好）。
func encodeLabel(ctx context.Context, enc TextEncoder, fam embed.ModelFamily, td TagDef) ([]float32, error) {
	var prompts []string
	if fam == embed.FamilyCLIP {
		en := td.EN
		if en == "" {
			en = td.Label
		}
		prompts = []string{"a photo of " + en, en}
	} else {
		prompts = []string{"一张" + td.Label + "的照片", td.Label}
	}
	sum := make([]float32, 0, embed.EmbeddingDim)
	for i, p := range prompts {
		v, err := enc.EncodeText(ctx, p)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			sum = append(sum, v...)
			continue
		}
		for j := range sum {
			if j < len(v) {
				sum[j] += v[j]
			}
		}
	}
	n := float32(len(prompts))
	for i := range sum {
		sum[i] /= n
	}
	return embed.L2Normalize(sum), nil
}

// Suggest 对一张媒体的图像向量产出建议（不落库）。
// vec 为 media.embedding（已 L2 归一化）；为空或维度不符时返回 nil。
func (c *Classifier) Suggest(vec []float32) []Suggestion {
	if c == nil || len(vec) == 0 || len(c.labels) == 0 {
		return nil
	}
	scored := make([]Suggestion, 0, len(c.labels))
	for _, l := range c.labels {
		if len(l.vec) != len(vec) {
			continue
		}
		scored = append(scored, Suggestion{
			Tag:        l.label,
			Class:      l.class,
			Group:      l.group,
			Confidence: embed.CosineSimilarity(vec, l.vec),
		})
	}
	if len(scored) == 0 {
		return nil
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Confidence > scored[j].Confidence })

	top1 := scored[0].Confidence
	floor := c.minSim
	if r := top1 * c.topR; r > floor {
		floor = r
	}
	seenGroup := map[string]bool{}
	out := make([]Suggestion, 0, c.maxTags)
	for _, s := range scored {
		if len(out) >= c.maxTags {
			break
		}
		if s.Confidence < floor {
			break // 已降序，后续只会更低
		}
		if s.Group != "" {
			if seenGroup[s.Group] {
				continue
			}
			seenGroup[s.Group] = true
		}
		out = append(out, s)
	}
	return out
}

// AllScores 返回该图像向量与**全部**标签的相似度（降序，不做阈值过滤）。
// 供 -calibrate 标定分布使用；常规打标请用 Suggest。
func (c *Classifier) AllScores(vec []float32) []Suggestion {
	if c == nil || len(vec) == 0 || len(c.labels) == 0 {
		return nil
	}
	scored := make([]Suggestion, 0, len(c.labels))
	for _, l := range c.labels {
		if len(l.vec) != len(vec) {
			continue
		}
		scored = append(scored, Suggestion{
			Tag:        l.label,
			Class:      l.class,
			Group:      l.group,
			Confidence: embed.CosineSimilarity(vec, l.vec),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Confidence > scored[j].Confidence })
	return scored
}

// WithThresholds 以相同标签向量派生一个不同阈值的分类器（标定扫描用，不重新编码）。
func (c *Classifier) WithThresholds(minSim, topRatio float64, maxTags int) *Classifier {
	if c == nil {
		return nil
	}
	cp := *c
	cp.minSim = resolveMinSim(minSim, c.family)
	cp.topR = resolveTopRatio(topRatio)
	cp.maxTags = resolveMaxTags(maxTags)
	return &cp
}

// LabelCount 词表标签数。
func (c *Classifier) LabelCount() int {
	if c == nil {
		return 0
	}
	return len(c.labels)
}

// Params 返回生效的阈值参数（日志/标定用）。
func (c *Classifier) Params() (minSim, topRatio float64, maxTags int, family string) {
	if c == nil {
		return 0, 0, 0, ""
	}
	return c.minSim, c.topR, c.maxTags, string(c.family)
}

// Family 返回模型族（供调用方按族决策）。
func (c *Classifier) Family() embed.ModelFamily {
	if c == nil {
		return ""
	}
	return c.family
}

func resolveMinSim(v float64, fam embed.ModelFamily) float64 {
	if v > 0 {
		return v
	}
	if s := strings.TrimSpace(os.Getenv("TAG_MIN_SIM")); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0.05 && f <= 1.0 {
			return f
		}
	}
	if fam == embed.FamilyCLIP {
		return defaultMinSimCLIP
	}
	return defaultMinSimChineseCLIP
}

func resolveTopRatio(v float64) float64 {
	if v > 0 {
		return v
	}
	if s := strings.TrimSpace(os.Getenv("TAG_TOP_RATIO")); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0.1 && f <= 1.0 {
			return f
		}
	}
	return defaultTopRatio
}

func resolveMaxTags(v int) int {
	if v > 0 {
		return v
	}
	if s := strings.TrimSpace(os.Getenv("TAG_MAX_PER_MEDIA")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 20 {
			return n
		}
	}
	return defaultMaxTags
}
