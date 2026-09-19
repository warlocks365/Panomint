package tags

// 零样本分类（CLIP 文本塔 × 已缓存图像向量）：
//
//   - 启动时把词表逐标签编码为 512 维文本向量并缓存（一次成本，之后复用）；
//     结果会**持久化**到 tag_label_vectors 表（见 labelcache.go），二次启动直接载入、
//     一次 EncodeText 都不调 —— 否则 114 标签 × 5 模板 = 570 次编码会让 API 启动等近 2 分钟；
//     每个标签用**多提示词模板集成**编码（中/英两族各 5 条，逐条 L2 归一化后取平均再归一化），
//     目的是抑制单一措辞带来的偏差，提升标签向量的语义指向性与相似度的区分度；
//   - 对每张媒体直接用 **已存在的 media.embedding**（无需再跑视觉塔）算余弦相似度；
//   - 阈值判定：sim ≥ max(绝对下限, top1 × TopRatio)，每图至多 MaxTags 个，同组只留最高；
//   - 可选的「入选门槛」MinTop1：top1 本身不达标时直接返回空结果，使
//     「这张图不属于任何标签」成为合法结论（默认关闭，见 defaultMinTop1）。
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
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"panoalbum/internal/embed"
	"panoalbum/internal/vecutil"
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

// 默认「入选门槛」（top1 绝对下限）：0 = 关闭。
//
// 动机：min_sim 与 top_ratio 都无法表达「这张图不属于任何标签」这一结论 ——
// 当全库相似度分布被压缩时（chinese-clip 实测 top1 仅 0.395~0.464，跨度 0.069），
// 相对判据 0.88 × top1 ≈ 0.38 低于绝对下限 0.40（等于不生效），而绝对下限 0.40
// 又几乎人人可达，于是每张图都能凑满 3 个标签。MinTop1 直接约束 top1 本身：
// 一旦 top1 不达标，该图返回空结果，「无标签」成为合法输出。
//
// ⚠️ 本轮**不启用**（保持 0，即现有生产行为不变）：门槛值必须在部署后用
// `taggen -mode calibrate` 拿到**全量未过滤**的 top1 分布，标定后固化。
// 凭现有 media_tags 里「已通过阈值」的样本回算会系统性高估，直接启用有整库饥饿风险。
const defaultMinTop1 = 0

// ClassifyConfig 阈值与上限配置（零值 = 使用族感知默认）。
type ClassifyConfig struct {
	MinSim   float64 // 绝对相似度下限；<=0 时按族默认/env
	TopRatio float64 // 相对 top1 比例；<=0 时默认 0.88
	MaxTags  int     // 每图上限；<=0 时默认 3
	MinTop1  float64 // 入选门槛：top1 低于该值即判为「不属于任何标签」；<=0 时按 TAG_MIN_TOP1/env，默认 0（关闭）

	// ModelDir 文本塔模型目录（EMBED_MODEL_DIR）。**仅参与 cache_key 计算**，不改变编码行为。
	// 不放进 cache_key 会导致「换了模型目录却复用旧模型的向量」——向量空间不同，结果全错。
	ModelDir string
	// Cache 标签向量持久缓存；nil = 不使用缓存（每次启动照常编码，与改动前行为一致）。
	// 读写失败一律降级为编码，绝不阻断启动（见 NewClassifier）。
	Cache LabelVectorCache
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
	minTop1 float64 // 入选门槛（0 = 关闭）
}

// NewClassifier 用已初始化的文本塔编码词表并缓存；v 为 nil 时用 DefaultVocab()。
// cfg 的零值字段将由族默认/env 补齐。
//
// **缓存优先**（缺陷 1）：若 cfg.Cache 非空且命中（标签集合与当前词表完全一致），直接用
// 持久化的向量构建分类器，**一次 EncodeText 都不调** —— 二次启动因此从 ~110s 降到数秒。
// 未命中则走原编码路径，并把结果写回缓存。
//
// 失败降级是硬要求：DB 不可用、表不存在、读写失败都只记警告并**照常编码**，
// 绝不因为缓存机制引入新的启动失败模式（原编码路径本身仍会返回 error）。
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
		minTop1: resolveMinTop1(cfg.MinTop1),
	}
	if enc == nil {
		return c, nil // 无编码器：仅用于启发式路径/测试（也不使用缓存）
	}

	key := labelCacheKey(fam, cfg.ModelDir, v)
	if cfg.Cache != nil {
		items, err := cfg.Cache.Load(ctx, key)
		if err != nil {
			log.Printf("标签向量缓存读取失败，降级为重新编码（不影响启动）: %v", err)
		} else if labels, ok := matchCachedLabels(items, v); ok {
			c.labels = labels
			return c, nil
		}
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

	if cfg.Cache != nil {
		if err := cfg.Cache.Save(ctx, key, toCachedLabels(c.labels)); err != nil {
			log.Printf("标签向量缓存写入失败（不影响启动）: %v", err)
		}
	}
	return c, nil
}

// chineseCLIPTemplates 中文族提示词模板（%s = 中文标签名）。
//
// 多模板集成依据：CLIP 文本塔对措辞高度敏感，单一模板会把某个句式的偏差固化进标签向量。
// 用多条语义等价、指向同一概念的句式分别编码，可以看成在文本嵌入空间里对该概念做
// 「多视角采样」，天然抑制单个措辞的噪声方向，从而让标签向量更贴近概念本身。
// 原实现只有 2 条（「一张{标签}的照片」+ 裸标签），其中裸标签过于抽象，是误命中的主要来源；
// 扩到 5 条后裸标签权重从 1/2 降到 1/5，抽象标签的误命中被显著稀释。
var chineseCLIPTemplates = []string{
	"一张关于%s的照片",
	"%s的照片",
	"画面中有%s",
	"这是一张%s的照片",
	"%s",
}

