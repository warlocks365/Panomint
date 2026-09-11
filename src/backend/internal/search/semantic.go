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
	"strings"

	"panoalbum/internal/embed"
)

// SemanticThreshold 语义召回的余弦距离上限（距离越小越相似）。
// 超过该值视为不相关，不并入候选集——避免把无关媒体注入结果。
// 取值由真实数据上的距离分布标定（见 Job000010 验证记录）。
const SemanticThreshold = 0.85

// VectorRecaller 基于 CLIP + pgvector 的语义召回实现。
type VectorRecaller struct {
	Enc    *embed.Encoder
	Store  *embed.Store
	TopK   int
	Logger *log.Logger
}

// Recall 查询文本 → 512 维向量 → 余弦 top-K → 媒体 ID。
// Q 为空或未启用（依赖缺失）时返回 nil，等同于不召回（保持 Stage 3 行为）。
func (r *VectorRecaller) Recall(ctx context.Context, p SearchParams) ([]string, error) {
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

	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		if h.Distance > SemanticThreshold {
			break // 结果按距离升序，后续只会更不相关
		}
		ids = append(ids, h.ID)
	}
	return ids, nil
}
