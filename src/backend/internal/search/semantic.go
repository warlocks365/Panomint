package search

// Stage 4 语义召回（Job000010）：pgvector + CLIP 文本塔。
//
// 与既有契约的关系：Recaller 返回的媒体 ID 会被 buildWhere 以
// `... OR m.id = ANY($n::uuid[])` 的形式并入结构化过滤（并集语义），
// 因此语义命中的媒体即使不满足文本条件也会进入候选集。
//
// 推理全部在本地 CPU 完成（ONNX Runtime），不依赖任何远程 GPU 节点。

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"

	"panoalbum/internal/embed"
)

// 语义召回的余弦距离上限（距离越小越相似）；超过该值视为不相关，不并入候选集。
//
// 语义召回的余弦距离上限（距离越小越相似）；超过该值视为不相关，不并入候选集。
//
// ⚠️ 该值与**模型族强相关**，必须按族标定（两族的距离尺度不同）：
//
//	chinese-clip（中文，ViT-B/16）：相关命中 0.55~0.60，全库分布 0.55~0.71
//	  → 0.63 兼顾召回与抑制噪声（实测 "极光"→极光-夜空 0.5487、"夜景天际线"→帝国大厦-夜景 0.5931）
//	clip（英文，ViT-B/32）：相关命中 0.70~0.78，全库分布 0.70~0.85
//	  → 0.78（实测 "aurora"→极光-夜空 0.7026、"city skyline at night"→帝国大厦-夜景 0.7429）
//
// 可用环境变量 EMBED_SEMANTIC_MAX_DIST 覆盖（便于按实际库内容调优，无需重新编译）。
const (
	defaultThresholdChineseCLIP = 0.63
	defaultThresholdCLIP        = 0.78
)

// threshold 解析阈值：显式配置 > 环境变量 > 按模型族默认值。
func (r *VectorRecaller) threshold() float64 {
	if r.MaxDist > 0 {
		return r.MaxDist
	}
	if v := strings.TrimSpace(os.Getenv("EMBED_SEMANTIC_MAX_DIST")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0.30 && f <= 1.0 {
			return f
		}
	}
	if r.Enc != nil && r.Enc.Family() == embed.FamilyChineseCLIP {
		return defaultThresholdChineseCLIP
	}
	return defaultThresholdCLIP
}

// VectorRecaller 基于 CLIP + pgvector 的语义召回实现。
type VectorRecaller struct {
	Enc    *embed.Encoder
	Store  *embed.Store
	TopK   int
	Logger *log.Logger
	// MaxDist 余弦距离上限；<=0 时用 env/默认值。
	MaxDist float64
}

// Recall 查询文本 → 512 维向量 → 余弦 top-K → 命中（ID + 相似度）。
// Q 为空或未启用（依赖缺失）时返回 nil，等同于不召回（保持 Stage 3 行为）。
func (r *VectorRecaller) Recall(ctx context.Context, p SearchParams) ([]RecallHit, error) {
	if r == nil || r.Enc == nil || r.Store == nil {
		return nil, nil
	}
	q := strings.TrimSpace(p.Q)
	if q == "" {
		return nil, nil
	}
	k := r.TopK
	if k <= 0 {
		k = 50
	}

	vec, err := r.Enc.EncodeText(ctx, q)
	if err != nil {
		// 编码失败不应让整个搜索 500：降级为不召回
		if r.Logger != nil {
			r.Logger.Printf("语义召回：文本编码失败，降级跳过：%v", err)
		}
		return nil, nil
	}
	hits, err := r.Store.SearchByVector(ctx, vec, k)
	if err != nil {
		if r.Logger != nil {
			r.Logger.Printf("语义召回：向量检索失败，降级跳过：%v", err)
		}
		return nil, nil
	}

	maxDist := r.threshold()

	out := make([]RecallHit, 0, len(hits))
	for _, h := range hits {
		if h.Distance > maxDist {
			break // 结果按距离升序，后续只会更不相关
		}
		sim := 1 - h.Distance
		if sim < 0 {
			sim = 0
		}
		out = append(out, RecallHit{ID: h.ID, Similarity: sim})
	}
	if r.Logger != nil {
		r.Logger.Printf("语义召回：q=%q 命中 %d/%d（阈值 %.2f）", q, len(out), len(hits), maxDist)
	}
	return out, nil
}