// clipCLIPTemplates 英文族提示词模板（%s = 英文名），与中文族一一对应。
var clipCLIPTemplates = []string{
	"a photo of %s",
	"a photo about %s",
	"a picture of %s",
	"this is a photo of %s",
	"%s",
}

// templatesFor 返回某族使用的提示词模板集合（顺序固定，便于测试、复现与 cache_key 稳定）。
//
// 两族模板**不得交叉**：chinese-clip 的文本塔不认英文提示词，clip 的文本塔不认中文，
// 混用会让该标签的向量彻底失去意义。零值族（未设置）按中文族处理，与改动前一致。
func templatesFor(fam embed.ModelFamily) []string {
	if fam == embed.FamilyCLIP {
		return clipCLIPTemplates
	}
	return chineseCLIPTemplates
}

// labelPrompts 按模型族把标签展开为提示词列表（顺序固定，便于测试与复现）。
//
// 英文名为空时回退中文标签（词表自洽性由单测守住，正常不应发生；
// 真发生了说明词表缺 EN，应修词表而不是在这里兜底）。
func labelPrompts(td TagDef, fam embed.ModelFamily) []string {
	tmpls := templatesFor(fam)
	name := td.Label
	if fam == embed.FamilyCLIP && td.EN != "" {
		name = td.EN
	}
	out := make([]string, 0, len(tmpls))
	for _, tmpl := range tmpls {
		out = append(out, fmt.Sprintf(tmpl, name))
	}
	return out
}

// encodeLabel 把一个标签编码为文本向量：**每条提示词先独立 L2 归一化**，再取平均，
// 最后整体 L2 归一化。
//
// 为什么必须先逐条归一化：不同模板编码出的向量模长并不一致（取决于文本塔原始输出），
// 先求和再归一化会让模长大的模板支配平均值，多模板集成就退化回单模板。
// 归一化后取平均 = 各模板在单位球面上的质心方向，语义指向更稳定。
func encodeLabel(ctx context.Context, enc TextEncoder, fam embed.ModelFamily, td TagDef) ([]float32, error) {
	prompts := labelPrompts(td, fam)
	var sum []float32
	n := 0
	for _, p := range prompts {
		v, err := enc.EncodeText(ctx, p)
		if err != nil {
			return nil, err
		}
		if len(v) == 0 {
			continue
		}
		if sum == nil {
			sum = make([]float32, len(v))
		}
		if len(v) != len(sum) {
			continue // 维度不一致的模板跳过（正常不会发生）
		}
		u := vecutil.L2Normalize(append([]float32(nil), v...)) // 拷贝后再归一化，避免污染调用方数据
		for j := range sum {
			sum[j] += u[j]
		}
		n++
	}
	if n == 0 {
		// 与改动前一致的失败行为：无可编码模板时不报错，该标签因维度不符在 Suggest 中被跳过。
		return nil, nil
	}
	for i := range sum {
		sum[i] /= float32(n)
	}
	return vecutil.L2Normalize(sum), nil
}

// Suggest 对一张媒体的图像向量产出建议（不落库）。
// vec 为 media.embedding（已 L2 归一化）；为空或维度不符时返回 nil。
//
// 判决顺序：
//  1. 入选门槛 minTop1：top1 不达标 → 直接返回空（「这张图不属于任何标签」）；
//  2. 计算统一 floor = max(min_sim 绝对下限, top_ratio × top1 相对下限)；
//  3. 按分数降序扫描：低于 floor 即 break（后续只会更低）；
//  4. 同组互斥（Group 非空时只保留该组首个 = 最高分者）；
//  5. 累计到 max_tags 即停。
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
			Confidence: vecutil.CosineSimilarity(vec, l.vec),
		})
	}
	if len(scored) == 0 {
		return nil
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Confidence > scored[j].Confidence })

	top1 := scored[0].Confidence

	// 入选门槛：top1 本身不达标 → 判为「这张图不属于任何标签」。
	// 这是唯一能让「无标签」自然发生的判据：绝对下限/相对下限都是在**已入选**的候选里
	// 做取舍，只要 top1 越过下限就必然产出至少 1 个标签。
	// 默认 0（关闭），保持既有生产行为；启用后需重新标定 min_sim / top_ratio。
	if c.minTop1 > 0 && top1 < c.minTop1 {
		return nil
	}

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
			Confidence: vecutil.CosineSimilarity(vec, l.vec),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Confidence > scored[j].Confidence })
	return scored
}

// WithThresholds 以相同标签向量派生一个不同阈值的分类器（标定扫描用，不重新编码）。
// 入选门槛 minTop1 原样保留（结构体拷贝），避免标定扫描时静默改变判决行为。
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

// MinTop1 返回生效的入选门槛（0 = 关闭）。
// 独立 getter：Params() 的签名被 cmd/taggen 依赖，不扩参以免影响调用方。
func (c *Classifier) MinTop1() float64 {
	if c == nil {
		return 0
	}
	return c.minTop1
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

// resolveMinTop1 解析入选门槛：显式值优先 → TAG_MIN_TOP1 环境变量 → 默认 0（关闭）。
// 环境变量取值需落在 (0.05, 1.0]，与 resolveMinSim 的合法性区间保持一致。
func resolveMinTop1(v float64) float64 {
	if v > 0 {
		return v
	}
	if s := strings.TrimSpace(os.Getenv("TAG_MIN_TOP1")); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0.05 && f <= 1.0 {
			return f
		}
	}
	return defaultMinTop1
}
